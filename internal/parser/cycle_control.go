package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf16"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
)

// CycleControl фиксирует поколение до чтения входного тела HTTP.
// Он сам не создаёт таймер и не запускает цикл при создании.
type CycleControl struct {
	DB                   *sql.DB
	secret, openAIEnvKey string
}

func NewCycleControl(db *sql.DB, secret, openAIEnvKey string) CycleControl {
	return CycleControl{DB: db, secret: secret, openAIEnvKey: openAIEnvKey}
}
func (c CycleControl) String() string   { return "CycleControl{credentials:redacted}" }
func (c CycleControl) GoString() string { return c.String() }
func (c CycleControl) PrepareCycle(ctx context.Context) (func(context.Context, string) (json.RawMessage, error), error) {
	if c.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, id string) (json.RawMessage, error) {
		if !generationPattern.MatchString(id) {
			return nil, &SettingsError{Message: "Передайте UUID запуска."}
		}
		runs := RunRepository{DB: c.DB, Generation: generation}
		w, err := NewWorker(runs, apifysettings.NewRepository(c.DB, c.secret), aiproviders.NewRepository(c.DB, c.secret, c.openAIEnvKey))
		if err != nil {
			return nil, err
		}
		w.ScheduleSources = true
		result, err := w.Cycle(ctx, WorkerOptions{Run: RunOptions{RequestID: id, Trigger: "manual"}, MaxJobs: 50})
		if err != nil {
			return nil, err
		}
		query, _ := json.Marshal(map[string]string{"requestId": id})
		raw, err := (RunReadRepository{DB: c.DB}).read(ctx, query, generation)
		if err != nil {
			return nil, err
		}
		if result.Reused {
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

// PrepareTick фиксирует поколение до проверки входного сигнала.
func (c CycleControl) PrepareTick(ctx context.Context) (func(context.Context, string, bool) (json.RawMessage, error), error) {
	if c.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if err := c.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || !generationPattern.MatchString(generation) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, id string, diagnostic bool) (json.RawMessage, error) {
		if diagnostic {
			return c.probe(generation)(ctx, id)
		}
		if len(utf16.Encode([]rune(id))) > 100 {
			return nil, &SettingsError{Message: "Некорректный сигнал."}
		}
		if id == "" {
			id = fmt.Sprintf("hour:%d", time.Now().UnixMilli()/3600000)
		}
		runs := RunRepository{DB: c.DB, Generation: generation}
		w, err := NewWorker(runs, apifysettings.NewRepository(c.DB, c.secret), aiproviders.NewRepository(c.DB, c.secret, c.openAIEnvKey))
		if err != nil {
			return nil, err
		}
		w.ScheduleSources = true
		result, err := w.Cycle(ctx, WorkerOptions{Run: RunOptions{RequestID: id, Trigger: "automatic"}, MaxJobs: 50})
		if err != nil {
			return nil, err
		}
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
		rows, err := dashboardRows(ctx, tx, "SELECT id,generation,`trigger`,started_at,finished_at,status,ready,processed,succeeded,errors,remaining,reason,cursor_id,lease_until FROM scheduler_runs WHERE id=? AND generation=?", runID(generation, id), generation)
		if err != nil || len(rows) != 1 {
			return nil, ErrSettingsStorage
		}
		if result.Reused {
			rows[0]["reused"] = true
		}
		raw, err := json.Marshal(rows[0])
		if err != nil {
			return nil, ErrSettingsStorage
		}
		if tx.Commit() != nil {
			return nil, ErrSettingsStorage
		}
		return raw, nil
	}, nil
}
