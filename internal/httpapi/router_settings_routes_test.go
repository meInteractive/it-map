package httpapi

import (
	"context"
	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type routerHTTPProbe struct {
	openAIProbe
	operations []string
}

func (p *routerHTTPProbe) Check(context.Context) (map[string]any, error) {
	p.operations = append(p.operations, "check")
	return map[string]any{}, nil
}
func (p *routerHTTPProbe) Refresh(context.Context) (map[string]any, error) {
	p.operations = append(p.operations, "refresh")
	return map[string]any{}, nil
}
func (p *routerHTTPProbe) Logs(context.Context) (map[string]any, error) {
	p.operations = append(p.operations, "logs")
	return map[string]any{"logs": []any{}}, nil
}
func TestRouterHTTPAccessAndMethods(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"})
	if e != nil {
		t.Fatal(e)
	}
	p := &routerHTTPProbe{}
	h := RouterSettingsHandler(m, p)
	token := func(login string) string {
		u, ok := m.Authenticate(login, map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"}[login])
		if !ok {
			t.Fatal("auth")
		}
		v, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	for _, x := range []struct {
		method, path, user, origin, body string
		status                           int
	}{
		{"GET", "/api/openrouter", "", "", "", 401}, {"GET", "/api/openrouter", "demo", "", "", 403}, {"GET", "/api/openrouter", "eugene", "", "", 200},
		{"GET", "/api/openrouter/", "eugene", "", "", 404}, {"GET", "/api/openrouter/unknown", "eugene", "", "", 404}, {"PUT", "/api/openrouter", "eugene", "", "", 405},
		{"POST", "/api/openrouter/check", "eugene", "https://foreign.invalid", "", 403}, {"POST", "/api/openrouter/check", "eugene", "https://example.com", "", 200},
		{"POST", "/api/openrouter/refresh", "eugene", "", "", 200}, {"GET", "/api/openrouter/logs", "eugene", "", "", 200},
		{"POST", "/api/openrouter/test", "eugene", "", "", 503}, {"GET", "/api/openrouter/jev", "eugene", "", "", 503},
		{"PUT", "/api/openrouter/key", "eugene", "", "{}", 200}, {"PUT", "/api/openrouter/key", "eugene", "", strings.Repeat("x", 2049), 413},
	} {
		r := httptest.NewRequest(x.method, "https://example.com"+x.path, strings.NewReader(x.body))
		if x.user != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token(x.user)})
		}
		if x.origin != "" {
			r.Header.Set("Origin", x.origin)
		}
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != x.status {
			t.Fatalf("%s %s got%d want%d", x.method, x.path, w.Code, x.status)
		}
	}
	p.generationErr = aiproviders.ErrStorage
	b := &orderedBody{t: t, p: &p.openAIProbe}
	r := httptest.NewRequest("PUT", "https://example.com/api/openrouter/key", nil)
	r.Body = b
	r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token("eugene")})
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != 503 || b.read {
		t.Fatal("generation failure read body")
	}
}
