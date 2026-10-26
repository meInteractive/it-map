//go:build mariadb && vacancydeletes

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

type vacancyDeleteHTTP struct {
	f       *vacancyCreateFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openVacancyDeleteHTTP(f *vacancyCreateFixture, writer vacancies.Repository) *vacancyDeleteHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic vacancy creation auth")
	}
	h := &vacancyDeleteHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, Vacancies: f.repo, VacancyDetails: f.repo, VacancyDeletes: writer})
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
func (h *vacancyDeleteHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
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
func vacancyDeleteJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected vacancy creation API JSON shape")
	}
	return v
}
func (h *vacancyDeleteHTTP) error(data []byte) {
	h.f.t.Helper()
	v := vacancyDeleteJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("vacancy creation API error missing")
	}
}

func TestMariaDBVacancyDeleteHTTPDisposable(t *testing.T) {
	t.Run("admin archive delete guards legacy JSON and retained CRM", func(t *testing.T) {
		f, id := prepareVacancyDeletion(t, true)
		h := openVacancyDeleteHTTP(f, f.repo)
		path := fmt.Sprintf("/api/vacancies/%d", id)
		before, links, retained := f.detail("Demo", id), f.links(id), f.retainedCRM()
		h.error(h.call("", "DELETE", path, nil, 401))
		h.error(h.call("demo", "DELETE", path, nil, 403))
		h.error(h.call("nastya", "DELETE", path, nil, 403))
		h.error(h.call("eugene", "DELETE", path, nil, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		h.error(h.call("eugene", "DELETE", "/api/vacancies/0", nil, 400))
		h.error(h.call("eugene", "DELETE", "/api/vacancies/999999", nil, 404))
		if before.Revision != f.detail("Demo", id).Revision || links != f.links(id) {
			t.Fatal("invalid HTTP deletion changed owned vacancy or links")
		}
		data := h.call("eugene", "DELETE", path, nil, 200)
		archive := vacancyDeleteJSON[map[string]any](t, data)
		if len(archive) != 3 || archive["ok"] != true || archive["archived"] != true || archive["companyName"] != crudCompany {
			t.Fatal("archive JSON must match legacy keys exactly")
		}
		if _, ok := archive["preservedApplication"]; ok {
			t.Fatal("archive reply included delete-only flags")
		}
		if !f.detail("Demo", id).Archived || links != f.links(id) {
			t.Fatal("first HTTP delete did not preserve linked vacancy")
		}
		data = h.call("eugene", "DELETE", path, nil, 200)
		deleted := vacancyDeleteJSON[map[string]any](t, data)
		if len(deleted) != 5 || deleted["ok"] != true || deleted["archived"] != false || deleted["preservedApplication"] != true || deleted["preservedTask"] != true {
			t.Fatal("hard deletion JSON did not report preserved links")
		}
		f.assertDeleted(id, true)
		if retained != f.retainedCRM() {
			t.Fatal("HTTP deletion changed retained CRM content")
		}
		h.error(h.call("eugene", "GET", path, nil, 404))
		h.error(h.call("eugene", "DELETE", path, nil, 404))
	})
	t.Run("channel hard delete keeps false preservation flags and cascades source", func(t *testing.T) {
		f := openVacancyCreateFixture(t)
		input := f.input("channel-delete")
		input.CompanyName = ""
		input.EntityType = "channel"
		created := f.created(f.repo, input)
		h := openVacancyDeleteHTTP(f, f.repo)
		result := vacancyDeleteJSON[map[string]any](t, h.call("eugene", "DELETE", fmt.Sprintf("/api/vacancies/%d", created.ID), nil, 200))
		if len(result) != 5 || result["archived"] != false || result["preservedApplication"] != false || result["preservedTask"] != false || result["companyName"] != created.CompanyName {
			t.Fatal("channel hard-delete JSON changed")
		}
		if f.count(`SELECT COUNT(*) FROM company_vacancies WHERE id=?`, created.ID) != 0 || f.count(`SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=?`, created.ID) != 0 || f.count(`SELECT COUNT(*) FROM vacancy_role_assignments WHERE vacancy_id=?`, created.ID) != 0 || f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE vacancy_id=?`, created.ID) != 0 || f.count(`SELECT COUNT(*) FROM company_crm WHERE company_name=?`, created.CompanyName) != 1 {
			t.Fatal("channel delete lost CRM anchor or left FK children")
		}
	})

	t.Run("HTTP real cascade delete fault generic rollback and retry", func(t *testing.T) {
		f, id := prepareVacancyDeletion(t, true)
		if _, err := f.repo.DeleteManual(f.ctx, id); err != nil {
			t.Fatal("cannot prepare HTTP archived fixture")
		}
		before, links, retained := f.detail("Demo", id), f.links(id), f.retainedCRM()
		fault, db := openMutationFaultPool(t, "DELETE FROM company_vacancies")
		h := openVacancyDeleteHTTP(f, vacancies.Repository{DB: db})
		path := fmt.Sprintf("/api/vacancies/%d", id)
		data := h.call("eugene", "DELETE", path, nil, 503)
		h.error(data)
		if strings.Contains(string(data), errMutationFault.Error()) {
			t.Fatal("delete HTTP disclosed SQL fault")
		}
		fault.mu.Lock()
		fired := fault.fired
		fault.mu.Unlock()
		if !fired {
			t.Fatal("HTTP deletion fault never reached real cascade DELETE")
		}
		if !reflect.DeepEqual(before, f.detail("Demo", id)) || links != f.links(id) || retained != f.retainedCRM() {
			t.Fatal("HTTP delete failure left detached CRM or missing FK children")
		}
		result := vacancyDeleteJSON[vacancies.DeleteResult](t, h.call("eugene", "DELETE", path, nil, 200))
		if !result.OK || result.Archived || !result.PreservedApplication || !result.PreservedTask {
			t.Fatal("HTTP delete retry result changed")
		}
		f.assertDeleted(id, true)
	})
}
