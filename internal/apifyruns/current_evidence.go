package apifyruns

import (
	"context"
	"database/sql"
)

// currentUnresolvedPredicate is shared by admission and recovery diagnostics.
const currentUnresolvedPredicate = "generation=? AND (charged_usd IS NULL OR state IN ('starting','running','uncertain') OR BINARY run_id NOT REGEXP BINARY '^[A-Za-z0-9]+$')"

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
