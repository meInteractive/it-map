package parser

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	ErrRunBusy       = errors.New("scheduler cycle already active")
	ErrRunLeaseLost  = errors.New("scheduler cycle ownership lost")
	ErrRunGeneration = errors.New("scheduler generation changed")
	ErrRunInput      = errors.New("scheduler cycle input invalid")
	ErrRunStorage    = errors.New("scheduler cycle storage unavailable")
)
var generationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type RunRepository struct {
	DB         *sql.DB
	Generation string
}
type RunOptions struct {
	RequestID, Trigger string
	Duration           time.Duration
	Diagnostic         bool
}
type Run struct {
	ID, Generation, Trigger, Status string
	StartedAt, LeaseUntil           int64
	FinishedAt                      *int64
}

// Acquired is true ONLY for a new running owner. Replayed requests never work.
type RunStart struct {
	Run              Run
	Reused, Acquired bool
}

func runID(generation, requestID string) string {
	hash := sha256.Sum256([]byte(generation + ":" + requestID))
	return hex.EncodeToString(hash[:])
}
func storageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrRunStorage
}
func (r RunRepository) lockGeneration(ctx context.Context) (*sql.Tx, error) {
	if r.DB == nil || !generationPattern.MatchString(r.Generation) {
		return nil, ErrRunInput
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, storageError(ctx)
	}
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		tx.Rollback()
		return nil, storageError(ctx)
	}
	if generation != r.Generation {
		tx.Rollback()
		return nil, ErrRunGeneration
	}
	return tx, nil
}
func runClock(ctx context.Context, tx *sql.Tx) (int64, error) {
	var now int64
	err := tx.QueryRowContext(ctx, "SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)").Scan(&now)
	return now, err
}
func readRun(ctx context.Context, tx *sql.Tx, id string) (Run, error) {
	var run Run
	var finished sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT id,generation,`trigger`,status,started_at,lease_until,finished_at FROM scheduler_runs WHERE id=? FOR UPDATE", id).Scan(&run.ID, &run.Generation, &run.Trigger, &run.Status, &run.StartedAt, &run.LeaseUntil, &finished)
	if finished.Valid {
		run.FinishedAt = &finished.Int64
	}
	return run, err
}
func (r RunRepository) recover(ctx context.Context, tx *sql.Tx, now int64, exclude string) error {
	// Keep the legacy conservative money/result fields untouched. An interrupted
	// attempt is not proof that its provider operation was free or unexecuted.
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE scheduler_attempts SET finished_at=?,status='interrupted',reason_code='interrupted',message='Обработчик прервался; сохранённый этап продолжит очередь.' WHERE status='running' AND run_id IN (SELECT id FROM scheduler_runs WHERE status='running' AND lease_until<=? AND id<>?)`, []any{now, now, exclude}},
		{`UPDATE scheduler_runs SET status='interrupted',finished_at=?,lease_until=0,reason='Обработчик прервался' WHERE status='running' AND lease_until<=? AND id<>?`, []any{now, now, exclude}},
		{`UPDATE scheduler_jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'pending' END,locked_by='',locked_until=0,run_after=GREATEST(run_after,?),reason_code='interrupted',last_error='Предыдущая попытка прервана; результаты и резерв расходов сохранены.',updated_at=? WHERE generation=? AND status='running' AND locked_until<=?`, []any{now, now, r.Generation, now}},
	}
	for _, s := range statements {
		if _, err := tx.ExecContext(ctx, s.query, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// Begin serializes all Go cycle owners on the existing generation singleton.
// No background goroutine, settings write or provider call is started here.
func (r RunRepository) Begin(ctx context.Context, options RunOptions) (RunStart, error) {
	if options.RequestID == "" || !utf8.ValidString(options.RequestID) || len(utf16.Encode([]rune(options.RequestID))) > 500 {
		return RunStart{}, ErrRunInput
	}
	if options.Trigger == "" {
		options.Trigger = "manual"
	}
	if options.Trigger != "manual" && options.Trigger != "automatic" && options.Trigger != "probe" {
		return RunStart{}, ErrRunInput
	}
	if options.Duration == 0 {
		options.Duration = 110 * time.Second
	}
	if options.Duration < time.Millisecond || options.Duration > 5*time.Minute {
		return RunStart{}, ErrRunInput
	}
	tx, err := r.lockGeneration(ctx)
	if err != nil {
		return RunStart{}, err
	}
	defer tx.Rollback()
	now, err := runClock(ctx, tx)
	if err != nil {
		return RunStart{}, storageError(ctx)
	}
	var settingsJSON []byte
	if err = tx.QueryRowContext(ctx, "SELECT scheduler_json FROM firecrawl_settings WHERE id=1").Scan(&settingsJSON); err != nil {
		return RunStart{}, storageError(ctx)
	}
	var settings struct{ Enabled bool }
	if json.Unmarshal(settingsJSON, &settings) != nil {
		return RunStart{}, ErrRunStorage
	}
	id := runID(r.Generation, options.RequestID)
	old, err := readRun(ctx, tx, id)
	if err == nil {
		if old.Status == "running" && old.LeaseUntil <= now {
			if err = r.recover(ctx, tx, now, ""); err != nil {
				return RunStart{}, storageError(ctx)
			}
			old, err = readRun(ctx, tx, id)
			if err != nil {
				return RunStart{}, storageError(ctx)
			}
		}
		if err = tx.Commit(); err != nil {
			return RunStart{}, storageError(ctx)
		}
		return RunStart{Run: old, Reused: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RunStart{}, storageError(ctx)
	}
	var active int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_runs WHERE status='running' AND lease_until>?", now).Scan(&active); err != nil {
		return RunStart{}, storageError(ctx)
	}
	if active != 0 {
		return RunStart{}, ErrRunBusy
	}
	run := Run{ID: id, Generation: r.Generation, Trigger: options.Trigger, Status: "paused", StartedAt: now}
	acquired := settings.Enabled || options.Diagnostic
	if acquired {
		run.Status = "running"
		run.LeaseUntil = now + options.Duration.Milliseconds() + 30000
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status,lease_until) VALUES(?,?,?,?,?,?)", id, r.Generation, options.Trigger, now, run.Status, run.LeaseUntil); err != nil {
		return RunStart{}, storageError(ctx)
	}
	if acquired {
		if err = r.recover(ctx, tx, now, id); err != nil {
			return RunStart{}, storageError(ctx)
		}
	}
	if err = tx.Commit(); err != nil {
		return RunStart{}, storageError(ctx)
	}
	return RunStart{Run: run, Acquired: acquired}, nil
}

// AssertActive is a snapshot check, not a transaction around a paid operation.
// Future job mutations must carry the same fence in their atomic SQL statement.
func (r RunRepository) AssertActive(ctx context.Context, id string) error {
	if r.DB == nil || !generationPattern.MatchString(r.Generation) {
		return ErrRunInput
	}
	var active int
	err := r.DB.QueryRowContext(ctx, `SELECT 1 FROM scheduler_runs r JOIN app_runtime_generation g ON g.id=1 AND g.generation=r.generation WHERE r.id=? AND r.generation=? AND r.status='running' AND r.lease_until>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)`, id, r.Generation).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRunLeaseLost
	}
	if err != nil {
		return storageError(ctx)
	}
	return nil
}

// Finish can release only the current unexpired owner, never a replayed expired
// owner or an owner fenced by restore. Existing counters/reservations are kept.
func (r RunRepository) Finish(ctx context.Context, id, status string) error {
	if status != "completed" && status != "partial" && status != "interrupted" {
		return ErrRunInput
	}
	tx, err := r.lockGeneration(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, err := runClock(ctx, tx)
	if err != nil {
		return storageError(ctx)
	}
	result, err := tx.ExecContext(ctx, `UPDATE scheduler_runs SET status=?,finished_at=?,lease_until=0 WHERE id=? AND generation=? AND status='running' AND lease_until>?`, status, now, id, r.Generation, now)
	if err != nil {
		return storageError(ctx)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return storageError(ctx)
	}
	if changed != 1 {
		return ErrRunLeaseLost
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}
