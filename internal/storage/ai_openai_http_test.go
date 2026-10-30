//go:build mariadb && profilewrites

package storage

import (
	"context"
	"io"
	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type aiRestoreBody struct {
	r      io.Reader
	before func()
	done   bool
}

func (b *aiRestoreBody) Read(p []byte) (int, error) {
	if !b.done {
		b.done = true
		b.before()
	}
	return b.r.Read(p)
}
func (b *aiRestoreBody) Close() error { return nil }
func TestMariaDBOpenAISettingsHTTPDisposable(t *testing.T) {
	f := openProfileResumeFixture(t)
	if f.count("SELECT COUNT(*) FROM openai_settings") != 0 {
		t.Fatal("requires empty synthetic AI table")
	}
	t.Cleanup(func() { f.exec("DELETE FROM openai_settings WHERE id=1") })
	repo := aiproviders.NewRepository(f.db, "CI_SYNTHETIC_SECRET", "")
	g, e := repo.Generation(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"})
	if e != nil {
		t.Fatal(e)
	}
	handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: m, OpenAISettings: aiproviders.NewOpenAIService(repo)})
	cookie := func(login string) *http.Cookie {
		u, ok := m.Authenticate(login, map[string]string{"eugene": "CI_ADMIN", "demo": "CI_DEMO"}[login])
		if !ok {
			t.Fatal("auth")
		}
		token, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return &http.Cookie{Name: auth.CookieName, Value: token}
	}
	send := func(method, path, user string, body io.ReadCloser) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.com"+path, nil)
		if body != nil {
			r.Body = body
		}
		if user != "" {
			r.AddCookie(cookie(user))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := send("GET", "/api/openai", "eugene", nil); w.Code != 200 || f.count("SELECT COUNT(*) FROM openai_settings") != 0 {
		t.Fatal("unconfigured GET wrote storage")
	}
	for _, user := range []string{"", "demo"} {
		status := 401
		if user != "" {
			status = 403
		}
		if w := send("PUT", "/api/openai/key", user, io.NopCloser(strings.NewReader(`{}`))); w.Code != status {
			t.Fatal("key save not admin protected")
		}
	}
	key := "sk-" + strings.Repeat("x", 20)
	payload := `{"apiKey":"` + key + `"}`
	body := &aiRestoreBody{r: strings.NewReader(payload), before: func() {
		f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", "00000000-0000-4000-8000-000000000001")
	}}
	w := send("PUT", "/api/openai/key", "eugene", body)
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", g)
	if w.Code != 409 || f.count("SELECT COUNT(*) FROM openai_settings") != 0 {
		t.Fatal("generation changed during body accepted or bootstrapped key")
	}
	w = send("PUT", "/api/openai/key", "eugene", io.NopCloser(strings.NewReader(payload)))
	if w.Code != 200 || strings.Contains(w.Body.String(), key) {
		t.Fatal("key save failed or leaked plaintext")
	}
	got, e := repo.Resolve(context.Background(), "openai")
	if e != nil || got != key {
		t.Fatal("HTTP key save did not persist compatible encrypted key")
	}
	if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()") != 72 {
		t.Fatal("HTTP key save changed schema")
	}
}
