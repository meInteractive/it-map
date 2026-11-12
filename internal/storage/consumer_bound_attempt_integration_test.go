//go:build mariadb && schedulerwrites

package storage

import (
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/parser"
)

// Этот тест вызывается только в одноразовой MariaDB CI.
func testBoundAttemptLifecycle(t *testing.T, f *schedulerFixture, req consumercontrol.Request) {
	t.Helper()
	run, bound, err := f.repo.BeginBoundRun(f.ctx, parser.RunOptions{RequestID: "CI_bound_attempt_run", Diagnostic: true, Duration: time.Second}, req)
	if err != nil || bound == nil || !run.Acquired {
		t.Fatal("bound run", err)
	}
	defer f.exec("DELETE FROM scheduler_runs WHERE id=?", run.Run.ID)
	jobID := "CI_bound_attempt_job"
	schedulerSeedJob(f, jobID, nil)
	defer f.exec("DELETE FROM scheduler_jobs WHERE id=?", jobID)
	defer f.exec("DELETE FROM scheduler_attempts WHERE job_id=?", jobID)
	lease, err := bound.ClaimJob(f.ctx, parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: 10 * time.Second})
	if err != nil || lease == nil {
		t.Fatal("claim", err)
	}
	attempt, err := bound.StartBoundAttempt(f.ctx, lease)
	if err != nil || attempt == nil {
		t.Fatal("attempt start", err)
	}
	if _, err := bound.StartBoundAttempt(f.ctx, lease); !errors.Is(err, parser.ErrAttemptReplay) {
		t.Fatal("duplicate attempt", err)
	}
	f.exec(`UPDATE scheduler_attempts SET reserved_usd=0.26,result_json='{"synthetic":"retained"}' WHERE job_id=? AND run_id=?`, jobID, run.Run.ID)
	var ownerUntil int64
	if err := f.db.QueryRowContext(f.ctx, "SELECT lease_until FROM go_consumer_control WHERE id=1").Scan(&ownerUntil); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE go_consumer_control SET lease_until=1 WHERE id=1")
	if err := attempt.Interrupt(f.ctx); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("expired owner interrupted", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND run_id=? AND status='running' AND cost_usd IS NULL AND reserved_usd=0.26", jobID, run.Run.ID) != 1 {
		t.Fatal("failed interrupt changed reservation")
	}
	f.exec("UPDATE go_consumer_control SET lease_until=? WHERE id=1", ownerUntil)
	f.exec("UPDATE go_consumer_control SET mode='draining', revision=4 WHERE id=1")
	if _, err := bound.StartBoundAttempt(f.ctx, lease); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("draining accepted attempt", err)
	}
	if err := attempt.Interrupt(f.ctx); err != nil {
		t.Fatal("interrupt during draining", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND run_id=? AND status='interrupted' AND cost_usd IS NULL AND reserved_usd=0.26 AND result_json='{\"synthetic\":\"retained\"}'", jobID, run.Run.ID) != 1 {
		t.Fatal("paid evidence lost")
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='failed' AND locked_by='' AND locked_until=0", jobID) != 1 {
		t.Fatal("job was not fenced")
	}
	if err := attempt.Interrupt(f.ctx); !errors.Is(err, parser.ErrJobLeaseLost) {
		t.Fatal("second interrupt accepted", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='worker', revision=3 WHERE id=1")
	if err := f.repo.Finish(f.ctx, run.Run.ID, "completed"); err != nil {
		t.Fatal("finish run", err)
	}
}
