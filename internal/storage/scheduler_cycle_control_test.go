//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"itmap/internal/parser"
)

func TestMariaDBSchedulerCycleControlDisposable(t *testing.T) {
	f := billingFixture(t)
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false,"ownerName":"CI_OWNER","sourceIds":["hh"]}' WHERE id=1`)
	control := parser.NewCycleControl(f.db, "CI_SECRET", "")
	run, err := control.PrepareCycle(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := run(f.ctx, "12345678-1234-4234-8234-123456789abc")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Status != "paused" {
		t.Fatal("disabled cycle", result)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs") != 0 || f.count("SELECT COUNT(*) FROM scheduler_attempts") != 0 {
		t.Fatal("disabled cycle created work")
	}
	tick, err := control.PrepareTick(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = tick(f.ctx, "CI_DISABLED_TICK")
	if err != nil || json.Unmarshal(raw, &result) != nil || result.Status != "paused" {
		t.Fatal("disabled tick", err)
	}
	replay, err := tick(f.ctx, "CI_DISABLED_TICK")
	if err != nil || !strings.Contains(string(replay), `"reused":true`) || f.count("SELECT COUNT(*) FROM scheduler_jobs") != 0 {
		t.Fatal("tick replay", err)
	}
	stale, err := control.PrepareCycle(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.repo.Generation)
	f.exec("UPDATE app_runtime_generation SET generation='22222222-2222-4333-8444-555555555555' WHERE id=1")
	if _, err = stale(f.ctx, "12345678-1234-4234-8234-123456789abd"); !errors.Is(err, parser.ErrRunGeneration) {
		t.Fatal("stale generation accepted", err)
	}
}
