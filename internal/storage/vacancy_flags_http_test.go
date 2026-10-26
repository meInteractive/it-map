//go:build mariadb && vacancywrites

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
	"itmap/internal/vacancies"
)

type vacancyFlagHTTP struct {
	f       *vacancyFlagFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openVacancyFlagHTTP(f *vacancyFlagFixture, writer vacancies.Repository) *vacancyFlagHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic CRM auth")
	}
	h := &vacancyFlagHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, Vacancies: f.repo, VacancyDetails: f.repo, VacancyFlags: writer})
	for _, login := range []string{"demo", "nastya"} {
		form := url.Values{"login": {login}, "password": {"CI_SYNTHETIC_TASK_PASSWORD"}}
		req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://itmap.test")
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, req)
		if w.Code != 303 {
			f.t.Fatal("synthetic CRM login failed")
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
func (h *vacancyFlagHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
	h.f.t.Helper()
	var body []byte
	var err error
	if raw, ok := payload.(string); ok {
		body = []byte(raw)
	} else if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			h.f.t.Fatal("cannot encode synthetic CRM HTTP payload")
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
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != want {
		h.f.t.Fatalf("%s %s: status %d, expected %d", method, path, w.Code, want)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || !strings.Contains(w.Header().Get("Cache-Control"), "private") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		h.f.t.Fatal("CRM API response must be JSON/private/no-store")
	}
	return w.Body.Bytes()
}
func vacancyFlagJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected CRM API JSON shape")
	}
	return v
}
func (h *vacancyFlagHTTP) error(data []byte) {
	h.f.t.Helper()
	v := vacancyFlagJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("CRM API error missing")
	}
}

type vacancyFlagReply struct {
	OK bool `json:"ok"`
	vacancies.UserFlags
}

func TestMariaDBVacancyFlagHTTPDisposable(t *testing.T) {
	t.Run("signed flags list detail owner isolation and validation", func(t *testing.T) {
		f := openVacancyFlagFixture(t)
		f.seed("Demo", 63)
		h := openVacancyFlagHTTP(f, f.repo)
		path := "/api/vacancies/1/state"
		before := f.detail("Demo")
		h.error(h.call("", "PUT", path, `{"favorite":true}`, 401))
		h.error(h.call("demo", "PUT", path, `{"favorite":true}`, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		for _, raw := range []string{`{"favorite":null}`, `{"favorite":"true"}`, `{"favorite":true,"hidden":true}`, `{"favorite":true,"userName":"Nastya"}`, `{"favorite":true,"favorite":false}`, `{"revision":"invented","favorite":true}`, `{} {}`} {
			h.error(h.call("demo", "PUT", path, raw, 400))
		}
		h.error(h.call("demo", "PUT", "/api/vacancies/999999/state", `{"favorite":true}`, 404))
		if !reflect.DeepEqual(before, f.detail("Demo")) {
			t.Fatal("invalid vacancy state HTTP calls changed detail")
		}
		got := vacancyFlagJSON[vacancyFlagReply](t, h.call("demo", "PUT", path+"?userName=Nastya", `{"favorite":true}`, 200))
		if !got.OK || got.VacancyID != 1 || !got.Favorite || got.Hidden {
			t.Fatal("vacancy state reply changed")
		}
		got = vacancyFlagJSON[vacancyFlagReply](t, h.call("demo", "PUT", path, `{"hidden":true}`, 200))
		if !got.Favorite || !got.Hidden {
			t.Fatal("HTTP vacancy flags lost independent field")
		}
		read := vacancyFlagJSON[vacancies.DetailResponse](t, h.call("demo", "GET", "/api/vacancies/1", nil, 200))
		if !read.Vacancy.Favorite || !read.Vacancy.Hidden || read.Vacancy.Revision != before.Vacancy.Revision || read.Vacancy.Relevance == nil || *read.Vacancy.Relevance != 63 {
			t.Fatal("HTTP detail lost flags relevance or editing revision")
		}
		list := vacancyFlagJSON[vacancies.ListResponse](t, h.call("demo", "GET", "/api/vacancies/list", nil, 200))
		if len(list.Vacancies) != 1 || !list.Vacancies[0].Favorite || !list.Vacancies[0].Hidden {
			t.Fatal("HTTP compact list missed flags")
		}
		other := vacancyFlagJSON[vacancies.DetailResponse](t, h.call("nastya", "GET", "/api/vacancies/1?userName=Demo", nil, 200))
		if other.Vacancy.Favorite || other.Vacancy.Hidden || other.Vacancy.Relevance != nil {
			t.Fatal("HTTP vacancy flags leaked to another user")
		}
		otherList := vacancyFlagJSON[vacancies.ListResponse](t, h.call("nastya", "GET", "/api/vacancies/list?userName=Demo", nil, 200))
		if len(otherList.Vacancies) != 1 || otherList.Vacancies[0].Favorite || otherList.Vacancies[0].Hidden {
			t.Fatal("HTTP vacancy list leaked another user's flags")
		}
	})
	t.Run("HTTP late SQL error is generic rollback and retry safe", func(t *testing.T) {
		f := openVacancyFlagFixture(t)
		fault, db := openMutationFaultPool(t, "INSERT INTO company_vacancy_user_state")
		h := openVacancyFlagHTTP(f, vacancies.Repository{DB: db})
		path := "/api/vacancies/1/state"
		data := h.call("demo", "PUT", path, `{"favorite":true}`, 503)
		h.error(data)
		if strings.Contains(string(data), errMutationFault.Error()) {
			t.Fatal("HTTP exposed vacancy SQL fault")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("HTTP vacancy fault never reached SQL")
		}
		if f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='Demo' AND vacancy_id=1`) != 0 {
			t.Fatal("HTTP failed insert left vacancy state")
		}
		got := vacancyFlagJSON[vacancyFlagReply](t, h.call("demo", "PUT", path, `{"favorite":true}`, 200))
		if !got.OK || !got.Favorite {
			t.Fatal("HTTP vacancy flag retry failed")
		}
	})
}
