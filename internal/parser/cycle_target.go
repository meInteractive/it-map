package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"itmap/internal/apifysettings"
)

// PrepareTargetedHH закрепляет запрос за одной компанией и одной строкой очереди.
func (c CycleControl) PrepareTargetedHH(ctx context.Context) (func(context.Context, string, int64) (json.RawMessage, error), error) {
	if c.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, ErrSettingsStorage
	}
	return func(ctx context.Context, request string, company int64) (json.RawMessage, error) {
		if !generationPattern.MatchString(request) || company < 1 || company > maxSafeInteger {
			return nil, ErrRunInput
		}
		runs := RunRepository{DB: c.DB, Generation: generation}
		tx, err := runs.lockGeneration(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		row, err := readSettingsRow(ctx, tx, true)
		if err != nil {
			return nil, ErrSettingsStorage
		}
		view, err := settingsView(ctx, tx, row)
		if err != nil {
			return nil, ErrSettingsStorage
		}
		var settings Settings
		if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName == "" {
			return nil, ErrSettingsChanged
		}
		selected := false
		for _, id := range settings.SourceIDs {
			if id == "hh" {
				selected = true
			}
		}
		if !selected {
			return nil, ErrSettingsChanged
		}
		var name string
		if err = tx.QueryRowContext(ctx, "SELECT company_name FROM company_identity WHERE id=? FOR UPDATE", company).Scan(&name); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrHHListingChanged
		} else if err != nil {
			return nil, ErrSettingsStorage
		}
		if row.favorite != 0 {
			var n int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM company_user_state WHERE company_name=? AND user_name=? AND favorite=1", name, settings.OwnerName).Scan(&n); err != nil {
				return nil, ErrSettingsStorage
			}
			if n != 1 {
				return nil, ErrHHListingChanged
			}
		}
		source, err := apifysettings.NewRepository(c.DB, c.secret).ReadHHSourceTx(ctx, tx, company)
		if err != nil {
			if errors.Is(err, apifysettings.ErrSourceMissing) {
				return nil, ErrHHListingChanged
			}
			return nil, err
		}
		if !source.Enabled || !source.Supported {
			return nil, ErrHHListingChanged
		}
		var enabled int
		if err = tx.QueryRowContext(ctx, "SELECT enabled FROM apify_settings WHERE id=1 FOR UPDATE").Scan(&enabled); errors.Is(err, sql.ErrNoRows) || err == nil && enabled != 1 {
			return nil, ErrHHListingChanged
		} else if err != nil {
			return nil, ErrSettingsStorage
		}
		key := stageHash("target-hh:" + request)
		var job string
		var jobCompany sql.NullInt64
		var jobType string
		var jobSource sql.NullString
		err = tx.QueryRowContext(ctx, "SELECT id,company_id,type,source_id FROM scheduler_jobs WHERE generation=? AND dedupe_key=? FOR UPDATE", generation, key).Scan(&job, &jobCompany, &jobType, &jobSource)
		if err == nil {
			if !jobCompany.Valid || jobCompany.Int64 != company || jobType != "scan_source" || !jobSource.Valid || jobSource.String != "hh" {
				return nil, ErrRunBusy
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			var busy int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND company_id=? AND source_id='hh' AND type='scan_source' AND status IN ('pending','running')", generation, company).Scan(&busy); err != nil {
				return nil, ErrSettingsStorage
			}
			if busy != 0 {
				return nil, ErrRunBusy
			}
			var existing int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_runs WHERE generation=? AND id=?", generation, runID(generation, request)).Scan(&existing); err != nil {
				return nil, ErrSettingsStorage
			}
			if existing != 0 {
				return nil, ErrRunBusy
			}
			job, err = stageToken()
			if err != nil {
				return nil, err
			}
			now, err := runClock(ctx, tx)
			if err != nil {
				return nil, ErrSettingsStorage
			}
			payload, err := json.Marshal(map[string]any{"sourceRevision": source.Revision, "sourceUrl": source.URL, "manual": true, "scheduleRevision": settings.Revision})
			if err != nil {
				return nil, ErrRunInput
			}
			priority, ok := settings.Priorities["scan_source"]
			if !ok || priority < 0 || priority > 100 {
				return nil, ErrSettingsStorage
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,'scan_source',?,'hh',?,?,?,4,?,?)`, job, generation, key, company, string(payload), priority, now, now, now); err != nil {
				return nil, ErrSettingsStorage
			}
		} else {
			return nil, ErrSettingsStorage
		}
		if err = tx.Commit(); err != nil {
			return nil, ErrSettingsStorage
		}
		w, err := c.worker(generation)
		if err != nil {
			return nil, err
		}
		w.TargetJobID = job
		w.ScheduleSources = false
		w.ResumeJobID = ""
		w.ResumePreviousDataset = false
		result, err := w.Cycle(ctx, WorkerOptions{Run: RunOptions{RequestID: request, Trigger: "manual"}, MaxJobs: 1})
		if err != nil {
			return nil, err
		}
		if result.Reused {
			var n int
			if err = c.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE run_id=? AND job_id=?", result.Run.ID, job).Scan(&n); err != nil {
				return nil, ErrSettingsStorage
			}
			if n == 0 {
				return nil, ErrRunBusy
			}
		}
		query, _ := json.Marshal(map[string]string{"requestId": request})
		raw, err := (RunReadRepository{DB: c.DB}).read(ctx, query, generation)
		if err != nil {
			return nil, err
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) != nil {
			return nil, ErrSettingsStorage
		}
		body["jobId"], _ = json.Marshal(job)
		if result.Reused {
			body["reused"] = json.RawMessage("true")
		}
		out, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("targeted response: %w", err)
		}
		return out, nil
	}, nil
}
