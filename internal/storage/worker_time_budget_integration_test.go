//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"encoding/json"
	"testing"
	"time"

	"itmap/internal/apifysettings"
	"itmap/internal/parser"
)

func TestMariaDBHHWorkerTimeBudgetDisposable(t *testing.T) {
	provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
	_, s, scan, listing := hhScanFixture(t, provider)
	var run string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := scan.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, run, "completed"); err != nil {
		t.Fatal(err)
	}
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime}
	result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_TIME_BUDGET", Duration: 110 * time.Second}, MaxJobs: 20, MinRemaining: 110 * time.Second})
	if err != nil || result.Processed != 1 || provider.starts != 1 {
		t.Fatal("cycle did not stop after first bounded job", result, err, provider.starts)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND attempts=0") != 1 {
		t.Fatal("next AI job was claimed or not planned")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE type='classify_title'") != 0 {
		t.Fatal("attempt created before sufficient time")
	}
}
