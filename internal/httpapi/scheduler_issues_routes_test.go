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

type issuesHTTPProbe struct {
	reads, samples int
	err            error
}

func (p *issuesHTTPProbe) ReadIssues(context.Context) (json.RawMessage, error) {
	p.reads++
	return json.RawMessage(`{"checkedAt":123,"issues":[],"historicalFailed":0,"sampleLimited":false,"limit":120,"activeCounts":{"p1":0,"p2":0},"history":{"observedDays":0}}`), p.err
}
func (p *issuesHTTPProbe) Sample(context.Context) error {
	p.samples++
	return p.err
}

func TestSchedulerIssuesHTTPAccessAndFailure(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("x", 64), map[string]string{
		"eugene": "CI_pw", "demo": "CI_pw", "nastya": "CI_pw",
	})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, name := range []string{"eugene", "demo", "nastya"} {
		user, _ := manager.Authenticate(name, "CI_pw")
		tokens[name], err = manager.Issue(user, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	p := &issuesHTTPProbe{}
	request := func(method, user, body, origin string, store SchedulerIssuesStore) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.invalid/api/scheduler/issues", strings.NewReader(body))
		if user != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[user]})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: manager, SchedulerIssues: store}).ServeHTTP(w, r)
		return w
	}
	for _, user := range []string{"", "demo", "nastya"} {
		want := 403
		if user == "" {
			want = 401
		}
		if got := request("GET", user, "", "", p).Code; got != want {
			t.Fatalf("user %q status %d != %d", user, got, want)
		}
	}
	if p.reads != 0 || p.samples != 0 {
		t.Fatal("unauthorized request accessed private history")
	}
	if w := request("GET", "eugene", "", "", nil); w.Code != 503 {
		t.Fatal("missing storage must fail closed")
	}
	if w := request("GET", "eugene", "", "", p); w.Code != 200 || p.reads != 1 || !strings.Contains(w.Body.String(), "\"observedDays\":0") || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("admin history report failed")
	}
	if p.samples != 0 {
		t.Fatal("GET history must never write a sample")
	}
	if got := request("POST", "eugene", "{}", "https://other.invalid", p).Code; got != 403 {
		t.Fatal("cross-site write was allowed")
	}
	if got := request("POST", "eugene", "{}", "", p).Code; got != 405 {
		t.Fatal("wrong method was allowed")
	}
	p.err = errors.New("CI_PRIVATE_SQL_DETAIL")
	w := request("GET", "eugene", "", "", p)
	if w.Code != 500 || strings.Contains(w.Body.String(), "CI_PRIVATE") || strings.Contains(w.Body.String(), "\"history\"") {
		t.Fatal("database read failure leaked details or returned a false zero")
	}
}

func TestSchedulerTickSamplesWithoutUIAndWithoutPaidSideEffects(t *testing.T) {
	worker, store := &schedulerTickProbe{}, &issuesHTTPProbe{}
	call := func(token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.invalid/api/internal/scheduler/tick", strings.NewReader(body))
		r.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{SchedulerTick: worker, SchedulerToken: "CI_TOKEN", SchedulerIssues: store}).ServeHTTP(w, r)
		return w
	}
	if w := call("", `{"requestId":"CI_REJECT"}`); w.Code != 401 || store.samples != 0 {
		t.Fatal("unauthorized tick sampled")
	}
	if w := call("Bearer CI_TOKEN", `{"diagnostic":true}`); w.Code != 200 || store.samples != 0 {
		t.Fatal("diagnostic tick must not create measurements")
	}
	if w := call("Bearer CI_TOKEN", `{"requestId":"CI_VALID"}`); w.Code != 200 || store.samples != 1 || store.reads != 0 {
		t.Fatal("authorized tick did not sample without UI")
	}
	store.err = errors.New("CI_PRIVATE_STORAGE")
	if w := call("Bearer CI_TOKEN", `{"requestId":"CI_AGAIN"}`); w.Code != 200 || store.samples != 2 {
		t.Fatal("snapshot failure must not block existing worker")
	}
}
