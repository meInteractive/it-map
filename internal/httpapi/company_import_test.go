package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
)

type companyImportProbe struct{ calls int }

func (p *companyImportProbe) ImportCompanies(_ context.Context, in catalog.CompanyImport) (catalog.CompanyImportResult, error) {
	p.calls++
	if len(in.Companies) != 1 || in.Mode != "create" {
		return nil, catalog.ImportError("некорректный файл")
	}
	return catalog.CompanyImportResult{"added": 1}, nil
}

func TestCompanyImportRoute(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("i", 64), map[string]string{"eugene": "synthetic", "demo": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	probe := &companyImportProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, CompanyImports: probe})
	for _, tc := range []struct {
		method, user, origin, body string
		code                       int
	}{
		{"POST", "", "", "{}", 401},
		{"POST", "demo", "", "{}", 403},
		{"POST", "eugene", "https://other.invalid", "{}", 403},
		{"GET", "eugene", "", "", 405},
		{"POST", "eugene", "", "{", 400},
		{"POST", "eugene", "", "{}", 400},
		{"POST", "eugene", "https://itmap.test", `{"mode":"create","companies":[{"name":"CI Company"}]}`, 200},
	} {
		req := httptest.NewRequest(tc.method, "https://itmap.test/api/companies/import", strings.NewReader(tc.body))
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		if tc.user != "" {
			user, _ := manager.Authenticate(tc.user, "synthetic")
			token, e := manager.Issue(user, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("status %d want %d", w.Code, tc.code)
		}
	}
	if probe.calls != 2 {
		t.Fatal("отклонённый запрос дошёл до записи")
	}
}
