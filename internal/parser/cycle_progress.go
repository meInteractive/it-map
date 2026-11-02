package parser

import "context"

// reportCycle обновляет только текущий run под тем же generation/lease fence.
func (r RunRepository) reportCycle(ctx context.Context, runID string, processed int, initialize bool) error {
	tx, err := r.lockGeneration(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, err := activeRun(ctx, tx, r.Generation, runID)
	if err != nil {
		return err
	}
	initial := 0
	if initialize {
		initial = 1
	}
	result, err := tx.ExecContext(ctx, `UPDATE scheduler_runs SET ready=CASE WHEN ?=1 THEN (SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=?) ELSE ready END,processed=?,succeeded=(SELECT COUNT(*) FROM scheduler_attempts WHERE run_id=? AND status='completed'),errors=(SELECT COUNT(*) FROM scheduler_attempts WHERE run_id=? AND status IN ('failed','interrupted')),remaining=(SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND status IN ('pending','running')) WHERE id=? AND generation=? AND status='running' AND lease_until>?`, initial, r.Generation, now, processed, runID, runID, r.Generation, runID, r.Generation, now)
	if err != nil {
		return storageError(ctx)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storageError(ctx)
	}
	if n != 1 {
		return ErrRunLeaseLost
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}
