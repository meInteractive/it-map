package httpapi

import (
	"context"
	"itmap/internal/auth"
	"itmap/internal/integrations/aihttp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type connectionProbe struct{ calls int }

func (p *connectionProbe) Test(ctx context.Context, provider aihttp.Provider) (map[string]any, error) {
	p.calls++
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) < 300*time.Second {
		return nil, context.DeadlineExceeded
	}
	return map[string]any{"ok": true, "model": "synthetic-model", "state": map[string]any{"configured": true}}, nil
}
func TestAIConnectionTestsRequireExplicitAdminPost(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"})
	if e != nil {
		t.Fatal(e)
	}
	probe := &connectionProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, AIConnectionTests: probe})
	cookie := func(login string) *http.Cookie {
		u, ok := m.Authenticate(login, map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"}[login])
		if !ok {
			t.Fatal("auth")
		}
		v, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return &http.Cookie{Name: auth.CookieName, Value: v}
	}
	for _, path := range []string{"/api/openai/test", "/api/openrouter/test"} {
		for _, x := range []struct {
			method, user, origin string
			want                 int
		}{{"POST", "", "", 401}, {"POST", "demo", "", 403}, {"GET", "eugene", "", 405}, {"POST", "eugene", "https://foreign.invalid", 403}, {"POST", "eugene", "https://example.com", 200}} {
			before := probe.calls
			r := httptest.NewRequest(x.method, "https://example.com"+path, nil)
			if x.user != "" {
				r.AddCookie(cookie(x.user))
			}
			if x.origin != "" {
				r.Header.Set("Origin", x.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != x.want {
				t.Fatalf("manual test %s got%d want%d", path, w.Code, x.want)
			}
			if x.want != 200 && probe.calls != before {
				t.Fatal("unauthorized request invoked provider")
			}
		}
	}
	if probe.calls != 2 {
		t.Fatal("explicit manual POST not executed once per provider")
	}
}
