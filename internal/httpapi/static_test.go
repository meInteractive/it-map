package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticAndAPIIsolation(t *testing.T) {
	dir := t.TempDir()
	for name, value := range map[string]string{"index.html": "<!doctype html>React app", "assets/app.js": "console.log('ok')", "manifest.webmanifest": "{}"} {
		filename := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandlerWithWeb(nil, dir)
	cases := []struct {
		method, path string
		status       int
		contains     string
	}{
		{"GET", "/", 200, "React app"},
		{"GET", "/settings", 200, "React app"},
		{"GET", "/assets/app.js", 200, "console.log"},
		{"GET", "/assets/missing.js", 404, "404"},
		{"GET", "/api/session", 503, "backend_not_migrated"},
		{"GET", "/api", 503, "backend_not_migrated"},
		{"GET", "/api/company/foo", 503, "backend_not_migrated"},
		{"GET", "/auth/callback", 503, "backend_not_migrated"},
		{"POST", "/auth", 503, "backend_not_migrated"},
		{"GET", "/mcp/agent", 503, "backend_not_migrated"},
		{"POST", "/settings", 405, "method not allowed"},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("%s %s -> %d (%s), want %d %s", tc.method, tc.path, w.Code, w.Body.String(), tc.status, tc.contains)
		}
	}
}

func TestStaticUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	NewHandlerWithWeb(nil, t.TempDir()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 503 {
		t.Fatalf("unbuilt web must return 503; got %d", w.Code)
	}
}
