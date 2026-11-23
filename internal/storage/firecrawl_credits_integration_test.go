//go:build mariadb && schedulerwrites

package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"
	"time"

	"itmap/internal/integrations/firecrawl"
	"itmap/internal/legacycrypto"
	"itmap/internal/parser"
)

func TestMariaDBFirecrawlCreditsDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	for _, table := range []string{"firecrawl_usage_events", "firecrawl_usage_periods"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("credit ledger fixture must be empty")
		}
	}
	t.Cleanup(func() {
		f.exec("DELETE FROM firecrawl_usage_events")
		f.exec("DELETE FROM firecrawl_usage_periods")
	})
	const key = "CI_SYNTHETIC_FIRECRAWL_KEY"
	codec, err := legacycrypto.New("CI_SYNTHETIC_SESSION_SECRET", legacycrypto.Firecrawl)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := codec.Encrypt(key)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(digest[:])
	f.exec("UPDATE firecrawl_settings SET enabled=1,key_encrypted=?,free_key_hash=?,revision=5 WHERE id=1", ciphertext, hash)
	run := f.begin("CI_FIRECRAWL_CREDITS")
	jobID := "CI_FIRECRAWL_JOB"
	schedulerSeedJob(f, jobID, nil)
	f.exec("UPDATE scheduler_jobs SET type='scan_source',source_id='companySite' WHERE id=?", jobID)
	lease := schedulerRequireLease(t, f, run, jobID)
	executor := parser.FirecrawlScrapeExecutor{Codec: codec, Transport: rejectFirecrawlTransport{t}}
	step := parser.FirecrawlScrapeStep{ID: "listing", URL: "https://jobs.company.com", AllowedHosts: []string{"different.company.com"}}
	if _, err := executor.Execute(f.ctx, lease, step); !errors.Is(err, firecrawl.ErrInput) {
		t.Fatalf("unapproved source was not rejected before contacting Firecrawl: %v", err)
	}
	if n := f.count("SELECT COUNT(*) FROM firecrawl_usage_events"); n != 0 {
		t.Fatalf("unapproved source reserved credits: %d", n)
	}
	now := time.UnixMilli(schedulerClock(t, f.db)).Add(-time.Second)
	plan, remaining := float64(1000), float64(995)
	snapshot := parser.FirecrawlCreditSnapshot{ObservedAt: now, Usage: firecrawl.CreditUsage{
		PlanCredits: &plan, RemainingCredits: &remaining,
		PeriodStart: now.Add(-24 * time.Hour).Format(time.RFC3339Nano),
		PeriodEnd:   now.Add(28 * 24 * time.Hour).Format(time.RFC3339Nano),
	}}
	reserve := func(step string, revision int64) (*parser.FirecrawlCreditReservation, error) {
		return lease.ReserveFirecrawlCredits(f.ctx, step, hash, "free", revision, snapshot)
	}
	if _, err := reserve("listing", 4); !errors.Is(err, parser.ErrSettingsChanged) {
		t.Fatalf("stale configuration: %v", err)
	}
	reservation, err := reserve("listing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reserve("listing", 5); !errors.Is(err, parser.ErrFirecrawlAlreadyReserved) {
		t.Fatalf("same operation: %v", err)
	}
	if _, err = reserve("page-2", 5); !errors.Is(err, parser.ErrFirecrawlCreditBusy) {
		t.Fatalf("concurrent operation: %v", err)
	}
	if err = reservation.Settle(f.ctx, firecrawl.ScrapeResult{}, firecrawl.ErrNetwork); err != nil {
		t.Fatal(err)
	}
	if f.count("SELECT COUNT(*) FROM firecrawl_usage_events WHERE outcome='uncertain' AND charged_credits IS NULL AND reserved_credits=4") != 1 {
		t.Fatal("unknown charge lost its reservation")
	}
	f.exec("UPDATE firecrawl_usage_events SET active_until=0")
	if _, err = reserve("listing", 5); !errors.Is(err, parser.ErrFirecrawlAlreadyReserved) {
		t.Fatalf("expired operation admitted a replay: %v", err)
	}
	remaining = 1
	if _, err = reserve("page-2", 5); !errors.Is(err, parser.ErrFirecrawlCreditLimit) {
		t.Fatalf("insufficient provider credits: %v", err)
	}
	if f.count("SELECT COUNT(*) FROM firecrawl_usage_periods WHERE provider_used_high=999") != 1 {
		t.Fatal("denied reservation lost provider high-water mark")
	}
}

type rejectFirecrawlTransport struct{ t *testing.T }

func (r rejectFirecrawlTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Error("unapproved source contacted Firecrawl")
	return nil, errors.New("unexpected provider request")
}
