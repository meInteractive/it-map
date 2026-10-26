//go:build mariadb && vacancycreates

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/vacancies"
)

type vacancyCreateHTTP struct {
	f       *vacancyCreateFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openVacancyCreateHTTP(f *vacancyCreateFixture, writer vacancies.Repository) *vacancyCreateHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic vacancy creation auth")
	}
	h := &vacancyCreateHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, Vacancies: f.repo, VacancyDetails: f.repo, VacancyCreates: writer})
	for _, login := range []string{"eugene", "demo", "nastya"} {
		form := url.Values{"login": {login}, "password": {"CI_SYNTHETIC_TASK_PASSWORD"}}
		req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://itmap.test")
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, req)
		if w.Code != 303 {
			f.t.Fatal("synthetic vacancy creation login failed")
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
func (h *vacancyCreateHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
	h.f.t.Helper()
	var body []byte
	var err error
	if raw, ok := payload.(string); ok {
		body = []byte(raw)
	} else if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			h.f.t.Fatal("cannot encode synthetic vacancy creation HTTP payload")
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
		h.f.t.Fatal("vacancy creation API response must be JSON/private/no-store")
	}
	return w.Body.Bytes()
}
func vacancyCreateJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected vacancy creation API JSON shape")
	}
	return v
}
func (h *vacancyCreateHTTP) error(data []byte) {
	h.f.t.Helper()
	v := vacancyCreateJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("vacancy creation API error missing")
	}
}

type vacancyCreateReply struct {
	OK        bool                       `json:"ok"`
	Vacancies []vacancies.CompanyVacancy `json:"vacancies"`
}

func TestMariaDBVacancyCreateHTTPDisposable(t *testing.T) {
	t.Run("admin create guards full reply and private relevance", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		h := openVacancyCreateHTTP(f, f.repo)
		input := f.input("http")
		path := "/api/vacancies"
		h.error(h.call("", "POST", path, input, 401))
		for _, user := range []string{"demo", "nastya"} {
			h.error(h.call(user, "POST", path, input, 403))
		}
		h.error(h.call("eugene", "POST", path, input, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		h.error(h.call("eugene", "POST", path, input, 400, http.Header{"Content-Type": {"text/plain"}}))
		h.error(h.call("eugene", "POST", path, `{} {}`, 400))
		unknown := input
		unknown.CompanyName = "CI unknown owner"
		h.error(h.call("eugene", "POST", path, unknown, 404))
		invalid := input
		invalid.RoleIDs = []int64{f.root}
		h.error(h.call("eugene", "POST", path, invalid, 400))
		// The unchanged legacy POST route returns 404 for a missing channel owner.
		missing := input
		missing.CompanyName = ""
		missing.EntityType = "channel"
		id := int64(999999)
		missing.TelegramChannelID = &id
		h.error(h.call("eugene", "POST", path, missing, 404))
		raw, _ := json.Marshal(input)
		var personal map[string]any
		if err := json.Unmarshal(raw, &personal); err != nil {
			t.Fatal(err)
		}
		personal["coverLetterScope"] = "personal"
		h.error(h.call("eugene", "POST", path, personal, 400))
		f.assertAbsent()
		reply := vacancyCreateJSON[vacancyCreateReply](t, h.call("eugene", "POST", path+"?userName=Nastya", input, 201))
		if !reply.OK || len(reply.Vacancies) != 1 || reply.Vacancies[0].ID <= 1 || reply.Vacancies[0].Relevance == nil || *reply.Vacancies[0].Relevance != 63 || len(reply.Vacancies[0].Roles) != 1 || len(reply.Vacancies[0].TelegramSources) != 1 {
			t.Fatal("admin creation 201 response contract changed")
		}
		createdID := reply.Vacancies[0].ID
		own := vacancyCreateJSON[vacancies.DetailResponse](t, h.call("eugene", "GET", fmt.Sprintf("/api/vacancies/%d", createdID), nil, 200))
		if own.Vacancy.Relevance == nil || *own.Vacancy.Relevance != 63 || len(own.Vacancy.Revision) != 64 {
			t.Fatal("admin detail lost creation state/revision")
		}
		other := vacancyCreateJSON[vacancies.DetailResponse](t, h.call("nastya", "GET", fmt.Sprintf("/api/vacancies/%d?userName=Eugene", createdID), nil, 200))
		if other.Vacancy.Relevance != nil || other.Vacancy.Favorite || other.Vacancy.Hidden {
			t.Fatal("HTTP create trusted injected owner")
		}
		if f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE vacancy_id=? AND user_name='Eugene'`, createdID) != 1 || f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE vacancy_id=? AND user_name='Nastya'`, createdID) != 0 {
			t.Fatal("admin creation assigned personal state to wrong user")
		}
		h.error(h.call("eugene", "POST", path, input, 400))
		f.assertLockFree()
	})
	t.Run("HTTP late source fault generic rollback and retry", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		fault, db := openMutationFaultPool(t, "INSERT INTO vacancy_telegram_sources")
		h := openVacancyCreateHTTP(f, vacancies.Repository{DB: db})
		input := f.input("http-fault")
		data := h.call("eugene", "POST", "/api/vacancies", input, 503)
		h.error(data)
		if strings.Contains(string(data), errMutationFault.Error()) {
			t.Fatal("HTTP creator disclosed SQL interruption")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("HTTP creator never reached real source write")
		}
		f.assertAbsent()
		f.assertLockFree()
		reply := vacancyCreateJSON[vacancyCreateReply](t, h.call("eugene", "POST", "/api/vacancies", input, 201))
		if !reply.OK || len(reply.Vacancies) != 1 {
			t.Fatal("HTTP creator retry failed")
		}
	})
}
