package parser

import (
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
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled || e.owner != settings.OwnerName || !slices.Contains(settings.SourceIDs, "companySite") {
		return plan, ErrSettingsChanged
	}
	err = tx.QueryRowContext(ctx, `SELECT c.company_name,s.source_url,s.config_json,s.revision
		FROM company_identity c JOIN vacancy_source_parsing s ON s.company_id=c.id
		WHERE c.id=? AND s.source_id='companySite' AND s.enabled=1 AND s.auto_enabled=1
		AND (?=0 OR EXISTS(SELECT 1 FROM company_user_state u WHERE u.company_name=c.company_name AND u.user_name=? AND u.favorite=1))
		FOR UPDATE`, job.CompanyID, row.favorite, e.owner).Scan(&plan.name, &plan.url, &plan.config, &plan.revision)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, ErrCandidateChanged
	}
	if err != nil {
		return plan, storageError(ctx)
	}
	// This executor handles the generic Markdown adapter only.
	var config map[string]json.RawMessage
	if json.Unmarshal([]byte(plan.config), &config) != nil || len(config) != 0 {
		return plan, ErrWorkerUnsupported
	}
	u, err := url.Parse(plan.url)
	if err != nil || u.Hostname() == "" {
		return plan, ErrCandidateChanged
	}
	plan.host = strings.ToLower(u.Hostname())
	switch plan.host {
	case "jobs.yoomoney.ru", "selectel.ru":
		return plan, ErrWorkerUnsupported
	}
	return plan, nil
}

func (e firecrawlListingExecutor) Execute(ctx context.Context, lease *JobLease) (HHListingProgress, error) {
	job := lease.Job()
	if e.scrape == nil || job.Type != "scan_source" || job.CompanyID == nil || job.AgencyID != nil ||
		job.CandidateID != nil || job.SourceID == nil || *job.SourceID != "companySite" {
		return HHListingProgress{}, ErrJobInput
	}
	var plan firecrawlListingPlan
	if err := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		var err error
		plan, err = e.read(ctx, tx, job)
		return err
	}); err != nil {
		return HHListingProgress{}, err
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
		return HHListingProgress{}, errFirecrawlListing
	}
	cards, err := companyListingCards(doc.Markdown, doc.FinalURL)
	if err != nil {
		return HHListingProgress{}, err
	}
	result := HHListingProgress{Done: true, Page: 1}
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
		return HHListingProgress{}, err
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
		VALUES(?,?,?,?,?,?,?,?,'companySite','site',?,?,?,?)`,
		token, job.CompanyID, plan.name, card.Title, stageHash(card.Title), card.URL, card.URL,
		plan.url, now, now, now, now)
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
	if _, err := tx.ExecContext(ctx, "INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) VALUES(?,'firecrawl','companySite',?)", token, now); err != nil {
		return 0, storageError(ctx)
	}
	return id, nil
}
