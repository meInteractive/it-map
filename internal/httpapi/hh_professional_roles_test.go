package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/integrations/hhpublic"
)

type hhRolesProbe struct {
	calls int
	fail  bool
}

func (p *hhRolesProbe) FetchProfessionalRoles(context.Context) (*hhpublic.ProfessionalRoles, error) {
	p.calls++
	if p.fail {
		return nil, errors.New("provider sensitive details")
	}
	return &hhpublic.ProfessionalRoles{Categories: []hhpublic.ProfessionalRoleCategory{{ID: "1", Name: "IT", Roles: []hhpublic.ProfessionalRole{{ID: "36", Name: "CIO"}}}}}, nil
}

func TestHHProfessionalRolesHTTP(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("x", 48), map[string]string{"eugene": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("eugene", "pw")
	token, _ := m.Issue(u, time.Now())
	probe := &hhRolesProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, HHProfessionalRoles: probe})
	call := func(authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/hh/professional-roles", nil)
		if authorized {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := call(false); w.Code != 401 || probe.calls != 0 {
		t.Fatalf("unauthorized %d calls %d", w.Code, probe.calls)
	}
	if w := call(true); w.Code != 200 || !strings.Contains(w.Body.String(), `"id":"36"`) || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
	}
	probe.fail = true
	if w := call(true); w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatalf("unexpected failure %d %s", w.Code, w.Body.String())
	}
}
