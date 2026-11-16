package apifyruns

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"itmap/internal/integrations/apify"
)

func TestVerifyRecoveryObservation(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	base := Record{ID: "CIrow", State: "running", RunID: "CIRun", DatasetID: "CIData", AccountID: "CIAccount"}
	makeRun := func(fields map[string]any) apify.ActorRun {
		b, _ := json.Marshal(fields)
		return apify.ActorRun{ID: "CIRun", DefaultDatasetID: "CIData", Status: "SUCCEEDED", Raw: b}
	}
	valid := func() map[string]any {
		return map[string]any{"id": "CIRun", "defaultDatasetId": "CIData", "status": "SUCCEEDED", "userId": "CIAccount", "finishedAt": now.Add(-time.Minute).Format(time.RFC3339), "usageTotalUsd": 0.12}
	}
	got, err := VerifyRecoveryObservation(base, "CIAccount", makeRun(valid()), now)
	if err != nil || got.State != "succeeded" || got.ChargedUSD == nil || *got.ChargedUSD != 0.12 || base.ChargedUSD != nil {
		t.Fatal("valid observation rejected", err)
	}
	cases := []struct {
		name  string
		alter func(*Record, *string, map[string]any)
	}{
		{"different account", func(_ *Record, a *string, _ map[string]any) { *a = "CIOther" }},
		{"missing provider owner", func(_ *Record, _ *string, f map[string]any) { delete(f, "userId") }},
		{"different provider owner", func(_ *Record, _ *string, f map[string]any) { f["userId"] = "CIOther" }},
		{"different run", func(_ *Record, _ *string, f map[string]any) { f["id"] = "CIOther" }},
		{"different dataset", func(_ *Record, _ *string, f map[string]any) { f["defaultDatasetId"] = "CIOther" }},
		{"missing cost", func(_ *Record, _ *string, f map[string]any) { delete(f, "usageTotalUsd") }},
		{"negative cost", func(_ *Record, _ *string, f map[string]any) { f["usageTotalUsd"] = -1 }},
		{"missing finish", func(_ *Record, _ *string, f map[string]any) { delete(f, "finishedAt") }},
		{"too recent", func(_ *Record, _ *string, f map[string]any) {
			f["finishedAt"] = now.Add(-time.Second).Format(time.RFC3339)
		}},
		{"future finish", func(_ *Record, _ *string, f map[string]any) {
			f["finishedAt"] = now.Add(time.Hour).Format(time.RFC3339)
		}},
		{"different terminal state", func(r *Record, _ *string, _ map[string]any) { r.State = "failed" }},
		{"starting", func(r *Record, _ *string, _ map[string]any) { r.State = "starting" }},
		{"invalid old cost", func(r *Record, _ *string, _ map[string]any) { v := math.NaN(); r.ChargedUSD = &v }},
		{"decreasing cost", func(r *Record, _ *string, _ map[string]any) { v := 0.2; r.ChargedUSD = &v }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, a, f := base, "CIAccount", valid()
			tc.alter(&r, &a, f)
			if _, err := VerifyRecoveryObservation(r, a, makeRun(f), now); !errors.Is(err, ErrRecoveryEvidence) {
				t.Fatal("unsafe observation accepted", err)
			}
		})
	}
}
