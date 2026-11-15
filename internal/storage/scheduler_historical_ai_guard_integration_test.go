//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/parser"
)

func TestMariaDBHistoricalAIGuardDisposable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		orphan   bool
		status   string
		cost     any
		reserved float64
	}{{"orphan", true, "interrupted", nil, 0.25}, {"old_generation", false, "interrupted", nil, 0.25}, {"contradictory", true, "running", 0.1, 0.25},
		{"orphan_running_zero_reserve", true, "running", 0.1, 0}, {"historical_running_zero_reserve", false, "running", 0.0, 0},
		{"orphan_pending_cost", true, "pending", 0.1, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			f := billingFixture(t)
			billingReset(f)
			run := f.begin("CI historical AI guard")
			schedulerSeedJob(f, "CI_money_job", nil)
			f.exec("UPDATE scheduler_jobs SET type='classify_title' WHERE id='CI_money_job'")
			lease := schedulerRequireLease(t, f, run, "CI_money_job")
			attempt, err := lease.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			oldRun := "CI_missing_run"
			if !tc.orphan {
				old := "11111111-2222-4333-8444-555555555555"
				if old == f.repo.Generation {
					old = "11111111-2222-4333-8444-555555555556"
				}
				oldRun = "CI_old_ai_run"
				f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES(?,?,'automatic',1,'interrupted')", oldRun, old)
			}
			f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_old_ai','CI_other_job',?,'classify_title',1,?,?,?)", oldRun, tc.status, tc.reserved, tc.cost)
			calls := 0
			work := func(context.Context) (json.RawMessage, error) { calls++; return syntheticUsage, nil }
			if _, err := attempt.Run(f.ctx, syntheticPaid, work); !errors.Is(err, parser.ErrHistoricalAIUnresolved) || calls != 0 || attempt.Requests() != 0 || attempt.Unknown() {
				t.Fatal("old unknown did not block before reserve", err)
			}
			if _, err := attempt.Run(f.ctx, syntheticFree, func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`{}`), nil }); err != nil || calls != 1 {
				t.Fatal("free request was blocked", err)
			}
			f.exec("UPDATE scheduler_attempts SET status='completed',cost_usd=0.1 WHERE id='CI_old_ai'")
			if _, err := attempt.Run(f.ctx, syntheticPaid, work); err != nil || calls != 2 {
				t.Fatal("resolved history still blocked paid work", err)
			}
			if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "completed"}}); err != nil {
				t.Fatal(err)
			}
			f.finish(run)
		})
	}
	t.Run("non_ai", func(t *testing.T) {
		f := billingFixture(t)
		billingReset(f)
		run, _, attempt := billingStart(t, f, "CI other provider")
		f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_old_ai','CI_other_job','CI_missing_run','classify_title',1,'interrupted',0.25,NULL)")
		calls := 0
		if _, err := attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { calls++; return syntheticUsage, nil }); err != nil || calls != 1 {
			t.Fatal("AI guard blocked another kind of attempt", err)
		}
		if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "completed"}}); err != nil {
			t.Fatal(err)
		}
		f.finish(run)
	})
}
