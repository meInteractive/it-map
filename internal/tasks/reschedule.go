package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrCalendarSyncRequired prevents a task edit from silently diverging from an
// existing Google Calendar connection. Calendar syncing is not yet ported to Go.
var ErrCalendarSyncRequired = errors.New("google calendar synchronization required")

// Reschedule changes an unlinked task deadline using the same UTC millisecond
// representation returned by Date.toISOString() in the legacy API.
// A connected calendar or a previously linked event must be handled by the
// future Google Calendar integration; never update only the database in that case.
func (r Repository) Reschedule(ctx context.Context, user string, id int64, dueAt string) (string, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || id <= 0 {
		return "", errors.New("task DB, user or id unavailable")
	}
	normalized, err := normalizeTaskDeadline(dueAt)
	if err != nil {
		return "", err
	}

	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var eventID, syncStatus string
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(google_event_id,''),COALESCE(google_sync_status,'') FROM company_tasks WHERE user_name=? AND id=? FOR UPDATE`,
		user, id).Scan(&eventID, &syncStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrTaskNotFound
	}
	if err != nil {
		return "", err
	}
	var connected int
	// Even without an event ID, a user with a connected calendar expects new
	// deadlines to synchronize; do not silently bypass that behavior.
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM google_calendar_user_connection WHERE user_name=? LIMIT 1 FOR UPDATE`, user).Scan(&connected)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if connected == 1 || eventID != "" || (syncStatus != "" && syncStatus != "not_connected") {
		return "", ErrCalendarSyncRequired
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE company_tasks SET due_at=? WHERE user_name=? AND id=?`, normalized, user, id)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return normalized, nil
}

// Go time.Parse accepts +24:00/+00:60 and comma fractions; JS Date.parse
// rejects those forms. Keep one UTC-millisecond normalizer for task writes.
func normalizeTaskDeadline(raw string) (string, error) {
	if strings.Contains(raw, ",") {
		return "", ErrInvalidTask
	}
	due, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", ErrInvalidTask
	}
	if len(raw) >= 6 {
		zone := raw[len(raw)-6:]
		if (zone[0] == '+' || zone[0] == '-') && (zone[1:3] > "23" || zone[4:] > "59") {
			return "", ErrInvalidTask
		}
	}
	return due.UTC().Format("2006-01-02T15:04:05.000Z"), nil
}
