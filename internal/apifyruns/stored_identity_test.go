package apifyruns

import (
	"errors"
	"testing"
)

func TestValidateStoredIdentity(t *testing.T) {
	cost := 0.1
	for _, tc := range []struct {
		name  string
		row   Record
		valid bool
	}{
		{"running known", Record{State: "running", RunID: "Run123", DatasetID: "Data123"}, true},
		{"running missing dataset", Record{State: "running", RunID: "Run123"}, true},
		{"terminal no cost", Record{State: "succeeded", RunID: "Run123"}, true},
		{"terminal complete", Record{State: "succeeded", RunID: "Run123", DatasetID: "Data123", ChargedUSD: &cost}, true},
		{"bad run", Record{State: "running", RunID: "bad-run", DatasetID: "Data123"}, false},
		{"bad dataset", Record{State: "running", RunID: "Run123", DatasetID: "bad-data"}, false},
		{"terminal missing dataset", Record{State: "succeeded", RunID: "Run123", ChargedUSD: &cost}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStoredIdentity(tc.row)
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrIdentity) {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
