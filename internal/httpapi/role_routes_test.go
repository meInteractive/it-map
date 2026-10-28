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
	"itmap/internal/roles"
)

type roleProbe struct {
	calls int
	fail  bool
}

func (p *roleProbe) Read(_ context.Context) (roles.Catalog, error) {
	p.calls++
	if p.fail {
		return roles.Catalog{}, errors.New("sensitive SQL details")
	}
	return roles.Build([]roles.Role{{ID: 1, StableKey: "it", Name: "IT Manager", ChildCount: 1}, {ID: 2, StableKey: "head", Name: "Руководитель", ParentID: ptr(1)}})
}
func ptr(n int64) *int64 { return &n }
func TestVacancyRoleRoutes(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("x", 48), map[string]string{"eugene": "pw"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("eugene", "pw")
	token, _ := m.Issue(u, time.Now())
	p := &roleProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, Roles: p})
	call := func(path string, authorized bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authorized {
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := call("/api/vacancy-roles", false); w.Code != 401 || p.calls != 0 {
		t.Fatalf("unauthorized read %d / %d calls", w.Code, p.calls)
	}
	for _, tc := range []struct{ path, part string }{
		{"/api/vacancy-roles", `"tree"`},
		{"/api/vacancy-roles/roots", `"roles":[{"id":1`},
		{"/api/vacancy-roles/leaves", `"roles":[{"id":2`},
		{"/api/vacancy-roles/1/descendants", `"role":{"id":1`},
	} {
		w := call(tc.path, true)
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.part) {
			t.Fatalf("wrong response %s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if w := call("/api/vacancy-roles/500/descendants", true); w.Code != 404 {
		t.Fatalf("missing role %d", w.Code)
	}
	p.fail = true
	if w := call("/api/vacancy-roles/roots", true); w.Code != 503 || strings.Contains(w.Body.String(), "SQL details") {
		t.Fatalf("error leak %d %s", w.Code, w.Body.String())
	}
}
