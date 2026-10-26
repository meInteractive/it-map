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

type rescheduleProbe struct {
	calls int
	user  string
	id    int64
	input string
	err   error
}

func (p *rescheduleProbe) Reschedule(_ context.Context, user string, id int64, at string) (string, error) {
	p.calls++
	p.user = user
	p.id = id
	p.input = at
	return "2026-10-24T11:45:00.000Z", p.err
}
func TestTaskRescheduleHTTPContract(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("demo", "demo-password")
	token, err := m.Issue(u, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := &rescheduleProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, TaskReschedule: p})
	call := func(body, origin, typ string, loggedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "http://example.test/api/tasks/7", strings.NewReader(body))
		r.Header.Set("Content-Type", typ)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if loggedIn {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	good := `{"dueAt":"2026-10-24T14:45:00+03:00"}`
	if w := call(good, "", "application/json", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, tc := range []struct {
		body, origin, ctype string
		status              int
	}{
		{good, "http://evil.test", "application/json", 403},
		{good, "", "text/plain", 400},
		{`{"dueAt":"2026-10-24T12:00:00+24:00"}`, "", "application/json", 400},
		{`{"dueAt":"2026-10-24T12:00:00+00:60"}`, "", "application/json", 400},
		{`{"dueAt":"2026-02-30T12:00:00Z"}`, "", "application/json", 400},
		{`{"dueAt":"2026-10-24T12:00Z"}`, "", "application/json", 400},
		{`{"dueAt":"2026-10-24T12:00:00.1234Z"}`, "", "application/json", 400},
		{`{"dueAt":"2026-10-24T12:00:00Z","completed":true}`, "", "application/json", 400},
		{`{"dueAt":"2026-10-24T12:00:00Z","dueAt":"2026-10-24T12:00:01Z"}`, "", "application/json", 400},
		{`{"dueAt":"2026-10-24T12:00:00Z"}junk`, "", "application/json", 400},
		{`{"completed":false}`, "", "application/json", 503},
		{`{"completed":null}`, "", "application/json", 400},
	} {
		w := call(tc.body, tc.origin, tc.ctype, true)
		if w.Code != tc.status || p.calls != 0 {
			t.Fatalf("%s -> %d (%d calls): %s", tc.body, w.Code, p.calls, w.Body.String())
		}
	}
	w := call(good, "http://example.test", "application/json", true)
	if w.Code != 200 || p.calls != 1 || p.user != "Demo" || p.id != 7 || p.input != "2026-10-24T14:45:00+03:00" || !strings.Contains(w.Body.String(), `"not_connected"`) {
		t.Fatalf("%d %s probe=%+v", w.Code, w.Body.String(), p)
	}
	p.err = tasks.ErrCalendarSyncRequired
	if w = call(good, "", "application/json", true); w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	p.err = tasks.ErrTaskNotFound
	if w = call(good, "", "application/json", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	p.err = errors.New("DB_PASSWORD=private")
	if w = call(good, "", "application/json", true); w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
}
