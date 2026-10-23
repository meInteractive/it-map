package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrTaskNotFound also covers another user's task: callers must not expose its existence.
var ErrTaskNotFound = errors.New("task not found")

// SetCompleted changes only the authenticated user's completion flag. It is
// idempotent and does not modify Google Calendar linkage or task contents.
func (r Repository) SetCompleted(ctx context.Context, user string, id int64, completed bool) error {
	if r.DB == nil || strings.TrimSpace(user) == "" || id <= 0 {
		return errors.New("task DB, user or id unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous int64
	err = tx.QueryRowContext(ctx, `SELECT completed FROM company_tasks WHERE user_name=? AND id=? FOR UPDATE`, user, id).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTaskNotFound
	}
	if err != nil {
		return err
	}
	if (previous != 0) != completed {
		next := int64(0)
		if completed {
			next = 1
		}
		// SQLite CURRENT_TIMESTAMP was UTC; UTC_TIMESTAMP keeps that contract on a
		// MariaDB server even if the host's local timezone is configured differently.
		_, err = tx.ExecContext(ctx, `UPDATE company_tasks SET completed=?,
    completed_at=CASE WHEN ?=1 THEN UTC_TIMESTAMP() ELSE NULL END
    WHERE user_name=? AND id=?`, next, next, user, id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
