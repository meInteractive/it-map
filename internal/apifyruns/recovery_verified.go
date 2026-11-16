package apifyruns

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"itmap/internal/integrations/apify"
)

// ErrRecoveryEvidence denotes missing or conflicting evidence without exposing
// account, run, provider response or credential data.
var ErrRecoveryEvidence = errors.New("apify recovery evidence insufficient")

// VerifyRecoveryObservation compares an already recorded run with a fresh
// authenticated account and GET response. It only returns an in-memory
// candidate: it does not read or write the ledger, authorise POST, establish
// caller provenance, or protect against concurrent changes. A future writer
// must recheck generation, settings and the original row under locks.
func VerifyRecoveryObservation(row Record, authenticatedAccount string, run apify.ActorRun, now time.Time) (Record, error) {
	if now.IsZero() || validateStoredIdentity(row) != nil || authenticatedAccount == "" || authenticatedAccount != row.AccountID || (row.ChargedUSD != nil && (math.IsNaN(*row.ChargedUSD) || math.IsInf(*row.ChargedUSD, 0) || *row.ChargedUSD < 0)) {
		return Record{}, ErrRecoveryEvidence
	}
	if row.State != "running" && row.State != "uncertain" && row.State != "succeeded" && row.State != "failed" {
		return Record{}, ErrRecoveryEvidence
	}
	state, err := run.State()
	if err != nil || (state != "succeeded" && state != "failed") {
		return Record{}, ErrRecoveryEvidence
	}
	if (row.State == "succeeded" || row.State == "failed") && row.State != state {
		return Record{}, ErrRecoveryEvidence
	}
	var fields struct {
		UserID     string   `json:"userId"`
		FinishedAt string   `json:"finishedAt"`
		Cost       *float64 `json:"usageTotalUsd"`
	}
	if json.Unmarshal(run.Raw, &fields) != nil || fields.UserID == "" || fields.UserID != row.AccountID || fields.Cost == nil || math.IsNaN(*fields.Cost) || math.IsInf(*fields.Cost, 0) || *fields.Cost < 0 {
		return Record{}, ErrRecoveryEvidence
	}
	finished, err := time.Parse(time.RFC3339Nano, fields.FinishedAt)
	if err != nil || finished.After(now.Add(-10*time.Second)) || (row.ChargedUSD != nil && *fields.Cost < *row.ChargedUSD) {
		return Record{}, ErrRecoveryEvidence
	}
	result, err := mergeRun(row, run, now)
	if err != nil || result.ChargedUSD == nil {
		return Record{}, ErrRecoveryEvidence
	}
	return result, nil
}
