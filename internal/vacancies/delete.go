package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// DeleteResult follows the v316 DELETE /api/vacancies/{id} response contract.
// Applications and tasks are retained even when their vacancy is removed.
var ErrAgencySalaryProjectionPending = errors.New("agency salary projection migration pending")

type DeleteResult struct {
	OK                   bool   `json:"ok"`
	Archived             bool   `json:"archived"`
	CompanyName          string `json:"companyName"`
	PreservedApplication bool   `json:"preservedApplication"`
	PreservedTask        bool   `json:"preservedTask"`
}

// DeleteManual keeps the two-step legacy archive/delete semantics. Locking the
// parent row prevents a concurrent FK-referencing insert from racing the
// decision under InnoDB. MariaDB FK cascades remove role, source and personal
// state rows; application/task references must be detached without deleting
// their history. No orphan or stale writes are reported as successful.
func (r Repository) DeleteManual(ctx context.Context, id int64) (DeleteResult, error) {
	if r.DB == nil || id <= 0 {
		return DeleteResult{}, errors.New("vacancy deletion unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DeleteResult{}, err
	}
	defer tx.Rollback()

	var name, kind string
	var archived int64
	err = tx.QueryRowContext(ctx, `SELECT company_name, entity_type, archived FROM company_vacancies WHERE id=? FOR UPDATE`, id).Scan(&name, &kind, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return DeleteResult{}, ErrNotFound
	}
	if err != nil {
		return DeleteResult{}, err
	}
	if strings.TrimSpace(name) == "" {
		return DeleteResult{}, errors.New("invalid vacancy owner")
	}
	// The legacy route also recomputes agency salary ranges inside app_catalog.
	// Until the catalog JSON update is ported transactionally, refuse rather
	// than deleting an agency vacancy and leaving stale published estimates.
	if kind == "agency" {
		return DeleteResult{}, ErrAgencySalaryProjectionPending
	}

	var apps, tasks int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM company_applications WHERE vacancy_id=?`, id).Scan(&apps); err != nil {
		return DeleteResult{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM company_tasks WHERE vacancy_id=?`, id).Scan(&tasks); err != nil {
		return DeleteResult{}, err
	}
	if archived == 0 && (apps > 0 || tasks > 0) {
		result, err := tx.ExecContext(ctx, `UPDATE company_vacancies SET archived=1,updated_at=CURRENT_TIMESTAMP WHERE id=?`, id)
		if err != nil {
			return DeleteResult{}, err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return DeleteResult{}, errors.New("vacancy archive affected unexpected row count")
		}
		if err = tx.Commit(); err != nil {
			return DeleteResult{}, err
		}
		return DeleteResult{OK: true, Archived: true, CompanyName: name}, nil
	}

	// Perform explicit detachment before DELETE even though the converted
	// dump contains ON DELETE SET NULL, preserving the original D1 behavior.
	if _, err = tx.ExecContext(ctx, `UPDATE company_applications SET vacancy_id=NULL,updated_at=CURRENT_TIMESTAMP WHERE vacancy_id=?`, id); err != nil {
		return DeleteResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE company_tasks SET vacancy_id=NULL WHERE vacancy_id=?`, id); err != nil {
		return DeleteResult{}, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM company_vacancies WHERE id=?`, id)
	if err != nil {
		return DeleteResult{}, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return DeleteResult{}, errors.New("vacancy delete affected unexpected row count")
	}
	if err = tx.Commit(); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{OK: true, CompanyName: name, PreservedApplication: apps > 0, PreservedTask: tasks > 0}, nil
}
