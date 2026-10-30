//go:build mariadb && profilewrites

package storage

import (
	"context"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/parser"
	"itmap/internal/profiles"
	"strings"
	"testing"
)

type manualRunner func(context.Context, profiles.ModelInput) (aiproviders.AIResult, error)

func (f manualRunner) ConnectionTest(ctx context.Context, c profiles.ModelInput) (aiproviders.AIResult, error) {
	return f(ctx, c)
}

type manualPublic struct{}

func (manualPublic) Public(context.Context, bool) (map[string]any, error) {
	return map[string]any{"configured": true}, nil
}
func TestMariaDBAIManualTestDisposable(t *testing.T) {
	f := openProfileResumeFixture(t)
	for _, table := range []string{"openai_settings", "openrouter_settings", "openrouter_ai_logs"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("requires empty synthetic AI tables")
		}
		table := table
		t.Cleanup(func() { f.exec("DELETE FROM " + table) })
	}
	keys := aiproviders.NewRepository(f.db, "CI_SYNTHETIC_SECRET", "")
	g, e := keys.Generation(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []aihttp.Provider{aihttp.OpenAI, aihttp.OpenRouter} {
		prefix := "sk-"
		if p == aihttp.OpenRouter {
			prefix += "or-"
		}
		if e = keys.SaveKey(f.ctx, p, g, prefix+strings.Repeat("x", 20)); e != nil {
			t.Fatal(e)
		}
	}
	runner := &aiproviders.ManualTests{Keys: keys, OpenAI: manualPublic{}, Router: manualPublic{}}
	runner.Runner = manualRunner(func(_ context.Context, c profiles.ModelInput) (aiproviders.AIResult, error) {
		if c.Provider != "openrouter" {
			t.Fatal("wrong provider")
		}
		return aiproviders.AIResult{Model: "vendor/b", Attempted: []string{"vendor/a", "vendor/b"}, Fallback: true, Message: aiproviders.Message{Content: "CI_PRIVATE_RESPONSE"}}, nil
	})
	out, e := runner.Test(f.ctx, aihttp.OpenRouter)
	if e != nil || out["model"] != "vendor/b" {
		t.Fatal("manual test failed", e)
	}
	if f.count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='connection_test' AND requested_primary_model='vendor/a' AND actual_model='vendor/b' AND fallback_used=1 AND success=1") != 1 {
		t.Fatal("metadata receipt missing")
	}
	if f.count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE CONCAT(COALESCE(error_code,''),task,COALESCE(actual_model,'')) LIKE '%CI_PRIVATE%'") != 0 {
		t.Fatal("prompt/response leaked to logs")
	}
	runner.Runner = manualRunner(func(context.Context, profiles.ModelInput) (aiproviders.AIResult, error) {
		f.exec("UPDATE app_runtime_generation SET generation='00000000-0000-4000-8000-000000000001' WHERE id=1")
		return aiproviders.AIResult{Model: "stale"}, nil
	})
	_, e = runner.Test(f.ctx, aihttp.OpenRouter)
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", g)
	if !errors.Is(e, aiproviders.ErrGeneration) || f.count("SELECT COUNT(*) FROM openrouter_ai_logs") != 1 {
		t.Fatal("restore allowed stale result/log")
	}
	runner.Runner = manualRunner(func(context.Context, profiles.ModelInput) (aiproviders.AIResult, error) {
		if e := keys.SaveKey(f.ctx, aihttp.OpenRouter, g, "sk-"+"or-"+strings.Repeat("y", 20)); e != nil {
			t.Fatal(e)
		}
		return aiproviders.AIResult{Model: "stale"}, nil
	})
	if _, e = runner.Test(f.ctx, aihttp.OpenRouter); !errors.Is(e, aiproviders.ErrChanged) || f.count("SELECT COUNT(*) FROM openrouter_ai_logs") != 1 {
		t.Fatal("rotated key accepted stale receipt")
	}
}

var _ aiproviders.Billing = (*parser.Attempt)(nil)
