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
	syntheticRequest := []byte(`{"provider":"apify","model":"CI_actor","capUsd":0.26}`)
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
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0.1,"monthBudgetUsd":0.1}' WHERE id=1`)
	defer f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true}' WHERE id=1`)
	f.exec(`INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,result_json) VALUES('CI_prior_unknown',?,?,'diagnostic',?,'interrupted',0.26,'{"synthetic":"retained"}')`, jobID, run.Run.ID, run.Run.StartedAt)
	if err := attempt.Reserve(f.ctx, syntheticRequest); !errors.Is(err, parser.ErrMoneyUncertain) {
		t.Fatal("unresolved paid attempt did not block reserve", err)
	}
	f.assertAttempt("CI_prior_unknown", "interrupted")
	f.exec("DELETE FROM scheduler_attempts WHERE id='CI_prior_unknown'")
	if err := attempt.Reserve(f.ctx, syntheticRequest); !errors.Is(err, parser.ErrBudgetLimit) {
		t.Fatal("budget limit not enforced", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0 AND cost_usd IS NULL", jobID) != 1 {
		t.Fatal("rejected budget changed reservation")
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	results := make(chan error, 2)
	go func() { results <- attempt.Reserve(f.ctx, syntheticRequest) }()
	go func() { results <- attempt.Reserve(f.ctx, syntheticRequest) }()
	one, two := <-results, <-results
	if !((one == nil && errors.Is(two, parser.ErrMoneyUncertain)) || (two == nil && errors.Is(one, parser.ErrMoneyUncertain))) {
		t.Fatal("concurrent reserve was not single-use", one, two)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0.26 AND cost_usd IS NULL", jobID) != 1 {
		t.Fatal("reservation missing or duplicated")
	}
	f.exec(`UPDATE scheduler_attempts SET result_json='{"synthetic":"retained"}' WHERE job_id=? AND run_id=?`, jobID, run.Run.ID)
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
	if err := attempt.Reserve(f.ctx, syntheticRequest); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("draining accepted reservation", err)
	}
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
