package httpapi

import (
	"context"
	"errors"
	"io"
	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type openAIProbe struct {
	calls, saveCalls int
	generation       bool
	generationErr    error
}

func (p *openAIProbe) Public(context.Context, bool) (map[string]any, error) {
	p.calls++
	return map[string]any{"configured": false, "models": []any{}}, nil
}
func (p *openAIProbe) Generation(context.Context) (string, error) {
	p.generation = true
	return "CI_GENERATION", p.generationErr
}
func (p *openAIProbe) Save(_ context.Context, g string, b []byte) (map[string]any, error) {
	p.saveCalls++
	if g != "CI_GENERATION" {
		return nil, errors.New("bad generation")
	}
	return map[string]any{"configured": true, "key_mask": "••••xxxx"}, nil
}

type orderedBody struct {
	t    *testing.T
	p    *openAIProbe
	read bool
}

func (b *orderedBody) Read(p []byte) (int, error) {
	b.read = true
	if !b.p.generation {
		b.t.Fatal("body read before generation")
	}
	return 0, io.EOF
}
func (b *orderedBody) Close() error { return nil }
func TestOpenAIHTTPAccessAndGeneration(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"})
	if e != nil {
		t.Fatal(e)
	}
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
	p := &openAIProbe{}
	h := OpenAISettingsHandler(m, p)
	call := func(method, path, user, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.com"+path, strings.NewReader(body))
		if user != "" {
			r.AddCookie(cookie(user))
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	for _, x := range []struct {
		method, path, user, origin, body string
		status                           int
	}{
		{"GET", "/api/openai", "", "", "", 401}, {"GET", "/api/openai", "demo", "", "", 403},
		{"GET", "/api/openai", "eugene", "", "", 200}, {"POST", "/api/openai", "eugene", "", "", 405},
		{"GET", "/api/openai/unknown", "eugene", "", "", 404}, {"GET", "/api/openai/", "eugene", "", "", 404},
		{"PUT", "/api/openai/key", "eugene", "https://foreign.invalid", "{}", 403},
		{"PUT", "/api/openai/key", "eugene", "https://example.com", "{}", 200},
		{"PUT", "/api/openai/key", "eugene", "", strings.Repeat("x", 2049), 413},
		{"POST", "/api/openai/refresh", "eugene", "https://example.com", "", 200},
		{"POST", "/api/openai/test", "eugene", "https://example.com", "", 503},
	} {
		if w := call(x.method, x.path, x.user, x.origin, x.body); w.Code != x.status {
			t.Fatalf("%s %s: %d expected %d", x.method, x.path, w.Code, x.status)
		}
	}
	p.generation = false
	b := &orderedBody{t: t, p: p}
	r := httptest.NewRequest("PUT", "https://example.com/api/openai/key", nil)
	r.Body = b
	r.AddCookie(cookie("eugene"))
	w := httptest.NewRecorder()
	h(w, r)
	if !b.read || !p.generation {
		t.Fatal("generation/body not exercised")
	}
	p.generationErr = aiproviders.ErrStorage
	p.generation = false
	b.read = false
	w = httptest.NewRecorder()
	h(w, r)
	if w.Code != 503 || b.read {
		t.Fatal("body consumed after generation/storage failure")
	}
}
