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

type schedulerContinueProbe struct {
	calls int
	err   error
}

func (p *schedulerContinueProbe) PrepareTargetContinuation(context.Context) (func(context.Context, string, parser.ScanCandidateScope) (json.RawMessage, error), error) {
	return func(_ context.Context, request string, scope parser.ScanCandidateScope) (json.RawMessage, error) {
		p.calls++
		if request != "12345678-1234-4234-8234-123456789abc" || scope.CompanyID != 123 || scope.RequestID != "12345678-1234-4234-8234-123456789abd" || scope.ScanJobID != "12345678-1234-4234-8234-123456789abe" {
			panic("wrong identifiers")
		}
		return json.RawMessage(`{"status":"completed"}`), p.err
	}, nil
}
func TestSchedulerTargetContinuationAuthorizationAndInput(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if err != nil {
		t.Fatal(err)
	}
	token := func(name string) string {
		user, _ := manager.Authenticate(name, "CI_pw")
		value, err := manager.Issue(user, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	admin, demo := token("eugene"), token("demo")
	probe := &schedulerContinueProbe{}
	valid := `{"requestId":"12345678-1234-4234-8234-123456789abc","companyId":123,"scanRequestId":"12345678-1234-4234-8234-123456789abd","scanJobId":"12345678-1234-4234-8234-123456789abe"}`
	call := func(cookie, origin, body string) int {
		r := httptest.NewRequest("POST", "https://itmap.invalid/api/scheduler/target-hh/continue", strings.NewReader(body))
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		SchedulerTargetContinuationHandler(manager, probe)(w, r)
		return w.Code
	}
	for _, tc := range []struct {
		cookie, origin, body string
		want                 int
	}{
		{"", "", valid, 401}, {demo, "", valid, 403}, {admin, "https://foreign.invalid", valid, 403},
		{admin, "", `{}`, 400}, {admin, "", `{"requestId":"bad","companyId":0}`, 400},
		{admin, "", `{"requestId":"12345678-1234-4234-8234-123456789abc","companyId":123,"companyId":124}`, 400},
		{admin, "", valid + `{}`, 400},
		{admin, "", strings.TrimSuffix(valid, "}") + `,"extra":true}`, 400},
	} {
		if got := call(tc.cookie, tc.origin, tc.body); got != tc.want {
			t.Fatalf("status %d want %d", got, tc.want)
		}
	}
	if probe.calls != 0 || call(admin, "https://itmap.invalid", valid) != 200 || probe.calls != 1 {
		t.Fatal("authorization or invocation failed")
	}
	probe.err = parser.ErrHHListingChanged
	if call(admin, "", valid) != 409 {
		t.Fatal("unavailable source not reported")
	}
}
