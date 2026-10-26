//go:build mariadb && profilewrites

package storage

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/profiles"
)

type profileHTTP struct {
	f       *profileResumeFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openProfileHTTP(f *profileResumeFixture, writer profiles.Repository, configure ...func(*httpapi.Options)) *profileHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic profile auth")
	}
	h := &profileHTTP{f: f, cookies: map[string]*http.Cookie{}}
	opts := httpapi.Options{DB: f.db, Auth: manager, Resumes: writer}
	for _, apply := range configure {
		apply(&opts)
	}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), opts)
	for _, login := range []string{"eugene", "demo", "nastya"} {
		form := url.Values{"login": {login}, "password": {"CI_SYNTHETIC_TASK_PASSWORD"}}
		req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://itmap.test")
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, req)
		if w.Code != 303 {
			f.t.Fatal("synthetic profile login failed")
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == auth.CookieName && cookie.Secure && cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode {
				h.cookies[login] = cookie
			}
		}
		if h.cookies[login] == nil {
			f.t.Fatal("login did not issue a protected session cookie")
		}
	}
	return h
}
func (h *profileHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
	h.f.t.Helper()
	var body []byte
	var err error
	if raw, ok := payload.(string); ok {
		body = []byte(raw)
	} else if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			h.f.t.Fatal("cannot encode synthetic profile HTTP payload")
		}
	}
	req := httptest.NewRequest(method, "https://itmap.test"+path, bytes.NewReader(body)).WithContext(h.f.ctx)
	if user != "" {
		req.AddCookie(h.cookies[user])
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" {
		req.Header.Set("Origin", "https://itmap.test")
	}
	for _, header := range headers {
		for key, values := range header {
			req.Header[key] = values
		}
	}
	if req.Header.Get("Transfer-Encoding") == "chunked" {
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
	}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != want {
		h.f.t.Fatalf("%s %s: status %d, expected %d", method, path, w.Code, want)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		h.f.t.Fatal("profile API response must be JSON/private/no-store")
	}
	return w.Body.Bytes()
}
func profileJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected profile API JSON shape")
	}
	return v
}
func (h *profileHTTP) error(data []byte) {
	h.f.t.Helper()
	v := profileJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("profile API error missing")
	}
}

func TestMariaDBProfileResumeHTTPDisposable(t *testing.T) {
	t.Run("signed Unicode resume CAS clear limits and owner isolation", func(t *testing.T) {
		f := openProfileResumeFixture(t)
		h := openProfileHTTP(f, f.repo)
		path := "/api/profile/resume"
		h.error(h.call("", "GET", path, nil, 401))
		h.error(h.call("", "PUT", path, map[string]any{"resumeMd": "CI", "revision": 0}, 401))
		empty := profileJSON[profiles.Resume](t, h.call("demo", "GET", path+"?userName=Eugene", nil, 200))
		if !reflect.DeepEqual(empty, profiles.DefaultResume()) || f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Demo'`) != 0 {
			t.Fatal("HTTP GET defaults initialized a row or used a foreign owner")
		}
		h.error(h.call("demo", "PUT", path, `{"resumeMd":"CI","revision":0}`, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		for _, raw := range []string{`{"resumeMd":"CI","revision":0,"userName":"Nastya"}`, `{"resumeMd":"CI","revision":null}`, `{"resumeMd":"CI\u0000","revision":0}`, `{"ResumeMd":"CI","revision":0}`, `{} {}`} {
			h.error(h.call("demo", "PUT", path, raw, 400))
		}
		if f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name IN ('Demo','Nastya')`) != 0 {
			t.Fatal("invalid resume HTTP initialized profile rows")
		}
		text := strings.Repeat("😀", profiles.MaxResumeLength)
		payload := map[string]any{"resumeMd": text, "revision": 0}
		created := profileJSON[profiles.Resume](t, h.call("demo", "PUT", path+"?userName=Nastya", payload, 200, http.Header{"Content-Type": {"text/plain"}}))
		if created.ResumeMD != text || created.Revision != 1 || created.UpdatedAt == nil || created.MaxLength != 20000 {
			t.Fatal("HTTP resume lost Unicode text/revision/projection")
		}
		h.error(h.call("demo", "PUT", path, payload, 409))
		other := profileJSON[profiles.Resume](t, h.call("nastya", "GET", path+"?userName=Demo", nil, 200))
		if !reflect.DeepEqual(other, profiles.DefaultResume()) {
			t.Fatal("personal resume leaked to another member")
		}
		admin := profileJSON[profiles.Resume](t, h.call("eugene", "GET", path+"?userName=Demo", nil, 200))
		if !reflect.DeepEqual(admin, f.original) {
			t.Fatal("administrator could select someone else's resume")
		}
		edited := profileJSON[profiles.Resume](t, h.call("demo", "PUT", path, `{"resumeMd":"  CI preserved text  ","revision":1.0}`, 200))
		if edited.Revision != 2 || edited.ResumeMD != "  CI preserved text  " {
			t.Fatal("profile numeric revision/no-trim contract changed")
		}
		cleared := profileJSON[profiles.Resume](t, h.call("demo", "PUT", path, map[string]any{"resumeMd": "", "revision": 2}, 200))
		if cleared.Revision != 3 || cleared.ResumeMD != "" {
			t.Fatal("HTTP resume clear failed")
		}
		before := f.read("Demo")
		h.error(h.call("demo", "PUT", path, strings.Repeat(" ", profiles.ResumeBodyLimit+1), 413, http.Header{"Transfer-Encoding": {"chunked"}}))
		if !reflect.DeepEqual(before, f.read("Demo")) {
			t.Fatal("oversized resume stream changed stored data")
		}
		h.call("nastya", "PUT", path, map[string]any{"resumeMd": "CI independent profile", "revision": 0}, 200)
		if !reflect.DeepEqual(before, f.read("Demo")) {
			t.Fatal("other user's resume write changed Demo")
		}
	})
	t.Run("HTTP late real update error generic rollback and same revision retry", func(t *testing.T) {
		f := openProfileResumeFixture(t)
		fault, db := openMutationFaultPool(t, "UPDATE user_profiles SET")
		h := openProfileHTTP(f, profiles.Repository{DB: db})
		payload := map[string]any{"resumeMd": "CI synthetic private resume", "revision": 0}
		data := h.call("demo", "PUT", "/api/profile/resume", payload, 503)
		h.error(data)
		if strings.Contains(string(data), errMutationFault.Error()) {
			t.Fatal("resume API disclosed SQL details")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("resume HTTP fault never reached actual SQL update")
		}
		if !reflect.DeepEqual(f.read("Demo"), profiles.DefaultResume()) || f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name='Demo'`) != 0 {
			t.Fatal("failed HTTP save left a profile/revision")
		}
		saved := profileJSON[profiles.Resume](t, h.call("demo", "PUT", "/api/profile/resume", payload, 200))
		if saved.Revision != 1 || saved.ResumeMD != "CI synthetic private resume" {
			t.Fatal("HTTP resume retry failed")
		}
	})
}
