package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
)

type vacancyExportProbe struct{ users []string }

func (p *vacancyExportProbe) ExportXLSX(_ context.Context, user string) ([]byte, error) {
	p.users = append(p.users, user)
	return []byte("synthetic xlsx"), nil
}

func TestVacancyExportRoute(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("k", 64), map[string]string{"eugene": "synthetic", "demo": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	exporter := &vacancyExportProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyExports: exporter})
	for _, tc := range []struct {
		user, method string
		status       int
	}{
		{"", "GET", 401}, {"demo", "GET", 403}, {"eugene", "POST", 405}, {"eugene", "GET", 200},
	} {
		req := httptest.NewRequest(tc.method, "/api/exports/vacancies", nil)
		if tc.user != "" {
			user, _ := manager.Authenticate(tc.user, "synthetic")
			cookie, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != tc.status {
			t.Fatalf("%s %s: status=%d want=%d", tc.user, tc.method, response.Code, tc.status)
		}
		if tc.status == 200 && (response.Body.String() != "synthetic xlsx" ||
			!strings.Contains(response.Header().Get("Content-Disposition"), "it-employers-vacancies-") ||
			!strings.HasSuffix(response.Header().Get("Content-Type"), ".spreadsheetml.sheet") ||
			response.Header().Get("Cache-Control") != "private, no-store") {
			t.Fatal("неверный ответ скачивания")
		}
	}
	if len(exporter.users) != 1 || !strings.EqualFold(exporter.users[0], "eugene") {
		t.Fatalf("неверная область экспорта: %v", exporter.users)
	}
}
