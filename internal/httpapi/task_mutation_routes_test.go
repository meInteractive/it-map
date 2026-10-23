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

type completionProbe struct {
	calls     int
	user      string
	id        int64
	completed bool
	err       error
}

func (p *completionProbe) SetCompleted(_ context.Context, user string, id int64, complete bool) error {
	p.calls++
	p.user = user
	p.id = id
	p.completed = complete
	return p.err
}
func TestTaskCompletionContract(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("demo", "demo-password")
	token, err := m.Issue(u, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := &completionProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, TaskCompletion: p})
	call := func(id, body, origin, site, contentType string, loggedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PUT", "http://example.test/api/tasks/"+id, strings.NewReader(body))
		if loggedIn {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("9", `{"completed":true}`, "", "", "application/json", false); w.Code != 401 || p.calls != 0 {
		t.Fatal(w.Code, p.calls)
	}
	for _, tc := range []struct {
		id, body, origin, site, ctype string
		code                          int
	}{
		{"0", `{"completed":true}`, "", "", "application/json", 400},
		{"xx", `{"completed":true}`, "", "", "application/json", 400},
		{"9", `{"completed":true}`, "http://attacker.test", "", "application/json", 403},
		{"9", `{"completed":true}`, "", "same-site", "application/json", 403},
		{"9", `{"completed":true,"dueAt":"2026-10-24"}`, "", "", "application/json", 400},
		{"9", `{"completed":true,"completed":false}`, "", "", "application/json", 400},
		{"9", `{"completed":1}`, "", "", "application/json", 400},
		{"9", `{"completed":"true"}`, "", "", "application/json", 400},
		{"9", `{"completed":true}[]`, "", "", "application/json", 400},
		{"9", `{"completed":true}`, "", "", "text/plain", 400},
		{"9", strings.Repeat("a", 5000), "", "", "application/json", 400},
	} {
		if w := call(tc.id, tc.body, tc.origin, tc.site, tc.ctype, true); w.Code != tc.code || p.calls != 0 {
			t.Fatalf("%s returned %d (%d calls)", tc.body, w.Code, p.calls)
		}
	}
	if w := call("9", `{"completed":true}`, "http://example.test", "same-origin", "application/json", true); w.Code != 200 || p.calls != 1 || p.user != "Demo" || p.id != 9 || !p.completed {
		t.Fatalf("%d %s %+v", w.Code, w.Body.String(), p)
	}
	p.err = tasks.ErrTaskNotFound
	if w := call("9", `{"completed":false}`, "", "", "application/json", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	p.err = errors.New("database secret dsns")
	if w := call("9", `{"completed":false}`, "", "", "application/json", true); w.Code != 503 || strings.Contains(w.Body.String(), "database secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := httptest.NewRecorder(); w != nil {
		r := httptest.NewRequest("PUT", "/api/tasks/9", strings.NewReader(`{"dueAt":"2026-10-23T12:00:00Z"}`))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
