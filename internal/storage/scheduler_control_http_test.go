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
