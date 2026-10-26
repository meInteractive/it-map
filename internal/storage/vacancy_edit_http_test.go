//go:build mariadb && vacancyedits

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
	"reflect"
	"strings"
	"testing"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/vacancies"
)

type vacancyEditHTTP struct {
	f       *vacancyCreateFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openVacancyEditHTTP(f *vacancyCreateFixture, writer vacancies.Repository) *vacancyEditHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic vacancy creation auth")
	}
	h := &vacancyEditHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, Vacancies: f.repo, VacancyDetails: f.repo, VacancyUpdates: writer})
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
func (h *vacancyEditHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
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
func vacancyEditJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected vacancy creation API JSON shape")
	}
	return v
}
func (h *vacancyEditHTTP) error(data []byte) {
	h.f.t.Helper()
	v := vacancyEditJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("vacancy creation API error missing")
	}
}

type vacancyEditReply struct {
	OK        bool                       `json:"ok"`
	Vacancies []vacancies.CompanyVacancy `json:"vacancies"`
}

func editHTTPPayload(t *testing.T, input vacancies.UpdateRequest) map[string]any {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !input.LetterProvided {
		delete(result, "coverLetterSubject")
		delete(result, "coverLetterMd")
		delete(result, "coverLetterScope")
	}
	if !input.TelegramProvided {
		delete(result, "telegramChannelId")
		delete(result, "telegramMessageUrl")
		delete(result, "telegramContact")
	}
	return result
}
func TestMariaDBVacancyEditHTTPDisposable(t *testing.T) {
	t.Run("admin edit revision errors personal fields and actual response", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		created := f.created(f.repo, f.input("http-edit"))
		id := created.ID
		h := openVacancyEditHTTP(f, f.repo)
		path := fmt.Sprintf("/api/vacancies/%d", id)
		before := f.detail("Eugene", id)
		input := f.editInput(before)
		payload := editHTTPPayload(t, input)
		h.error(h.call("", "PUT", path, payload, 401))
		h.error(h.call("demo", "PUT", path, payload, 403))
		h.error(h.call("eugene", "PUT", path, payload, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		missing := editHTTPPayload(t, input)
		delete(missing, "revision")
		h.error(h.call("eugene", "PUT", path, missing, 428))
		bad := editHTTPPayload(t, input)
		bad["roleIds"] = []int64{f.root}
		h.error(h.call("eugene", "PUT", path, bad, 400))
		h.error(h.call("eugene", "PUT", "/api/vacancies/999999", payload, 404))
		if !reflect.DeepEqual(before, f.detail("Eugene", id)) {
			t.Fatal("invalid admin edits changed vacancy")
		}
		reply := vacancyEditJSON[vacancyEditReply](t, h.call("eugene", "PUT", path+"?userName=Nastya", payload, 200))
		if !reply.OK || len(reply.Vacancies) != 1 || reply.Vacancies[0].Title != input.Title {
			t.Fatal("edit response must contain updated company's vacancies")
		}
		h.error(h.call("eugene", "PUT", path, payload, 409))
		own := vacancyEditJSON[vacancies.DetailResponse](t, h.call("eugene", "GET", path, nil, 200))
		if own.Vacancy.Revision == before.Revision || own.Vacancy.Relevance == nil || *own.Vacancy.Relevance != 63 {
			t.Fatal("edit GET did not advance revision or signed-in relevance")
		}
		if f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='Nastya' AND vacancy_id=?`, id) != 0 {
			t.Fatal("query owner override affected edit user")
		}
		personal := f.editInput(own.Vacancy)
		personal.LetterProvided = true
		personal.CoverLetterScope = "personal"
		personal.CoverLetterSubject = "CI HTTP private subject"
		personal.CoverLetterMD = "CI HTTP private markdown"
		h.call("eugene", "PUT", path, editHTTPPayload(t, personal), 200)
		other := vacancyEditJSON[vacancies.DetailResponse](t, h.call("nastya", "GET", path+"?userName=Eugene", nil, 200))
		if other.Vacancy.CoverLetterScope != "shared" || other.Vacancy.CoverLetterMD == personal.CoverLetterMD {
			t.Fatal("personal editor letter leaked to another owner")
		}
		if f.count(`SELECT COUNT(*) FROM vacancy_cover_letters WHERE user_name='Eugene' AND vacancy_id=?`, id) != 1 {
			t.Fatal("admin personal letter assigned to wrong user")
		}
	})
	t.Run("HTTP real SQL failure generic rollback then same revision retry", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		created := f.created(f.repo, f.input("http-edit-fault"))
		id := created.ID
		before := f.detail("Eugene", id)
		fault, db := openMutationFaultPool(t, "UPDATE company_vacancies SET")
		h := openVacancyEditHTTP(f, vacancies.Repository{DB: db})
		path := fmt.Sprintf("/api/vacancies/%d", id)
		payload := editHTTPPayload(t, f.editInput(before))
		data := h.call("eugene", "PUT", path, payload, 503)
		h.error(data)
		if strings.Contains(string(data), errMutationFault.Error()) {
			t.Fatal("edit HTTP exposed SQL details")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("edit HTTP did not reach actual SQL update")
		}
		if !reflect.DeepEqual(before, f.detail("Eugene", id)) {
			t.Fatal("failed HTTP edit left partial fields or changed revision")
		}
		f.assertLockFree()
		reply := vacancyEditJSON[vacancyEditReply](t, h.call("eugene", "PUT", path, payload, 200))
		if !reply.OK || len(reply.Vacancies) != 1 {
			t.Fatal("HTTP edit retry using rolled-back revision failed")
		}
	})
}
