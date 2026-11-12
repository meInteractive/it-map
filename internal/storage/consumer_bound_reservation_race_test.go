//go:build mariadb && schedulerwrites

package storage

import (
	"errors"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/parser"
)

func testBoundReservationInterruptRace(t *testing.T, f *schedulerFixture, req consumercontrol.Request) {
	t.Helper()
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	defer f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true}' WHERE id=1`)
	run, bound, err := f.repo.BeginBoundRun(f.ctx, parser.RunOptions{RequestID: "CI_bound_reserve_interrupt", Diagnostic: true, Duration: time.Second}, req)
	if err != nil || bound == nil || !run.Acquired {
		t.Fatal("bound run", err)
	}
	defer f.exec("DELETE FROM scheduler_runs WHERE id=?", run.Run.ID)
	jobID := "CI_bound_reserve_interrupt_job"
	schedulerSeedJob(f, jobID, nil)
	defer f.exec("DELETE FROM scheduler_jobs WHERE id=?", jobID)
	defer f.exec("DELETE FROM scheduler_attempts WHERE job_id=?", jobID)
	lease, err := bound.ClaimJob(f.ctx, parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: 10 * time.Second})
	if err != nil || lease == nil {
		t.Fatal("claim", err)
	}
	attempt, err := bound.StartBoundAttempt(f.ctx, lease)
	if err != nil || attempt == nil {
		t.Fatal("attempt", err)
	}
	start := make(chan struct{})
	reserveResult := make(chan error, 1)
	interruptResult := make(chan error, 1)
	go func() {
		<-start
		reserveResult <- attempt.Reserve(f.ctx, []byte(`{"provider":"apify","model":"CI_actor","capUsd":0.26}`))
	}()
	go func() { <-start; interruptResult <- attempt.Interrupt(f.ctx) }()
	close(start)
	reserveErr, interruptErr := <-reserveResult, <-interruptResult
	if interruptErr != nil {
		t.Fatal("interrupt failed", interruptErr)
	}
	if reserveErr != nil && !errors.Is(reserveErr, parser.ErrJobLeaseLost) {
		t.Fatal("unexpected reserve outcome", reserveErr)
	}
	var reserve float64
	var status string
	var costValid bool
	if err := f.db.QueryRowContext(f.ctx, "SELECT reserved_usd,status,cost_usd IS NOT NULL FROM scheduler_attempts WHERE job_id=? AND run_id=?", jobID, run.Run.ID).Scan(&reserve, &status, &costValid); err != nil {
		t.Fatal(err)
	}
	if status != "interrupted" || costValid || (reserveErr == nil && reserve != 0.26) || (reserveErr != nil && reserve != 0) {
		t.Fatal("reservation did not match serialized outcome")
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='failed' AND locked_by=''", jobID) != 1 {
		t.Fatal("job not fenced")
	}
	if err := f.repo.Finish(f.ctx, run.Run.ID, "completed"); err != nil {
		t.Fatal("finish run", err)
	}
}
