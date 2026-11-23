package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"time"

	"itmap/internal/integrations/firecrawl"
)

var (
	ErrFirecrawlCreditSnapshot  = errors.New("Firecrawl credit snapshot unavailable")
	ErrFirecrawlCreditLimit     = errors.New("Firecrawl credit limit reached")
	ErrFirecrawlCreditBusy      = errors.New("Firecrawl scrape temporarily busy")
	ErrFirecrawlAlreadyReserved = errors.New("Firecrawl operation already reserved")
)

const firecrawlScrapeReserve = 4
const firecrawlPeriodFormat = "2006-01-02T15:04:05.000Z"

type FirecrawlCreditSnapshot struct {
	Usage      firecrawl.CreditUsage
	ObservedAt time.Time
}

type FirecrawlCreditReservation struct {
	lease *JobLease
	id    string
}

// ReserveFirecrawlCredits records one scrape per stable job and step. An
// existing reservation, even after its active window, never authorizes a POST.
func (j *JobLease) ReserveFirecrawlCredits(ctx context.Context, step, keyHash, mode string, revision int64, snapshot FirecrawlCreditSnapshot) (*FirecrawlCreditReservation, error) {
	return j.reserveFirecrawlCredits(ctx, step, keyHash, mode, revision, snapshot, nil)
}

