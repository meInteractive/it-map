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
