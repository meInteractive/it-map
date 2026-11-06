package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type schedulerTickProbe struct {
	calls      int
	id         string
	diagnostic bool
}

func (p *schedulerTickProbe) PrepareTick(context.Context) (func(context.Context, string, bool) (json.RawMessage, error), error) {
	return func(_ context.Context, id string, diagnostic bool) (json.RawMessage, error) {
		p.calls++
		p.id = id
		p.diagnostic = diagnostic
		return json.RawMessage(`{"status":"completed"}`), nil
	}, nil
}
func TestSchedulerTickHTTPAuthorization(t *testing.T) {
	p := &schedulerTickProbe{}
	call := func(token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.invalid/api/internal/scheduler/tick", strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", token)
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{SchedulerTick: p, SchedulerToken: "CI_TOKEN"}).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		token, body string
		status      int
	}{
		{"", `{}`, 401}, {"Bearer bad", `{}`, 401}, {"Bearer CI_TOKEN", `{"unknown":1}`, 400}, {"Bearer CI_TOKEN", `{"requestId":12}`, 400}, {"Bearer CI_TOKEN", `{"requestId":null}`, 400}, {"Bearer CI_TOKEN", `{"diagnostic":null}`, 400},
	} {
		if w := call(tc.token, tc.body); w.Code != tc.status {
			t.Fatalf("tick rejected status %d want %d", w.Code, tc.status)
		}
	}
	if p.calls != 0 {
		t.Fatal("rejected tick started worker")
	}
	w := call("Bearer CI_TOKEN", `{"requestId":"CI_TICK"}`)
	if w.Code != 200 || p.calls != 1 || p.id != "CI_TICK" {
		t.Fatal("authorized tick", w.Code, p.calls)
	}
	w = call("Bearer CI_TOKEN", "\ufeff"+`{"requestId":"CI_BOM"}`)
	if w.Code != 200 || p.calls != 2 {
		t.Fatal("BOM tick", w.Code)
	}
	w = call("Bearer CI_TOKEN", `{"diagnostic":true}`)
	if w.Code != 200 || p.calls != 3 || !p.diagnostic {
		t.Fatal("diagnostic tick", w.Code, p.calls)
	}
}

type schedulerTickProfilesProbe struct {
	schedulerTickProbe
	profileCalls int
	profile, requestID string
}

func (p *schedulerTickProfilesProbe) PrepareProfileTick(context.Context) (func(context.Context, string, string) (json.RawMessage, error), error) {
	return func(_ context.Context, id, profile string) (json.RawMessage, error) {
		p.profileCalls++
		p.requestID, p.profile = id, profile
		return json.RawMessage(`{"status":"completed","processed":0,"errors":0}`), nil
	}, nil
}

func TestSchedulerTickIndependentProfileHTTP(t *testing.T) {
	p := &schedulerTickProfilesProbe{}
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.invalid/api/internal/scheduler/tick", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer CI_TOKEN")
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{SchedulerTick: p, SchedulerToken: "CI_TOKEN"}).ServeHTTP(w, r)
		return w
	}
	for _, bad := range []string{
		`{"profile":"launch"}`,
		`{"requestId":"CI","profile":"unknown"}`,
		`{"requestId":"CI","profile":null}`,
		`{"requestId":"CI","profile":"launch","diagnostic":true}`,
	} {
		if w := call(bad); w.Code != 400 || p.profileCalls != 0 || p.calls != 0 {
			t.Fatalf("invalid profile reached worker: %s (%d)", bad, w.Code)
		}
	}
	for _, profile := range []string{"launch", "reconcile", "pipeline"} {
		if w := call(`{"requestId":"CI_SCHEDULE","profile":"`+profile+`"}`); w.Code != 200 || p.profileCalls == 0 || p.profile != profile || p.requestID != "CI_SCHEDULE" || p.calls != 0 {
			t.Fatalf("profile %s not dispatched safely", profile)
		}
	}
	if w := call(`{"requestId":"CI_DEFAULT"}`); w.Code != 200 || p.calls != 1 {
		t.Fatal("legacy tick changed", w.Code)
	}
}
