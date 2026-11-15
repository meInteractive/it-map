package parser

import (
	"context"
	"database/sql"
	"time"

	"itmap/internal/retryafter"
)

const (
	hhReadRetryReason     = "hh_public_retry"
	apifyReadRetryReason  = "apify_read_retry"
	maxRecentReadFailures = 5
)

// readBackoff keeps provider's minimum and bounds retry growth. It never
// authorizes a paid POST; the normal admission checks run on every retry.
func readBackoff(previous int, hint time.Duration) time.Duration {
	if previous < 0 {
		previous = 0
	}
	if previous > maxRecentReadFailures {
		previous = maxRecentReadFailures
	}
	delay := time.Minute * time.Duration(1<<previous)
	if hint >= retryafter.MinDelay && hint <= retryafter.MaxDelay && hint > delay {
		delay = hint
	}
	return delay
}

// recentReadFailures counts only completed, unspent deferrals for this job and
// category in the last hour. Ownership and generation are checked by the lease.
func recentReadFailures(ctx context.Context, lease *JobLease, attemptID, reason string) (int, error) {
	if lease == nil || attemptID == "" || (reason != hhReadRetryReason && reason != apifyReadRetryReason) {
		return 0, ErrJobInput
	}
	var count int
	err := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM scheduler_attempts WHERE job_id=? AND id<>? AND status='pending' AND reason_code=? AND reserved_usd=0 AND cost_usd=0 AND started_at>=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)-3600000 ORDER BY started_at DESC LIMIT 5) recent`, lease.Job().ID, attemptID, reason).Scan(&count); err != nil {
			return storageError(ctx)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
