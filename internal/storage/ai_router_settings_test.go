//go:build mariadb && profilewrites

package storage

import (
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMariaDBRouterSettingsDisposable(t *testing.T) {
	f := openProfileResumeFixture(t)
	tables := []string{"openrouter_settings", "openrouter_model_metrics", "openrouter_ai_logs"}
	for _, table := range tables {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("requires empty synthetic router tables")
		}
		table := table
		t.Cleanup(func() { f.exec("DELETE FROM " + table) })
	}
	fault, db := openMutationFaultPool(t, "")
	read := aiproviders.NewRepository(db, "CI_SYNTHETIC_SECRET", "")
	empty, e := read.ReadRouter(f.ctx)
	fault.mu.Lock()
	writes := fault.writes
	fault.mu.Unlock()
	if e != nil || empty.Configured || len(empty.Models) != 0 || writes != 0 {
		t.Fatal("router GET initialized/wrote settings")
	}
	path := filepath.Join("..", "aiproviders", "testdata", "router-public-contract.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	actual, e := exec.Command("node", "../../scripts/check-openrouter-public.mjs").Output()
	if e != nil {
		t.Fatal("source oracle failed", e)
	}
	var want, got any
	_ = json.Unmarshal(raw, &want)
	_ = json.Unmarshal(actual, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatal("source router public fixture drift")
	}
	var frames []struct {
		Row      map[string]any
		Metrics  []map[string]any
		Tested   string
		Expected map[string]any
	}
	if json.Unmarshal(actual, &frames) != nil || len(frames) != 4 {
		t.Fatal("bad source corpus")
	}
	r := aiproviders.NewRepository(f.db, "CI_SYNTHETIC_SECRET", "")
	for _, v := range frames {
		f.exec("DELETE FROM openrouter_settings")
		f.exec("DELETE FROM openrouter_model_metrics")
		f.exec("DELETE FROM openrouter_ai_logs")
		x := v.Row
		f.exec("INSERT INTO openrouter_settings(id,key_encrypted,key_suffix,revision,connection_status,last_error,cache_json,primary_model_id,models_synced_at,last_used_model,last_used_at) VALUES(1,?,?,?,?,?,?,?,?,?,?)", x["key_encrypted"], x["key_suffix"], x["revision"], x["connection_status"], x["last_error"], x["cache_json"], x["primary_model_id"], x["models_synced_at"], x["last_used_model"], x["last_used_at"])
		for _, m := range v.Metrics {
			f.exec("INSERT INTO openrouter_model_metrics(model_id,success_count,total_duration_ms,last_duration_ms,last_success_at,failure_count,last_failure_duration_ms,last_failure_at) VALUES(?,?,?,?,?,?,?,?)", m["model_id"], m["success_count"], m["total_duration_ms"], m["last_duration_ms"], m["last_success_at"], m["failure_count"], m["last_failure_duration_ms"], m["last_failure_at"])
		}
		f.exec("INSERT INTO openrouter_ai_logs(created_at,task,actual_model,success) VALUES('2026-10-30T00:00:00.000Z','connection_test',?,1)", v.Tested)
		s, e := r.ReadRouter(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(s.Public())
		var projection map[string]any
		_ = json.Unmarshal(b, &projection)
		if !reflect.DeepEqual(v.Expected, projection) {
			t.Fatal("full source router public DTO differs")
		}
	}
	for _, table := range tables {
		f.exec("DELETE FROM " + table)
	}
	g, e := r.Generation(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	key := "sk-" + "or-" + strings.Repeat("x", 20)
	if e = r.SaveKey(f.ctx, aihttp.OpenRouter, g, key); e != nil {
		t.Fatal(e)
	}
	models := []map[string]any{{"id": "synthetic-model"}}
	if e = r.CacheRouter(f.ctx, g, 1, models, time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = r.CacheRouter(f.ctx, "stale-generation", 1, nil, time.Now()); !errors.Is(e, aiproviders.ErrGeneration) {
		t.Fatal("stale generation cache accepted")
	}
	if e = r.SaveKey(f.ctx, aihttp.OpenRouter, g, key); e != nil {
		t.Fatal(e)
	}
	if e = r.CacheRouter(f.ctx, g, 1, nil, time.Now()); !errors.Is(e, aiproviders.ErrChanged) {
		t.Fatal("old key revision cache overwrote new key state")
	}
	s, e := r.ReadRouter(f.ctx)
	if e != nil || len(s.Models) != 1 || s.Models[0]["id"] != "synthetic-model" {
		t.Fatal("failed cache update lost prior models")
	}
	if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()") != 72 {
		t.Fatal("router settings changed schema")
	}
}
