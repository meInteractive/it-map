package aiproviders

import (
	"context"
	"database/sql"
	"errors"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
	"regexp"
	"time"
)

type ConnectionRunner interface {
	ConnectionTest(context.Context, profiles.ModelInput) (AIResult, error)
}
type PublicProvider interface {
	Public(context.Context, bool) (map[string]any, error)
}
type ManualTests struct {
	Keys           Repository
	Runner         ConnectionRunner
	OpenAI, Router PublicProvider
}

func (t *ManualTests) Test(ctx context.Context, p aihttp.Provider) (map[string]any, error) {
	if t == nil || t.Keys.DB == nil || t.Runner == nil || !validProvider(p) {
		return nil, ErrStorage
	}
	generation, e := t.Keys.Generation(ctx)
	if e != nil {
		return nil, e
	}
	settings, e := t.Keys.Read(ctx, p)
	if e != nil {
		return nil, e
	}
	cfg := profiles.ModelInput{Provider: string(p), OpenRouterModel: "best_free"}
	if p == aihttp.OpenAI {
		models, e := (profiles.Repository{DB: t.Keys.DB}).ReadModels(ctx)
		if e != nil {
			return nil, ErrStorage
		}
		c := models["cover_letter"]
		cfg.OpenAIModel = c.OpenAIModel
		cfg.ReasoningEffort = c.ReasoningEffort
	}
	result, e := t.Runner.ConnectionTest(ctx, cfg)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if p == aihttp.OpenRouter {
		if write := t.Keys.recordRouterTest(ctx, generation, settings.Revision, result, e); write != nil {
			return nil, write
		}
	} else {
		latest, check := t.Keys.Generation(ctx)
		if check != nil {
			return nil, check
		}
		if latest != generation {
			return nil, ErrGeneration
		}
		current, check := t.Keys.Read(ctx, p)
		if check != nil {
			return nil, check
		}
		if current.Revision != settings.Revision {
			return nil, ErrChanged
		}
	}
	if e != nil {
		return nil, e
	}
	service := t.OpenAI
	if p == aihttp.OpenRouter {
		service = t.Router
	}
	if service == nil {
		return nil, ErrStorage
	}
	state, e := service.Public(ctx, false)
	if e != nil {
		return nil, e
	}
	response := map[string]any{"ok": true, "model": result.Model, "state": state}
	if p == aihttp.OpenRouter {
		response["fallback_used"] = result.Fallback
		response["attempted_models"] = result.Attempted
	}
	return response, nil
}

var safeLogCode = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

func (r Repository) recordRouterTest(ctx context.Context, g string, revision int64, result AIResult, failure error) error {
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
	if actual != g {
		return ErrGeneration
	}
	var current int64
	if e = tx.QueryRowContext(ctx, "SELECT revision FROM openrouter_settings WHERE id=1 FOR UPDATE").Scan(&current); errors.Is(e, sql.ErrNoRows) {
		return ErrMissing
	} else if e != nil {
		return ErrStorage
	}
	if current != revision {
		return ErrChanged
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	success := 0
	var primary, model, code any
	if len(result.Attempted) > 0 {
		primary = result.Attempted[0]
	}
	if failure == nil {
		success = 1
		model = result.Model
		if _, e = tx.ExecContext(ctx, "UPDATE openrouter_settings SET last_used_model=?,last_used_at=? WHERE id=1", result.Model, now); e != nil {
			return ErrStorage
		}
	} else {
		value := "internal"
		var provider *aihttp.Error
		var service *ServiceError
		if errors.As(failure, &provider) {
			value = provider.Code
		} else if errors.As(failure, &service) {
			value = service.Code
		} else if errors.Is(failure, ErrMissing) {
			value = "missing_key"
		} else if errors.Is(failure, ErrDecrypt) {
			value = "decrypt"
		}
		if !safeLogCode.MatchString(value) {
			value = "internal"
		}
		code = value
	}
	fallback := 0
	if result.Fallback {
		fallback = 1
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO openrouter_ai_logs(created_at,task,requested_primary_model,actual_model,fallback_used,success,error_code) VALUES(?,'connection_test',?,?,?,?,?)", now, primary, model, fallback, success, code); e != nil {
		return ErrStorage
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM openrouter_ai_logs WHERE id NOT IN (SELECT id FROM (SELECT id FROM openrouter_ai_logs ORDER BY id DESC LIMIT 200) AS retained)"); e != nil {
		return ErrStorage
	}
	if tx.Commit() != nil {
		return ErrStorage
	}
	return nil
}
