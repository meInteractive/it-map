//go:build mariadb && profilewrites

package storage

import (
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"strings"
	"testing"
)

func TestMariaDBJEVReceiptDisposable(t *testing.T) {
	f := openProfileResumeFixture(t)
	for _, table := range []string{"openrouter_settings", "openrouter_ai_logs"} {
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
	if e = keys.SaveKey(f.ctx, aihttp.OpenRouter, g, "sk-or-"+strings.Repeat("x", 20)); e != nil {
		t.Fatal(e)
	}
	row, e := keys.ReadRouter(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	result := aiproviders.AIResult{Model: aiproviders.JEVModelID, Attempted: []string{aiproviders.JEVModelID}, Message: aiproviders.Message{Content: "CI_PRIVATE_ANSWER"}}
	for _, task := range []string{"relevance", "vacancy_role", "title_classification"} {
		if e = keys.RecordRouterGeneration(f.ctx, g, row.Revision, task, result, nil); e != nil {
			t.Fatal(e)
		}
	}
	if f.count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE actual_model='typesafe/jev-1.13' AND success=1") != 3 {
		t.Fatal("JEV metadata missing")
	}
	failure := &aiproviders.ServiceError{Code: "invalid_output", Message: "CI_PRIVATE_ERROR", Status: 502}
	if e = keys.RecordRouterGeneration(f.ctx, g, row.Revision, "title_classification", result, failure); e != nil {
		t.Fatal(e)
	}
	if f.count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE success=0 AND error_code='invalid_output'") != 1 {
		t.Fatal("partial title failure was success")
	}
	if f.count("SELECT COUNT(*) FROM openrouter_ai_logs WHERE CONCAT(COALESCE(error_code,''),task,COALESCE(actual_model,'')) LIKE '%CI_PRIVATE%'") != 0 {
		t.Fatal("private answer/error leaked")
	}
	f.exec("UPDATE app_runtime_generation SET generation='00000000-0000-4000-8000-000000000001' WHERE id=1")
	e = keys.RecordRouterGeneration(f.ctx, g, row.Revision, "relevance", result, nil)
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", g)
	if !errors.Is(e, aiproviders.ErrGeneration) {
		t.Fatal("restore allowed stale metadata", e)
	}
	if e = keys.SaveKey(f.ctx, aihttp.OpenRouter, g, "sk-or-"+strings.Repeat("y", 20)); e != nil {
		t.Fatal(e)
	}
	if e = keys.RecordRouterGeneration(f.ctx, g, row.Revision, "vacancy_role", result, nil); !errors.Is(e, aiproviders.ErrChanged) {
		t.Fatal("key rotation allowed stale metadata", e)
	}
	if f.count("SELECT COUNT(*) FROM openrouter_ai_logs") != 4 {
		t.Fatal("late metadata inserted")
	}
}
