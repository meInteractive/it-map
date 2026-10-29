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
	"itmap/internal/parser"
)

type schedulerSettingsProbe struct {
	calls int
	owner string
	err   error
}

func (p *schedulerSettingsProbe) Save(_ context.Context, _ json.RawMessage, owner string) (json.RawMessage, error) {
	p.calls++
	p.owner = owner
	return json.RawMessage(`{"enabled":true,"revision":8,"cloudConnected":true}`), p.err
}

func (p *schedulerSettingsProbe) PrepareSave(_ context.Context, owner string) (func(context.Context, json.RawMessage) (json.RawMessage, error), error) {
	return func(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
		return p.Save(ctx, body, owner)
	}, nil
}

func TestSchedulerSettingsHTTPBoundary(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "nastya", "demo"} {
		u, _ := m.Authenticate(login, "CI_pw")
		tokens[login], e = m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
	}
	const valid = `{"enabled":true,"revision":7,"dayBudgetUsd":0,"monthBudgetUsd":0}`
	call := func(store SchedulerSettingsWriter, user, method, path, body, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.invalid"+path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		if user != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tokens[user]})
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: m, SchedulerSettings: store}).ServeHTTP(w, r)
		return w
	}
	p := &schedulerSettingsProbe{}
	for _, c := range []struct {
		user, method, body, origin string
		status                     int
	}{
		{"", "PUT", "{", "https://other.invalid", 401},
		{"demo", "PUT", "{", "https://other.invalid", 403},
		{"nastya", "PUT", valid, "", 403},
		{"eugene", "PUT", "{", "https://other.invalid", 403},
		{"eugene", "PUT", "", "", 500},
		{"eugene", "PUT", "[]", "", 500},
		{"eugene", "PUT", "\v" + valid, "", 500},
		{"eugene", "PUT", valid + "{}", "", 500},
		{"eugene", "PUT", strings.Repeat("x", 32769), "", 413},
		{"eugene", "POST", "{}", "", 405},
		{"eugene", "GET", "", "", 500},
	} {
		if w := call(p, c.user, c.method, "/api/scheduler/settings", c.body, c.origin); w.Code != c.status {
			t.Fatalf("guard/body status=%d wanted=%d", w.Code, c.status)
		}
	}
	if p.calls != 0 {
		t.Fatal("rejected HTTP request reached SQL writer")
	}
	if w := call(nil, "eugene", "PUT", "/api/scheduler/settings", valid, "https://other.invalid"); w.Code != 503 || !strings.Contains(w.Body.String(), "Хранилище не подключено") {
		t.Fatal("storage must precede Origin")
	}
	for _, body := range []string{valid, "\ufeff" + valid, valid + strings.Repeat(" ", 32768-len(valid))} {
		w := call(p, "eugene", "PUT", "/api/scheduler/settings", body, "https://example.invalid")
		if w.Code != 200 || p.owner != "Eugene" || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("authorized settings update/public no-store DTO failed")
		}
	}
	for _, c := range []struct {
		e      error
		status int
		code   string
	}{
		{&parser.SettingsError{Message: "CI source validation"}, 400, "input"},
		{parser.ErrSettingsChanged, 409, "changed"},
		{parser.ErrSettingsGeneration, 409, "restored"},
		{errors.New("CI_PRIVATE_SQL_DETAILS"), 500, "storage"},
	} {
		p.err = c.e
		w := call(p, "eugene", "PUT", "/api/scheduler/settings", valid, "")
		if w.Code != c.status || !strings.Contains(w.Body.String(), `"code":"`+c.code+`"`) || strings.Contains(w.Body.String(), "CI_PRIVATE_SQL_DETAILS") {
			t.Fatal("source settings error mapping/private failure differs")
		}
	}
	for _, path := range []string{"/api/scheduler", "/api/scheduler/run", "/api/scheduler/probe", "/api/internal/scheduler/tick", "/api/scheduler/cloud-owner"} {
		if w := call(p, "eugene", "POST", path, "{}", ""); w.Code != 503 {
			t.Fatal("unported execution endpoint returned implicit success")
		}
	}
}
