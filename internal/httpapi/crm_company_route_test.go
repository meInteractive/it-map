package httpapi

import (
	"context"
	"errors"
	"itmap/internal/auth"
	"itmap/internal/crm"
	"net/http"
	"net/http/httptest"
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
