//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/parser"
)

// Синтетический провайдер не делает HTTP: проверяем границы платного вызова
// и восстановления с настоящими SQL lease и ledger только в disposable CI.
func TestMariaDBAIStageFaultRecoveryNoPaidReplayDisposable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		beforePOST  bool
		lostACK     bool
		knownCost   bool
	}{
		{"cancel_before_post", true, false, false},
		{"cancel_after_settlement_before_stage_save", false, false, true},
		{"cancel_after_post_lost_ack", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := billingFixture(t)
			lease := aiExecutionLease(t, f, "extract_vacancy")
			store := &aiStageFixtureStore{}
			runner := &aiStageFixtureRunner{unknown: tc.lostACK}
			stage := &parser.AIStageExecutor{Store: store, Generator: runner}
			ctx, cancel := context.WithCancel(f.ctx)
			defer cancel()
			switch {
			case tc.beforePOST:
				cancel()
			case tc.lostACK:
				// После принятия POST ответ/ACK не получен, исход и стоимость неизвестны.
				runner.after = cancel
			default:
				// Settlement зафиксирован, но запись результата ещё не начиналась.
				runner.afterBilling = cancel
			}
			if _, err := stage.Execute(ctx, lease); !errors.Is(err, context.Canceled) {
				t.Fatal("synthetic cancelled stage was not interrupted", err)
			}
			initialPOST := 1
			if tc.beforePOST {
				initialPOST = 0
			}
			if runner.calls != initialPOST || store.saves != 0 {
				t.Fatal("unexpected POST or result write on cancelled stage", runner.calls, store.saves)
			}
			var originalReserve float64
			var originalCost any
			if tc.beforePOST {
				if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id='CI_AI_JOB'") != 0 {
					t.Fatal("cancel-before-POST created an attempt")
				}
			} else {
				var costValue interface{}
				if err := f.db.QueryRowContext(f.ctx, "SELECT reserved_usd,cost_usd FROM scheduler_attempts WHERE job_id='CI_AI_JOB' AND status='running'").Scan(&originalReserve, &costValue); err != nil {
					t.Fatal("accepted synthetic POST lost running receipt", err)
				}
				originalCost = costValue
				if originalReserve <= 0 || (tc.knownCost && costValue == nil) || (!tc.knownCost && costValue != nil) {
					t.Fatal("payment evidence before recovery is wrong", originalReserve, costValue)
				}
			}
			// Только тестовая БД: имитируем истечение lease и штатную recovery.
			f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
			f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
			if err := f.repo.RecoverExpired(f.ctx); err != nil {
				t.Fatal("expired run recovery failed", err)
			}
			if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_AI_JOB' AND status='pending' AND locked_by=''") != 1 {
				t.Fatal("expired job not recovered into pending")
			}
			if err := lease.Finish(f.ctx, parser.FinishOptions{Status: "completed"}); err == nil {
				t.Fatal("expired owner was allowed to finish a recovered job")
			}
			if !tc.beforePOST {
				var reserve float64
				var cost interface{}
				var status string
				if err := f.db.QueryRowContext(f.ctx, "SELECT reserved_usd,cost_usd,status FROM scheduler_attempts WHERE job_id='CI_AI_JOB'").Scan(&reserve, &cost, &status); err != nil {
					t.Fatal("recovered ledger unreadable", err)
				}
				if status != "interrupted" || reserve != originalReserve || (cost == nil) != (originalCost == nil) {
					t.Fatal("recovery changed payment evidence", status, reserve, cost)
				}
			}
			newRun := f.begin("CI_AI_FAULT_RECOVERY")
			replacement := schedulerRequireLease(t, f, newRun, "CI_AI_JOB")
			attempt, err := replacement.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal("replacement attempt cannot start", err)
			}
			replayedPOST := 0
			_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
				replayedPOST++
				return syntheticUsage, nil
			})
			if tc.beforePOST {
				if err != nil || replayedPOST != 1 || attempt.Requests() != 1 {
					t.Fatal("pre-POST cancellation prevented a fresh request", err, replayedPOST)
				}
			} else {
				if !errors.Is(err, parser.ErrMoneyUncertain) || replayedPOST != 0 || attempt.Requests() != 0 || attempt.Unknown() {
					t.Fatal("recovered paid attempt repeated a possible POST", err, replayedPOST)
				}
				if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id='CI_AI_JOB' AND status='interrupted' AND reserved_usd>0") != 1 {
					t.Fatal("prior cost and reserve were not retained")
				}
			}
			finish := parser.FinishOptions{Status: "failed", Code: "synthetic_guard"}
			if tc.beforePOST {
				finish = parser.FinishOptions{Status: "completed"}
			}
			if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: finish}); err != nil {
				t.Fatal("replacement receipt failed", err)
			}
			f.finish(newRun)
		})
	}
}
