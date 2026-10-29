package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"itmap/internal/backup"
)

var ErrRunReadNotFound = errors.New("scheduler saved run not found")

type RunReadRepository struct{ DB *sql.DB }

// PrepareRunRead связывает чтение с поколением до проверки query-параметров.
func (r RunReadRepository) PrepareRunRead(ctx context.Context) (func(context.Context, json.RawMessage) (json.RawMessage, error), error) {
	if r.DB == nil {
		return nil, ErrSettingsStorage
	}
	var generation string
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || !generationPattern.MatchString(generation) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, query json.RawMessage) (json.RawMessage, error) {
		return r.read(ctx, query, generation)
	}, nil
}
func (r RunReadRepository) read(ctx context.Context, query json.RawMessage, expected string) (body json.RawMessage, failure error) {
	defer func() {
		if !errors.Is(failure, ErrRunReadNotFound) {
			failure = settingsFailure(ctx, failure)
		}
	}()
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var generation string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil {
		return nil, e
	}
	if generation != expected {
		return nil, ErrSettingsGeneration
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(query, &input) != nil {
		return nil, &SettingsError{Message: "Передайте UUID запуска."}
	}
	for key := range input {
		if key != "requestId" {
			return nil, &SettingsError{Message: "Неизвестные параметры запроса."}
		}
	}
	var requestID string
	if json.Unmarshal(input["requestId"], &requestID) != nil || !generationPattern.MatchString(requestID) {
		return nil, &SettingsError{Message: "Передайте UUID запуска."}
	}
	rows, e := dashboardRows(ctx, tx, "SELECT id,generation,`trigger`,started_at,finished_at,status,ready,processed,succeeded,errors,remaining,reason,cursor_id,lease_until FROM scheduler_runs WHERE id=? AND generation=?", runID(generation, requestID), generation)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, ErrRunReadNotFound
	}
	if len(rows) != 1 {
		return nil, ErrSettingsStorage
	}
	raw, e := json.Marshal(rows[0])
	if e != nil {
		return nil, e
	}
	body, e = backup.StringifyParsedJSON(raw)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return body, nil
}
