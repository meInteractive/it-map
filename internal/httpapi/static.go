package httpapi

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// StaticHandler serves built SPA assets from a public-only directory. A missing
// build never triggers a fallback to source files or the working directory.
// Route permission checks happen before this handler is called.
func StaticHandler(directory string) http.Handler {
	dir := os.DirFS(directory)
	files := http.FileServer(http.FS(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		info, err := fs.Stat(dir, name)
		if err != nil || info.IsDir() {
			// Missing JS, CSS, icons, etc. must yield a real 404, not index.html.
			if name != "index.html" && (path.Ext(name) != "" || strings.HasPrefix(name, "assets/")) {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			if _, err := fs.Stat(dir, name); err != nil {
				http.Error(w, "web build unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		if name == "index.html" {
			f, err := dir.Open("index.html")
			if err != nil {
				http.Error(w, "web build unavailable", http.StatusServiceUnavailable)
				return
			}
			defer f.Close()
			// Native POST forms can send Origin: null under no-referrer. Keep
			// same-origin referrers for the SPA login form without disclosing
			// them to external sites or relaxing the /auth Origin check.
			w.Header().Set("Referrer-Policy", "same-origin")
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if r.Method == http.MethodGet {
				_, _ = io.Copy(w, f)
			}
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		request := r.Clone(r.Context())
		request.URL.Path = "/" + name
		// Check unreadable targets early; FileServer still handles content type/ranges.
		if _, err := fs.Stat(dir, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "static asset unavailable", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, request)
	})
}
