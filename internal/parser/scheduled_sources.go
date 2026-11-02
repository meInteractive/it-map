package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"itmap/internal/apifysettings"
)

// scheduleHH добавляет только ранее разрешённые источники текущего владельца.
// Блокировки поколения и run не выпускаются до записи заданий.
func scheduleHH(ctx context.Context, runs RunRepository, settingsRepo apifysettings.Repository, runID, owner string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := runs.lockGeneration(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, err := activeRun(ctx, tx, runs.Generation, runID)
	if err != nil {
		return err
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName != owner {
		return ErrSettingsChanged
	}
	if settings.IntervalDays != 1 && settings.IntervalDays != 3 && settings.IntervalDays != 7 {
		return ErrSettingsStorage
	}
	selected := false
	for _, id := range settings.SourceIDs {
		if id == "hh" {
			selected = true
		}
	}
	if !selected {
		if err = tx.Commit(); err != nil {
			return storageError(ctx)
		}
		return nil
	}
	var due int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=? AND source_id='hh'`, runs.Generation, now).Scan(&due); err != nil {
		return ErrSettingsStorage
	}
	if due != 0 {
		if err = tx.Commit(); err != nil {
			return storageError(ctx)
		}
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.company_id,COALESCE(s.next_run_at,0),s.interval_days,s.last_success_at,COALESCE(s.last_attempt_at,0) FROM vacancy_source_parsing s JOIN company_identity c ON c.id=s.company_id WHERE s.source_id='hh' AND s.enabled=1 AND (COALESCE(s.next_run_at,0)<=? OR s.interval_days<>?) AND (?=0 OR EXISTS(SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1)) AND NOT EXISTS(SELECT 1 FROM scheduler_jobs j WHERE j.generation=? AND j.company_id=s.company_id AND j.source_id='hh' AND j.type='scan_source' AND j.status IN ('pending','running')) FOR UPDATE`, now, settings.IntervalDays, row.favorite, owner, runs.Generation)
	if err != nil {
		return ErrSettingsStorage
	}
	type dueSource struct{ id, next, lastAttempt int64 }
	sources := []dueSource{}
	for rows.Next() {
		var source dueSource
		var interval int64
		var lastSuccess sql.NullString
		if rows.Scan(&source.id, &source.next, &interval, &lastSuccess, &source.lastAttempt) != nil {
			rows.Close()
			return ErrSettingsStorage
		}
		if interval != int64(settings.IntervalDays) && lastSuccess.Valid {
			if last, err := time.Parse(time.RFC3339Nano, lastSuccess.String); err == nil && last.UnixMilli() != 0 {
				source.next = last.UnixMilli() + int64(settings.IntervalDays)*86400000
			}
		}
		if source.next <= now {
			sources = append(sources, source)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrSettingsStorage
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].next != sources[j].next {
			return sources[i].next < sources[j].next
		}
		if sources[i].lastAttempt != sources[j].lastAttempt {
			return sources[i].lastAttempt < sources[j].lastAttempt
		}
		return fmt.Sprintf("company:%d", sources[i].id) < fmt.Sprintf("company:%d", sources[j].id)
	})
	scheduled := 0
	for _, candidate := range sources {
		if scheduled >= 5 {
			break
		}
		source, e := settingsRepo.ReadHHSourceTx(ctx, tx, candidate.id)
		if errors.Is(e, apifysettings.ErrSourceMissing) {
			continue
		}
		if e != nil {
			return e
		}
		if !source.Enabled || !source.Supported {
			continue
		}
		key := fmt.Sprintf("scan:%d:hh:%d:%d:%d", candidate.id, source.Revision, candidate.next, settings.Revision)
		var known int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=?`, runs.Generation, stageHash(key)).Scan(&known); e != nil {
			return ErrSettingsStorage
		}
		if known != 0 {
			continue
		}
		if _, e = tx.ExecContext(ctx, `UPDATE vacancy_source_parsing SET interval_days=?,next_run_at=? WHERE company_id=? AND source_id='hh' AND revision=? AND source_url=?`, settings.IntervalDays, candidate.next, candidate.id, source.Revision, source.URL); e != nil {
			return ErrSettingsStorage
		}
		payload, e := json.Marshal(map[string]any{"sourceRevision": source.Revision, "sourceUrl": source.URL, "manual": false, "scheduleRevision": settings.Revision, "dueAt": candidate.next})
		if e != nil {
			return ErrJobInput
		}
		id, e := stageToken()
		if e != nil {
			return e
		}
		priority, ok := settings.Priorities["scan_source"]
		if !ok || priority < 0 || priority > 100 {
			return ErrSettingsStorage
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,'scan_source',?,'hh',?,?,?,4,?,?) ON DUPLICATE KEY UPDATE id=id`, id, runs.Generation, stageHash(key), candidate.id, string(payload), priority, now, now, now)
		if e != nil {
			return ErrSettingsStorage
		}
		scheduled++
	}
	if _, err = activeRun(ctx, tx, runs.Generation, runID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}

// completeScheduledHH сохраняет срок следующего сканирования в одной транзакции с receipt.
func completeScheduledHH(ctx context.Context, tx *sql.Tx, job Job, owner string, code string) error {
	if job.CompanyID == nil {
		return ErrJobInput
	}
	cursor, exists, err := readHHListingCursor(ctx, tx, "go399:"+job.ID, owner, job)
	if err != nil {
		return err
	}
	if !exists || !cursor.Done {
		return ErrHHListingChanged
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return ErrSettingsStorage
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName != owner {
		return ErrSettingsChanged
	}
	if settings.IntervalDays != 1 && settings.IntervalDays != 3 && settings.IntervalDays != 7 {
		return ErrSettingsStorage
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return ErrSettingsStorage
	}
	result := "success"
	if code != "" {
		result = "partial"
	}
	successAt := time.UnixMilli(now).UTC().Format("2006-01-02T15:04:05.000Z")
	update, err := tx.ExecContext(ctx, `UPDATE vacancy_source_parsing SET next_run_at=?,last_attempt_at=?,last_result=?,last_error=?,last_success_at=CASE WHEN ?='success' THEN ? ELSE last_success_at END,interval_days=? WHERE company_id=? AND source_id='hh' AND enabled=1 AND revision=? AND source_url=?`, now+int64(settings.IntervalDays)*86400000, now, result, "", result, successAt, settings.IntervalDays, job.CompanyID, cursor.Operation.SourceRevision, cursor.Operation.SourceURL)
	if err != nil {
		return ErrSettingsStorage
	}
	changed, err := update.RowsAffected()
	if err != nil {
		return ErrSettingsStorage
	}
	if changed != 1 {
		return ErrHHListingChanged
	}
	return nil
}
