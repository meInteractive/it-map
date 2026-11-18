//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
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
	// Заведомо недостаточное время не должно позволять даже первый claim.
	// Duration уже начал отсчитываться до проверки: remaining строго меньше minimum.
	late, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_TIME_BUDGET_LATE", Duration: 110 * time.Second}, MaxJobs: 20, MinRemaining: 110 * time.Second})
	if err != nil || late.Processed != 0 || provider.starts != 0 {
		t.Fatal("late first claim started work", late, err, provider.starts)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending'") == 0 {
		t.Fatal("job was not preserved for the next cycle")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE type='classify_title'") != 0 {
		t.Fatal("late cycle created AI attempt")
	}
	// Следующий цикл с достаточным бюджетом продолжает работу без потери задания.
	result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_TIME_BUDGET_FRESH", Duration: 110 * time.Second}, MaxJobs: 1, MinRemaining: 60 * time.Second})
	if err != nil || result.Processed != 1 || provider.starts != 1 {
		t.Fatal("fresh cycle did not resume the first job", result, err, provider.starts)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND attempts=0") != 1 {
		t.Fatal("next AI job was claimed or not planned")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE type='classify_title'") != 0 {
		t.Fatal("AI attempt created before sufficient time")
	}
}

// Короткий внешний deadline не допускает даже первый pipeline claim,
// хотя MinRemaining здесь не установлен: интервал нужен для bounded cleanup.
func TestMariaDBHHWorkerPipelineCleanupAdmissionDisposable(t *testing.T) {
	provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(124, "CI_TITLE")}}}
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
	first, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_CLEANUP_PREP", Duration: 110 * time.Second}, MaxJobs: 1})
	if err != nil || first.Processed != 1 || provider.starts != 1 {
		t.Fatal("synthetic scan did not prepare AI job", first, err, provider.starts)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND attempts=0") != 1 {
		t.Fatal("synthetic AI job not queued")
	}
	short, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	late, err := w.Cycle(short, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_CLEANUP_LATE", Duration: 100 * time.Second}, MaxJobs: 1, QueueProfile: "pipeline"})
	if err != nil || late.Processed != 0 || late.Run.Status != "completed" {
		t.Fatal("short pipeline cycle claimed work without cleanup budget", late, err)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE type='classify_title'") != 0 ||
		s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND attempts=0") != 1 ||
		provider.starts != 1 {
		t.Fatal("short pipeline cycle created an attempt, lost a job, or repeated POST")
	}
}

// GO-431 P0: the 60-second rule must be checked AFTER the SQL claim.
// We hold the synthetic job row only in disposable CI long enough for a
// pre-claim minimum to expire. No AI attempt or provider call is permitted.
func TestMariaDBHHWorkerPostClaimFenceDisposable(t *testing.T) {
	f := billingFixture(t)
	billingReset(f)
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"sourceIds":["hh"],"ownerName":"CI_USER","dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	const id = "CI_POST_CLAIM_AI"
	schedulerSeedJob(f, id, nil)
	f.exec("UPDATE scheduler_jobs SET type='classify_title',source_id='hh',payload='{}' WHERE id=?", id)
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal("cannot hold synthetic pending AI row", err)
	}
	defer tx.Rollback()
	var held string
	if err := tx.QueryRowContext(f.ctx, "SELECT id FROM scheduler_jobs WHERE id=? FOR UPDATE", id).Scan(&held); err != nil || held != id {
		t.Fatal("synthetic claim row lock failed", err)
	}
	worker := parser.Worker{Runs: f.repo, Settings: apifysettings.NewRepository(f.db, "CI_SECRET")}
	type cycleResult struct {
		result parser.WorkerResult
		err    error
	}
	done := make(chan cycleResult, 1)
	go func() {
		result, err := worker.Cycle(f.ctx, parser.WorkerOptions{
			Run: parser.RunOptions{RequestID: "CI_POST_CLAIM_FENCE_RUN", Duration: 30 * time.Second},
			MaxJobs: 1, QueueProfile: "pipeline", MinRemaining: 29 * time.Second,
		})
		done <- cycleResult{result, err}
	}()
	// Wait for the owned scheduler run before releasing the job-row lock;
	// a failed run or stalled initialization must never hang the test.
	owned := false
	for i := 0; i < 30; i++ {
		if f.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='running'") == 1 {
			owned = true
			break
		}
		select {
		case <-done:
			t.Fatal("worker finished before synthetic claim contention")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !owned {
		t.Fatal("synthetic owned run did not begin")
	}
	// This leaves <29 s from the 30-second cycle at the moment the held
	// job can be claimed, while the pre-claim check has already passed.
	time.Sleep(2 * time.Second)
	if err := tx.Commit(); err != nil {
		t.Fatal("cannot release synthetic lock", err)
	}
	select {
	case outcome := <-done:
		if outcome.err != nil || outcome.result.Processed != 0 || outcome.result.Run.Status != "completed" {
			t.Fatal("late claimed job was executed or run left open", outcome)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("worker did not release late claimed job after SQL contention")
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='pending' AND attempts=0 AND reason_code='deferred' AND locked_by='' AND locked_until=0", id) != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=?", id) != 0 {
		t.Fatal("post-claim budget did not defer job without AI attempt")
	}
}
