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
	w = call("Bearer CI_TOKEN", `{"diagnostic":true}`)
	if w.Code != 200 || p.calls != 2 || !p.diagnostic {
		t.Fatal("diagnostic tick", w.Code, p.calls)
	}
}
