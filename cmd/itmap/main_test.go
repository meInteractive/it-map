package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDisabledDatabaseFailsReadinessWithoutPanic(t *testing.T) {
	t.Setenv("WEB_DIST_DIR", "")
	h := httpHandler(nil, nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "mariadb_not_connected") {
		t.Fatalf("disabled DB readiness=%d %s, wanted 503 not_ready", w.Code, w.Body.String())
	}
}
