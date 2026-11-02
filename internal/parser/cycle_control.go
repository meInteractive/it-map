package parser

import (
	"context"
	"database/sql"
	"encoding/json"

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
