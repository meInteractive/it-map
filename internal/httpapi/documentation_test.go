package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	userdocs "itmap/docs/user"
	"itmap/internal/auth"
)

func TestDocumentationRoutes(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("d", 64), map[string]string{"eugene": "synthetic", "demo": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager})
	request := func(method, path, login string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://itmap.test"+path, nil)
		if login != "" {
			user, ok := manager.Authenticate(login, "synthetic")
			if !ok {
				t.Fatal("не удалось создать тестовую сессию")
			}
			token, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/docs", "/api/docs/user", "/api/docs/database?download=1", "/api/docs/missing"} {
		if got := request(http.MethodGet, path, "").Code; got != 401 {
			t.Fatalf("%s без сессии: %d", path, got)
		}
		if got := request(http.MethodGet, path, "demo").Code; got != 403 {
			t.Fatalf("%s без прав администратора: %d", path, got)
		}
	}
	for _, path := range []string{"/api/docs", "/api/docs/user"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodHead} {
			if got := request(method, path, "eugene").Code; got != 405 {
				t.Fatalf("%s %s: %d", method, path, got)
			}
		}
	}
	for _, path := range []string{"/api/docs/", "/api/docs/missing", "/api/docs/%2e%2e%2fDATABASE.md"} {
		if got := request(http.MethodGet, path, "eugene").Code; got != 404 {
			t.Fatalf("%s: %d", path, got)
		}
	}
	index := request(http.MethodGet, "/api/docs", "eugene")
	var manifest struct {
		Revision int
		Versions map[string]any
		Sections []documentationSection
	}
	if index.Code != 200 || !strings.HasPrefix(index.Header().Get("Content-Type"), "application/json") ||
		index.Header().Get("Cache-Control") != "private, no-store" ||
		json.Unmarshal(index.Body.Bytes(), &manifest) != nil || manifest.Revision == 0 ||
		len(manifest.Versions) == 0 || len(manifest.Sections) != 8 {
		t.Fatal("некорректный манифест документации")
	}
	for _, section := range manifest.Sections {
		path := "/api/docs/" + section.ID
		want, err := userdocs.Files.ReadFile(section.File)
		if err != nil || !strings.HasPrefix(string(want), "# ") || strings.Contains(string(want), "{{DATABASE_TABLES}}") {
			t.Fatalf("не готов Markdown раздела %s", section.ID)
		}
		for query, mode := range map[string]string{"": "inline", "?download=1": "attachment"} {
			w := request(http.MethodGet, path+query, "eugene")
			if w.Code != 200 || w.Header().Get("Content-Type") != "text/markdown; charset=utf-8" ||
				w.Header().Get("Cache-Control") != "private, no-store" ||
				w.Header().Get("Content-Disposition") != mode+`; filename="`+section.File+`"` ||
				w.Body.String() != string(want) {
				t.Fatalf("%s%s: некорректная выдача Markdown", path, query)
			}
		}
	}
}
