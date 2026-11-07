//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"itmap/internal/parser"
)

func TestMariaDBSchedulerAISpendingDebugDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := dashboardFixture(t)
	now := time.Date(2026, 11, 7, 16, 0, 0, 0, time.UTC)
	day := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC).UnixMilli()
	for i := 0; i < 13; i++ {
		jobID := fmt.Sprintf("CI_AI_JOB_%02d", i)
		provider := "openai"
		if i%2 == 1 { provider = "openrouter" }
		if i == 12 { provider = "firecrawl" }
		at := day + int64(i*1000)
		if i == 11 { at = time.Date(2026,10,30,12,0,0,0,time.UTC).UnixMilli() }
		f.exec(`INSERT INTO scheduler_jobs
		  (id,generation,dedupe_key,type,source_id,payload,status,priority,run_after,created_at,updated_at)
		  VALUES(?,?,?,'classify_title','hh','{}','completed',1,?,?,?)`,
			jobID, f.generation, "CI_AI_DEDUPE_"+jobID, at, at, at)
		var cost any = 0.01
		var reserve any = 0.02
		if i == 9 { cost = 0.0 }
		if i == 10 { cost = nil; reserve = 0.05 }
		f.exec(`INSERT INTO scheduler_attempts
		  (id,job_id,run_id,type,source_id,started_at,status,provider,model,reserved_usd,cost_usd)
		  VALUES(?,?,'CI_AI_SYNTHETIC_RUN','classify_title','hh',?,'completed',?,'ci-model',?,?)`,
			fmt.Sprintf("CI_AI_ATT_%02d", i), jobID, at, provider, reserve, cost)
	}
	before := dashboardFingerprint(f)
	repo := parser.AISpendingDebugRepository{DB: f.reader, Now: func() time.Time{return now}}
	raw, err := repo.ReadAISpending(context.Background())
	if err != nil { t.Fatal(err) }
	var report struct {
		Total int `json:"total"`
		Month struct {
			ConfirmedUsd float64 `json:"confirmedUsd"`
			UnknownReservedUsd float64 `json:"unknownReservedUsd"`
			UpperEstimateUsd float64 `json:"upperEstimateUsd"`
			UnknownCostOperations int `json:"unknownCostOperations"`
		} `json:"month"`
		Latest []struct {
			AttemptID string `json:"attemptId"`
			CostUsd *float64 `json:"costUsd"`
		} `json:"latest"`
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(raw, &report); err != nil { t.Fatal(err) }
	if report.Total != 12 || len(report.Latest) != 10 ||
		report.Latest[0].AttemptID != "CI_AI_ATT_10" ||
		report.Latest[0].CostUsd != nil ||
		math.Abs(report.Month.ConfirmedUsd-0.09)>1e-9 ||
		math.Abs(report.Month.UnknownReservedUsd-0.05)>1e-9 ||
		math.Abs(report.Month.UpperEstimateUsd-0.14)>1e-9 ||
		report.Month.UnknownCostOperations != 1 || len(report.Models) != 2 {
		t.Fatalf("unexpected local AI ledger: %s",raw)
	}
	if strings.Contains(string(raw), "CI_AI_DEDUPE") || strings.Contains(string(raw), "firecrawl") ||
		strings.Contains(string(raw), "payload") || strings.Contains(string(raw), "result_json") {
		t.Fatal("private job/provider data leaked")
	}
	if dashboardFingerprint(f)!=before {t.Fatal("read-only report changed MariaDB")}
}
