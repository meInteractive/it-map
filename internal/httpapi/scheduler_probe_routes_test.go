package httpapi

import (
	"context"
	"encoding/json"
	"itmap/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type schedulerProbeStub struct{ calls int }

func (p *schedulerProbeStub) PrepareProbe(context.Context) (func(context.Context, string) (json.RawMessage, error), error) {
	return func(_ context.Context, _ string) (json.RawMessage, error) {
		p.calls++
		return json.RawMessage(`{"externalRequests":0}`), nil
	}, nil
}
func TestSchedulerProbeHTTPAuthorization(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	token := func(name string) string {
		u, _ := m.Authenticate(name, "CI_pw")
		v, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	admin, demo := token("eugene"), token("demo")
	p := &schedulerProbeStub{}
	call := func(cookie, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/probe", strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: m, SchedulerProbe: p}).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		cookie, origin, body string
		status               int
	}{{"", "", "{}", 401}, {demo, "", "{}", 403}, {admin, "https://other.invalid", "{}", 403}, {admin, "", `{"x":1}`, 400}} {
		if w := call(tc.cookie, tc.origin, tc.body); w.Code != tc.status {
			t.Fatal("probe rejected", w.Code, tc.status)
		}
	}
	if p.calls != 0 {
		t.Fatal("rejected probe executed")
	}
	w := call(admin, "", "{}")
	if w.Code != 200 || p.calls != 1 || !strings.Contains(w.Body.String(), `"externalRequests":0`) {
		t.Fatal("probe", w.Code, p.calls)
	}
}
