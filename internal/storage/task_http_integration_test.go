//go:build mariadb && taskwrites

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
	"itmap/internal/tasks"
)

type taskHTTP struct {
	f       *taskFixture
	handler http.Handler
	cookies map[string]*http.Cookie
}

func openTaskHTTP(f *taskFixture, writer tasks.Repository) *taskHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(strings.Repeat("t", 64), map[string]string{"eugene": "CI_SYNTHETIC_TASK_PASSWORD", "demo": "CI_SYNTHETIC_TASK_PASSWORD", "nastya": "CI_SYNTHETIC_TASK_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic task auth")
	}
	h := &taskHTTP{f: f, cookies: map[string]*http.Cookie{}}
	h.handler = httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{DB: f.db, Auth: manager, Tasks: f.repo, TaskCreates: writer, TaskCompletion: writer, TaskReschedule: writer})
	for _, login := range []string{"demo", "nastya"} {
		form := url.Values{"login": {login}, "password": {"CI_SYNTHETIC_TASK_PASSWORD"}}
		req := httptest.NewRequest("POST", "https://itmap.test/auth", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://itmap.test")
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, req)
		if w.Code != 303 {
			f.t.Fatal("synthetic task login failed")
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
func (h *taskHTTP) call(user, method, path string, payload any, want int, headers ...http.Header) []byte {
	h.f.t.Helper()
	var body []byte
	var err error
	if raw, ok := payload.(string); ok {
		body = []byte(raw)
	} else if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			h.f.t.Fatal("cannot encode synthetic task HTTP payload")
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
		h.f.t.Fatal("task API response must be JSON/private/no-store")
	}
	return w.Body.Bytes()
}
func taskJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal("unexpected task API JSON shape")
	}
	return v
}
func (h *taskHTTP) error(data []byte) {
	h.f.t.Helper()
	v := taskJSON[map[string]any](h.f.t, data)
	if value, ok := v["error"].(string); !ok || value == "" {
		h.f.t.Fatal("task API error missing")
	}
}

type taskCreateReply struct {
	OK           bool  `json:"ok"`
	TaskID       int64 `json:"taskId"`
	CalendarSync struct {
		Status string `json:"status"`
	} `json:"calendarSync"`
}