func (j *JobLease) reserveFirecrawlCredits(ctx context.Context, step, keyHash, mode string, revision int64, snapshot FirecrawlCreditSnapshot, validate func(context.Context, *sql.Tx) error) (*FirecrawlCreditReservation, error) {
	if !validFirecrawlStep(j, step) || !validFirecrawlKeyHash(keyHash) || revision < 0 {
		return nil, ErrJobInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, now, err := j.beginOwned(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	plan, remaining, start, end, cap, rate, ok := firecrawlCreditPeriod(mode, snapshot, time.UnixMilli(now))
	if !ok {
		return nil, ErrFirecrawlCreditSnapshot
	}

	if validate != nil {
		if err := validate(ctx, tx); err != nil {
			return nil, err
		}
	}

	var enabled int
	var storedMode, storedKeyHash string
	var storedRevision int64
	err = tx.QueryRowContext(ctx, `SELECT enabled, mode, revision,
		CASE WHEN mode='hobby' THEN hobby_key_hash ELSE free_key_hash END
		FROM firecrawl_settings WHERE id=1 FOR UPDATE`).Scan(&enabled, &storedMode, &storedRevision, &storedKeyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSettingsChanged
	}
	if err != nil {
		return nil, storageError(ctx)
	}
	if enabled != 1 || mode != storedMode || revision != storedRevision || keyHash != storedKeyHash {
		return nil, ErrSettingsChanged
	}

	digest := sha256.Sum256([]byte(j.repo.Generation + "\x00" + j.jobID + "\x00" + step))
	id := "go-scrape:" + hex.EncodeToString(digest[:])
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM firecrawl_usage_events WHERE id=?)", id).Scan(&exists); err != nil {
		return nil, storageError(ctx)
	}
	if exists {
		return nil, ErrFirecrawlAlreadyReserved
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO firecrawl_usage_periods
		(key_hash,period_start,period_end,provider_used_high,remaining_credits,plan_credits,checked_at)
		VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE
		provider_used_high=GREATEST(provider_used_high,VALUES(provider_used_high)),
		remaining_credits=IF(checked_at<=VALUES(checked_at),VALUES(remaining_credits),remaining_credits),
		plan_credits=IF(checked_at<=VALUES(checked_at),VALUES(plan_credits),plan_credits),
		checked_at=GREATEST(checked_at,VALUES(checked_at))`,
		keyHash, start, end, plan-remaining, remaining, plan, snapshot.ObservedAt.UnixMilli())
	if err != nil {
		return nil, storageError(ctx)
	}
	var providerHigh, providerRemaining int
	err = tx.QueryRowContext(ctx, `SELECT provider_used_high, remaining_credits FROM firecrawl_usage_periods
		WHERE key_hash=? AND period_start=? AND period_end=?`, keyHash, start, end).Scan(&providerHigh, &providerRemaining)
	if err != nil {
		return nil, storageError(ctx)
	}

	var localUpper float64
	var active, recent int
	err = tx.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN period_start=? AND period_end=? AND (profile=? OR key_hash=?)
			THEN COALESCE(charged_credits,reserved_credits) ELSE 0 END),0),
		COUNT(CASE WHEN active_until>? THEN 1 END),
		COUNT(CASE WHEN started_at>? THEN 1 END)
		FROM firecrawl_usage_events`, start, end, mode, keyHash, now, now-60000).Scan(&localUpper, &active, &recent)
	if err != nil {
		return nil, storageError(ctx)
	}
	var denied error
	switch {
	case active > 0 || recent >= rate:
		denied = ErrFirecrawlCreditBusy
	case providerRemaining < firecrawlScrapeReserve || float64(providerHigh)+firecrawlScrapeReserve+localUpper > float64(cap):
		denied = ErrFirecrawlCreditLimit
	}
	if denied != nil {
		// Preserve the provider high-water mark even when no scrape is admitted.
		if err = tx.Commit(); err != nil {
			return nil, storageError(ctx)
		}
		return nil, denied
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO firecrawl_usage_events
		(id,key_hash,profile,period_start,period_end,operation,reserved_credits,outcome,started_at,active_until,generation)
		VALUES(?,?,?,?,?,'scrape',?,'reserved',?,?,?)`, id, keyHash, mode, start, end,
		firecrawlScrapeReserve, now, now+360000, j.repo.Generation)
	if err != nil {
		return nil, storageError(ctx)
	}
	if err = tx.Commit(); err != nil {
		return nil, storageError(ctx)
	}
	return &FirecrawlCreditReservation{lease: j, id: id}, nil
}

// Settle records accounting evidence for a dispatched request. Unknown
// charges retain the full reservation in the period total.
func (r *FirecrawlCreditReservation) Settle(ctx context.Context, result firecrawl.ScrapeResult, scrapeErr error) error {
	if r == nil || r.lease == nil || r.id == "" {
		return ErrJobInput
	}
	charge, outcome := firecrawlCreditOutcome(result, scrapeErr)
	return r.lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		now, err := runClock(ctx, tx)
		if err != nil {
			return storageError(ctx)
		}
		var status any
		if result.ProviderStatus >= 100 && result.ProviderStatus <= 599 {
			status = result.ProviderStatus
		}
		updated, err := tx.ExecContext(ctx, `UPDATE firecrawl_usage_events SET
			charged_credits=?, outcome=?, finished_at=?,
			active_until=CASE WHEN ?='uncertain' THEN active_until ELSE ? END,provider_status=?
			WHERE id=? AND generation=? AND outcome='reserved'`,
			charge, outcome, now, outcome, now, status, r.id, r.lease.repo.Generation)
		if err != nil {
			return storageError(ctx)
		}
		n, err := updated.RowsAffected()
		if err != nil {
			return storageError(ctx)
		}
		if n != 1 {
			return ErrFirecrawlAlreadyReserved
		}
		return nil
	})
}

func validFirecrawlStep(j *JobLease, step string) bool {
	return j != nil && j.job.SourceID != nil && *j.job.SourceID != "hh" &&
		(j.job.Type == "scan_source" || j.job.Type == "fetch_full_text") && len(step) > 0 && len(step) <= 128
}

func validFirecrawlKeyHash(value string) bool {
	hash, err := hex.DecodeString(value)
	return err == nil && len(hash) == sha256.Size && hex.EncodeToString(hash) == value
}

func firecrawlCreditPeriod(mode string, snapshot FirecrawlCreditSnapshot, now time.Time) (plan, remaining int, start, end string, cap, rate int, ok bool) {
	switch mode {
	case "free":
		cap, rate = 1000, 10
	case "hobby":
		cap, rate = 5000, 100
	default:
		return
	}
	u := snapshot.Usage
	if u.PlanCredits == nil || u.RemainingCredits == nil || snapshot.ObservedAt.IsZero() ||
		snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > time.Minute {
		return
	}
	p, left := *u.PlanCredits, *u.RemainingCredits
	if (p != 1000 && p != 5000) || (mode == "hobby" && p != 5000) ||
		math.IsNaN(left) || left < 0 || left > p || math.Trunc(left) != left {
		return
	}
	a, aErr := time.Parse(time.RFC3339Nano, u.PeriodStart)
	b, bErr := time.Parse(time.RFC3339Nano, u.PeriodEnd)
	if aErr != nil || bErr != nil || a.After(snapshot.ObservedAt) || !b.After(now) ||
		b.Sub(a) < 24*time.Hour || b.Sub(a) > 35*24*time.Hour {
		return
	}
	return int(p), int(left), a.UTC().Format(firecrawlPeriodFormat), b.UTC().Format(firecrawlPeriodFormat), cap, rate, true
}

func firecrawlCreditOutcome(result firecrawl.ScrapeResult, scrapeErr error) (*float64, string) {
	if credits := result.ReportedCredits; credits != nil && !math.IsInf(*credits, 0) && !math.IsNaN(*credits) && *credits >= 0 {
		if scrapeErr != nil {
			return credits, "error"
		}
		return credits, "complete"
	}
	if errors.Is(scrapeErr, firecrawl.ErrRejected) {
		switch result.ProviderStatus {
		case 401, 402, 429:
			zero := float64(0)
			return &zero, "error"
		}
	}
	return nil, "uncertain"
}
