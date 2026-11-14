//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"itmap/internal/parser"
)

func TestMariaDBHistoricalSchedulerEvidenceDisposable(t *testing.T) {
	f := billingFixture(t)
	billingReset(f)
	t.Cleanup(func() { billingReset(f) })
	old := "11111111-2222-4333-8444-555555555555"
	if old == f.repo.Generation {
		old = "11111111-2222-4333-8444-555555555556"
	}
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_old_run',?,'automatic',1,'interrupted')", old)
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_current_run',?,'automatic',1,'interrupted')", f.repo.Generation)
	insert := func(id, run, status string, cost any) {
		t.Helper()
		f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES(?,'CIjob',?,'diagnostic',1,?,0.25,?)", id, run, status, cost)
	}
	insert("CI_old_unknown", "CI_old_run", "interrupted", nil)
	insert("CI_old_known", "CI_old_run", "done", 0.0)
	insert("CI_orphan_unknown", "CI_missing_run", "interrupted", nil)
	insert("CI_orphan_known", "CI_missing_run", "completed", 0.1)
	insert("CI_current_unknown", "CI_current_run", "running", nil)
	got, err := f.repo.InspectHistoricalPaidAttempts(f.ctx)
	if err != nil || got.HistoricalUnknown != 1 || got.HistoricalCostRecorded != 1 || got.OrphanUnknown != 1 || got.OrphanCostRecorded != 1 {
		t.Fatal("scheduler evidence counts invalid", got, err)
	}
	for _, row := range [][3]string{{"CI_pending_budget", "CI_old_run", "budget"}, {"CI_pending_deferred", "CI_missing_run", "deferred"}, {"CI_pending_disabled", "CI_current_run", "paid_start_disabled"}} {
		f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reason_code,reserved_usd,cost_usd) VALUES(?,'CIjob',?,'diagnostic',1,'pending',?,0,0)", row[0], row[1], row[2])
	}
	got, err = f.repo.InspectHistoricalPaidAttempts(f.ctx)
	if err != nil || got.HistoricalUnknown != 1 || got.HistoricalCostRecorded != 2 || got.OrphanUnknown != 1 || got.OrphanCostRecorded != 2 {
		t.Fatal("pending zero receipts changed historical evidence", got, err)
	}
	for _, change := range []string{"reason_code='unexpected'", "reserved_usd=0.1", "cost_usd=0.1", "cost_usd=NULL"} {
		f.exec("UPDATE scheduler_attempts SET " + change + " WHERE id='CI_pending_budget'")
		if result, err := f.repo.InspectHistoricalPaidAttempts(f.ctx); !errors.Is(err, parser.ErrHistoricalAttemptsIncompatible) || result != (parser.HistoricalPaidAttempts{}) {
			t.Fatal("invalid pending receipt accepted", change, err)
		}
		f.exec("UPDATE scheduler_attempts SET reason_code='budget',reserved_usd=0,cost_usd=0 WHERE id='CI_pending_budget'")
	}
	other := parser.RunRepository{DB: f.db, Generation: old}
	if _, err = other.InspectHistoricalPaidAttempts(f.ctx); !errors.Is(err, parser.ErrRunGeneration) {
		t.Fatal("stale generation accepted", err)
	}
	holder, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var locked string
	if err = holder.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&locked); err != nil {
		holder.Rollback()
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	_, err = f.repo.InspectHistoricalPaidAttempts(blocked)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		holder.Rollback()
		t.Fatal("generation lock bypassed", err)
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE scheduler_attempts SET status='CI_invalid' WHERE id='CI_old_unknown'")
	if result, err := f.repo.InspectHistoricalPaidAttempts(f.ctx); !errors.Is(err, parser.ErrHistoricalAttemptsIncompatible) || result != (parser.HistoricalPaidAttempts{}) {
		t.Fatal("invalid attempt accepted", err)
	}
	f.exec("UPDATE scheduler_attempts SET status='interrupted' WHERE id='CI_old_unknown'")
	var q strings.Builder
	q.WriteString("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd) VALUES ")
	args := make([]any, 0, 1025)
	for i := 0; i < 1025; i++ {
		if i > 0 {
			q.WriteByte(',')
		}
		q.WriteString("(?,'CIjob','CI_old_run','diagnostic',1,'interrupted',0.25)")
		args = append(args, fmt.Sprintf("CI_history_limit_%04d", i))
	}
	f.exec(q.String(), args...)
	if result, err := f.repo.InspectHistoricalPaidAttempts(f.ctx); !errors.Is(err, parser.ErrHistoricalAttemptsLimit) || result != (parser.HistoricalPaidAttempts{}) {
		t.Fatal("attempt limit did not fail closed", err)
	}
}
