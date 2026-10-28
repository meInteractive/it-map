package backup

import (
	"bytes"
	"encoding/json"
	"testing"
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
