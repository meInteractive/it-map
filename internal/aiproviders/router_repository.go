package aiproviders

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"math"
	"time"
	"unicode/utf8"
)

var ErrChanged = errors.New("provider settings changed during operation")

type RouterSnapshot struct {
	Configured                                               bool
	Mask                                                     string
	Revision                                                 int64
	Status, Warning                                          string
	Models                                                   []map[string]any
	Primary, SyncedAt, LastUsedModel, LastUsedAt, ActiveTest *string
}

func optional(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
func (r Repository) ReadRouter(ctx context.Context) (RouterSnapshot, error) {
	out := RouterSnapshot{Status: "missing", Models: []map[string]any{}}
	if r.DB == nil {
		return out, ErrStorage
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return out, ErrStorage
	}
	defer tx.Rollback()
	var suffix, cache string
	var primary, synced, lastModel, lastAt sql.NullString
	e = tx.QueryRowContext(ctx, "SELECT key_encrypted<>'',key_suffix,revision,connection_status,last_error,cache_json,primary_model_id,models_synced_at,last_used_model,last_used_at FROM openrouter_settings WHERE id=1").Scan(&out.Configured, &suffix, &out.Revision, &out.Status, &out.Warning, &cache, &primary, &synced, &lastModel, &lastAt)
	if errors.Is(e, sql.ErrNoRows) {
		if tx.Commit() != nil {
			return out, ErrStorage
		}
		return out, nil
	}
	if e != nil {
		return out, ErrStorage
	}
	if suffix != "" {
		out.Mask = "••••" + suffix
	}
	out.Primary = optional(primary)
	out.SyncedAt = optional(synced)
	out.LastUsedModel = optional(lastModel)
	out.LastUsedAt = optional(lastAt)
	if json.Unmarshal([]byte(cache), &out.Models) != nil {
		out.Models = []map[string]any{}
	}
	if out.Models == nil {
		out.Models = []map[string]any{}
	}
	metrics := map[string]map[string]any{}
	rows, e := tx.QueryContext(ctx, "SELECT model_id,success_count,total_duration_ms,last_duration_ms,last_success_at,failure_count,last_failure_duration_ms,last_failure_at FROM openrouter_model_metrics")
	if e != nil {
		return out, ErrStorage
	}
	for rows.Next() {
		var id string
		var successes, total, failures int64
		var last, failureMS sql.NullInt64
		var lastSuccess, lastFailure sql.NullString
		if e = rows.Scan(&id, &successes, &total, &last, &lastSuccess, &failures, &failureMS, &lastFailure); e != nil {
			rows.Close()
			return out, ErrStorage
		}
		var avg any
		if successes != 0 {
			avg = math.Floor(float64(total)/float64(successes) + 0.5)
		}
		var lastMS, badMS any
		if last.Valid {
			lastMS = last.Int64
		}
		if failureMS.Valid {
			badMS = failureMS.Int64
		}
		metrics[id] = map[string]any{"success_count": successes, "average_ms": avg, "last_ms": lastMS, "last_success_at": optional(lastSuccess), "failure_count": failures, "last_failure_ms": badMS, "last_failure_at": optional(lastFailure)}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, ErrStorage
	}
	var tested sql.NullString
	e = tx.QueryRowContext(ctx, "SELECT actual_model FROM openrouter_ai_logs WHERE task='connection_test' AND success=1 AND actual_model IS NOT NULL ORDER BY id DESC LIMIT 1").Scan(&tested)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, ErrStorage
	}
	for _, m := range out.Models {
		if m == nil {
			return out, ErrStorage
		}
		m["letter_timing"] = metrics[text(m["id"])]
		if out.ActiveTest == nil && tested.Valid && (baseID(text(m["id"])) == baseID(tested.String) || baseID(text(m["canonical_slug"])) == baseID(tested.String)) {
			id := text(m["id"])
			out.ActiveTest = &id
		}
	}
	if tx.Commit() != nil {
		return out, ErrStorage
	}
	return out, nil
}
func (s RouterSnapshot) Public() map[string]any {
	empty := ""
	if s.Configured && len(s.Models) == 0 {
		empty = "Нет бесплатных текстовых моделей с контекстом от 262 144 токенов и поддержкой рассуждений."
	}
	return map[string]any{"configured": s.Configured, "key_mask": s.Mask, "connection_status": s.Status, "warning": s.Warning, "models": s.Models, "primary_model_id": s.Primary, "active_test_model_id": s.ActiveTest, "models_synced_at": s.SyncedAt, "last_used_model": s.LastUsedModel, "last_used_at": s.LastUsedAt, "empty_message": empty, "cache_hours": 6}
}
func (r Repository) ResolveRouter(ctx context.Context, revision int64) (string, error) {
	if r.DB == nil {
		return "", ErrStorage
	}
	s, e := read(ctx, r.DB, aihttp.OpenRouter)
	if e != nil {
		return "", e
	}
	if s.revision != revision {
		return "", ErrChanged
	}
	if s.encrypted == "" {
		return "", ErrMissing
	}
	return r.decryptStored(aihttp.OpenRouter, s.encrypted)
}
func (r Repository) routerUpdate(ctx context.Context, g string, revision int64, query string, args ...any) error {
	if r.DB == nil {
		return ErrStorage
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return ErrStorage
	}
	defer tx.Rollback()
	var actual string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual) != nil {
		return ErrStorage
	}
	if actual != g || utf8.RuneCountInString(actual) != 36 {
		return ErrGeneration
	}
	var current int64
	if tx.QueryRowContext(ctx, "SELECT revision FROM openrouter_settings WHERE id=1 FOR UPDATE").Scan(&current) != nil {
		return ErrStorage
	}
	if current != revision {
		return ErrChanged
	}
	if _, e = tx.ExecContext(ctx, query, args...); e != nil {
		return ErrStorage
	}
	if tx.Commit() != nil {
		return ErrStorage
	}
	return nil
}
func (r Repository) CacheRouter(ctx context.Context, g string, revision int64, models []map[string]any, at time.Time) error {
	raw, e := json.Marshal(models)
	if e != nil {
		return ErrStorage
	}
	var primary any
	if len(models) > 0 {
		primary = models[0]["id"]
	}
	return r.routerUpdate(ctx, g, revision, "UPDATE openrouter_settings SET cache_json=?,primary_model_id=?,models_synced_at=?,connection_status='connected',last_error='' WHERE id=1", string(raw), primary, at.UTC().Format("2006-01-02T15:04:05.000Z"))
}
func (r Repository) RouterConnection(ctx context.Context, g string, revision int64, status, warning string) error {
	switch status {
	case "missing", "unchecked", "connected", "error":
	default:
		return ErrStorage
	}
	return r.routerUpdate(ctx, g, revision, "UPDATE openrouter_settings SET connection_status=?,last_error=? WHERE id=1", status, warning)
}
func (r Repository) RouterLogs(ctx context.Context) ([]map[string]any, error) {
	if r.DB == nil {
		return nil, ErrStorage
	}
	rows, e := r.DB.QueryContext(ctx, "SELECT id,created_at,task,requested_primary_model,actual_model,fallback_used,success,error_code FROM openrouter_ai_logs ORDER BY id DESC LIMIT 50")
	if e != nil {
		return nil, ErrStorage
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, fallback, success int64
		var at, task string
		var primary, actual, code sql.NullString
		if rows.Scan(&id, &at, &task, &primary, &actual, &fallback, &success, &code) != nil {
			return nil, ErrStorage
		}
		out = append(out, map[string]any{"id": id, "created_at": at, "task": task, "requested_primary_model": optional(primary), "actual_model": optional(actual), "fallback_used": fallback, "success": success, "error_code": optional(code)})
	}
	if rows.Err() != nil {
		return nil, ErrStorage
	}
	return out, nil
}
