package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// PrepareResume фиксирует поколение до чтения запроса и запускает только одно
// явно указанное отложенное HH-задание с уже известным dataset.
func (c CycleControl) PrepareResume(ctx context.Context) (func(context.Context, string, string) (json.RawMessage, error), error) {
	if c.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, requestID, jobID string) (json.RawMessage, error) {
		if !generationPattern.MatchString(requestID) || !generationPattern.MatchString(jobID) {
			return nil, ErrRunInput
		}
		var exists int
		id := runID(generation, requestID)
		if err := c.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_runs WHERE id=? AND generation=?", id, generation).Scan(&exists); err != nil {
			return nil, settingsFailure(ctx, err)
		}
		if exists != 0 {
			var matching int
			if err := c.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE run_id=? AND job_id=?", id, jobID).Scan(&matching); err != nil {
				return nil, settingsFailure(ctx, err)
			}
			if matching == 0 {
				return nil, ErrRunBusy
			}
			query, _ := json.Marshal(map[string]string{"requestId": requestID})
			raw, err := (RunReadRepository{DB: c.DB}).read(ctx, query, generation)
			if err != nil {
				return nil, err
			}
			var body map[string]json.RawMessage
			if json.Unmarshal(raw, &body) != nil {
				return nil, ErrSettingsStorage
			}
			body["reused"] = json.RawMessage("true")
			return json.Marshal(body)
		}
		tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
		if err != nil {
			return nil, settingsFailure(ctx, err)
		}
		defer tx.Rollback()
		var actual, owner string
		var data []byte
		err = tx.QueryRowContext(ctx, `SELECT g.generation,d.user_name,d.result_json FROM scheduler_jobs j JOIN app_runtime_generation g ON g.id=1 AND g.generation=j.generation JOIN vacancy_discovery_runs d ON d.id=CONCAT('go399:',j.id) AND d.generation=j.generation AND d.company_id=j.company_id AND d.source_id='hh' JOIN vacancy_source_parsing s ON s.company_id=j.company_id AND s.source_id='hh' AND s.enabled=1 JOIN apify_settings a ON a.id=1 AND a.enabled=1 WHERE j.id=? AND j.generation=? AND j.status='pending' AND j.type='scan_source' AND j.source_id='hh' AND j.agency_id IS NULL`, jobID, generation).Scan(&actual, &owner, &data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrHHListingChanged
		}
		if err != nil {
			return nil, settingsFailure(ctx, err)
		}
		var cursor hhListingCursor
		if actual != generation || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Done || cursor.RunID == "" || cursor.DatasetID == "" || cursor.Metadata == nil {
			return nil, ErrHHListingChanged
		}
		row, err := readSettingsRow(ctx, tx, false)
		if err != nil {
			return nil, settingsFailure(ctx, err)
		}
		view, err := settingsView(ctx, tx, row)
		if err != nil {
			return nil, settingsFailure(ctx, err)
		}
		var settings Settings
		if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || settings.OwnerName != owner {
			return nil, ErrHHListingChanged
		}
		if err = tx.Commit(); err != nil {
			return nil, settingsFailure(ctx, err)
		}
		w, err := c.worker(generation)
		if err != nil {
			return nil, err
		}
		w.ResumePreviousDataset = true
		w.ResumeJobID = jobID
		result, err := w.Cycle(ctx, WorkerOptions{Run: RunOptions{RequestID: requestID, Trigger: "manual"}, MaxJobs: 1})
		if err != nil {
			return nil, err
		}
		query, _ := json.Marshal(map[string]string{"requestId": requestID})
		raw, err := (RunReadRepository{DB: c.DB}).read(ctx, query, generation)
		if err != nil {
			return nil, err
		}
		if result.Reused {
			var sameJob int
			if err = c.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE run_id=? AND job_id=?", result.Run.ID, jobID).Scan(&sameJob); err != nil {
				return nil, settingsFailure(ctx, err)
			}
			if sameJob == 0 {
				return nil, ErrRunBusy
			}
			var body map[string]json.RawMessage
			if json.Unmarshal(raw, &body) != nil {
				return nil, ErrSettingsStorage
			}
			body["reused"] = json.RawMessage("true")
			return json.Marshal(body)
		}
		return raw, nil
	}, nil
}
