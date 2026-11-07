package apifyruns

import (
	"context"
	"database/sql"
	"math"
	"regexp"
)

// PaidOperationView contains only the local operation ID and permitted ledger
// columns. Account/key/Actor/Dataset identities, request bodies and URLs are
// deliberately excluded even from the authenticated admin response.
type PaidOperationView struct {
	OperationID       string   `json:"operationId"`
	Profile           string   `json:"profile"`
	State             string   `json:"state"`
	CreatedAt         int64    `json:"createdAt"`
	UpdatedAt         int64    `json:"updatedAt"`
	ChargedUSD        *float64 `json:"chargedUsd"`
	ReservedUSD       float64  `json:"reservedUsd"`
	ErrorCode         *string  `json:"errorCode"`
	HasProviderRunID  bool     `json:"hasProviderRunId"`
	StartOutcomeKnown bool     `json:"startOutcomeKnown"`
}

var safeOperationID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var safeApifyErrorCode = regexp.MustCompile(`^apify_[a-z0-9_]{1,48}$`)

// readUsageOperations uses the SAME read-only RepeatableRead transaction as
// ReadUsage, so total and last 10 belong to one consistent local snapshot.
// An operation is a reservation/ledger row; it is NOT evidence of a POST,
// GET/reconcile call or actual billed amount when charged_usd is NULL.
func readUsageOperations(ctx context.Context, tx *sql.Tx, out *UsageView) error {
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_runs").Scan(&out.OperationsTotal); err != nil {
		return ErrStorage
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,profile,state,run_id,error_code,reserved_usd,charged_usd,created_at,updated_at
		FROM apify_runs ORDER BY created_at DESC,BINARY id DESC LIMIT 10`)
	if err != nil {
		return ErrStorage
	}
	defer rows.Close()
	for rows.Next() {
		var id, profile, state, runID, errorCode string
		var charged sql.NullFloat64
		var item PaidOperationView
		if err := rows.Scan(&id,&profile,&state,&runID,&errorCode,
			&item.ReservedUSD,&charged,&item.CreatedAt,&item.UpdatedAt); err != nil {
			return ErrStorage
		}
		if !safeOperationID.MatchString(id) || (profile != "free" && profile != "starter") ||
			!validApifyState(state) || item.CreatedAt < 0 || item.UpdatedAt < item.CreatedAt ||
			item.ReservedUSD < 0 || math.IsNaN(item.ReservedUSD) || math.IsInf(item.ReservedUSD,0) {
			return ErrStorage
		}
		if charged.Valid {
			if charged.Float64 < 0 || math.IsNaN(charged.Float64) || math.IsInf(charged.Float64,0) {
				return ErrStorage
			}
			value := charged.Float64
			item.ChargedUSD = &value
		}
		item.OperationID, item.Profile, item.State = id, profile, state
		// Do not return run_id. Known provider identity is enough to distinguish
		// a safe GET reconciliation path from an unresolved start.
		item.HasProviderRunID = runID != ""
		item.StartOutcomeKnown = runID != ""
		if safeApifyErrorCode.MatchString(errorCode) {
			code := errorCode
			item.ErrorCode = &code
		}
		out.RecentOperations = append(out.RecentOperations,item)
	}
	if err := rows.Err(); err != nil {
		return ErrStorage
	}
	out.OperationHistoryAvailable = true
	return nil
}

func validApifyState(s string) bool {
	switch s {
	case "starting","uncertain","running","succeeded","failed":
		return true
	default:
		return false
	}
}
