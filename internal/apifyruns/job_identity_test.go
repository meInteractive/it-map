package apifyruns

import (
	"math"
	"strings"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

func TestHHJobKeyBindsStableJobAndStepWithoutExposingInputs(t *testing.T) {
	generation := "11111111-2222-4333-8444-555555555555"
	job := "CI secret-shaped job"
	step := "CI page 1"
	key, err := HHJobKey(generation, job, step)
	if err != nil {
		t.Fatal(err)
	}
	again, err := HHJobKey(generation, job, step)
	if err != nil || again != key {
		t.Fatal("retry identity changed", err)
	}
	if strings.Contains(key, job) || strings.Contains(key, step) || strings.Contains(key, generation) || !strings.HasPrefix(key, "go431job:") {
		t.Fatal("identity exposes source input")
	}
	for _, input := range [][3]string{{"11111111-2222-4333-8444-555555555556", job, step}, {generation, job + "x", step}, {generation, job, step + "x"}, {generation, "ab", "c"}, {generation, "a", "bc"}} {
		next, err := HHJobKey(input[0], input[1], input[2])
		if err != nil || next == key {
			t.Fatal("different operation reused key", err)
		}
	}
	a, _ := HHJobKey(generation, "ab", "c")
	b, _ := HHJobKey(generation, "a", "bc")
	if a == b {
		t.Fatal("ambiguous serialization")
	}
	v := apifysettings.OperationVersion{Generation: generation, CompanyID: 991302, SourceRevision: 1, SourceURL: "https://hh.ru/employer/456"}
	identity, err := HHOperationIdentity(v, key, apify.HHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	v.SourceRevision++
	changed, err := HHOperationIdentity(v, key, apify.HHOptions{})
	if err != nil || identity == changed {
		t.Fatal("changed source reused provider operation", err)
	}
	for _, input := range [][3]string{{"", job, step}, {generation, "", step}, {generation, job, ""}, {generation, strings.Repeat("a", 257), step}, {generation, job, strings.Repeat("a", 513)}, {generation, string([]byte{0xff}), step}} {
		if _, err := HHJobKey(input[0], input[1], input[2]); err == nil {
			t.Fatal("invalid job identity accepted")
		}
	}
}

func TestRecoveryEvidenceNeverTreatsUnknownAsRetryPermission(t *testing.T) {
	cost := 0.08
	zero := 0.0
	negative := -1.0
	nan := math.NaN()
	tests := []struct {
		name, state, run, dataset string
		cost                      *float64
		want                      RecoveryEvidence
		invalid                   bool
	}{
		{"unclaimed", "starting", "", "", nil, RecoveryUnknown, false},
		{"lost response", "uncertain", "", "", nil, RecoveryUnknown, false},
		{"known running", "running", "Run123", "Data123", nil, RecoveryReconcile, false},
		{"known uncertain", "uncertain", "Run123", "Data123", nil, RecoveryReconcile, false},
		{"terminal unknown cost", "succeeded", "Run123", "Data123", nil, RecoveryReconcile, false},
		{"terminal recorded cost", "succeeded", "Run123", "Data123", &cost, RecoveryTerminalCostRecorded, false},
		{"terminal recorded zero", "failed", "Run123", "Data123", &zero, RecoveryTerminalCostRecorded, false},
		{"missing dataset", "running", "Run123", "", nil, "", true},
		{"missing run", "failed", "", "", nil, "", true},
		{"starting with run", "starting", "Run123", "Data123", nil, "", true},
		{"active cost", "running", "Run123", "Data123", &cost, "", true},
		{"negative cost", "failed", "Run123", "Data123", &negative, "", true},
		{"nan cost", "failed", "Run123", "Data123", &nan, "", true},
		{"unknown state", "new", "Run123", "Data123", nil, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClassifyRecoveryEvidence(Record{ID: "CI", AccountID: "CIAccount", State: tc.state, RunID: tc.run, DatasetID: tc.dataset, ChargedUSD: tc.cost})
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid evidence accepted")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatal("wrong evidence", got, err)
			}
		})
	}
}
