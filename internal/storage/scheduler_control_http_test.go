//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/parser"
)

func TestMariaDBSchedulerControlHTTPDisposable(t *testing.T) {
	for _, kind := range []string{"manual", "tick"} {
		t.Run(kind, func(t *testing.T) {
			provider := &hhScanProvider{rows: [][]json.RawMessage{{}}}
			_, s, lease, listing := hhScanFixture(t, provider)
			var oldRun string
			if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&oldRun); err != nil {
				t.Fatal(err)
			}
			if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "cancelled"}); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.Finish(s.ctx, oldRun, "completed"); err != nil {
				t.Fatal(err)
			}
			s.exec("DELETE FROM scheduler_jobs WHERE id='CI_HH_SCAN'")
			control := parser.NewCycleControlWithFactory(s.db, func(r parser.RunRepository) (parser.Worker, error) {
				return parser.Worker{Runs: r, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime}, nil
			})
			manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
			if err != nil {
				t.Fatal(err)
			}
			user, ok := manager.Authenticate("eugene", "CI_pw")
			if !ok {
				t.Fatal("synthetic login")
			}
			token, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, SchedulerCycle: control, SchedulerTick: control, SchedulerToken: "CI_TICK_TOKEN"})
			call := func() *httptest.ResponseRecorder {
				path, body := "/api/scheduler/run", `{"requestId":"12345678-1234-4234-8234-123456789abc"}`
				if kind == "tick" {
					path = "/api/internal/scheduler/tick"
					body = `{"requestId":"CI_TICK_REQUEST"}`
				}
				request := httptest.NewRequest("POST", "https://example.invalid"+path, strings.NewReader(body))
				if kind == "manual" {
					request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
				} else {
					request.Header.Set("Authorization", "Bearer CI_TICK_TOKEN")
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				return response
			}
			first := call()
			var result map[string]any
			if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &result) != nil || result["status"] != "completed" || provider.starts != 1 {
				t.Fatal("first HTTP cycle", first.Code, provider.starts)
			}
			if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='completed'") != 1 || s.count("SELECT COUNT(*) FROM scheduler_runs WHERE processed=1 AND succeeded=1 AND status='completed'") != 1 {
				t.Fatal("HTTP cycle receipts")
			}
			second := call()
			if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"reused":true`) || provider.starts != 1 {
				t.Fatal("HTTP replay", second.Code, provider.starts, fmt.Sprint(result["status"]))
			}
		})
	}
}

func TestMariaDBSchedulerConcurrentDisposable(t *testing.T) {
	provider := &hhScanProvider{rows: [][]json.RawMessage{{}}}
	_, s, lease, listing := hhScanFixture(t, provider)
	var oldRun string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&oldRun); err != nil {
		t.Fatal(err)
	}
	if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "cancelled"}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, oldRun, "completed"); err != nil {
		t.Fatal(err)
	}
	s.exec("DELETE FROM scheduler_jobs WHERE id='CI_HH_SCAN'")
	started, release := make(chan struct{}), make(chan struct{})
	provider.afterAccount = func() { close(started); <-release }
	control := parser.NewCycleControlWithFactory(s.db, func(r parser.RunRepository) (parser.Worker, error) {
		return parser.Worker{Runs: r, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime}, nil
	})
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("eugene", "CI_pw")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, SchedulerCycle: control})
	call := func(id string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/run", strings.NewReader(fmt.Sprintf(`{"requestId":%q}`, id)))
		request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- call("12345678-1234-4234-8234-123456789abc") }()
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		close(release)
		t.Fatal("first cycle not started")
	}
	second := call("12345678-1234-4234-8234-123456789abd")
	close(release)
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), `"scheduler_busy"`) {
		t.Fatal("parallel cycle accepted", second.Code)
	}
	select {
	case result := <-first:
		if result.Code != http.StatusOK || provider.starts != 1 {
			t.Fatal("first cycle", result.Code, provider.starts)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("first cycle did not finish")
	}
}

func TestMariaDBSchedulerFailureHTTPDisposable(t *testing.T) {
	provider := &hhScanProvider{rows: [][]json.RawMessage{{}}}
	_, s, lease, listing := hhScanFixture(t, provider)
	var oldRun string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&oldRun); err != nil {
		t.Fatal(err)
	}
	if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, oldRun, "completed"); err != nil {
		t.Fatal(err)
	}
	provider.afterAccount = func() {
		s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.enabled',false) WHERE id=1`)
	}
	control := parser.NewCycleControlWithFactory(s.db, func(r parser.RunRepository) (parser.Worker, error) {
		return parser.Worker{Runs: r, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime}, nil
	})
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("eugene", "CI_pw")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, SchedulerCycle: control})
	request := httptest.NewRequest("POST", "https://example.invalid/api/scheduler/run", strings.NewReader(`{"requestId":"12345678-1234-4234-8234-123456789abc"}`))
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result struct {
		Status string `json:"status"`
		Errors int    `json:"errors"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Status != "partial" || result.Errors != 1 || provider.starts != 0 {
		t.Fatal("failure receipt", response.Code, result, provider.starts)
	}
}
