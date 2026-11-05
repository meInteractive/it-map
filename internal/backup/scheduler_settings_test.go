package backup

import (
	"bytes"
	"encoding/json"
	"testing"

	"itmap/internal/v39"
)

func TestSchedulerSettingsOwnsStateAndRedactsGenericJSON(t *testing.T) {
	view, e := ReadSchedulerSettingsJSON([]byte(`{"revision":7,"cloudOwnerId":"CI_SYNTHETIC_OWNER","future":{"keep":[1,null]}}`), true, []string{"hh"})
	if e != nil {
		t.Fatal(e)
	}
	before := view.PublicJSON()
	change, e := view.NextFromValidated(json.RawMessage(`{"enabled":false,"revision":7,"dayBudgetUsd":0,"monthBudgetUsd":0,"priorities":{"scan_source":95},"favoritesOnly":false}`), "Eugene")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, view.PublicJSON()) {
		t.Fatal("serialization mutated the shared old view")
	}
	storage := change.StorageJSON()
	original := append([]byte{}, storage...)
	if !bytes.Contains(storage, []byte(`"cloudOwnerId":"CI_SYNTHETIC_OWNER"`)) || !bytes.Contains(storage, []byte(`"future":{"keep":[1,null]}`)) {
		t.Fatal("private/unknown source members lost")
	}
	storage[0] = '['
	if !bytes.Equal(original, change.StorageJSON()) {
		t.Fatal("storage output aliases owned change")
	}
	public := view.PublicJSON()
	public[0] = '['
	if !bytes.Equal(before, view.PublicJSON()) {
		t.Fatal("public output aliases owned view")
	}
	for _, value := range []any{view, change} {
		encoded, e := json.Marshal(value)
		if e != nil || bytes.Contains(encoded, []byte("cloudOwnerId")) || bytes.Contains(encoded, []byte("CI_SYNTHETIC_OWNER")) {
			t.Fatal("generic JSON exposed private owner")
		}
	}
}

func TestSchedulerUpdatePreservesHHRoleSelections(t *testing.T) {
	view, err := ReadSchedulerSettingsJSON([]byte(`{"revision":1,"hhProfessionalRoles":{"10":{"revision":2,"roleIds":["36","107"]}}}`), true, nil)
	if err != nil {
		t.Fatal(err)
	}
	change, err := view.NextFromValidated(json.RawMessage(`{"enabled":false,"revision":1,"dayBudgetUsd":0,"monthBudgetUsd":0}`), "Eugene")
	if err != nil || !bytes.Contains(change.StorageJSON(), []byte(`"hhProfessionalRoles":{"10":{"revision":2,"roleIds":["36","107"]}}`)) {
		t.Fatalf("HH roles lost: %s, %v", change.StorageJSON(), err)
	}
}

func TestRestorePreservesHHRoleSelections(t *testing.T) {
	stored, _ := json.Marshal(`{"revision":4,"enabled":true,"hhProfessionalRoles":{"10":{"revision":2,"roleIds":["36"]}}}`)
	row := map[string]json.RawMessage{"scheduler_json": stored}
	if err := restoreScheduler(row, nil, v39.RestorePolicy()); err != nil {
		t.Fatal(err)
	}
	var decoded string
	if err := json.Unmarshal(row["scheduler_json"], &decoded); err != nil {
		t.Fatal(err)
	}
	var restored map[string]json.RawMessage
	if err := json.Unmarshal([]byte(decoded), &restored); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(restored["hhProfessionalRoles"], []byte(`"36"`)) || string(restored["enabled"]) != "false" {
		t.Fatalf("unexpected restore: %s", decoded)
	}
}
