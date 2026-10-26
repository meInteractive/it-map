package httpapi

import (
	"context"
	"errors"
	"itmap/internal/auth"
	"itmap/internal/crm"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type companyCRMProbe struct {
	calls         int
	user, company string
	err           error
}

func (p *companyCRMProbe) Company(_ context.Context, user, company string) (crm.CompanyState, error) {
	p.calls++
	p.user = user
	p.company = company
	if p.err != nil {
		return crm.CompanyState{}, p.err
	}
	return crm.CompanyState{Favorite: true, History: []crm.StatusChange{}}, nil
}
func TestCompanyCRMRoute(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("demo", "demo-password")
	token, _ := m.Issue(u, time.Now())
	p := &companyCRMProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, CompanyCRM: p})
	call := func(path string, loggedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if loggedIn {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("/api/crm?company=Employer", false); w.Code != 401 || p.calls != 0 {
		t.Fatal(w.Code)
	}
	for _, path := range []string{"/api/crm", "/api/crm?company=" + strings.Repeat("A", 256)} {
		if w := call(path, true); w.Code != 400 || p.calls != 0 {
			t.Fatal(w.Code)
		}
	}
	if w := call("/api/crm?company=Employer", true); w.Code != 200 || p.user != "Demo" || p.company != "Employer" || !strings.Contains(w.Body.String(), `"favorite":true`) {
		t.Fatal(w.Code, w.Body.String(), p)
	}
	p.err = crm.ErrCompanyNotFound
	if w := call("/api/crm?company=Unknown", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	p.err = errors.New("DB_PASSWORD leaked")
	if w := call("/api/crm?company=Employer", true); w.Code != 503 || strings.Contains(w.Body.String(), "DB_PASSWORD") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestCompanyCRMUnicodeNameMatchesFlagWriteLimit(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("n", 64), map[string]string{"eugene": "CI_SYNTHETIC_ADMIN_PASSWORD", "demo": "CI_SYNTHETIC_PASSWORD"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("demo", "CI_SYNTHETIC_PASSWORD")
	token, _ := manager.Issue(user, time.Now())
	name := "CI CRM " + strings.Repeat("я", 130)
	reader := &unicodeCRMProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, CompanyCRM: reader})
	request := httptest.NewRequest("GET", "/api/crm?company="+url.QueryEscape(name), nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || reader.company != name {
		t.Fatal("valid VARCHAR255 Unicode company rejected before CRM read")
	}
}

type unicodeCRMProbe struct{ company string }

func (p *unicodeCRMProbe) Company(_ context.Context, _ string, name string) (crm.CompanyState, error) {
	p.company = name
	return crm.CompanyState{History: []crm.StatusChange{}}, nil
}
