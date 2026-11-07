package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
)

type aiSpendingHTTPProbe struct {
	calls int
	fail error
}

func (p *aiSpendingHTTPProbe) ReadAISpending(_ context.Context) (json.RawMessage, error) {
	p.calls++
	if p.fail != nil {
		return nil, p.fail
	}
	return json.RawMessage(`{"month":{"confirmedUsd":0.5},"latest":[],"total":0}`), nil
}

func TestAISpendingDebugHTTPAdminOnlyReadAndErrors(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("x", 64), map[string]string{
		"eugene": "CI_pw", "demo": "CI_pw", "nastya": "CI_pw",
	})
	if err != nil { t.Fatal(err) }
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "demo", "nastya"} {
		user, _ := manager.Authenticate(login, "CI_pw")
		tokens[login], err = manager.Issue(user, time.Now())
		if err != nil { t.Fatal(err) }
	}
	probe := &aiSpendingHTTPProbe{}
	call := func(method, user, url string, store SchedulerAISpendingReader) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.invalid"+url, nil)
		if user != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[user]})
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: manager, SchedulerAISpending: store}).ServeHTTP(w, r)
		return w
	}
	for _, user := range []string{"", "demo", "nastya"} {
		want := http.StatusForbidden
		if user == "" { want = http.StatusUnauthorized }
		if w := call("GET", user, "/api/scheduler/ai-spending/debug", probe); w.Code != want {
			t.Fatalf("%q got %d want %d", user, w.Code, want)
		}
	}
	if probe.calls != 0 { t.Fatal("unauthorized access reached storage") }
	if w := call("GET", "eugene", "/api/scheduler/ai-spending/debug", nil); w.Code != 503 {
		t.Fatal("nil storage must fail closed")
	}
	if w := call("POST", "eugene", "/api/scheduler/ai-spending/debug", probe); w.Code != 405 {
		t.Fatal("write method accepted")
	}
	if w := call("GET", "eugene", "/api/scheduler/ai-spending/debug?profile=all", probe); w.Code != 400 {
		t.Fatal("unreviewed query parameters accepted")
	}
	if probe.calls != 0 { t.Fatal("method validation reached storage") }
	w := call("GET", "eugene", "/api/scheduler/ai-spending/debug", probe)
	if w.Code != 200 || probe.calls != 1 || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("safe read failed",w.Code,w.Body.String())
	}
	probe.fail = errors.New("PRIVATE_SQL_FAILURE")
	w = call("GET", "eugene", "/api/scheduler/ai-spending/debug", probe)
	if w.Code != 500 || strings.Contains(w.Body.String(),"PRIVATE_SQL_FAILURE") || strings.Contains(w.Body.String(),"\"confirmedUsd\"") {
		t.Fatal("storage failure not private")
	}
}
