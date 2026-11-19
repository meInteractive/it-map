package tasks

import (
	"context"
	"errors"
)

// CalendarSyncStatus читает только метку уже созданной задачи конкретного
// пользователя. Он не начинает синхронизацию и не раскрывает Google token.
func (r Repository) CalendarSyncStatus(ctx context.Context, user string, id int64) (string, error) {
	if r.DB == nil || user == "" || id <= 0 {
		return "", ErrTaskNotFound
	}
	var status string
	if err := r.DB.QueryRowContext(ctx,
		"SELECT google_sync_status FROM company_tasks WHERE user_name=? AND id=?", user, id).Scan(&status); err != nil {
		return "", errors.New("calendar status unavailable")
	}
	return status, nil
}
