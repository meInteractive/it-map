package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrationShell(t *testing.T) {
	handler := NewHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		method, path string
		status       int
		want         string
	}{
		{"GET", "/health/live", 200, `"status":"alive"`},
		{"GET", "/health/ready", 503, `"status":"not_ready"`},
		{"GET", "/api/session", 503, `"error":"backend_not_migrated"`},
		{"POST", "/auth", 503, `"error":"backend_not_migrated"`},
		{"GET", "/api/internal/database-export", 503, `"error":"backend_not_migrated"`},
		{"GET", "/", 503, `"error":"backend_not_migrated"`},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, w.Code, tc.status)
		}
		if !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s: unexpected body %q", tc.path, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("%s: missing no-store", tc.path)
		}
	}
}

func TestNoImplicitSuccessForUnknownRoutes(t *testing.T) {
	handler := NewHandler(nil)
	for _, path := range []string{"/api/unknown", "/api/admin", "/mcp", "/private", "/health/ready"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code < 400 {
			t.Fatalf("%q unexpectedly returned %d", path, w.Code)
		}
	}
}
