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
