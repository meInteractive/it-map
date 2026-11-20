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

	"itmap/internal/applications"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/vacancies"
)

type vacancyDeleteApplicationDetail struct {
	Record applications.Record `json:"record"`
}

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
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, Vacancies: f.repo, VacancyDetails: f.repo, VacancyDeletes: writer, Applications: applications.Repository{DB: f.db}})
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
	t.Run("lost acknowledgement after real MariaDB COMMIT keeps archived vacancy or retained CRM", func(t *testing.T) {
		for _, stage := range []string{"archive", "hard-delete"} {
			t.Run(stage, func(t *testing.T) {
				f, id := prepareVacancyDeletion(t, true)
				h := openVacancyDeleteHTTP(f, f.repo)
				vacancyPath := fmt.Sprintf("/api/vacancies/%d", id)
				var demoID, nastyaID int64
				f.scan("SELECT id FROM company_applications WHERE vacancy_id=? AND user_name='Demo'", []any{id}, &demoID)
				f.scan("SELECT id FROM company_applications WHERE vacancy_id=? AND user_name='Nastya'", []any{id}, &nastyaID)
				demoPath := fmt.Sprintf("/api/applications/%d", demoID)
				nastyaPath := fmt.Sprintf("/api/applications/%d", nastyaID)
				before := vacancyDeleteJSON[vacancyDeleteApplicationDetail](t, h.call("demo", "GET", demoPath, nil, 200)).Record
				otherBefore := vacancyDeleteJSON[vacancyDeleteApplicationDetail](t, h.call("nastya", "GET", nastyaPath, nil, 200)).Record
				if before.VacancyID == nil || *before.VacancyID != id || otherBefore.VacancyID == nil || *otherBefore.VacancyID != id || len(before.History) != 1 || len(otherBefore.History) != 1 {
					t.Fatal("linked application HTTP fixture missing owned vacancy and history")
				}
				h.error(h.call("nastya", "GET", demoPath, nil, 404))
				h.error(h.call("demo", "GET", nastyaPath, nil, 404))
				links, retained := f.links(id), f.retainedCRM()
				if stage == "hard-delete" {
					first := vacancyDeleteJSON[vacancies.DeleteResult](t, h.call("eugene", "DELETE", vacancyPath, nil, 200))
					if !first.OK || !first.Archived {
						t.Fatal("cannot prepare archived vacancy for lost hard-delete acknowledgement")
					}
				}
				// Меняем только доставку ответа: реальный Go handler уже выполнил SQL COMMIT.
				backend := h.handler
				deleteCalls := 0
				h.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.Method != http.MethodDelete || req.URL.Path != vacancyPath {
						backend.ServeHTTP(w, req)
						return
					}
					deleteCalls++
					if deleteCalls != 1 {
						backend.ServeHTTP(w, req)
						return
					}
					actual := httptest.NewRecorder()
					backend.ServeHTTP(actual, req)
					if actual.Code != http.StatusOK {
						t.Fatalf("real SQL-backed DELETE returned %d, cannot simulate lost ACK", actual.Code)
					}
					for key, values := range actual.Header() {
						w.Header()[key] = append([]string(nil), values...)
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":"CI synthetic response lost after COMMIT"}`)
				})
				h.error(h.call("eugene", "DELETE", vacancyPath, nil, 503))
				if deleteCalls != 1 {
					t.Fatal("failed to simulate precisely one committed DELETE")
				}
				if stage == "archive" {
					// Клиентское восстановление должно увидеть archived:true и НЕ повторить DELETE.
					got := vacancyDeleteJSON[vacancies.DetailResponse](t, h.call("eugene", "GET", vacancyPath, nil, 200))
					if !got.Vacancy.Archived || f.links(id) != links || retained != f.retainedCRM() {
						t.Fatal("lost archive ACK changed CRM links or did not expose new archived state")
					}
					demo := vacancyDeleteJSON[vacancyDeleteApplicationDetail](t, h.call("demo", "GET", demoPath, nil, 200)).Record
					if demo.VacancyID == nil || *demo.VacancyID != id || !reflect.DeepEqual(demo.History, before.History) {
						t.Fatal("archived vacancy lost linked application/history")
					}
				} else {
					// После COMMIT hard-delete GET 404 исключает второй DELETE без нового подтверждения.
					h.error(h.call("eugene", "GET", vacancyPath, nil, 404))
					f.assertDeleted(id, true)
					if retained != f.retainedCRM() {
						t.Fatal("hard-delete with lost ACK changed retained CRM data")
					}
					for _, owner := range []struct {
						user string
						path string
						previous applications.Record
					}{{"demo", demoPath, before}, {"nastya", nastyaPath, otherBefore}} {
						after := vacancyDeleteJSON[vacancyDeleteApplicationDetail](t, h.call(owner.user, "GET", owner.path, nil, 200)).Record
						if after.ID != owner.previous.ID || after.VacancyID != nil || after.VacancyTitle != "" ||
							after.CompanyName != owner.previous.CompanyName || after.Status != owner.previous.Status ||
							after.Comment != owner.previous.Comment || !reflect.DeepEqual(after.History, owner.previous.History) {
							t.Fatal("detached application HTTP lost owner, content or status history")
						}
					}
					h.error(h.call("nastya", "GET", demoPath, nil, 404))
					h.error(h.call("demo", "GET", nastyaPath, nil, 404))
				}
				if deleteCalls != 1 {
					t.Fatal("client recovery must not issue another DELETE before a new confirmation")
				}
			})
		}
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
