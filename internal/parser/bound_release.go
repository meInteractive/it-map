package parser

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"itmap/internal/consumercontrol"
)

// ReleaseUnstarted возвращает экспериментально захваченный job в очередь,
// только если у этого run/job не было ни одной attempt. Содержимое и денежные
// записи не меняются. До полноценного consumer fence выполнение остаётся
// запрещено; операция не подключена к runtime и не является recovery paid-state.
func (b *BoundRun) ReleaseUnstarted(ctx context.Context, j *JobLease) error {
	if b == nil || j == nil || !j.consumerBound || b.runID == "" ||
		b.repo.DB == nil || b.repo.DB != j.repo.DB || b.repo.Generation != j.repo.Generation ||
		b.runID != j.runID || j.jobID == "" || j.owner == "" {
		return ErrJobInput
	}
	options, payload, err := j.prepareFinish(FinishOptions{Status: "pending", Deferred: true})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := b.repo.lockGeneration(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = consumercontrol.GuardCompletion(ctx, tx, b.consumer); err != nil {
		return err
	}
	if _, err = activeRun(ctx, tx, b.repo.Generation, b.runID); err != nil {
		return err
	}
	run, err := readRun(ctx, tx, b.runID)
	if err != nil {
		return storageError(ctx)
	}
	var owner, status string
	var until int64
	err = tx.QueryRowContext(ctx, "SELECT locked_by,locked_until,status FROM scheduler_jobs WHERE id=? AND generation=? FOR UPDATE", j.jobID, b.repo.Generation).Scan(&owner, &until, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrJobLeaseLost
	}
	if err != nil {
		return storageError(ctx)
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return storageError(ctx)
	}
	if status != "running" || owner != j.owner || until <= now {
		return ErrJobLeaseLost
	}
	if err = consumercontrol.GuardCompletionLeases(ctx, tx, b.consumer, run.LeaseUntil, until); err != nil {
		return err
	}
	var attempts int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND run_id=?", j.jobID, b.runID).Scan(&attempts); err != nil {
		return storageError(ctx)
	}
	if attempts != 0 {
		return ErrBoundJobUnavailable
	}
	if err = j.finishLocked(ctx, tx, now, options, payload); err != nil {
		return err
	}
	// Блокировки не останавливают часы. Проверить оба оставшихся lease после
	// записи и непосредственно перед commit; при отказе вся запись откатится.
	if err = consumercontrol.GuardCompletionLeases(ctx, tx, b.consumer, run.LeaseUntil, until); err != nil {
		return err
	}
	if _, err = activeRun(ctx, tx, b.repo.Generation, b.runID); err != nil {
		return err
	}
	now, err = runClock(ctx, tx)
	if err != nil {
		return storageError(ctx)
	}
	if until <= now {
		return ErrJobLeaseLost
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}
