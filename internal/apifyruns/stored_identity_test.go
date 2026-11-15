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
		{"running known", Record{ID: "Row1", AccountID: "Account1", State: "running", RunID: "Run123", DatasetID: "Data123"}, true},
		{"running missing dataset", Record{ID: "Row1", AccountID: "Account1", State: "running", RunID: "Run123"}, true},
		{"terminal no cost", Record{ID: "Row1", AccountID: "Account1", State: "succeeded", RunID: "Run123"}, true},
		{"terminal complete", Record{ID: "Row1", AccountID: "Account1", State: "succeeded", RunID: "Run123", DatasetID: "Data123", ChargedUSD: &cost}, true},
		{"bad run", Record{ID: "Row1", AccountID: "Account1", State: "running", RunID: "bad-run", DatasetID: "Data123"}, false},
		{"bad dataset", Record{ID: "Row1", AccountID: "Account1", State: "running", RunID: "Run123", DatasetID: "bad-data"}, false},
		{"bad account", Record{ID: "Row1", AccountID: "bad-account", State: "succeeded", RunID: "Run123", DatasetID: "Data123", ChargedUSD: &cost}, false},
		{"missing row id", Record{AccountID: "Account1", State: "succeeded", RunID: "Run123", DatasetID: "Data123", ChargedUSD: &cost}, false},
		{"terminal missing dataset", Record{ID: "Row1", AccountID: "Account1", State: "succeeded", RunID: "Run123", ChargedUSD: &cost}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStoredIdentity(tc.row)
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrIdentity) {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
