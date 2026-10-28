package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
)

type dashboardProbe struct {
	calls int
	err   error
}

func (p *dashboardProbe) ReadDashboard(context.Context) (json.RawMessage, error) {
	p.calls++
	return json.RawMessage(`{"counts":{},"attempts":[]}`), p.err
}
func TestSchedulerDashboardAccess(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("d", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw", "nastya": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "demo", "nastya"} {
		u, _ := m.Authenticate(login, "CI_pw")
		tokens[login], e = m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
	}
	call := func(store SchedulerDashboardReader, user, method, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.invalid/api/scheduler", strings.NewReader(body))
		r.Header.Set("Origin", origin)
		if user != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[user]})
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: m, SchedulerDashboard: store}).ServeHTTP(w, r)
		return w
	}
	p := &dashboardProbe{}
	for _, user := range []string{"", "demo", "nastya"} {
		want := 403
		if user == "" {
			want = 401
		}
		if w := call(p, user, "GET", "", ""); w.Code != want {
			t.Fatal("dashboard rights changed")
		}
	}
	if p.calls != 0 {
		t.Fatal("private dashboard read reached for denied user")
	}
	if w := call(nil, "eugene", "GET", "", ""); w.Code != 503 {
		t.Fatal("missing dashboard returned success")
	}
	if w := call(p, "eugene", "GET", "", ""); w.Code != 200 || p.calls != 1 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("admin dashboard read failed")
	}
	if w := call(p, "eugene", "POST", "{}", "https://other.invalid"); w.Code != 403 {
		t.Fatal("dashboard wrong method bypassed Origin")
	}
	if w := call(p, "eugene", "POST", "{}", ""); w.Code != 405 {
		t.Fatal("dashboard method fallback changed")
	}
	if w := call(p, "eugene", "POST", "{", ""); w.Code != 500 {
		t.Fatal("source body-before-method failure changed")
	}
	p.err = errors.New("CI_PRIVATE_SQL_ERROR")
	if w := call(p, "eugene", "GET", "", ""); w.Code != 500 || strings.Contains(w.Body.String(), "CI_PRIVATE") {
		t.Fatal("dashboard failed open or leaked SQL details")
	}
}
