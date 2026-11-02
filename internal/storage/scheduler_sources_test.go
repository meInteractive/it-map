//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"encoding/json"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/parser"
)

func TestMariaDBSchedulerSourcesDisposable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		favorites bool
		shortened bool
		want      int
	}{{"eligible source", false, false, 1}, {"outside favorites", true, false, 0}, {"shortened interval", false, true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &hhScanProvider{rows: [][]json.RawMessage{{}}}
			_, s, lease, listing := hhScanFixture(t, provider)
			var run string
			if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
				t.Fatal(err)
			}
			if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "cancelled"}); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.Finish(s.ctx, run, "completed"); err != nil {
				t.Fatal(err)
			}
			s.exec("DELETE FROM scheduler_jobs WHERE id='CI_HH_SCAN'")
			if tc.favorites {
				s.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
			}
			if tc.shortened {
				s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.intervalDays',1) WHERE id=1`)
				s.exec(`UPDATE vacancy_source_parsing SET interval_days=7,last_success_at='2000-01-01T00:00:00.000Z',next_run_at=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)+432000000 WHERE company_id=991302 AND source_id='hh'`)
			}
			w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime, ScheduleSources: true}
			first, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_SOURCE_TICK_1"}, MaxJobs: 20})
			if err != nil || first.Processed != tc.want || provider.starts != tc.want {
				t.Fatal("first source tick", first, provider.starts, err)
			}
			if s.count("SELECT COUNT(*) FROM scheduler_runs WHERE id=? AND processed=? AND succeeded=?", first.Run.ID, tc.want, tc.want) != 1 {
				t.Fatal("cycle counters missing")
			}
			if tc.want == 1 && s.count("SELECT COUNT(*) FROM vacancy_source_parsing WHERE company_id=991302 AND source_id='hh' AND next_run_at>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) AND last_result='success'") != 1 {
				t.Fatal("next source run not recorded")
			}
			second, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_SOURCE_TICK_2"}, MaxJobs: 20})
			if err != nil || second.Processed != 0 || provider.starts != tc.want {
				t.Fatal("source duplicate", second, provider.starts, err)
			}
		})
	}
}

func TestMariaDBSchedulerStopDisposable(t *testing.T) {
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
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime, ScheduleSources: true, ContinueOnStageFailure: true}
	result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_STOP_DURING_ACCOUNT"}, MaxJobs: 20})
	if err != nil || result.Run.Status != "partial" || provider.starts != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
		t.Fatal("disabled scheduler started actor", err, provider.starts)
	}
}
