package apifyruns

import (
	"context"
	"database/sql"
)

// currentUnresolvedPredicate is shared by admission and recovery diagnostics.
const unresolvedEvidencePredicate = "(charged_usd IS NULL OR state IN ('starting','running','uncertain') OR BINARY run_id NOT REGEXP BINARY '^[A-Za-z0-9]+$' OR BINARY dataset_id NOT REGEXP BINARY '^[A-Za-z0-9]+$' OR BINARY account_id NOT REGEXP BINARY '^[A-Za-z0-9]+$' OR id='')"
const currentUnresolvedPredicate = "generation=? AND " + unresolvedEvidencePredicate
const historicalUnresolvedPredicate = "generation<>? AND (" + unresolvedEvidencePredicate + " OR BINARY generation NOT REGEXP BINARY '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')"

// CountCurrentUnresolvedTx requires the caller to hold the generation lock.
// The count describes saved ledger entries and does not establish completeness.
func CountCurrentUnresolvedTx(ctx context.Context, tx *sql.Tx, generation string) (int, error) {
	if ctx == nil || tx == nil || generation == "" {
		return 0, ErrStorage
	}
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_runs WHERE "+currentUnresolvedPredicate, generation).Scan(&count); err != nil {
		return 0, ErrStorage
	}
	return count, nil
}
