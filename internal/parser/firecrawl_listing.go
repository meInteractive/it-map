package parser

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"

	"itmap/internal/integrations/firecrawl"
)

type firecrawlListingExecutor struct {
	owner  string
	scrape companyFirecrawlScraper
}

type companyFirecrawlScraper interface {
	Execute(context.Context, *JobLease, FirecrawlScrapeStep) (firecrawl.ScrapeResult, error)
}

type firecrawlListingPlan struct {
	name, url, host, config string
	revision                int64
}

func (e firecrawlListingExecutor) read(ctx context.Context, tx *sql.Tx, job Job) (firecrawlListingPlan, error) {
	var plan firecrawlListingPlan
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return plan, err
	}
	view, err := settingsView(ctx, tx, row)
	if err != nil {
		return plan, err
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || e.owner != settings.OwnerName || !slices.Contains(settings.SourceIDs, *job.SourceID) {
		return plan, ErrSettingsChanged
	}
	err = tx.QueryRowContext(ctx, `SELECT c.company_name,s.source_url,s.config_json,s.revision
		FROM company_identity c JOIN vacancy_source_parsing s ON s.company_id=c.id
		WHERE c.id=? AND s.source_id=? AND s.enabled=1 AND s.auto_enabled=1
		AND (?=0 OR EXISTS(SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1))
		FOR UPDATE`, job.CompanyID, *job.SourceID, row.favorite, e.owner).Scan(&plan.name, &plan.url, &plan.config, &plan.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, ErrCandidateChanged
	}
	if err != nil {
		return plan, storageError(ctx)
	}
	host, ok := firecrawlListingHost(*job.SourceID, plan.url, plan.config)
	if !ok {
		return plan, ErrWorkerUnsupported
	}
	var payload companyScanPayload
	if json.Unmarshal(job.Payload, &payload) != nil || payload.Version != 1 || payload.SourceRevision != plan.revision ||
		payload.SourceURL != plan.url || payload.ScheduleRevision != settings.Revision {
		return plan, ErrCandidateChanged
	}
	plan.host = host
	return plan, nil
}

func isFirecrawlSource(source string) bool {
	return source == "companySite" || source == "getmatch"
}

