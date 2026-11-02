package parser

import (
	"context"
	"database/sql"
	"time"
)

// interruptWorkerJob сохраняет неизвестную стоимость и закрывает все попытки
// текущего владельца вместе с заданием. Утрата lease запрещает любые записи.
func interruptWorkerJob(ctx context.Context, lease *JobLease, options FinishOptions) error {
	options, payload, err := lease.prepareFinish(options)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, now, err := lease.beginOwned(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = interruptWorkerAttempts(ctx, tx, lease, now); err != nil {
		return err
	}
	if err = lease.finishLocked(ctx, tx, now, options, payload); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}

func interruptWorkerAttempts(ctx context.Context, tx *sql.Tx, lease *JobLease, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE scheduler_attempts SET finished_at=?,status='interrupted',reason_code='interrupted',message='Обработчик прерван; денежный резерв сохранён.' WHERE job_id=? AND run_id=? AND status='running'`, now, lease.jobID, lease.runID)
	if err != nil {
		return storageError(ctx)
	}
	return nil
}
