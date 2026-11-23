package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"itmap/internal/integrations/firecrawl"
	"itmap/internal/legacycrypto"
)

var ErrFirecrawlSettings = errors.New("Firecrawl settings or key unavailable")

// FirecrawlScrapeExecutor performs one paid step of a claimed job. The caller
// supplies the stable step name and the URL and hosts from its source plan.
type FirecrawlScrapeExecutor struct {
	Codec     *legacycrypto.Codec
	Transport http.RoundTripper
}

type FirecrawlScrapeStep struct {
	validate        func(context.Context, *sql.Tx) error
	ID              string
	URL             string
	AllowedHosts    []string
	MarkdownOnly    bool
	HTML            bool
	OnlyMainContent bool
}

type firecrawlScrapeSettings struct {
	mode     string
	revision int64
	hash     string
	timeout  time.Duration
	key      string
}

func (e FirecrawlScrapeExecutor) Execute(ctx context.Context, lease *JobLease, step FirecrawlScrapeStep) (firecrawl.ScrapeResult, error) {
	if !validFirecrawlStep(lease, step.ID) {
		return firecrawl.ScrapeResult{}, ErrJobInput
	}
	settings, err := e.settings(ctx, lease)
	if err != nil {
		return firecrawl.ScrapeResult{}, err
	}
	target, err := firecrawl.Preflight(ctx, step.URL, step.AllowedHosts)
	if err != nil {
		return firecrawl.ScrapeResult{}, err
	}
	client, err := firecrawl.New(settings.key, e.Transport)
	if err != nil {
		return firecrawl.ScrapeResult{}, ErrFirecrawlSettings
	}
	checkedAt := time.Now()
	usage, err := client.CreditUsage(ctx)
	if err != nil {
		return firecrawl.ScrapeResult{}, err
	}
	reservation, err := lease.reserveFirecrawlCredits(ctx, step.ID, settings.hash, settings.mode, settings.revision, FirecrawlCreditSnapshot{Usage: usage, ObservedAt: checkedAt}, step.validate)
	if err != nil {
		return firecrawl.ScrapeResult{}, err
	}
	result, scrapeErr := client.Scrape(ctx, target, firecrawl.ScrapeOptions{
		AllowedHosts: step.AllowedHosts, Timeout: settings.timeout, MarkdownOnly: step.MarkdownOnly,
		HTML: step.HTML, OnlyMainContent: step.OnlyMainContent,
	})
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return result, errors.Join(scrapeErr, reservation.Settle(cleanup, result, scrapeErr))
}

func (e FirecrawlScrapeExecutor) settings(ctx context.Context, lease *JobLease) (firecrawlScrapeSettings, error) {
	var settings firecrawlScrapeSettings
	var encrypted, proxy string
	var enabled int
	var timeoutMS int64
	err := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT enabled,mode,revision,timeout_ms,proxy,
			CASE WHEN mode='hobby' THEN hobby_key_encrypted ELSE key_encrypted END,
			CASE WHEN mode='hobby' THEN hobby_key_hash ELSE free_key_hash END
			FROM firecrawl_settings WHERE id=1`).Scan(&enabled, &settings.mode, &settings.revision, &timeoutMS, &proxy, &encrypted, &settings.hash)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrFirecrawlSettings
		}
		if err != nil {
			return storageError(ctx)
		}
		return nil
	})
	if err != nil {
		return settings, err
	}
	if enabled != 1 || (settings.mode != "free" && settings.mode != "hobby") || proxy != "basic" || timeoutMS < 5000 || timeoutMS > 60000 {
		return settings, ErrFirecrawlSettings
	}
	key, err := e.Codec.Decrypt(encrypted)
	if err != nil {
		return settings, ErrFirecrawlSettings
	}
	digest := sha256.Sum256([]byte(key))
	if settings.hash != hex.EncodeToString(digest[:]) {
		return settings, ErrFirecrawlSettings
	}
	settings.key, settings.timeout = key, time.Duration(timeoutMS)*time.Millisecond
	return settings, nil
}
