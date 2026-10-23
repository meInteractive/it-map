package applications

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Delete removes an application and its history as one atomic user-scoped
// operation. The legacy API does not require a revision on DELETE; it returns
// the owning company so a response composer can reproduce readCompanyApplications.
func (r Repository) Delete(ctx context.Context, user string, id int64) (string, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || id < 1 {
		return "", ErrInvalidInput
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var company string
	err = tx.QueryRowContext(ctx, `SELECT company_name FROM company_applications
       WHERE user_name=? AND id=? FOR UPDATE`, user, id).Scan(&company)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM company_application_status_history
        WHERE application_id=?`, id); err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM company_applications
        WHERE user_name=? AND id=?`, user, id)
	if err != nil {
		return "", err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if affected != 1 {
		return "", ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return company, nil
}
