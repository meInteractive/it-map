//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"itmap/internal/parser"
)

// Успех HTTP не подтверждает финальный SQL COMMIT. Fault-коннектор
// возвращает ошибку как до COMMIT, так и после реально совершённого COMMIT.
func TestMariaDBAISettlementCommitAmbiguousNoReplayDisposable(t *testing.T) {
	for _, tc := range []struct {
		name         string
		beforeCommit bool
		wantReserve  float64
	}{
		{"before_commit_rollback", true, 0.26},
		{"after_real_commit_ack_lost", false, 0.01},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := billingFixture(t)
			billingReset(f)
			run := f.begin("CI_AI_SETTLEMENT_COMMIT")
			schedulerSeedJob(f, "CI_money_job", nil)
			f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id='CI_money_job'")
			repo, fired := billingCommitRepositoryMode(t, f.repo.Generation, "UPDATE scheduler_attempts SET reserved_usd=GREATEST(0,reserved_usd-?)+?", tc.beforeCommit)
			id := "CI_money_job"
			lease, err := repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
			if err != nil || lease == nil {
				t.Fatal("cannot claim synthetic AI job", err)
			}
			attempt, err := lease.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal("cannot create AI attempt", err)
			}
			calls := 0
			work := func(context.Context) (json.RawMessage, error) {
				calls++
				return syntheticUsage, nil
			}
			if _, err = attempt.Run(f.ctx, syntheticPaid, work); !errors.Is(err, parser.ErrRunStorage) || !fired.Load() || calls != 1 || attempt.Requests() != 1 || !attempt.Unknown() {
				t.Fatal("uncertain settlement incorrectly accepted or provider repeated", err, calls)
			}
			reserve, cost, status := billingAmount(t, f, attempt.ID())
			if status != "running" || cost.Valid || math.Abs(reserve-tc.wantReserve) > 0.000000001 {
				t.Fatal("ambiguous SQL settlement changed money evidence", reserve, cost, status)
			}
			if _, err = attempt.Run(f.ctx, syntheticPaid, work); !errors.Is(err, parser.ErrMoneyUncertain) || calls != 1 {
				t.Fatal("same attempt repeated paid POST after unknown COMMIT", err, calls)
			}

			// Только тестовый namespace: после гибели владельца восстанавливаем lease.
			f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
			f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id=?", id)
			if err := f.repo.RecoverExpired(f.ctx); err != nil {
				t.Fatal("run recovery after ambiguous settlement failed", err)
			}
			if err := lease.Finish(f.ctx, parser.FinishOptions{Status: "completed"}); err == nil {
				t.Fatal("old owner finished job after recovery")
			}
			if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id=? AND status='interrupted' AND cost_usd IS NULL", attempt.ID()) != 1 {
				t.Fatal("unknown receipt not interrupted conservatively")
			}
			recoveredReserve, recoveredCost, _ := billingAmount(t, f, attempt.ID())
			if recoveredCost.Valid || math.Abs(recoveredReserve-reserve) > 0.000000001 {
				t.Fatal("recovery altered reserved spend", recoveredReserve)
			}
			next := f.begin("CI_AI_SETTLEMENT_RESTART")
			replacement := schedulerRequireLease(t, f, next, id)
			nextAttempt, err := replacement.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal("cannot start recovered attempt", err)
			}
			if _, err = nextAttempt.Run(f.ctx, syntheticPaid, work); !errors.Is(err, parser.ErrMoneyUncertain) || calls != 1 || nextAttempt.Requests() != 0 || nextAttempt.Unknown() {
				t.Fatal("unknown settlement allowed a second provider POST", err, calls)
			}
			if err := nextAttempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "failed", Code: "unresolved_commit"}}); err != nil {
				t.Fatal("cannot close blocked synthetic replacement", err)
			}
			f.finish(next)
		})
	}
}
