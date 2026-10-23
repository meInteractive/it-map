package httpapi

import (
	"context"
	"errors"
	"itmap/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type companyFlagProbe struct {
	calls      int
	user, flag string
	err        error
}

func (p *companyFlagProbe) CompanyNames(_ context.Context, user, flag string) ([]string, error) {
	p.calls++
	p.user = user
	p.flag = flag
	if p.err != nil {
		return nil, p.err
	}
	return []string{"Example"}, nil
}
func TestCompanyFlagLists(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "root-pass", "demo": "demo-pass"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("demo", "demo-pass")
	token, _ := m.Issue(u, time.Now())
	p := &companyFlagProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, CompanyFlags: p})
	for _, tc := range []struct{ path, flag string }{{"/api/crm/favorites", "favorite"}, {"/api/crm/hidden-companies", "hidden"}} {
		previous := p.calls
		req := httptest.NewRequest("GET", tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 || p.calls != previous {
			t.Fatal(w.Code, p.calls)
		}
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 || p.flag != tc.flag || p.user != "Demo" || !strings.Contains(w.Body.String(), `"companyNames":["Example"]`) {
			t.Fatal(w.Code, w.Body.String(), p)
		}
	}
	p.err = errors.New("secret credentials")
	r := httptest.NewRequest("GET", "/api/crm/favorites", nil)
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
}