func firecrawlListingHost(source, address, config string) (string, bool) {
	if source == "companySite" {
		return genericCompanyListingHost(address, config)
	}
	if source != "getmatch" {
		return "", false
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	// The legacy Getmatch adapter uses Markdown and accepts only Getmatch URLs.
	return host, (host == "getmatch.ru" || strings.HasSuffix(host, ".getmatch.ru")) &&
		genericListingConfig(config, "getmatch")
}

func genericCompanyListingHost(rawURL, rawConfig string) (string, bool) {
	if !genericListingConfig(rawConfig, "generic") {
		return "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Port() != "" {
		return "", false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	switch host {
	case "jobs.yoomoney.ru", "selectel.ru", "www.selectel.ru":
		return "", false
	}
	return host, true
}

// Accept both the empty object and the explicit defaults saved by Sites.
func genericListingConfig(raw, adapter string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || fields == nil {
		return false
	}
	for key, value := range fields {
		var canonical bytes.Buffer
		if json.Compact(&canonical, value) != nil {
			return false
		}
		var expected string
		switch key {
		case "adapter":
			if adapter == "getmatch" && string(value) == `"generic"` {
				continue
			}
			expected = `"` + adapter + `"`
		case "html", "useEnhancedProxy":
			expected = "false"
		case "titleClass", "detailPathPrefix":
			expected = `""`
		case "allowedHosts":
			expected = "[]"
		case "pagination":
			expected = "null"
		default:
			return false
		}
		if canonical.String() != expected {
			return false
		}
	}
	return true
}

func (e firecrawlListingExecutor) Execute(ctx context.Context, lease *JobLease) (HHListingProgress, error) {
	job := lease.Job()
	if e.scrape == nil || job.Type != "scan_source" || job.CompanyID == nil || job.AgencyID != nil ||
		job.CandidateID != nil || job.SourceID == nil || !isFirecrawlSource(*job.SourceID) {
		return HHListingProgress{}, ErrJobInput
	}
	var plan firecrawlListingPlan
	if err := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		var err error
		plan, err = e.read(ctx, tx, job)
		return err
	}); err != nil {
		return HHListingProgress{}, firecrawlFailure("firecrawl_source", err)
	}
	validate := func(ctx context.Context, tx *sql.Tx) error {
		current, err := e.read(ctx, tx, job)
		if err != nil {
			return err
		}
		if current != plan {
			return ErrCandidateChanged
		}
		return nil
	}
	bounded, cancel := context.WithDeadline(ctx, time.UnixMilli(job.LockedUntil))
	defer cancel()
	doc, err := e.scrape.Execute(bounded, lease, FirecrawlScrapeStep{ID: "company-listing-v1", URL: plan.url,
		AllowedHosts: []string{plan.host}, MarkdownOnly: true, OnlyMainContent: true, validate: validate})
	if err != nil {
		return HHListingProgress{}, err
	}
	if doc.TargetStatus < 200 || doc.TargetStatus >= 300 {
		return HHListingProgress{}, firecrawlFailure("firecrawl_target", errFirecrawlListing)
	}
	cards, err := firecrawlListingCards(*job.SourceID, doc.Markdown, doc.FinalURL)
	if err != nil {
		code := "firecrawl_listing"
		if errors.Is(err, errGetmatchListingUnavailable) {
			code = "firecrawl_listing_unavailable"
		}
		return HHListingProgress{}, firecrawlFailure(code, err)
	}
	result := HHListingProgress{Done: true, Page: 1}
	if len(cards) == 0 {
		result.PartialCode = "firecrawl_empty_confirmed"
	}
	err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		if err := validate(ctx, tx); err != nil {
			return err
		}
		handoff := CandidateHandoff{Planner: SQLCandidateHandoffPlanner{Owner: e.owner}}
		for _, card := range cards {
			id, err := insertCompanyListingCandidate(ctx, tx, job, plan, card)
			if err != nil {
				return err
			}
			if id == 0 {
				continue
			}
			result.Created++
			candidate := job
			candidate.CandidateID = &id
			if err := handoff.Enqueue(ctx, tx, candidate); err != nil {
				return err
			}
		}
		result.Candidates = int64(len(cards))
		return nil
	})
	if err != nil {
		return HHListingProgress{}, firecrawlFailure("firecrawl_save", err)
	}
	return result, nil
}

func insertCompanyListingCandidate(ctx context.Context, tx *sql.Tx, job Job, plan firecrawlListingPlan, card companyListingCard) (int64, error) {
	identity := "url:" + stageHash(card.URL)
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM vacancy_candidate_identities WHERE identity_key=?)
		OR EXISTS(SELECT 1 FROM vacancy_candidates WHERE canonical_url=?)`, identity, card.URL).Scan(&exists)
	if err != nil {
		return 0, storageError(ctx)
	}
	if exists {
		return 0, nil
	}
	token, err := stageToken()
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	inserted, err := tx.ExecContext(ctx, `INSERT INTO vacancy_candidates
		(creation_token,company_id,company_name_raw,title_raw,title_hash,vacancy_url,canonical_url,
		source_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,'site',?,?,?,?)`,
		token, job.CompanyID, plan.name, card.Title, stageHash(card.Title), card.URL, card.URL,
		plan.url, *job.SourceID, now, now, now, now)
	if err != nil {
		return 0, storageError(ctx)
	}
	id, err := inserted.LastInsertId()
	if err != nil {
		return 0, storageError(ctx)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO vacancy_candidate_identities(identity_key,candidate_id) VALUES(?,?)", identity, id); err != nil {
		return 0, storageError(ctx)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) VALUES(?,'firecrawl',?,?)", token, *job.SourceID, now); err != nil {
		return 0, storageError(ctx)
	}
	return id, nil
}
