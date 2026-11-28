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

func (p *vacancyExportProbe) ExportCompaniesXLSX(_ context.Context) ([]byte, error) {
	p.users = append(p.users, "companies")
	return []byte("synthetic xlsx"), nil
}

func TestVacancyExportRoute(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("k", 64), map[string]string{"eugene": "synthetic", "demo": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	exporter := &vacancyExportProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, VacancyExports: exporter, CompanyExports: exporter})
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
	for _, tc := range []struct {
		user   string
		status int
	}{{"", 401}, {"demo", 403}, {"eugene", 200}} {
		req := httptest.NewRequest("GET", "/api/exports/companies", nil)
		if tc.user != "" {
			user, _ := manager.Authenticate(tc.user, "synthetic")
			token, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status || (w.Code == 200 && !strings.Contains(w.Header().Get("Content-Disposition"), "it-employers-companies-")) {
			t.Fatalf("экспорт компаний: статус %d, ожидается %d", w.Code, tc.status)
		}
	}
	if len(exporter.users) != 2 || exporter.users[1] != "companies" {
		t.Fatal("экспорт компаний вызван вне запроса администратора")
	}

}

type sourceExportProbe struct {
	users    []string
	unranked []bool
}

func (p *sourceExportProbe) ExportVacancySourcesXLSX(_ context.Context, user string, unranked bool) ([]byte, error) {
	p.users = append(p.users, user)
	p.unranked = append(p.unranked, unranked)
	return []byte("vacancy sources"), nil
}

func (p *sourceExportProbe) ExportReviewSourcesXLSX(_ context.Context, user string) ([]byte, error) {
	p.users = append(p.users, user)
	return []byte("review sources"), nil
}

func TestCompanySourceExportRoutes(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("k", 64), map[string]string{"eugene": "synthetic", "demo": "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	probe := &sourceExportProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, CompanySourceExports: probe})
	for _, tc := range []struct {
		user, method, path string
		status             int
		filename, body     string
	}{
		{"", "GET", "companies-vacancies", 401, "", ""},
		{"demo", "GET", "companies-reviews", 403, "", ""},
		{"eugene", "POST", "companies-vacancies", 405, "", ""},
		{"eugene", "HEAD", "companies-reviews", 405, "", ""},
		{"eugene", "GET", "companies-vacancies?unranked=0", 200, "vacancy-sources-", "vacancy sources"},
		{"eugene", "GET", "companies-vacancies?unranked=1", 200, "vacancy-sources-unranked-", "vacancy sources"},
		{"eugene", "GET", "companies-reviews?unranked=1", 200, "review-sources-", "review sources"},
	} {
		req := httptest.NewRequest(tc.method, "/api/exports/"+tc.path, nil)
		if tc.user != "" {
			user, _ := manager.Authenticate(tc.user, "synthetic")
			token, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s %s: HTTP %d, ожидается %d", tc.method, tc.path, w.Code, tc.status)
		}
		if tc.status == 200 && (w.Body.String() != tc.body ||
			!strings.Contains(w.Header().Get("Content-Disposition"), "it-employers-"+tc.filename+time.Now().UTC().Format("2006-01-02")+".xlsx") ||
			!strings.HasSuffix(w.Header().Get("Content-Type"), ".spreadsheetml.sheet") ||
			w.Header().Get("Cache-Control") != "private, no-store") {
			t.Fatalf("%s: неверный ответ скачивания", tc.path)
		}
	}
	if !strings.EqualFold(strings.Join(probe.users, ","), "eugene,eugene,eugene") ||
		len(probe.unranked) != 2 || probe.unranked[0] || !probe.unranked[1] {
		t.Fatalf("неверные параметры экспорта: %v %v", probe.users, probe.unranked)
	}
}
