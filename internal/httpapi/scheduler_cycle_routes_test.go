package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type schedulerCycleProbe struct {
	calls     int
	requestID string
	err       error
}

func (p *schedulerCycleProbe) PrepareCycle(context.Context) (func(context.Context, string) (json.RawMessage, error), error) {
	return func(_ context.Context, id string) (json.RawMessage, error) {
		p.calls++
		p.requestID = id
		if p.err != nil {
			return nil, p.err
		}
		return json.RawMessage(`{"status":"completed"}`), nil
	}, nil
}
func TestSchedulerCycleHTTPAuthorization(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if err != nil {
		t.Fatal(err)
	}
	token := func(name string) string {
		u, ok := m.Authenticate(name, "CI_pw")
		if !ok {
			t.Fatal("synthetic login")
		}
		v, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	admin, demo := token("eugene"), token("demo")
	p := &schedulerCycleProbe{}
	call := func(cookie, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/run", strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(nil, Options{Auth: m, SchedulerCycle: p}).ServeHTTP(w, r)
		return w
	}
	valid := `{"requestId":"12345678-1234-4234-8234-123456789abc"}`
	for _, tc := range []struct {
		cookie, origin, body string
		status               int
	}{
		{"", "", valid, 401}, {demo, "", valid, 403}, {admin, "https://other.invalid", valid, 403},
		{admin, "", `{"requestId":"invalid"}`, 400}, {admin, "", `{"requestId":"12345678-1234-4234-8234-123456789abc","ownerName":"other"}`, 400},
	} {
		if w := call(tc.cookie, tc.origin, tc.body); w.Code != tc.status {
			t.Fatalf("rejected request status %d want %d", w.Code, tc.status)
		}
	}
	if p.calls != 0 {
		t.Fatal("rejected request started worker")
	}
	w := call(admin, "https://example.invalid", valid)
	if w.Code != 200 || p.calls != 1 || p.requestID != "12345678-1234-4234-8234-123456789abc" || !strings.Contains(w.Body.String(), `"status":"completed"`) {
		t.Fatal("authorized cycle", w.Code, p.calls)
	}
	w = call(admin, "", "\ufeff"+valid)
	if w.Code != 200 || p.calls != 2 {
		t.Fatal("BOM cycle", w.Code)
	}
	p.err = parser.ErrRunBusy
	w = call(admin, "", valid)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"scheduler_busy"`) {
		t.Fatal("busy run", w.Code)
	}
}

func TestSchedulerCycleStreamContract(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	user, _ := m.Authenticate("eugene", "CI_pw")
	token, e := m.Issue(user, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	p := &schedulerCycleProbe{}
	request := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/run", strings.NewReader(`{"requestId":"12345678-1234-4234-8234-123456789abc"}`))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	request.Header.Set("Accept", "application/x-ndjson")
	response := httptest.NewRecorder()
	NewHandlerWithOptions(nil, Options{Auth: m, SchedulerCycle: p}).ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Header().Get("Content-Type"), "application/x-ndjson") || !strings.Contains(response.Body.String(), `"type":"progress"`) || !strings.Contains(response.Body.String(), `"type":"done"`) {
		t.Fatal("stream contract", response.Code, response.Header().Get("Content-Type"))
	}
}

type schedulerBlockingCycle struct {
	started chan struct{}
	stopped chan struct{}
}

func (p schedulerBlockingCycle) PrepareCycle(context.Context) (func(context.Context, string) (json.RawMessage, error), error) {
	return func(ctx context.Context, _ string) (json.RawMessage, error) {
		close(p.started)
		<-ctx.Done()
		close(p.stopped)
		return nil, ctx.Err()
	}, nil
}
func TestSchedulerCycleStreamDisconnectCancelsWork(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	user, _ := m.Authenticate("eugene", "CI_pw")
	token, e := m.Issue(user, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := schedulerBlockingCycle{started: make(chan struct{}), stopped: make(chan struct{})}
	request := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/run", strings.NewReader(`{"requestId":"12345678-1234-4234-8234-123456789abc"}`)).WithContext(ctx)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	request.Header.Set("Accept", "application/x-ndjson")
	response := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		NewHandlerWithOptions(nil, Options{Auth: m, SchedulerCycle: p}).ServeHTTP(response, request)
		close(finished)
	}()
	select {
	case <-p.started:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case <-p.stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not cancel worker")
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("stream handler did not stop")
	}
}

type schedulerPanicCycle struct{}

func (schedulerPanicCycle) PrepareCycle(context.Context) (func(context.Context, string) (json.RawMessage, error), error) {
	return func(context.Context, string) (json.RawMessage, error) { panic("CI_PRIVATE_ERROR") }, nil
}
func TestSchedulerCycleStreamPanicIsRedacted(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	user, _ := m.Authenticate("eugene", "CI_pw")
	token, e := m.Issue(user, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/run", strings.NewReader(`{"requestId":"12345678-1234-4234-8234-123456789abc"}`))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	request.Header.Set("Accept", "application/x-ndjson")
	response := httptest.NewRecorder()
	NewHandlerWithOptions(nil, Options{Auth: m, SchedulerCycle: schedulerPanicCycle{}}).ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), `"type":"error"`) || strings.Contains(response.Body.String(), "CI_PRIVATE_ERROR") {
		t.Fatal("panic stream not safely reported")
	}
}
