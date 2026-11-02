package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"itmap/internal/apifysettings"
)

// scheduleHH добавляет только ранее разрешённые источники текущего владельца.
// Блокировки поколения и run не выпускаются до записи заданий.
func scheduleHH(ctx context.Context, runs RunRepository, settingsRepo apifysettings.Repository, runID, owner string) error {
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
	rows, err := tx.QueryContext(ctx, `SELECT s.company_id,COALESCE(s.next_run_at,0) FROM vacancy_source_parsing s JOIN company_identity c ON c.id=s.company_id WHERE s.source_id='hh' AND s.enabled=1 AND COALESCE(s.next_run_at,0)<=? AND (?=0 OR EXISTS(SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1)) AND NOT EXISTS(SELECT 1 FROM scheduler_jobs j WHERE j.generation=? AND j.company_id=s.company_id AND j.source_id='hh' AND j.type='scan_source' AND j.status IN ('pending','running')) ORDER BY COALESCE(s.next_run_at,0),COALESCE(s.last_attempt_at,0),s.company_id LIMIT 5 FOR UPDATE`, now, row.favorite, owner, runs.Generation)
	if err != nil {
		return ErrSettingsStorage
	}
	type dueSource struct{ id, next int64 }
	sources := []dueSource{}
	for rows.Next() {
		var source dueSource
		if rows.Scan(&source.id, &source.next) != nil {
			rows.Close()
			return ErrSettingsStorage
		}
		sources = append(sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrSettingsStorage
	}
	for _, candidate := range sources {
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
	update, err := tx.ExecContext(ctx, `UPDATE vacancy_source_parsing SET next_run_at=?,last_attempt_at=?,last_result=?,last_error=? WHERE company_id=? AND source_id='hh' AND enabled=1 AND revision=? AND source_url=?`, now+int64(settings.IntervalDays)*86400000, now, result, "", job.CompanyID, cursor.Operation.SourceRevision, cursor.Operation.SourceURL)
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
