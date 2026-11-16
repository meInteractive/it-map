package apifyruns

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"itmap/internal/integrations/apify"
)

type readProviderFake struct {
	account            apify.BillingAccount
	run                apify.ActorRun
	accountErr, runErr error
	accounts, runs     int
}

func (p *readProviderFake) Account(context.Context) (apify.BillingAccount, error) {
	p.accounts++
	return p.account, p.accountErr
}
func (p *readProviderFake) Run(_ context.Context, id string) (apify.ActorRun, error) {
	p.runs++
	if id != "CIRun" {
		return apify.ActorRun{}, errors.New("unexpected run")
	}
	return p.run, p.runErr
}

func TestRecoveryReaderVerifiesAccountBeforeKnownRun(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal(map[string]any{"id": "CIRun", "defaultDatasetId": "CIData", "status": "SUCCEEDED", "userId": "CIAccount", "finishedAt": now.Add(-time.Minute).Format(time.RFC3339), "usageTotalUsd": 0.12})
	row := Record{ID: "CIrow", State: "running", RunID: "CIRun", DatasetID: "CIData", AccountID: "CIAccount"}
	newFake := func() *readProviderFake {
		return &readProviderFake{account: apify.BillingAccount{AccountID: "CIAccount"}, run: apify.ActorRun{ID: "CIRun", DefaultDatasetID: "CIData", Status: "SUCCEEDED", Raw: raw}}
	}
	for _, tc := range []struct {
		name     string
		change   func(*readProviderFake)
		wantOK   bool
		wantRuns int
	}{
		{"same account after token rotation", func(*readProviderFake) {}, true, 1},
		{"other account", func(p *readProviderFake) { p.account.AccountID = "CIOther" }, false, 0},
		{"account error", func(p *readProviderFake) { p.accountErr = errors.New("private") }, false, 0},
		{"run error", func(p *readProviderFake) { p.runErr = errors.New("private") }, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newFake()
			tc.change(p)
			reader := RecoveryReader{Resolve: func(context.Context, string) (string, error) { return "rotated-token", nil }, NewClient: func(string) (RecoveryReadProvider, error) { return p, nil }, Now: func() time.Time { return now }}
			result, err := reader.Verify(context.Background(), row, "free")
			if (err == nil) != tc.wantOK || p.accounts != 1 || p.runs != tc.wantRuns {
				t.Fatal("unexpected verification", err, p.accounts, p.runs)
			}
			if tc.wantOK && (result.ChargedUSD == nil || *result.ChargedUSD != 0.12 || row.ChargedUSD != nil) {
				t.Fatal("invalid candidate")
			}
			if !tc.wantOK && !errors.Is(err, ErrRecoveryEvidence) {
				t.Fatal("unsafe error", err)
			}
		})
	}
}

func TestRecoveryReaderDoesNotContactProviderForInvalidOrCancelledInput(t *testing.T) {
	calls := 0
	reader := RecoveryReader{Resolve: func(context.Context, string) (string, error) { calls++; return "", nil }}
	row := Record{ID: "CIrow", State: "running", RunID: "CIRun", DatasetID: "CIData", AccountID: "CIAccount"}
	if _, err := reader.Verify(context.Background(), row, "unknown"); !errors.Is(err, ErrRecoveryEvidence) || calls != 0 {
		t.Fatal("invalid profile reached resolver")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Verify(ctx, row, "free"); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("cancelled context reached resolver")
	}
}
