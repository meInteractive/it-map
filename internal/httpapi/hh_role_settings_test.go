package httpapi

import (
	"context"
	"itmap/internal/auth"
	"itmap/internal/parser"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type hhSettingsProbe struct {
	calls int
	err   error
}

func (p *hhSettingsProbe) Read(context.Context) (parser.HHRoleSettingsView, error) {
	return parser.HHRoleSettingsView{Generation: "00000000-0000-0000-0000-000000000001", Roles: []parser.HHRoleSetting{}}, p.err
}
func (p *hhSettingsProbe) Save(_ context.Context, _ string, id, revision int64, ids []string) (parser.HHRoleSetting, error) {
	p.calls++
	return parser.HHRoleSetting{RoleID: id, Revision: revision + 1, RoleIDs: ids}, p.err
}

func TestHHRoleSettingsHTTP(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("x", 48), map[string]string{"eugene": "pw", "demo": "pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "demo"} {
		u, _ := m.Authenticate(login, "pw")
		tokens[login], _ = m.Issue(u, time.Now())
	}
	store := &hhSettingsProbe{}
	catalog := &hhRolesProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, HHRoleSettings: store, HHProfessionalRoles: catalog})
	call := func(user, method, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.invalid/api/hh/role-settings", strings.NewReader(body))
		if user != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[user]})
		}
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	valid := `{"generation":"00000000-0000-0000-0000-000000000001","roleId":1,"revision":0,"roleIds":["36"]}`
	for _, tc := range []struct {
		user, method, body, origin string
		status                     int
	}{{"", "GET", "", "", 401}, {"demo", "GET", "", "", 403}, {"eugene", "PUT", valid, "https://evil.invalid", 403}, {"eugene", "PUT", `{}`, "", 400}, {"eugene", "PUT", strings.Replace(valid, `"36"`, `"37"`, 1), "", 400}, {"eugene", "POST", "", "", 405}} {
		if w := call(tc.user, tc.method, tc.body, tc.origin); w.Code != tc.status {
			t.Fatalf("%s %s: got %d want %d", tc.user, tc.method, w.Code, tc.status)
		}
	}
	if store.calls != 0 {
		t.Fatal("rejected write reached store")
	}
	if w := call("eugene", "GET", "", ""); w.Code != 200 {
		t.Fatalf("read %d", w.Code)
	}
	if w := call("eugene", "PUT", valid, ""); w.Code != 200 || store.calls != 1 {
		t.Fatalf("save %d calls %d", w.Code, store.calls)
	}
	store.err = parser.ErrSettingsChanged
	if w := call("eugene", "PUT", valid, ""); w.Code != 409 {
		t.Fatalf("stale save %d", w.Code)
	}
	catalog.fail = true
	if w := call("eugene", "PUT", valid, ""); w.Code != 503 || store.calls != 2 {
		t.Fatalf("unavailable catalog %d calls %d", w.Code, store.calls)
	}
}
