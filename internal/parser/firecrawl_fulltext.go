package parser

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"time"

	"itmap/internal/integrations/firecrawl"
)

var ErrFirecrawlContent = errors.New("Firecrawl did not return a verified vacancy description")

type firecrawlFullTextExecutor struct {
	owner  string
	scrape interface {
		Execute(context.Context, *JobLease, FirecrawlScrapeStep) (firecrawl.ScrapeResult, error)
	}
}

type firecrawlTextPlan struct {
	Mode                                             string
	FirecrawlRevision                                int64
	Title                                            CandidateTitleInput
	URL, Listing, Host, Updated, SnapshotID, Created string
	SourceRevision                                   int64
	Cached                                           bool
}

func (e firecrawlFullTextExecutor) read(ctx context.Context, tx *sql.Tx, job Job) (firecrawlTextPlan, error) {
	var plan firecrawlTextPlan
	title, err := ReadCandidateTitleInput(ctx, tx, job, e.owner)
	if err != nil {
		return plan, err
	}
	plan.Title = title
	matched := false
	for _, role := range title.Roles {
		if role.Decision == nil {
			return plan, ErrCandidateChanged
		}
		matched = matched || *role.Decision == "match"
	}
	if !matched {
		return plan, ErrCandidateChanged
	}
	var main sql.NullInt64
	var updated sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT vacancy_url,title_updated_at,main_vacancy_id FROM vacancy_candidates WHERE id=? FOR UPDATE`, job.CandidateID).Scan(&plan.URL, &updated, &main)
	if err != nil {
		return plan, storageError(ctx)
	}
	plan.Updated = updated.String
	if main.Valid {
		return plan, ErrCandidateChanged
	}
	err = tx.QueryRowContext(ctx, `SELECT source_url,revision FROM vacancy_source_parsing WHERE company_id=? AND source_id='companySite' AND enabled=1 FOR UPDATE`, job.CompanyID).Scan(&plan.Listing, &plan.SourceRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return plan, ErrCandidateChanged
	}
	if err != nil {
		return plan, storageError(ctx)
	}
	listing, lErr := url.Parse(plan.Listing)
	detail, dErr := url.Parse(plan.URL)
	if lErr != nil || dErr != nil || listing.Hostname() == "" || !strings.EqualFold(listing.Hostname(), detail.Hostname()) ||
		sameFirecrawlPage(plan.Listing, plan.URL) {
		return plan, ErrCandidateChanged
	}
	plan.Host = listing.Hostname()
	if err := tx.QueryRowContext(ctx, "SELECT mode,revision FROM firecrawl_settings WHERE id=1 FOR UPDATE").Scan(&plan.Mode, &plan.FirecrawlRevision); err != nil {
		return plan, storageError(ctx)
	}
	err = tx.QueryRowContext(ctx, `SELECT id,created_at FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE`, job.CandidateID, plan.URL).Scan(&plan.SnapshotID, &plan.Created)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return plan, storageError(ctx)
	}
	cached, err := ReadRawSnapshot(ctx, tx, *job.CandidateID, plan.URL, "companySite")
	if errors.Is(err, ErrSnapshotMissing) || errors.Is(err, ErrSnapshotInvalid) || errors.Is(err, ErrSnapshotAmbiguous) {
		return plan, nil
	}
	if err != nil {
		return plan, err
	}
	if sameFirecrawlPage(plan.URL, cached.FinalURL) && len(CheckFulltextContent(cached.Markdown, 200)) == 0 {
		input := CandidateStageInput{TitleUpdatedAt: plan.Updated, Snapshot: &CandidateStageSnapshot{ID: cached.ID, FetchedAt: cached.FetchedAt}}
		refresh, err := candidateTextRefresh(input)
		plan.Cached = !refresh
		return plan, err
	}
	return plan, nil
}

func sameFirecrawlPage(a, b string) bool {
	x, xErr := url.Parse(a)
	y, yErr := url.Parse(b)
	return xErr == nil && yErr == nil && x.Hostname() != "" && strings.EqualFold(x.Hostname(), y.Hostname()) &&
		strings.TrimSuffix(x.EscapedPath(), "/") == strings.TrimSuffix(y.EscapedPath(), "/") && x.RawQuery == y.RawQuery
}

func (e firecrawlFullTextExecutor) Execute(ctx context.Context, lease *JobLease) (HHFullTextResult, error) {
	job := lease.Job()
	if e.scrape == nil || job.Type != "fetch_full_text" || job.CompanyID == nil || job.AgencyID != nil ||
		job.CandidateID == nil || job.SourceID == nil || *job.SourceID != "companySite" {
		return HHFullTextResult{}, ErrJobInput
	}
	var plan firecrawlTextPlan
	if err := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		var err error
		plan, err = e.read(ctx, tx, job)
		return err
	}); err != nil {
		return HHFullTextResult{}, err
	}
	validate := func(ctx context.Context, tx *sql.Tx) error {
		current, err := e.read(ctx, tx, job)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, plan) {
			return ErrCandidateChanged
		}
		return nil
	}
	var document firecrawl.ScrapeResult
	started := time.Now()
	if !plan.Cached {
		bounded, cancel := context.WithDeadline(ctx, time.UnixMilli(job.LockedUntil))
		defer cancel()
		var err error
		document, err = e.scrape.Execute(bounded, lease, FirecrawlScrapeStep{ID: "company-detail-v1", URL: plan.URL, AllowedHosts: []string{plan.Host}, MarkdownOnly: true, OnlyMainContent: true, validate: validate})
		if err != nil {
			return HHFullTextResult{}, err
		}
		if !sameFirecrawlPage(plan.URL, document.FinalURL) || len(CheckFulltextContent(document.Markdown, document.TargetStatus)) != 0 {
			return HHFullTextResult{}, ErrFirecrawlContent
		}
	}
	result := HHFullTextResult{SnapshotID: plan.SnapshotID, Reused: plan.Cached}
	err := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		if err := validate(ctx, tx); err != nil {
			return err
		}
		if !plan.Cached {
			var err error
			result.SnapshotID, err = storeFirecrawlFullText(ctx, tx, job, plan, document, started)
			if err != nil {
				return err
			}
		}
		handoff := CandidateHandoff{Planner: SQLCandidateHandoffPlanner{Owner: e.owner}}
		return handoff.Enqueue(ctx, tx, job)
	})
	return result, err
}

func storeFirecrawlFullText(ctx context.Context, tx *sql.Tx, job Job, plan firecrawlTextPlan, doc firecrawl.ScrapeResult, started time.Time) (string, error) {
	id, err := stageToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	if old, err := time.Parse(time.RFC3339Nano, plan.Created); err == nil && now.Truncate(time.Millisecond).Compare(old) <= 0 {
		now = old.Add(time.Millisecond)
	}
	err = insertFullTextSnapshot(ctx, tx, id, *job.CandidateID, plan.URL, doc.FinalURL, doc.Markdown, stageHash(doc.Markdown),
		now.Format("2006-01-02T15:04:05.000Z"), plan.Mode, doc.ReportedCredits, []byte(`{"valid":true,"errors":[],"provider":"firecrawl"}`))
	if err != nil {
		return "", err
	}
	fetchID, err := stageToken()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO vacancy_content_fetches
		(id,candidate_id,snapshot_id,source_url,final_url,firecrawl_mode,credits,started_at,duration_ms,status,message)
		VALUES(?,?,?,?,?,?,?,?,?,'success','')`, fetchID, job.CandidateID, id, plan.URL, doc.FinalURL, plan.Mode, doc.ReportedCredits,
		started.UnixMilli(), time.Since(started).Milliseconds())
	if err != nil {
		return "", storageError(ctx)
	}
	return id, nil
}
