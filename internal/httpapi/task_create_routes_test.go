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

type createTaskProbe struct {
	calls int
	user  string
	data  tasks.CreateInput
	err   error
}

func (p *createTaskProbe) Create(_ context.Context, user string, data tasks.CreateInput) (int64, error) {
	p.calls++
	p.user = user
	p.data = data
	return 42, p.err
}
func TestTaskCreateHTTPContract(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-password", "demo": "demo-password"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := m.Authenticate("demo", "demo-password")
	token, err := m.Issue(u, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	p := &createTaskProbe{}
	h := NewHandlerWithOptions(nil, Options{Auth: m, TaskCreates: p})
	call := func(payload, origin, ctype string, loggedIn bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://example.test/api/tasks", strings.NewReader(payload))
		r.Header.Set("Content-Type", ctype)
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
	good := `{"companyName":"Example","vacancyId":7,"type":"call","note":"Follow up","dueAt":"2026-10-24T14:45:00+03:00"}`
	if w := call(good, "", "application/json", false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	for _, tc := range []struct {
		payload, origin, ctype string
		code                   int
	}{
		{good, "https://attacker.test", "application/json", 403},
		{good, "", "text/plain", 400},
		{`{"companyName":"Example","companyName":"Evil","type":"call","dueAt":"2026-10-24T12:00:00Z"}`, "", "application/json", 400},
		{`{"companyName":"Example","type":"call","dueAt":"2026-10-24T12:00:00Z","isAdmin":true}`, "", "application/json", 400},
		{`{"companyName":"Example","type":"call","dueAt":"bad"}`, "", "application/json", 400},
		{`{"companyName":"Example","type":"call","dueAt":"2026-02-30T12:00:00Z"}`, "", "application/json", 400},
		{`{"companyName":"Example","type":"call","dueAt":"2026-10-24T12:00:00Z"}[]`, "", "application/json", 400},
	} {
		w := call(tc.payload, tc.origin, tc.ctype, true)
		if w.Code != tc.code || p.calls != 0 {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), p)
		}
	}
	if w := call(good, "http://example.test", "application/json", true); w.Code != 201 || p.calls != 1 || p.user != "Demo" || p.data.CompanyName != "Example" || !strings.Contains(w.Body.String(), `"taskId":42`) {
		t.Fatal(w.Code, w.Body.String(), p)
	}
	p.err = tasks.ErrCalendarSyncRequired
	if w := call(good, "", "application/json", true); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	p.err = tasks.ErrCompanyNotFound
	if w := call(good, "", "application/json", true); w.Code != 404 {
		t.Fatal(w.Code)
	}
	p.err = errors.New("secret=never-print")
	if w := call(good, "", "application/json", true); w.Code != 503 || strings.Contains(w.Body.String(), "never-print") {
		t.Fatal(w.Code, w.Body.String())
	}
}
