//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"itmap/internal/aiproviders"
	"itmap/internal/parser"
	"itmap/internal/profiles"
)

// Синтетический генератор закрывает StopClaims точно до либо уже внутри
// Billing.Run. Он не открывает сеть и не обращается к платным провайдерам.
type stopBeforePaidRunner struct {
	stop        chan struct{}
	beforeRun   bool
	duringWork  bool
	calls       int
}

func (r *stopBeforePaidRunner) Automatic(ctx context.Context, req aiproviders.GenerationRequest, _ profiles.ModelInput, b aiproviders.Billing) (aiproviders.AIResult, error) {
	if req.CheckActive != nil {
		if err := req.CheckActive(ctx); err != nil {
			return aiproviders.AIResult{}, err
		}
	}
	if r.beforeRun {
		close(r.stop)
	}
	_, err := b.Run(ctx, json.RawMessage(`{"provider":"openrouter","model":"typesafe/jev-1.13","contextLength":100,"pricing":{"prompt":"0.00001","completion":"0"},"payload":{"questions":{"q":{}}}}`), func(context.Context) (json.RawMessage, error) {
		r.calls++
		if r.duringWork {
			close(r.stop)
		}
		return json.RawMessage(`{"usage":{"cost":0.001}}`), nil
	})
	return aiproviders.AIResult{Model: aiproviders.JEVModelID, Message: aiproviders.Message{Content: "CI_PRIVATE_ANSWER"}}, err
}

// GO-431: сигнал остановки при работающем grace-context не должен запускать
// новый платный POST; уже начатый — должен сохранять settlement и результат.
func TestMariaDBAIStopClaimsBeforePaidDispatchDisposable(t *testing.T) {
	for _, tc := range []struct {
		name       string
		before     bool
		during     bool
		atStart    bool
	}{
		{"already_draining", false, false, true},
		{"after_attempt_before_billing", true, false, false},
		{"during_inflight_paid_request", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := billingFixture(t)
			if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001") != 0 {
				t.Fatal("CI candidate ID collision")
			}
			f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990001,'CI_AI_STOP_CANDIDATE','CI_TITLE',?,'manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')", strings.Repeat("a", 64))
			t.Cleanup(func() {
				f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
				f.exec("DELETE FROM vacancy_candidates WHERE id=990001")
			})
			lease := aiExecutionLease(t, f, "extract_vacancy")
			stop := make(chan struct{})
			if tc.atStart {
				close(stop)
			}
			runner := &stopBeforePaidRunner{stop: stop, beforeRun: tc.before, duringWork: tc.during}
			store := &aiStageFixtureStore{}
			stage := &parser.AIStageExecutor{Store: store, Generator: runner, StopClaims: stop}
			_, err := stage.Execute(f.ctx, lease)
			if tc.during {
				if err != nil || runner.calls != 1 || store.saves != 1 ||
					f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_AI_JOB' AND status='completed'") != 1 ||
					f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id='CI_AI_JOB' AND status='completed' AND cost_usd=0.001") != 1 {
					t.Fatal("in-flight paid request did not finish with known cost during grace", err, runner.calls, store.saves)
				}
				return
			}
			if !errors.Is(err, parser.ErrWorkerDrainBeforePaid) || runner.calls != 0 || store.saves != 0 {
				t.Fatal("drain allowed new POST or returned wrong error", err, runner.calls, store.saves)
			}
			var attempts, reserved int
			var cost sql.NullFloat64
			var provider sql.NullString
			if err := f.db.QueryRowContext(f.ctx, "SELECT COUNT(*),COALESCE(SUM(reserved_usd),0) FROM scheduler_attempts WHERE job_id='CI_AI_JOB'").Scan(&attempts, &reserved); err != nil {
				t.Fatal(err)
			}
			expected := 1
			if tc.atStart {
				expected = 0
			}
			if attempts != expected || reserved != 0 ||
				f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id='CI_AI_JOB' AND COALESCE(provider,'')<>''") != 0 {
				t.Fatal("pre-dispatch drain wrote paid evidence or lost attempt", attempts, reserved)
			}
			if !tc.atStart {
				if err := f.db.QueryRowContext(f.ctx, "SELECT cost_usd,provider FROM scheduler_attempts WHERE job_id='CI_AI_JOB'").Scan(&cost, &provider); err != nil || cost.Valid || provider.Valid {
					t.Fatal("unspent attempt has paid receipt", err, cost, provider)
				}
			}
			// Обычная lease recovery после остановки должна сохранять
			// незатраченные попытки без ложного платного запрета.
			f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
			f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
			if err := f.repo.RecoverExpired(f.ctx); err != nil {
				t.Fatal("CI recovery failed", err)
			}
			if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_AI_JOB' AND status='pending'") != 1 {
				t.Fatal("job was lost after drain")
			}
			newRun := f.begin("CI_AI_STOP_RESTART")
			replacement := schedulerRequireLease(t, f, newRun, "CI_AI_JOB")
			a, err := replacement.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal("safe replacement attempt failed", err)
			}
			replayed := 0
			_, err = a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
				replayed++
				return syntheticUsage, nil
			})
			if err != nil || replayed != 1 || a.Unknown() {
				t.Fatal("known-unsent drain incorrectly blocked a new synthetic request", err, replayed)
			}
			if err := a.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "completed"}}); err != nil {
				t.Fatal("safe attempt could not finish", err)
			}
			f.finish(newRun)
		})
	}
}
