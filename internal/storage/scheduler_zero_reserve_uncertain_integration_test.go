//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/parser"
)

func TestMariaDBPriorZeroReserveUnknownBlocksPaidReplayDisposable(t *testing.T) {
	for _, tc := range []struct {
		name, status, provider string
		cost                   any
		blocked                bool
	}{
		{"possible_zero_reserve_send", "interrupted", "openrouter", nil, true},
		{"interrupted_confirmed_zero_cost", "interrupted", "openrouter", 0.0, true},
		{"interrupted_confirmed_paid", "interrupted", "openrouter", 0.001, true},
		{"running_confirmed_paid", "running", "openrouter", 0.001, true},
		{"finished_confirmed_zero_cost", "completed", "openrouter", 0.0, false},
		{"never_selected_provider", "interrupted", "", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := billingFixture(t)
			billingReset(f)
			run := f.begin("CI prior zero reserve")
			schedulerSeedJob(f, "CI_money_job", nil)
			f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id='CI_money_job'")
			lease := schedulerRequireLease(t, f, run, "CI_money_job")
			attempt, err := lease.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_prior_run',?,'automatic',1,'interrupted')", f.repo.Generation)
			f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,provider,started_at,status,reserved_usd,cost_usd) VALUES('CI_prior_zero','CI_money_job','CI_prior_run','extract_vacancy',?,1,?,0,?)", tc.provider, tc.status, tc.cost)
			calls := 0
			_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { calls++; return syntheticUsage, nil })
			if tc.blocked {
				if !errors.Is(err, parser.ErrMoneyUncertain) || calls != 0 || attempt.Requests() != 0 || attempt.Unknown() {
					t.Fatal("unknown previous provider call admitted", err, calls)
				}
				if _, e := attempt.Run(f.ctx, syntheticFree, func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }); e != nil {
					t.Fatal("free local progress blocked", e)
				}
			} else if err != nil || calls != 1 {
				t.Fatal("known or unsent previous call blocked", err, calls)
			}
			if err := attempt.Finish(f.ctx, parser.AttemptFinish{}); err != nil {
				t.Fatal(err)
			}
			f.finish(run)
		})
	}
}
