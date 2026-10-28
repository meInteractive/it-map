package parser

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"itmap/internal/backup"
)

var ErrJobLeaseLost = errors.New("scheduler job ownership lost")

type Job struct {
	ID, Generation, DedupeKey, Type, Status                                      string
	CompanyID, AgencyID, CandidateID                                             *int64
	SourceID                                                                     *string
	Payload                                                                      json.RawMessage
	Priority, RunAfter, Attempts, MaxAttempts, LockedUntil, CreatedAt, UpdatedAt int64
	LastError, ReasonCode                                                        string
	CompletedAt                                                                  *int64
}
type ClaimOptions struct {
	Types                         []string
	Lease                         time.Duration
	Diagnostic, DeferredApifyOnly bool
	AllowApify, AllowApifyStarts  *bool
	JobID                         *string
}
type FinishOptions struct {
	Status, Code, Message string
	Delay                 time.Duration
	Payload               json.RawMessage
	Deferred              bool
}

// JobLease's owner/generation/run/job fences are opaque. A mutable Job view can
// never be rebound to another run or used as an authorization credential.
type JobLease struct {
	repo                RunRepository
	runID, jobID, owner string
	job                 Job
	claimedAt           int64
}

func cloneInt(p *int64) *int64 {
	if p == nil {
		return nil
	}
	n := *p
	return &n
}
func (j *JobLease) Job() Job {
	if j == nil {
		return Job{}
	}
	out := j.job
	out.Payload = bytes.Clone(out.Payload)
	out.CompanyID = cloneInt(out.CompanyID)
	out.AgencyID = cloneInt(out.AgencyID)
	out.CandidateID = cloneInt(out.CandidateID)
	out.CompletedAt = cloneInt(out.CompletedAt)
	if out.SourceID != nil {
		s := *out.SourceID
		out.SourceID = &s
	}
	return out
}
func (j *JobLease) ClaimedAt() int64 {
	if j == nil {
		return 0
	}
	return j.claimedAt
}
func nullableInt(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

const jobColumns = "id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,status,priority,run_after,attempts,max_attempts,locked_until,last_error,reason_code,created_at,updated_at,completed_at"

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var company, agency, candidate, completed sql.NullInt64
	var source sql.NullString
	e := row.Scan(&j.ID, &j.Generation, &j.DedupeKey, &j.Type, &company, &agency, &candidate, &source, &j.Payload, &j.Status, &j.Priority, &j.RunAfter, &j.Attempts, &j.MaxAttempts, &j.LockedUntil, &j.LastError, &j.ReasonCode, &j.CreatedAt, &j.UpdatedAt, &completed)
	j.CompanyID, j.AgencyID, j.CandidateID, j.CompletedAt = nullableInt(company), nullableInt(agency), nullableInt(candidate), nullableInt(completed)
	if source.Valid {
		j.SourceID = &source.String
	}
	return j, e
}
func activeRun(ctx context.Context, tx *sql.Tx, generation, id string) (int64, error) {
	run, e := readRun(ctx, tx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, ErrRunLeaseLost
	}
	if e != nil {
		return 0, storageError(ctx)
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return 0, storageError(ctx)
	}
	if run.Generation != generation || run.Status != "running" || run.LeaseUntil <= now {
		return 0, ErrRunLeaseLost
	}
	return now, nil
}

