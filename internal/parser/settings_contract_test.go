package parser

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestParserSettingsLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/settings-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Defaults Settings
		Options  []SourceOption
		Previous Settings
		Vectors  []struct {
			Name       string
			Input      json.RawMessage
			Valid      bool
			Normalized SettingsUpdate
			Error      string
			Status     int
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) < 60 {
		t.Fatal("required legacy settings vectors missing")
	}
	if !reflect.DeepEqual(DefaultSettings(), fixture.Defaults) || !reflect.DeepEqual(SourceOptions(), fixture.Options) {
		t.Fatal("source options/default settings differ from legacy JS")
	}
	original, _ := json.Marshal(fixture.Previous)
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := ValidateSettingsUpdate(v.Input, fixture.Previous, "Eugene")
			if !v.Valid {
				if !errors.Is(err, ErrSettingsInput) || err.Error() != v.Error || v.Status != 400 {
					t.Fatal("legacy validation rejection differs")
				}
				return
			}
			if err != nil {
				t.Fatal("valid legacy settings rejected")
			}
			if !reflect.DeepEqual(got, v.Normalized) {
				t.Fatal("legacy settings normalization differs")
			}
		})
	}
	unchanged, _ := json.Marshal(fixture.Previous)
	if string(original) != string(unchanged) {
		t.Fatal("validation mutated previous settings")
	}
}
func TestParserSettingsStrictJSONBoundary(t *testing.T) {
	for _, input := range []string{`{"enabled":true,"enabled":false,"revision":0,"dayBudgetUsd":0,"monthBudgetUsd":0}`, `{"enabled":true,"revision":0,"dayBudgetUsd":1e999,"monthBudgetUsd":0}`, `{"enabled":false,"revision":0,"dayBudgetUsd":0,"monthBudgetUsd":0} {}`, `{"enabled":false,"revision":0,"dayBudgetUsd":0,"monthBudgetUsd":0,"priorities":{"scan_source":1,"scan_source":2}}`} {
		if _, err := ValidateSettingsUpdate([]byte(input), DefaultSettings(), "Eugene"); !errors.Is(err, ErrSettingsInput) {
			t.Fatal("ambiguous/nonfinite/trailing JSON accepted")
		}
	}
}
