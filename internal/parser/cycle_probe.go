package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
	"unicode/utf16"
)

// PrepareProbe создаёт только диагностическое задание без сетевых провайдеров.
func (c CycleControl) PrepareProbe(ctx context.Context) (func(context.Context, string) (json.RawMessage, error), error) {
	if c.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return c.probe(generation), nil
}
func (c CycleControl) probe(generation string) func(context.Context, string) (json.RawMessage, error) {
	return func(ctx context.Context, requestID string) (json.RawMessage, error) {
		if len(utf16.Encode([]rune(requestID))) > 100 {
			return nil, &SettingsError{Message: "Некорректный сигнал."}
		}
		if requestID == "" {
			var err error
			requestID, err = stageToken()
			if err != nil {
				return nil, err
			}
		}
		runRequest := "probe:" + requestID
		runs := RunRepository{DB: c.DB, Generation: generation}
		start, err := runs.Begin(ctx, RunOptions{RequestID: runRequest, Trigger: "probe", Duration: 10 * time.Second, Diagnostic: true})
		if err != nil {
			return nil, err
		}
		if !start.Acquired {
			return c.readProbe(ctx, generation, start.Run.ID, requestID, true)
		}
		id, err := stageToken()
		if err != nil {
			return nil, err
		}
		tx, err := runs.lockGeneration(ctx)
		if err != nil {
			return nil, err
		}
		now, err := activeRun(ctx, tx, generation, start.Run.ID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		payload, _ := json.Marshal(map[string]string{"nonce": requestID})
		_, err = tx.ExecContext(ctx, `INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,'diagnostic',?,0,?,4,?,?)`, id, generation, stageHash("probe:"+requestID), string(payload), now, now, now)
		if err != nil {
			tx.Rollback()
			return nil, storageError(ctx)
		}
		if err = tx.Commit(); err != nil {
			return nil, storageError(ctx)
		}
		if err = runs.reportCycle(ctx, start.Run.ID, 0, true); err != nil {
			return nil, err
		}
		lease, err := runs.ClaimJob(ctx, start.Run.ID, ClaimOptions{JobID: &id, Types: []string{"diagnostic"}, Diagnostic: true, Lease: 10 * time.Second})
		if err != nil {
			return nil, err
		}
		if lease == nil {
			return nil, ErrJobInput
		}
		attempt, err := lease.StartAttempt(ctx)
		if err != nil {
			return nil, err
		}
		if err = attempt.Finish(ctx, AttemptFinish{Job: FinishOptions{Status: "completed"}}); err != nil {
			return nil, err
		}
		if err = runs.reportCycle(ctx, start.Run.ID, 1, false); err != nil {
			return nil, err
		}
		if err = runs.Finish(ctx, start.Run.ID, "completed"); err != nil {
			return nil, err
		}
		return c.readProbe(ctx, generation, start.Run.ID, requestID, false)
	}
}

func (c CycleControl) readProbe(ctx context.Context, generation, runID, nonce string, reused bool) (json.RawMessage, error) {
	tx, err := c.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, settingsFailure(ctx, err)
	}
	defer tx.Rollback()
	var current string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&current) != nil {
		return nil, ErrSettingsStorage
	}
	if current != generation {
		return nil, ErrSettingsGeneration
	}
	runs, err := dashboardRows(ctx, tx, "SELECT id,generation,`trigger`,started_at,finished_at,status,ready,processed,succeeded,errors,remaining,reason,cursor_id,lease_until FROM scheduler_runs WHERE id=? AND generation=?", runID, generation)
	if err != nil || len(runs) != 1 {
		return nil, ErrSettingsStorage
	}
	jobs, err := dashboardRows(ctx, tx, "SELECT id,status FROM scheduler_jobs WHERE generation=? AND type='diagnostic' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.nonce'))=?", generation, nonce)
	if err != nil || len(jobs) > 1 {
		return nil, ErrSettingsStorage
	}
	var job any
	if len(jobs) == 1 {
		job = jobs[0]
	}
	if reused {
		runs[0]["reused"] = true
	}
	body, err := json.Marshal(map[string]any{"run": runs[0], "job": job, "externalRequests": 0})
	if err != nil {
		return nil, ErrSettingsStorage
	}
	if tx.Commit() != nil {
		return nil, ErrSettingsStorage
	}
	return body, nil
}
