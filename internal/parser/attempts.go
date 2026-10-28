package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"itmap/internal/backup"
)

var (
	ErrAttemptReplay  = errors.New("scheduler attempt already started")
	ErrAttemptClosed  = errors.New("scheduler attempt is not active")
	ErrBudgetLimit    = errors.New("scheduler day or month budget exhausted")
	ErrMoneyUncertain = errors.New("scheduler request outcome unresolved; paid replay denied")
)

// Attempt is an execution handle, never reconstructed from a public receipt ID.
// Every copy shares state, so copying a handle cannot bypass its work mutex.
type Attempt struct{ state *attemptState }
type attemptState struct {
	mu              sync.Mutex
	lease           JobLease
	id              string
	unknown, closed atomic.Bool
	requests        atomic.Int64
}
type AttemptFinish struct {
	Job     FinishOptions
	Credits *float64
	Result  json.RawMessage
}

func attemptIdentity(j *JobLease) string {
	b := sha256.Sum256([]byte(j.repo.Generation + ":" + j.jobID + ":" + j.owner))
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func (j *JobLease) StartAttempt(ctx context.Context) (*Attempt, error) {
	if j == nil || j.owner == "" {
		return nil, ErrJobInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, now, e := j.beginOwned(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	id := attemptIdentity(j)
	var exists int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE id=?", id).Scan(&exists); e != nil {
		return nil, storageError(ctx)
	}
	if exists != 0 {
		return nil, ErrAttemptReplay
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO scheduler_attempts(id,job_id,run_id,type,company_id,source_id,started_at,status) VALUES(?,?,?,?,?,?,?,'running')", id, j.jobID, j.runID, j.job.Type, j.job.CompanyID, j.job.SourceID, now)
	if e != nil {
		return nil, storageError(ctx)
	}
	if e = tx.Commit(); e != nil {
		return nil, storageError(ctx)
	}
	return &Attempt{state: &attemptState{lease: *j, id: id}}, nil
}
func (a *Attempt) ID() string {
	if a == nil || a.state == nil {
		return ""
	}
	return a.state.id
}
func (a *Attempt) Requests() int {
	if a == nil || a.state == nil {
		return 0
	}
	s := a.state
	return int(s.requests.Load())
}
func (a *Attempt) Unknown() bool {
	if a == nil || a.state == nil {
		return true
	}
	s := a.state
	return s.unknown.Load()
}

func (s *attemptState) locked(ctx context.Context) (*sql.Tx, int64, error) {
	tx, now, e := s.lease.beginOwned(ctx)
	if e != nil {
		return nil, 0, e
	}
	var job, run, status string
	e = tx.QueryRowContext(ctx, "SELECT job_id,run_id,status FROM scheduler_attempts WHERE id=? FOR UPDATE", s.id).Scan(&job, &run, &status)
	if e != nil {
		tx.Rollback()
		if errors.Is(e, sql.ErrNoRows) {
			return nil, 0, ErrAttemptClosed
		}
		return nil, 0, storageError(ctx)
	}
	if job != s.lease.jobID || run != s.lease.runID || status != "running" {
		tx.Rollback()
		return nil, 0, ErrAttemptClosed
	}
	return tx, now, nil
}

// Only JSON numbers in validated scheduler settings authorize paid requests.
// Invalid/missing limits fail closed; they cannot exploit SQLite text ordering.
func budgetLimit(raw []byte, key string) (float64, error) {
	v, e := jsonPathFirst(raw, []string{key})
	if e != nil {
		return 0, ErrBudgetUnknown
	}
	if len(v) == 0 || string(v) == "null" {
		return 0, nil
	}
	var n float64
	if json.Unmarshal(v, &n) != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 10000 {
		return 0, ErrBudgetUnknown
	}
	return n, nil
}
func (s *attemptState) reserve(ctx context.Context, upper float64, provider, model string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, now, e := s.locked(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if upper > 0 {
		var unresolved int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND id<>? AND cost_usd IS NULL AND reserved_usd>0", s.lease.jobID, s.id).Scan(&unresolved); e != nil {
			return storageError(ctx)
		}
		if unresolved != 0 {
			return ErrMoneyUncertain
		}
		var raw []byte
		if tx.QueryRowContext(ctx, "SELECT scheduler_json FROM firecrawl_settings WHERE id=1").Scan(&raw) != nil {
			return storageError(ctx)
		}
		day, e := budgetLimit(raw, "dayBudgetUsd")
		if e != nil {
			return e
		}
		month, e := budgetLimit(raw, "monthBudgetUsd")
		if e != nil {
			return e
		}
		period := SchedulerPeriod(time.UnixMilli(now))
		var daily, monthly float64
		// Global ledger deliberately has no generation predicate. Restore cannot
		// reset spending, including reservations owned by an expired generation.
		if e = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(CASE WHEN started_at>=? THEN COALESCE(cost_usd,reserved_usd) ELSE 0 END),0),COALESCE(SUM(COALESCE(cost_usd,reserved_usd)),0) FROM scheduler_attempts WHERE started_at>=?", period.Day, period.Month).Scan(&daily, &monthly); e != nil {
			return storageError(ctx)
		}
		if !finiteMoney(daily) || !finiteMoney(monthly) || daily+upper > day || monthly+upper > month {
			return ErrBudgetLimit
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE scheduler_attempts SET reserved_usd=reserved_usd+?,provider=?,model=? WHERE id=?", upper, provider, model, s.id); e != nil {
		return storageError(ctx)
	}
	if e = tx.Commit(); e != nil {
		return storageError(ctx)
	}
	return nil
}
func finiteMoney(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 }
func (s *attemptState) settle(ctx context.Context, upper, cost float64) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, _, e := s.locked(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "UPDATE scheduler_attempts SET reserved_usd=GREATEST(0,reserved_usd-?)+? WHERE id=?", upper, cost, s.id); e != nil {
		return storageError(ctx)
	}
	if e = tx.Commit(); e != nil {
		return storageError(ctx)
	}
	return nil
}

// Run performs one explicitly requested work invocation after durable reserve.
// It never retries work or a COMMIT. Provider I/O is outside every SQL TX.
// A paid provider adapter still needs its existing durable operation identity;
// this scheduler wrapper does not implement or activate a provider protocol.
func (a *Attempt) Run(ctx context.Context, request json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	if a == nil || a.state == nil || work == nil {
		return nil, ErrJobInput
	}
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return nil, ErrAttemptClosed
	}
	if s.unknown.Load() {
		return nil, ErrMoneyUncertain
	}
	upper, e := UpperCostJSON(request)
	if e != nil {
		return nil, e
	}
	var fields map[string]json.RawMessage
	var meta struct{ Provider, Model string }
	if json.Unmarshal(request, &fields) != nil || json.Unmarshal(fields["provider"], &meta.Provider) != nil || json.Unmarshal(fields["model"], &meta.Model) != nil || meta.Provider == "" || meta.Model == "" || !utf8.ValidString(meta.Provider) || !utf8.ValidString(meta.Model) {
		return nil, ErrBudgetUnknown
	}
	if e = s.reserve(ctx, upper, meta.Provider, meta.Model); e != nil {
		if !errors.Is(e, ErrBudgetLimit) && !errors.Is(e, ErrBudgetUnknown) && !errors.Is(e, ErrMoneyUncertain) {
			s.unknown.Store(true)
		}
		return nil, e
	}
	s.requests.Add(1)
	// From the durable reservation until a known settlement COMMIT, work may
	// have happened. Keep this true even if an outer HTTP boundary recovers a
	// panic from the adapter instead of receiving its normal error return.
	s.unknown.Store(true)
	window, e := s.lease.remaining(ctx)
	if e != nil {
		s.unknown.Store(true)
		return nil, e
	}
	workCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	if e = workCtx.Err(); e != nil {
		s.unknown.Store(true)
		return nil, e
	}
	response, e := work(workCtx)
	if e != nil {
		s.unknown.Store(true)
		return nil, e
	}
	cost, known := actualCost(request, response)
	if !known {
		s.unknown.Store(true)
		return response, nil
	}
	if e = s.settle(ctx, upper, cost); e != nil {
		s.unknown.Store(true)
		return nil, e
	}
	s.unknown.Store(false)
	return response, nil
}

func (a *Attempt) Finish(ctx context.Context, options AttemptFinish) error {
	if a == nil || a.state == nil {
		return ErrJobInput
	}
	s := a.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return ErrAttemptClosed
	}
	job, payload, e := s.lease.prepareFinish(options.Job)
	if e != nil {
		return e
	}
	if options.Credits != nil && (math.IsNaN(*options.Credits) || math.IsInf(*options.Credits, 0)) {
		return ErrJobInput
	}
	result := options.Result
	if len(result) == 0 {
		result = []byte("{}")
	}
	result, e = backup.StringifyParsedJSON(result)
	if e != nil {
		return ErrJobInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, now, e := s.locked(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "UPDATE scheduler_attempts SET finished_at=?,status=?,reason_code=?,message=?,cost_usd=CASE WHEN ? THEN NULL ELSE reserved_usd END,credits=?,result_json=? WHERE id=?", now, job.Status, job.Code, job.Message, s.unknown.Load(), options.Credits, string(result), s.id); e != nil {
		return storageError(ctx)
	}
	if e = s.lease.finishLocked(ctx, tx, now, job, payload); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return storageError(ctx)
	}
	s.closed.Store(true)
	return nil
}
