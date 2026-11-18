//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/parser"
)

// GO-431 P0: подтверждённый COMMIT денежного резерва сам по себе ещё не
// означает отправку POST. Отмена сразу после COMMIT должна остановить
// вызываемый mock provider, но НЕ должна удалить денежную неопределённость.
// Транзакция и все данные существуют только в одноразовой MariaDB CI.
func TestMariaDBAIReserveCommittedCancellationNoReplayDisposable(t *testing.T) {
	f := billingFixture(t)
	billingReset(f)
	run := f.begin("CI_POST_RESERVE_CANCEL")
	jobID := "CI_money_job"
	schedulerSeedJob(f, jobID, nil)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id=?", jobID)

	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	// Hook срабатывает только после реального успешного COMMIT UPDATE reserve.
	// В отличие от lost-ACK fault коннектор возвращает nil — результат
	// COMMIT известен вызывающему, но контекст уже отменён до provider I/O.
	repo, fired := billingCommitRepositoryHook(t, f.repo.Generation,
		"UPDATE scheduler_attempts SET reserved_usd=reserved_usd+", false, cancel)
	lease, err := repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &jobID})
	if err != nil || lease == nil {
		t.Fatal("CI claim for post-reserve cancellation failed", err)
	}
	attempt, err := lease.StartAttempt(f.ctx)
	if err != nil || attempt == nil {
		t.Fatal("CI attempt not created", err)
	}
	postCount := 0
	work := func(context.Context) (json.RawMessage, error) {
		postCount++
		return syntheticUsage, nil
	}
	_, err = attempt.Run(ctx, syntheticPaid, work)
	if !fired.Load() || !errors.Is(err, context.Canceled) || postCount != 0 ||
		attempt.Requests() != 1 || !attempt.Unknown() {
		t.Fatal("cancelled committed reserve dispatched POST or lost unknown state",
			err, fired.Load(), postCount, attempt.Requests(), attempt.Unknown())
	}
	var reserve float64
	var cost sql.NullFloat64
	var status, provider, model string
	err = f.db.QueryRowContext(f.ctx,
		"SELECT reserved_usd,cost_usd,status,COALESCE(provider,''),COALESCE(model,'') FROM scheduler_attempts WHERE id=?",
		attempt.ID()).Scan(&reserve, &cost, &status, &provider, &model)
	if err != nil || reserve != 0.26 || cost.Valid || status != "running" ||
		provider != "apify" || model != "CI_actor" {
		t.Fatal("post-reserve cancellation did not retain original money evidence",
			err, reserve, cost, status, provider, model)
	}
	// Даже тот же handle с новым живым контекстом не имеет права
	// превратить подтверждённый reserve в известный нулевой расход.
	_, err = attempt.Run(f.ctx, syntheticPaid, work)
	if !errors.Is(err, parser.ErrMoneyUncertain) || postCount != 0 {
		t.Fatal("committed reserve was replayed on same attempt", err, postCount)
	}

	// Истечение lease и recovery не должны переоткрывать оплачиваемую работу.
	// Изменения здесь применяются только к тестовой БД.
	f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
	f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id=?", jobID)
	if err := f.repo.RecoverExpired(f.ctx); err != nil {
		t.Fatal("post-reserve recovery failed", err)
	}
	reserveAfter, costAfter, statusAfter := billingAmount(t, f, attempt.ID())
	if reserveAfter != reserve || costAfter.Valid || statusAfter != "interrupted" {
		t.Fatal("recovery changed uncertain paid evidence", reserveAfter, costAfter, statusAfter)
	}
	newRun := f.begin("CI_POST_RESERVE_REPLACEMENT")
	replacement := schedulerRequireLease(t, f, newRun, jobID)
	newAttempt, err := replacement.StartAttempt(f.ctx)
	if err != nil || newAttempt == nil {
		t.Fatal("replacement attempt not created", err)
	}
	_, err = newAttempt.Run(f.ctx, syntheticPaid, work)
	if !errors.Is(err, parser.ErrMoneyUncertain) || postCount != 0 ||
		newAttempt.Requests() != 0 || newAttempt.Unknown() {
		t.Fatal("replacement reran synthetic paid provider despite reserve", err, postCount)
	}
	if err := newAttempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{
		Status: "failed", Code: "unresolved_post_reserve_cancel",
	}}); err != nil {
		t.Fatal("replacement receipt failed", err)
	}
	f.finish(newRun)
}