// ClaimJob selects source-compatible due work under generation/run ownership.
// It does not enqueue work, start a goroutine or contact a provider. Candidate
// scanning is streamed; only provider cursor keys and the current winner stay
// in memory. SQL ownership changes only after complete selection succeeds.
func (r RunRepository) ClaimJob(ctx context.Context, runID string, options ClaimOptions) (*JobLease, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if options.Lease == 0 {
		options.Lease = 5 * time.Minute
	}
	if options.Lease < time.Millisecond || options.Lease > time.Hour {
		return nil, ErrJobInput
	}
	tx, e := r.lockGeneration(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	now, e := activeRun(ctx, tx, r.Generation, runID)
	if e != nil {
		return nil, e
	}
	settings := DefaultSettings()
	var raw []byte
	if e = tx.QueryRowContext(ctx, "SELECT scheduler_json FROM firecrawl_settings WHERE id=1").Scan(&raw); e != nil || json.Unmarshal(raw, &settings) != nil {
		return nil, storageError(ctx)
	}
	if !settings.Enabled && !options.Diagnostic {
		return nil, nil
	}
	settings.AgenciesEnabled, e = claimAgenciesEnabled(raw)
	if e != nil {
		return nil, storageError(ctx)
	}
	types, e := claimTypes(options.Types)
	if e != nil {
		return nil, e
	}
	if len(types) == 0 {
		return nil, nil
	}
	view, e := readClaimView(ctx, tx, r.Generation)
	if e != nil {
		return nil, storageError(ctx)
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+jobColumns+" FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=?", r.Generation, now)
	if e != nil {
		return nil, storageError(ctx)
	}
	var best *Job
	bestPage := false
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, storageError(ctx)
		}
		eligible, page, err := claimEligible(job, now, settings, options, types, view)
		if err != nil {
			rows.Close()
			return nil, storageError(ctx)
		}
		if eligible && (best == nil || claimBefore(job, page, *best, bestPage, now)) {
			copy := job
			best = &copy
			bestPage = page
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, storageError(ctx)
	}
	if best == nil {
		return nil, nil
	}
	if _, e = activeRun(ctx, tx, r.Generation, runID); e != nil {
		return nil, e
	}
	var token [16]byte
	if _, e = rand.Read(token[:]); e != nil {
		return nil, ErrRunStorage
	}
	token[6] = token[6]&15 | 64
	token[8] = token[8]&63 | 128
	owner := fmt.Sprintf("%x-%x-%x-%x-%x", token[:4], token[4:6], token[6:8], token[8:10], token[10:])
	result, e := tx.ExecContext(ctx, "UPDATE scheduler_jobs SET status='running',locked_by=?,locked_until=?,attempts=attempts+1,updated_at=? WHERE id=? AND generation=? AND status='pending'", owner, now+options.Lease.Milliseconds(), now, best.ID, r.Generation)
	if e != nil {
		return nil, storageError(ctx)
	}
	changed, e := result.RowsAffected()
	if e != nil {
		return nil, storageError(ctx)
	}
	if changed != 1 {
		return nil, ErrJobLeaseLost
	}
	if e = tx.Commit(); e != nil {
		return nil, storageError(ctx)
	}
	best.Status = "running"
	best.Attempts++
	best.LockedUntil = now + options.Lease.Milliseconds()
	best.UpdatedAt = now
	return &JobLease{repo: r, runID: runID, jobID: best.ID, owner: owner, job: *best, claimedAt: now}, nil
}
func (j *JobLease) beginOwned(ctx context.Context) (*sql.Tx, int64, error) {
	if j == nil || j.owner == "" || j.jobID == "" {
		return nil, 0, ErrJobInput
	}
	tx, e := j.repo.lockGeneration(ctx)
	if e != nil {
		return nil, 0, e
	}
	fail := func(e error) (*sql.Tx, int64, error) { tx.Rollback(); return nil, 0, e }
	if _, e = activeRun(ctx, tx, j.repo.Generation, j.runID); e != nil {
		return fail(e)
	}
	var owner, status string
	var until int64
	if e = tx.QueryRowContext(ctx, "SELECT locked_by,locked_until,status FROM scheduler_jobs WHERE id=? AND generation=? FOR UPDATE", j.jobID, j.repo.Generation).Scan(&owner, &until, &status); errors.Is(e, sql.ErrNoRows) {
		return fail(ErrJobLeaseLost)
	} else if e != nil {
		return fail(storageError(ctx))
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return fail(storageError(ctx))
	}
	if owner != j.owner || status != "running" || until <= now {
		return fail(ErrJobLeaseLost)
	}
	// Recheck run time after acquiring the job row, which may have waited.
	if _, e = activeRun(ctx, tx, j.repo.Generation, j.runID); e != nil {
		return fail(e)
	}
	return tx, now, nil
}

