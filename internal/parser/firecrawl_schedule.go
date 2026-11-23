package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const companyScanBacklog = 5

type companyScanPayload struct {
	Version          int    `json:"firecrawlVersion"`
	SourceRevision   int64  `json:"sourceRevision"`
	ScheduleRevision int64  `json:"scheduleRevision"`
	SourceURL        string `json:"sourceUrl"`
	DueAt            int64  `json:"dueAt"`
}

type dueCompanySite struct {
	id, revision, due     int64
	url, config, sourceID string
}

// ScheduleCompanySites queues one bounded batch under an active worker run.
// The scheduler advances each admission slot atomically with its job insert.
func (r RunRepository) ScheduleCompanySites(ctx context.Context, runID, owner string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.lockGeneration(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, err := activeRun(ctx, tx, r.Generation, runID)
	if err != nil {
		return err
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return err
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return err
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil {
		return ErrSettingsStorage
	}
	if !settings.Enabled || settings.OwnerName != owner || (!slices.Contains(settings.SourceIDs, "companySite") && !slices.Contains(settings.SourceIDs, "getmatch")) {
		return nil
	}
	var enabled bool
	if err := tx.QueryRowContext(ctx, "SELECT enabled=1 AND proxy='basic' FROM firecrawl_settings WHERE id=1").Scan(&enabled); err != nil {
		return storageError(ctx)
	}
	if !enabled {
		return nil
	}
	priority, ok := settings.Priorities["scan_source"]
	if !ok || priority < 0 || priority > 100 {
		return ErrSettingsStorage
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND type='scan_source'
		AND source_id IN ('companySite','getmatch') AND status IN ('pending','running')
		AND JSON_EXTRACT(payload,'$.firecrawlVersion')=1`, r.Generation).Scan(&pending); err != nil {
		return storageError(ctx)
	}
	if pending >= companyScanBacklog {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,s.revision,COALESCE(s.next_run_at,0),s.source_url,s.config_json,s.source_id
		FROM vacancy_source_parsing s JOIN company_identity c ON c.id=s.company_id
		WHERE s.source_id IN ('companySite','getmatch') AND s.enabled=1 AND s.auto_enabled=1 AND COALESCE(s.next_run_at,0)<=?
		AND (?=0 OR EXISTS(SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1))
		AND NOT EXISTS(SELECT 1 FROM scheduler_jobs j WHERE j.generation=? AND j.company_id=c.id AND j.source_id=s.source_id
			AND j.type='scan_source' AND j.status IN ('pending','running'))
		ORDER BY COALESCE(s.next_run_at,0), COALESCE(s.last_attempt_at,0), c.id FOR UPDATE`, now, row.favorite, owner, r.Generation)
	if err != nil {
		return storageError(ctx)
	}
	var selected []dueCompanySite
	for rows.Next() {
		var source dueCompanySite
		if err = rows.Scan(&source.id, &source.revision, &source.due, &source.url, &source.config, &source.sourceID); err != nil {
			break
		}
		if _, ok := firecrawlListingHost(source.sourceID, source.url, source.config); ok && slices.Contains(settings.SourceIDs, source.sourceID) {
			selected = append(selected, source)
		}
		if len(selected) >= companyScanBacklog-pending {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return storageError(ctx)
	}
	for _, source := range selected {
		if err := enqueueCompanyScan(ctx, tx, r.Generation, source, settings, priority, now); err != nil {
			return err
		}
	}
	if _, err := activeRun(ctx, tx, r.Generation, runID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}

func enqueueCompanyScan(ctx context.Context, tx *sql.Tx, generation string, source dueCompanySite, settings Settings, priority, now int64) error {
	if settings.IntervalDays != 1 && settings.IntervalDays != 3 && settings.IntervalDays != 7 {
		return ErrSettingsStorage
	}
	payload, err := json.Marshal(companyScanPayload{Version: 1, SourceRevision: source.revision, ScheduleRevision: settings.Revision, SourceURL: source.url, DueAt: source.due})
	if err != nil {
		return err
	}
	id, err := stageToken()
	if err != nil {
		return err
	}
	keyPrefix := "firecrawl:scan:"
	if source.sourceID == "getmatch" {
		keyPrefix += "getmatch:"
	}
	key := stageHash(fmt.Sprintf("%s%d:%d:%d:%d", keyPrefix, source.id, source.revision, settings.Revision, source.due))
	if _, err := tx.ExecContext(ctx, `INSERT INTO scheduler_jobs
		(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at)
		VALUES(?,?,?,'scan_source',?,?,?,?,?,1,?,?) ON DUPLICATE KEY UPDATE id=id`,
		id, generation, key, source.id, source.sourceID, string(payload), priority, now, now, now); err != nil {
		return storageError(ctx)
	}
	next := now + int64(settings.IntervalDays)*int64(24*time.Hour/time.Millisecond)
	if _, err := tx.ExecContext(ctx, `UPDATE vacancy_source_parsing SET interval_days=?,next_run_at=?
		WHERE company_id=? AND source_id=? AND revision=? AND source_url=?`, settings.IntervalDays, next, source.id, source.sourceID, source.revision, source.url); err != nil {
		return storageError(ctx)
	}
	return nil
}

func completeCompanyScan(ctx context.Context, tx *sql.Tx, job Job) error {
	var payload companyScanPayload
	if job.CompanyID == nil || json.Unmarshal(job.Payload, &payload) != nil || payload.Version != 1 {
		return ErrJobInput
	}
	now, err := runClock(ctx, tx)
	if err != nil {
		return storageError(ctx)
	}
	_, err = tx.ExecContext(ctx, `UPDATE vacancy_source_parsing SET last_success_at=?, last_attempt_at=?, last_error='',last_result='success'
		WHERE company_id=? AND source_id=? AND revision=? AND source_url=? AND enabled=1`,
		time.UnixMilli(now).UTC().Format(firecrawlPeriodFormat), now, job.CompanyID, job.SourceID, payload.SourceRevision, payload.SourceURL)
	if err != nil {
		return storageError(ctx)
	}
	return nil
}