func TestMariaDBTaskHTTPDisposable(t *testing.T) {
	t.Run("owned HTTP roundtrip filters errors and calendar guard", func(t *testing.T) {
		f := openTaskFixture(t)
		h := openTaskHTTP(f, f.repo)
		input := f.input()
		h.error(h.call("", "GET", "/api/tasks", nil, 401))
		h.error(h.call("", "POST", "/api/tasks", input, 401))
		h.error(h.call("demo", "POST", "/api/tasks", input, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		h.error(h.call("demo", "POST", "/api/tasks", input, 400, http.Header{"Content-Type": {"text/plain"}}))
		h.error(h.call("demo", "POST", "/api/tasks", `{"companyName":"CI Synthetic Employer","companyName":"Other","type":"call","dueAt":"2026-10-26T12:00:00Z"}`, 400))
		h.error(h.call("demo", "POST", "/api/tasks", `{"companyName":"CI Synthetic Employer","type":"call","dueAt":"2026-10-26T12:00:00Z","userName":"Eugene"}`, 400))
		for _, bad := range []string{"2026-10-26T12:00:00+24:00", "2026-10-26T12:00:00+00:60", "2026-02-30T12:00:00Z", "2026-10-26T12:00:00"} {
			v := input
			v.DueAt = bad
			h.error(h.call("demo", "POST", "/api/tasks", v, 400))
		}
		long := input
		long.Note = strings.Repeat("😀", 601)
		h.error(h.call("demo", "POST", "/api/tasks", long, 400))
		unknown := input
		unknown.CompanyName = "CI Unknown Synthetic Employer"
		h.error(h.call("demo", "POST", "/api/tasks", unknown, 404))
		missing := int64(999999)
		invalid := input
		invalid.VacancyID = &missing
		h.error(h.call("demo", "POST", "/api/tasks", invalid, 400))
		if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 0 {
			t.Fatal("invalid HTTP requests left task rows")
		}
		input.Note = "\uFEFFCI synthetic task HTTP\uFEFF"
		created := taskJSON[taskCreateReply](t, h.call("demo", "POST", "/api/tasks", input, 201))
		if !created.OK || created.TaskID <= 1 || created.CalendarSync.Status != "not_connected" {
			t.Fatal("task create response contract changed")
		}
		path := fmt.Sprintf("/api/tasks/%d", created.TaskID)
		listed := taskJSON[tasks.Response](t, h.call("demo", "GET", "/api/tasks?company="+url.QueryEscape(taskCompany)+"&vacancy=1", nil, 200))
		if len(listed.Tasks) != 1 || len(listed.Entities) != 1 || listed.Tasks[0].ID != created.TaskID || listed.Tasks[0].Note != "CI synthetic task HTTP" || listed.Tasks[0].DueAt != "2026-10-26T21:15:00.123Z" {
			t.Fatal("task HTTP list lost normalization/filters/join")
		}
		if foreign := taskJSON[tasks.Response](t, h.call("nastya", "GET", "/api/tasks", nil, 200)); len(foreign.Tasks) != 0 {
			t.Fatal("personal task leaked through HTTP")
		}
		h.error(h.call("nastya", "PUT", path, map[string]any{"completed": true}, 404))
		h.error(h.call("nastya", "PUT", path, map[string]any{"dueAt": "2026-11-01T12:00:00Z"}, 404))
		h.error(h.call("demo", "PUT", path, map[string]any{"completed": true}, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		before := f.row(created.TaskID)
		for _, raw := range []string{`{"dueAt":"2026-10-26T12:00:00+24:00"}`, `{"dueAt":"2026-10-26T12:00:00+00:60"}`, `{"completed":true,"dueAt":"2026-11-01T12:00:00Z"}`, `{"completed":null}`, `{"completed":true,"completed":false}`, `{} {}`} {
			h.error(h.call("demo", "PUT", path, raw, 400))
		}
		if !reflect.DeepEqual(before, f.row(created.TaskID)) {
			t.Fatal("invalid task HTTP updates changed the row")
		}
		moved := taskJSON[struct {
			OK           bool   `json:"ok"`
			DueAt        string `json:"dueAt"`
			CalendarSync struct {
				Status string `json:"status"`
			} `json:"calendarSync"`
		}](t, h.call("demo", "PUT", path, map[string]any{"dueAt": "2026-10-28T23:15:00.987-03:30"}, 200))
		if !moved.OK || moved.DueAt != "2026-10-29T02:45:00.987Z" || moved.CalendarSync.Status != "not_connected" {
			t.Fatal("reschedule HTTP UTC/calendar response changed")
		}
		expected := before
		expected.DueAt = moved.DueAt
		if !reflect.DeepEqual(expected, f.row(created.TaskID)) {
			t.Fatal("HTTP reschedule changed unrelated fields")
		}
		done := taskJSON[map[string]bool](t, h.call("demo", "PUT", path, map[string]any{"completed": true}, 200))
		if !done["ok"] {
			t.Fatal("completion acknowledgement missing")
		}
		completed := f.row(created.TaskID)
		if !completed.Completed || completed.CompletedAt == nil {
			t.Fatal("completion timestamp missing")
		}
		h.call("demo", "PUT", path, map[string]any{"completed": true}, 200)
		if !reflect.DeepEqual(completed, f.row(created.TaskID)) {
			t.Fatal("repeat HTTP completion changed task")
		}
		if active := taskJSON[tasks.Response](t, h.call("demo", "GET", "/api/tasks", nil, 200)); len(active.Tasks) != 0 {
			t.Fatal("active list includes a completed task")
		}
		if all := taskJSON[tasks.Response](t, h.call("demo", "GET", "/api/tasks?includeCompleted=1", nil, 200)); len(all.Tasks) != 1 {
			t.Fatal("includeCompleted filter failed")
		}
		h.call("demo", "PUT", path, map[string]any{"completed": false}, 200)
		if row := f.row(created.TaskID); row.Completed || row.CompletedAt != nil {
			t.Fatal("HTTP reopen did not clear timestamp")
		}
		if _, err := f.db.ExecContext(f.ctx, "INSERT INTO google_calendar_user_connection(user_name,refresh_token_encrypted) VALUES(?,?)", taskUser, taskCalendarMarker); err != nil {
			t.Fatal("cannot seed synthetic Calendar marker")
		}
		guarded := f.row(created.TaskID)
		h.error(h.call("demo", "POST", "/api/tasks", input, 409))
		h.error(h.call("demo", "PUT", path, map[string]any{"dueAt": "2026-11-01T12:00:00Z"}, 409))
		if !reflect.DeepEqual(guarded, f.row(created.TaskID)) || f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 1 {
			t.Fatal("Calendar HTTP refusal changed task data")
		}
	})
	t.Run("SQL write failures map to 503 without data loss", func(t *testing.T) {
		for _, stage := range []string{"create", "reschedule", "completion"} {
			t.Run(stage, func(t *testing.T) {
				f := openTaskFixture(t)
				var id int64
				var before tasks.Task
				if stage != "create" {
					id = f.create()
					before = f.row(id)
				}
				prefix := map[string]string{"create": "INSERT INTO company_tasks", "reschedule": "UPDATE company_tasks SET due_at", "completion": "UPDATE company_tasks SET completed"}[stage]
				repo, _, _ := f.faultRepo(prefix)
				h := openTaskHTTP(f, repo)
				method, path := "PUT", fmt.Sprintf("/api/tasks/%d", id)
				var payload any
				switch stage {
				case "create":
					method, path, payload = "POST", "/api/tasks", f.input()
				case "reschedule":
					payload = map[string]any{"dueAt": "2026-11-01T12:00:00Z"}
				case "completion":
					payload = map[string]any{"completed": true}
				}
				data := h.call("demo", method, path, payload, 503)
				h.error(data)
				if strings.Contains(string(data), errTaskWriteFault.Error()) {
					t.Fatal("task HTTP leaked an internal SQL failure")
				}
				if stage == "create" {
					if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 0 {
						t.Fatal("HTTP failed INSERT did not roll back")
					}
				} else if !reflect.DeepEqual(before, f.row(id)) {
					t.Fatal("HTTP failed UPDATE changed task data")
				}
				status := 200
				if stage == "create" {
					status = 201
				}
				h.call("demo", method, path, payload, status)
				if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 1 {
					t.Fatal("task retry after known rollback lost/duplicated rows")
				}
			})
		}
	})
	t.Run("completion and reschedule preserve both fields", testTaskConcurrentEdits)
}
