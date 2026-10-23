package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/tasks"
)

type taskProbe struct {
	calls            int
	user, company    string
	includeCompleted bool
	id               *int64
	err              error
}

func (p *taskProbe) List(_ context.Context, user, company string, all bool, id *int64) (tasks.Response, error) {
	p.calls++
	p.user = user
	p.company = company
	p.includeCompleted = all
	p.id = id
	if p.err != nil {
		return tasks.Response{}, p.err
	}
	return tasks.Response{Tasks: []tasks.Task{{ID: 3, CompanyName: "Example", DueAt: "2026-10-25T10:00:00.000Z"}}, Entities: []tasks.Entity{}}, nil
}
func TestTaskListAuthFiltersAndErrorMapping(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-secret", "demo": "demo-pass"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("demo", "demo-pass")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	probe := &taskProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, Tasks: probe})
	call := func(method, path string, loggedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if loggedIn {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/api/tasks", false); w.Code != 401 || probe.calls != 0 {
		t.Fatal("task data not protected")
	}
	for _, url := range []string{"/api/tasks?vacancy=abc", "/api/tasks?vacancy=0", "/api/tasks?vacancy=9223372036854775808"} {
		if w := call("GET", url, true); w.Code != 400 || probe.calls != 0 {
			t.Fatalf("invalid filter %s returned %d", url, w.Code)
		}
	}
	w := call("GET", "/api/tasks?company=Example&vacancy=9&includeCompleted=1", true)
	if w.Code != 200 || probe.calls != 1 || probe.user != "Demo" || probe.company != "Example" || !probe.includeCompleted || probe.id == nil || *probe.id != 9 {
		t.Fatalf("valid read %d %+v", w.Code, probe)
	}
	if !strings.Contains(w.Body.String(), `"tasks"`) {
		t.Fatalf("unexpected response %s", w.Body.String())
	}
	probe.err = tasks.ErrCompanyNotFound
	if w = call("GET", "/api/tasks?company=Unknown", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	probe.err = errors.New("mysql://secret")
	if w = call("GET", "/api/tasks", true); w.Code != 503 || strings.Contains(w.Body.String(), "mysql://") {
		t.Fatalf("leaked DB failure %s", w.Body.String())
	}
	if w = call("POST", "/api/tasks", true); w.Code != 503 {
		t.Fatalf("unimplemented task mutation returned %d", w.Code)
	}
}
