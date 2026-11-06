//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"encoding/json"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/parser"
)

// Проверяет цепочку: один оплаченный POST, затем GET результата в отдельном
// режиме и handoff в кандидаты. Все обращения — к mock Actor и disposable DB.
func TestMariaDBHHSeparatedPhasesReuseActor(t *testing.T) {
	p := &hhScanProvider{
		rows: [][]json.RawMessage{{hhScanItem(123, "CI old HH candidate")}},
		startPending: true,
		pollSuccess: true,
		requireMoscow: true,
	}
	f, s, lease, listing := hhScanFixture(t, p)
	var originalRun string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&originalRun); err != nil {
		t.Fatal(err)
	}
	if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, originalRun, "completed"); err != nil {
		t.Fatal(err)
	}
	w := parser.Worker{
		Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"),
		ListingRuntime: listing.Runtime, ScheduleSources: true,
		VerifiedHHOnly: true, ContinueOnStageFailure: true,
	}
	started, err := w.Cycle(f.ctx, parser.WorkerOptions{
		Run: parser.RunOptions{RequestID: "CI_GO430_START"}, QueueProfile: "launch", MaxJobs: 50,
	})
	if err != nil || started.Processed != 1 || p.starts != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending'") != 1 {
		t.Fatal("launch phase did not defer existing Actor", started, err, p.starts)
	}
	if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 0 {
		t.Fatal("running Actor unexpectedly published candidates")
	}
	// Моделируем наступление срока deferred без ожидания реального времени.
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE id='CI_HH_SCAN'")
	w.ScheduleSources = false
	reconciled, err := w.Cycle(f.ctx, parser.WorkerOptions{
		Run: parser.RunOptions{RequestID: "CI_GO430_RECONCILE"}, QueueProfile: "reconcile", MaxJobs: 50,
	})
	if err != nil || reconciled.Processed != 1 || p.starts != 1 {
		t.Fatal("existing paid Actor was not reconciled exactly once", reconciled, err, p.starts)
	}
	if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE company_id=991302") != 1 ||
		s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending'") != 1 ||
		s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='completed'") != 1 {
		t.Fatal("GET-only phase failed to save candidate and schedule JEV")
	}
	if s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
		t.Fatal("reconciliation created a second paid operation")
	}
	replay, err := w.Cycle(f.ctx, parser.WorkerOptions{
		Run: parser.RunOptions{RequestID: "CI_GO430_RECONCILE"}, QueueProfile: "reconcile", MaxJobs: 50,
	})
	if err != nil || !replay.Reused || p.starts != 1 {
		t.Fatal("replayed phase performed work", err, replay)
	}
}
