package parser

import (
	"context"
	"database/sql"
	"errors"
	"regexp"

	"itmap/internal/dbretry"
)

var ErrJobActionChanged = errors.New("scheduler job action target changed")

// Source допускает любой такой 36-символьный ID, без проверки UUID version/variant.
var jobActionID = regexp.MustCompile(`^[0-9a-f-]{36}$`)

type JobActionsRepository struct{ DB *sql.DB }

// PrepareJobAction связывает команду с поколением до чтения HTTP body.
// Операция не удерживает соединение и не может запустить worker или провайдера.
func (r JobActionsRepository) PrepareJobAction(ctx context.Context) (func(context.Context, string, string) error, error) {
	if r.DB == nil {
		return nil, ErrSettingsStorage
	}
	var expected string
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&expected); e != nil || !generationPattern.MatchString(expected) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, id, action string) error { return r.apply(ctx, expected, id, action) }, nil
}
func (r JobActionsRepository) apply(ctx context.Context, expected, id, action string) error {
	if !jobActionID.MatchString(id) || (action != "retry" && action != "cancel") {
		return ErrJobInput
	}
	status := "cancelled"
	if action == "retry" {
		status = "pending"
	}
	_, e := dbretry.Run(ctx, func() (struct{}, error) {
		tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if e != nil {
			return struct{}{}, e
		}
		defer tx.Rollback()
		var current string
		if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&current); e != nil {
			return struct{}{}, e
		}
		if current != expected {
			return struct{}{}, ErrSettingsGeneration
		}
		now, e := runClock(ctx, tx)
		if e != nil {
			return struct{}{}, e
		}
		result, e := tx.ExecContext(ctx, `UPDATE scheduler_jobs SET status=?,run_after=?,attempts=CASE WHEN ?='pending' THEN 0 ELSE attempts END,last_error='',reason_code='',updated_at=? WHERE id=? AND generation=? AND status IN ('pending','failed','cancelled')`, status, now, status, now, id, expected)
		if e != nil {
			return struct{}{}, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return struct{}{}, e
		}
		if n == 0 {
			return struct{}{}, ErrJobActionChanged
		}
		if n != 1 {
			return struct{}{}, ErrSettingsStorage
		}
		return struct{}{}, tx.Commit()
	})
	if errors.Is(e, ErrJobActionChanged) {
		return e
	}
	return settingsFailure(ctx, e)
}