// WithinOwned atomically fences every SQL read/write with current generation,
// unexpired run and the opaque job owner. fn must contain bounded SELECT/DML
// only, no DDL, transaction control or external work. The callback and all partial changes roll back together on failure.
func (j *JobLease) WithinOwned(ctx context.Context, fn func(*sql.Tx) error) error {
	if fn == nil {
		return ErrJobInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, _, e := j.beginOwned(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = fn(tx); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return storageError(ctx)
	}
	return nil
}
func (j *JobLease) prepareFinish(options FinishOptions) (FinishOptions, json.RawMessage, error) {
	if j == nil {
		return options, nil, ErrJobInput
	}
	if options.Status == "" {
		options.Status = "completed"
	}
	switch options.Status {
	case "pending", "running", "completed", "failed", "cancelled":
	default:
		return options, nil, ErrJobInput
	}
	if !utf8.ValidString(options.Code) || !utf8.ValidString(options.Message) {
		return options, nil, ErrJobInput
	}
	payload := options.Payload
	if len(bytes.TrimSpace(payload)) == 0 || bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		payload = j.job.Payload
	}
	canonical, e := backup.StringifyParsedJSON(payload)
	if e != nil {
		return options, nil, ErrJobInput
	}
	return options, canonical, nil
}
func (j *JobLease) Finish(ctx context.Context, options FinishOptions) error {
	options, canonical, e := j.prepareFinish(options)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, now, e := j.beginOwned(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = j.finishLocked(ctx, tx, now, options, canonical); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return storageError(ctx)
	}
	return nil
}

// Caller holds generation/run/job locks. The attempt receipt can join this TX.
func (j *JobLease) finishLocked(ctx context.Context, tx *sql.Tx, now int64, options FinishOptions, canonical json.RawMessage) error {
	var completed any
	if options.Status == "completed" || options.Status == "failed" || options.Status == "cancelled" {
		completed = now
	}
	deferred := 0
	if options.Deferred {
		deferred = 1
	}
	result, e := tx.ExecContext(ctx, "UPDATE scheduler_jobs SET status=?,attempts=GREATEST(0,attempts-?),reason_code=?,last_error=?,run_after=?,payload=?,locked_by='',locked_until=0,completed_at=?,updated_at=? WHERE id=? AND generation=? AND locked_by=? AND status='running' AND locked_until>?", options.Status, deferred, options.Code, options.Message, now+options.Delay.Milliseconds(), string(canonical), completed, now, j.jobID, j.repo.Generation, j.owner, now)
	if e != nil {
		return storageError(ctx)
	}
	n, e := result.RowsAffected()
	if e != nil {
		return storageError(ctx)
	}
	if n != 1 {
		return ErrJobLeaseLost
	}
	return nil
}

// Snapshot assertion is useful just before external work but cannot replace
// WithinOwned for SQL writes. Provider work never belongs inside an SQL TX.
func (j *JobLease) AssertActive(ctx context.Context) error {
	_, e := j.remaining(ctx)
	return e
}
func (j *JobLease) remaining(ctx context.Context) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if j == nil || j.owner == "" {
		return 0, ErrJobInput
	}
	var remaining int64
	clock := "CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)"
	e := j.repo.DB.QueryRowContext(ctx, "SELECT LEAST(j.locked_until,r.lease_until)-"+clock+" FROM scheduler_jobs j JOIN scheduler_runs r ON r.id=? AND r.generation=j.generation JOIN app_runtime_generation g ON g.id=1 AND g.generation=j.generation WHERE j.id=? AND j.generation=? AND j.locked_by=? AND j.status='running' AND j.locked_until>"+clock+" AND r.status='running' AND r.lease_until>"+clock, j.runID, j.jobID, j.repo.Generation, j.owner).Scan(&remaining)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, ErrJobLeaseLost
	}
	if e != nil {
		return 0, storageError(ctx)
	}
	if remaining <= 0 {
		return 0, ErrJobLeaseLost
	}
	if remaining > 100000 {
		remaining = 100000
	}
	return time.Duration(remaining) * time.Millisecond, nil
}
