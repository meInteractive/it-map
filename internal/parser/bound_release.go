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
	tx, fence, err := b.beginBoundCompletion(ctx, j)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND run_id=?", j.jobID, b.runID).Scan(&attempts); err != nil {
		return storageError(ctx)
	}
	if attempts != 0 {
		return ErrBoundJobUnavailable
	}
	if err = j.finishLocked(ctx, tx, fence.now, options, payload); err != nil {
		return err
	}
	if err = b.recheckBoundCompletion(ctx, tx, fence); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}

// boundCompletionFence не является credential и действителен только под locks
// исходной транзакции. Ошибка каждого шага требует rollback вызывающим слоем.
type boundCompletionFence struct{ now, runUntil, jobUntil int64 }

// beginBoundCompletion блокирует generation, consumer, run и job в этом порядке.
// Не проверяет attempts или paid evidence и не выполняет commit.
func (b *BoundRun) beginBoundCompletion(ctx context.Context, j *JobLease) (*sql.Tx, boundCompletionFence, error) {
	return b.beginBoundCompletionMode(ctx, j, false)
}

func (b *BoundRun) beginBoundCompletionMode(ctx context.Context, j *JobLease, versioned bool) (*sql.Tx, boundCompletionFence, error) {
	if b == nil || j == nil || !j.consumerBound || b.runID == "" ||
		b.repo.DB == nil || b.repo.DB != j.repo.DB || b.repo.Generation != j.repo.Generation ||
		b.runID != j.runID || j.jobID == "" || j.owner == "" {
		return nil, boundCompletionFence{}, ErrJobInput
	}
	tx, err := b.repo.lockGeneration(ctx)
	if err != nil {
		return nil, boundCompletionFence{}, err
	}
	// Вызывающий владеет транзакцией только при успехе.
	good := false
	defer func() {
		if !good {
			tx.Rollback()
		}
	}()
	if versioned {
		if err = consumercontrol.GuardVersionedSingleton(ctx, tx, b.repo.Generation); err != nil {
			return nil, boundCompletionFence{}, err
		}
	}
	if err = consumercontrol.GuardCompletion(ctx, tx, b.consumer); err != nil {
		return nil, boundCompletionFence{}, err
	}
	if _, err = activeRun(ctx, tx, b.repo.Generation, b.runID); err != nil {
		return nil, boundCompletionFence{}, err
	}
	run, err := readRun(ctx, tx, b.runID)
	if err != nil {
		return nil, boundCompletionFence{}, storageError(ctx)
	}
	var owner, status string
	var until int64
	err = tx.QueryRowContext(ctx, "SELECT locked_by,locked_until,status FROM scheduler_jobs WHERE id=? AND generation=? FOR UPDATE", j.jobID, b.repo.Generation).Scan(&owner, &until, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, boundCompletionFence{}, ErrJobLeaseLost
	}
	if err != nil {
		return nil, boundCompletionFence{}, storageError(ctx)
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return nil, boundCompletionFence{}, storageError(ctx)
	}
	if status != "running" || owner != j.owner || until <= now {
		return nil, boundCompletionFence{}, ErrJobLeaseLost
	}
	if err = consumercontrol.GuardCompletionLeases(ctx, tx, b.consumer, run.LeaseUntil, until); err != nil {
		return nil, boundCompletionFence{}, err
	}
	good = true
	return tx, boundCompletionFence{now: now, runUntil: run.LeaseUntil, jobUntil: until}, nil
}

// recheckBoundCompletion используется только пока исходные row locks удержаны.
// Она не заменяет проверку изменённой job, attempts или внешнего provider state.
func (b *BoundRun) recheckBoundCompletion(ctx context.Context, tx *sql.Tx, fence boundCompletionFence) error {
	if err := consumercontrol.GuardCompletionLeases(ctx, tx, b.consumer, fence.runUntil, fence.jobUntil); err != nil {
		return err
	}
	if _, err := activeRun(ctx, tx, b.repo.Generation, b.runID); err != nil {
		return err
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return storageError(ctx)
	}
	if fence.jobUntil <= now {
		return ErrJobLeaseLost
	}
	return nil
}
