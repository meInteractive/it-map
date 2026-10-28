package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestRestoreInputVersionDispatch(t *testing.T) {
	original := emptyLegacyInput(t, 14)
	for _, raw := range []string{"14.0", "1.4e1", "15", "15.000", "14,\"version\":15"} {
		data := bytes.Replace(original, []byte(`"version":14`), []byte(`"version":`+raw), 1)
		p, e := validateRestoreInput(data)
		if e != nil || p.Version < 14 || p.Version > 15 {
			t.Fatalf("valid unsigned version rejected: %s %v", raw, e)
		}
	}
	for _, raw := range []string{`"14"`, `null`, `true`, `14,"version":16`, `99`} {
		data := bytes.Replace(original, []byte(`"version":14`), []byte(`"version":`+raw), 1)
		if _, e := validateRestoreInput(data); e == nil {
			t.Fatalf("invalid/unsigned modern accepted: %s", raw)
		}
	}
	upper := bytes.Replace(original, []byte(`"version":14`), []byte(`"Version":14`), 1)
	if _, e := validateRestoreInput(upper); e == nil {
		t.Fatal("case-insensitive dispatch accepted")
	}
	p, e := validateRestoreInput(original)
	if e != nil {
		t.Fatal(e)
	}
	wanted := preservedRestoreTables(p)
	if !wanted["google_calendar_connection"] || !wanted["vacancy_audit_runs"] {
		t.Fatal("missing legacy preserved sections")
	}
	p.Storage.Tables = append(p.Storage.Tables, Table{Name: "vacancy_audit_runs"})
	if preservedRestoreTables(p)["vacancy_audit_runs"] || !wanted["vacancy_audit_runs"] {
		t.Fatal("explicit audit read-set differs or mutated prior selection")
	}
}
func TestLegacyLossyInputRefusedBeforeSQL(t *testing.T) {
	for _, version := range []int{14, 15} {
		for _, date := range []string{`{"CI":"non-scalar"}`, `1`, `null`} {
			var root map[string]json.RawMessage
			if e := json.Unmarshal(emptyLegacyInput(t, version), &root); e != nil {
				t.Fatal(e)
			}
			root["createdAt"] = json.RawMessage(date)
			if date == "null" {
				reviews := map[string]string{}
				for _, key := range catalogPolicy().field("reviewKeys").items {
					reviews[stringFromUnits(key.text)] = ""
				}
				root["catalog"], _ = json.Marshal(map[string]any{"companies": []any{map[string]any{"name": "CI", "reviewSources": reviews, "reviews": map[string]any{}, "aiScore": nil, "aiEvidence": "", "roles": map[string]any{"toString": 7}}}})
			}
			data, _ := json.Marshal(root)
			if _, e := ValidateLegacyBackup(data); e != nil {
				t.Fatal("preflight vector failed structural validation", e)
			}
			if _, e := (Repository{}).ValidateRestore(context.Background(), data); ErrorStatus(e) != 400 {
				t.Fatal("lossy/invalid legacy validation reached SQL", e)
			}
			if _, e := (Repository{}).Restore(context.Background(), data); ErrorStatus(e) != 400 {
				t.Fatal("lossy/invalid legacy restore reached SQL", e)
			}
		}
	}
}
