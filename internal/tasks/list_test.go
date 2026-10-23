package tasks

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDirectoryOnlyIncludesKnownTaskEntities(t *testing.T) {
	catalog := []byte(`{"companies":[{"name":"Alpha","rank":1},{"name":"Beta","rank":2}],"agencies":[{"name":"Staff","rank":3}]}`)
	results, err := entitiesForTasks(catalog, map[string]bool{"Beta": true, "Staff": true, "Unknown": true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Name != "Beta" || results[1].EntityType != "agency" {
		t.Fatalf("unexpected entities %+v", results)
	}
	if _, err = entitiesForTasks(catalog, nil, "Staff"); err != nil {
		t.Fatal(err)
	}
	if _, err = entitiesForTasks(catalog, nil, "Missing"); !errors.Is(err, ErrCompanyNotFound) {
		t.Fatalf("unknown company: %v", err)
	}
	marshaled, err := json.Marshal(Response{Tasks: []Task{}, Entities: results})
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]json.RawMessage
	if err = json.Unmarshal(marshaled, &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["tasks"]; !ok {
		t.Fatal("missing empty task list")
	}
}
