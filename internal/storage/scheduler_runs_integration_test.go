//go:build mariadb && schedulerwrites

package storage

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/parser"
)

const schedulerAttempt = "CI_scheduler_attempt"

func schedulerDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("scheduler writes require disposable GitHub CI")
		}
		t.Skip("scheduler writes require disposable GitHub CI")
	}
	return openDisposableMariaDB(t)
}

type schedulerFixture struct {
	t    *testing.T
	ctx  context.Context
	db   *sql.DB
	repo parser.RunRepository
}

func (f *schedulerFixture) sub(t *testing.T) *schedulerFixture { copy := *f; copy.t = t; return &copy }
func (f *schedulerFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, query, args...); err != nil {
		f.t.Fatal("synthetic scheduler fixture write failed")
	}
}
func (f *schedulerFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		f.t.Fatal("synthetic scheduler fixture count failed")
	}
	return n
}
func (f *schedulerFixture) begin(key string) parser.RunStart {
	f.t.Helper()
	v, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: key})
	if err != nil || !v.Acquired || v.Reused || v.Run.Status != "running" || v.Run.LeaseUntil-v.Run.StartedAt != 140000 {
		f.t.Fatal("new scheduler owner not acquired")
	}
	return v
}
func (f *schedulerFixture) finish(run parser.RunStart) {
	f.t.Helper()
	if err := f.repo.Finish(f.ctx, run.Run.ID, "completed"); err != nil {
		f.t.Fatal("scheduler owner not released")
	}
}
func openSchedulerFixture(t *testing.T) *schedulerFixture {
	t.Helper()
	ctx, db := schedulerDB(t)
	f := &schedulerFixture{t: t, ctx: ctx, db: db, repo: parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}}
	for _, table := range []string{"scheduler_runs", "scheduler_attempts", "scheduler_jobs", "firecrawl_settings"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("scheduler test namespace must be empty")
		}
	}
	// Only these four previously empty tables are owned. Baseline business rows
	// and the run marker are checked again by all following readonly smoke gates.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range []string{"scheduler_attempts", "scheduler_jobs", "scheduler_runs", "firecrawl_settings"} {
			if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				t.Error("synthetic scheduler cleanup failed")
			}
			var n int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Error("synthetic scheduler namespace not restored")
			}
		}
		var gen string
		if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&gen); err != nil || gen != f.repo.Generation {
			t.Error("scheduler cleanup changed generation")
		}
	})
	f.exec(`INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,'{"enabled":true}')`)
	return f
}
func (f *schedulerFixture) attempt(run parser.RunStart, id string) {
	f.t.Helper()
	f.exec(`INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,result_json) VALUES(?,?,?,'diagnostic',?,'running',0.26,'{"synthetic":"retained"}')`, id, "CI_scheduler_job", run.Run.ID, run.Run.StartedAt)
}
func (f *schedulerFixture) assertAttempt(id, status string) {
	f.t.Helper()
	var got, result string
	var reserve float64
	var cost sql.NullFloat64
	if err := f.db.QueryRowContext(f.ctx, "SELECT status,reserved_usd,cost_usd,result_json FROM scheduler_attempts WHERE id=?", id).Scan(&got, &reserve, &cost, &result); err != nil || got != status || reserve != 0.26 || cost.Valid || result != `{"synthetic":"retained"}` {
		f.t.Fatal("recovery lost conservative reservation/result")
	}
}

func TestMariaDBSchedulerRunsDisposable(t *testing.T) {
	if os.Getenv("ITMAP_SCHEDULER_CRASH_CHILD") == "1" {
		schedulerCrashChild(t)
		return
	}
	f := openSchedulerFixture(t)
	t.Run("replay expiry recovery and job fences", func(t *testing.T) {
		f := f.sub(t)
		run := f.begin("CI recovery")
		f.attempt(run, schedulerAttempt)
		if err := f.repo.AssertActive(f.ctx, run.Run.ID); err != nil {
			t.Fatal("new owner not active")
		}
		replay, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: "CI recovery"})
		if err != nil || !replay.Reused || replay.Acquired || replay.Run.LeaseUntil != run.Run.LeaseUntil {
			t.Fatal("replay extended lease/acquired work")
		}
		for _, job := range []struct {
			id, gen  string
			attempts int
			lock     int64
		}{{"CI_expired_retry", f.repo.Generation, 1, 0}, {"CI_expired_failed", f.repo.Generation, 4, 0}, {"CI_unexpired", f.repo.Generation, 1, run.Run.LeaseUntil + 600000}, {"CI_old_generation", "11111111-2222-4333-8444-555555555555", 1, 0}} {
			f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,status,priority,run_after,attempts,max_attempts,locked_by,locked_until,created_at,updated_at) VALUES(?,?,?,'diagnostic','running',0,0,?,4,'CI_old_owner',?,0,0)`, job.id, job.gen, job.id, job.attempts, job.lock)
		}
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
		next := f.begin("CI replacement")
		f.assertAttempt(schedulerAttempt, "interrupted")
		if f.count("SELECT COUNT(*) FROM scheduler_runs WHERE id=? AND status='interrupted' AND lease_until=0", run.Run.ID) != 1 {
			t.Fatal("expired owner not terminal")
		}
		if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_expired_retry' AND status='pending' AND locked_by='' AND locked_until=0 AND attempts=1 AND run_after>=?", next.Run.StartedAt) != 1 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_expired_failed' AND status='failed' AND attempts=4 AND locked_until=0") != 1 {
			t.Fatal("expired jobs not recovered by max attempts")
		}
		if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_unexpired' AND status='running' AND locked_by='CI_old_owner' AND locked_until>?", next.Run.StartedAt) != 1 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_old_generation' AND status='running' AND locked_by='CI_old_owner'") != 1 {
			t.Fatal("unexpired/other-generation jobs were stolen")
		}
		if !errors.Is(f.repo.Finish(f.ctx, run.Run.ID, "completed"), parser.ErrRunLeaseLost) || !errors.Is(f.repo.AssertActive(f.ctx, run.Run.ID), parser.ErrRunLeaseLost) {
			t.Fatal("expired owner not fenced")
		}
		old, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: "CI recovery"})
		if err != nil || !old.Reused || old.Acquired || old.Run.Status != "interrupted" {
			t.Fatal("old request resurrected")
		}
		f.finish(next)
		again, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: "CI replacement"})
		if err != nil || again.Acquired || !again.Reused || again.Run.Status != "completed" {
			t.Fatal("terminal receipt replay reran work")
		}
	})
	t.Run("disabled versus diagnostic", func(t *testing.T) {
		f := f.sub(t)
		f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":false}' WHERE id=1`)
		paused, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: "CI paused"})
		if err != nil || paused.Acquired || paused.Run.Status != "paused" || paused.Run.LeaseUntil != 0 {
			t.Fatal("disabled parser acquired normal work")
		}
		probe, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: "CI probe", Trigger: "probe", Diagnostic: true})
		if err != nil || !probe.Acquired {
			t.Fatal("explicit diagnostic not acquired")
		}
		f.finish(probe)
		f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true}' WHERE id=1`)
	})
	t.Run("two actual connections compete for one owner", func(t *testing.T) {
		f := f.sub(t)
		_, dbA := schedulerDB(t)
		_, dbB := schedulerDB(t)
		dbA.SetMaxOpenConns(1)
		dbB.SetMaxOpenConns(1)
		var idA, idB int64
		if dbA.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&idA) != nil || dbB.QueryRowContext(f.ctx, "SELECT CONNECTION_ID()").Scan(&idB) != nil || idA == idB {
			t.Fatal("two different connections required")
		}
		held, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer held.Rollback()
		var gen string
		if held.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&gen) != nil {
			t.Fatal("cannot hold scheduler mutex")
		}
		type result struct {
			start parser.RunStart
			err   error
			key   string
		}
		results := make(chan result, 2)
		for i, db := range []*sql.DB{dbA, dbB} {
			go func(i int, db *sql.DB) {
				key := fmt.Sprintf("CI competing %d", i)
				start, err := (parser.RunRepository{DB: db, Generation: f.repo.Generation}).Begin(f.ctx, parser.RunOptions{RequestID: key})
				results <- result{start, err, key}
			}(i, db)
		}
		observed := false
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if f.count("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE 'SELECT generation FROM app_runtime_generation%FOR UPDATE'", idA, idB) == 2 {
				observed = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !observed {
			t.Fatal("both real callers must be blocked on generation row")
		}
		if err = held.Commit(); err != nil {
			t.Fatal(err)
		}
		first, second := <-results, <-results
		winner, loser := first, second
		if !winner.start.Acquired {
			winner, loser = second, first
		}
		if winner.err != nil || !winner.start.Acquired || !errors.Is(loser.err, parser.ErrRunBusy) || f.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='running'") != 1 {
			t.Fatal("competing workers did not produce exactly one owner")
		}
		f.finish(winner.start)
		retry, err := f.repo.Begin(f.ctx, parser.RunOptions{RequestID: loser.key})
		if err != nil || !retry.Acquired {
			t.Fatal("busy caller cannot retry after release")
		}
		f.finish(retry)
	})
	t.Run("generation fence also prevents a second owner after restore", func(t *testing.T) {
		f := f.sub(t)
		run := f.begin("CI before restore")
		nextGen := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := f.db.ExecContext(ctx, "UPDATE app_runtime_generation SET generation=? WHERE id=1", f.repo.Generation); err != nil {
				t.Error("cannot restore owned synthetic generation")
			}
		}()
		f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", nextGen)
		if !errors.Is(f.repo.AssertActive(f.ctx, run.Run.ID), parser.ErrRunLeaseLost) || !errors.Is(f.repo.Finish(f.ctx, run.Run.ID, "completed"), parser.ErrRunGeneration) {
			t.Fatal("previous generation retained mutation ownership")
		}
		other := parser.RunRepository{DB: f.db, Generation: nextGen}
		if _, err := other.Begin(f.ctx, parser.RunOptions{RequestID: "CI after restore"}); !errors.Is(err, parser.ErrRunBusy) {
			t.Fatal("new generation ignored prior active owner")
		}
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
		next, err := other.Begin(f.ctx, parser.RunOptions{RequestID: "CI after restore"})
		if err != nil || !next.Acquired {
			t.Fatal("new generation could not recover expired owner")
		}
		if other.Finish(f.ctx, next.Run.ID, "completed") != nil {
			t.Fatal("new generation owner not released")
		}
	})
	t.Run("real SQL rollback after insert and recovery", func(t *testing.T) {
		f := f.sub(t)
		for i, prefix := range []string{"INSERT INTO scheduler_runs", "UPDATE scheduler_runs SET status='interrupted'"} {
			var prior parser.RunStart
			if i == 1 {
				prior = f.begin("CI rollback prior")
				f.attempt(prior, "CI_rollback_attempt")
				f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", prior.Run.ID)
			}
			count := f.count("SELECT COUNT(*) FROM scheduler_runs")
			faulted, fired := schedulerFaultRepository(t, f.repo.Generation, prefix)
			key := fmt.Sprintf("CI rollback %d", i)
			if _, err := faulted.Begin(f.ctx, parser.RunOptions{RequestID: key}); !errors.Is(err, parser.ErrRunStorage) || !fired.Load() {
				t.Fatal("required late SQL error was not injected")
			}
			if f.count("SELECT COUNT(*) FROM scheduler_runs") != count {
				t.Fatal("failed transaction left a new owner")
			}
			if i == 1 {
				f.assertAttempt("CI_rollback_attempt", "running")
				if f.count("SELECT COUNT(*) FROM scheduler_runs WHERE id=? AND status='running' AND lease_until=0", prior.Run.ID) != 1 {
					t.Fatal("failed recovery changed original owner")
				}
			}
			fresh := f.begin(key)
			f.finish(fresh)
		}
	})
	t.Run("process kill leaves reservation for fresh pool recovery", func(t *testing.T) {
		f := f.sub(t)
		cmd := exec.Command(os.Args[0], "-test.run=^TestMariaDBSchedulerRunsDisposable$", "-test.count=1")
		cmd.Env = append(os.Environ(), "ITMAP_SCHEDULER_CRASH_CHILD=1")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		pipe, err := cmd.StdoutPipe()
		if err != nil || cmd.Start() != nil {
			t.Fatal("cannot start synthetic crash worker")
		}
		stopped := false
		defer func() {
			if !stopped {
				cmd.Process.Kill()
				cmd.Wait()
			}
		}()
		ready := make(chan bool, 1)
		go func() {
			scanner := bufio.NewScanner(pipe)
			for scanner.Scan() {
				if scanner.Text() == "CI_SCHEDULER_READY" {
					ready <- true
					return
				}
			}
			ready <- false
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("child never committed durable lease/reservation")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("crash child readiness timeout")
		}
		if err = cmd.Process.Kill(); err != nil {
			t.Fatal("cannot kill owned test child")
		}
		if cmd.Wait() == nil {
			t.Fatal("crash worker exited normally")
		}
		stopped = true
		f.assertAttempt("CI_crash_attempt", "running")
		if f.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='running'") != 1 {
			t.Fatal("process crash lost committed run")
		}
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
		_, db := schedulerDB(t)
		fresh := parser.RunRepository{DB: db, Generation: f.repo.Generation}
		recovered, err := fresh.Begin(f.ctx, parser.RunOptions{RequestID: "CI after crash"})
		if err != nil || !recovered.Acquired {
			t.Fatal("fresh pool failed to recover killed worker")
		}
		f.assertAttempt("CI_crash_attempt", "interrupted")
		if fresh.Finish(f.ctx, recovered.Run.ID, "completed") != nil {
			t.Fatal("recovered owner not released")
		}
	})
}
func schedulerCrashChild(t *testing.T) {
	ctx, db := schedulerDB(t)
	repo := parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}
	run, err := repo.Begin(ctx, parser.RunOptions{RequestID: "CI crash child"})
	if err != nil || !run.Acquired {
		t.Fatal("child lease unavailable")
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,result_json) VALUES('CI_crash_attempt','CI_crash_job',?,'diagnostic',?,'running',0.26,'{"synthetic":"retained"}')`, run.Run.ID, run.Run.StartedAt); err != nil {
		t.Fatal("child reservation not committed")
	}
	fmt.Println("CI_SCHEDULER_READY")
	for {
		time.Sleep(time.Second)
	}
}

type schedulerFaultConnector struct {
	base   driver.Connector
	prefix string
	fired  *atomic.Bool
}

func (c schedulerFaultConnector) Driver() driver.Driver { return c.base.Driver() }
func (c schedulerFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return schedulerFaultConn{Conn: conn, owner: c}, nil
}

type schedulerFaultConn struct {
	driver.Conn
	owner schedulerFaultConnector
}

func (c schedulerFaultConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}
func (c schedulerFaultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}
func (c schedulerFaultConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return schedulerFaultStmt{Stmt: stmt, query: query, owner: c.owner}, nil
}
func (c schedulerFaultConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return schedulerFaultStmt{Stmt: stmt, query: query, owner: c.owner}, nil
}
func (c schedulerFaultConnector) after(query string, result driver.Result, err error) (driver.Result, error) {
	if err == nil && strings.HasPrefix(query, c.prefix) && c.fired.CompareAndSwap(false, true) {
		return nil, errors.New("CI synthetic error after real scheduler SQL")
	}
	return result, err
}
func (c schedulerFaultConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
	return c.owner.after(query, result, err)
}

type schedulerFaultStmt struct {
	driver.Stmt
	query string
	owner schedulerFaultConnector
}

func (s schedulerFaultStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}
func (s schedulerFaultStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	result, err := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
	return s.owner.after(s.query, result, err)
}
func (s schedulerFaultStmt) Exec(args []driver.Value) (driver.Result, error) {
	result, err := s.Stmt.Exec(args)
	return s.owner.after(s.query, result, err)
}
func schedulerFaultRepository(t *testing.T, generation, prefix string) (parser.RunRepository, *atomic.Bool) {
	t.Helper()
	_, guarded := schedulerDB(t)
	_ = guarded // Apply parsed target/current-generation guard BEFORE building the fault pool.
	config, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil {
		t.Fatal("cannot parse guarded synthetic DSN")
	}
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatal("cannot configure guarded synthetic connector")
	}
	fired := &atomic.Bool{}
	db := sql.OpenDB(schedulerFaultConnector{connector, prefix, fired})
	t.Cleanup(func() { db.Close() })
	return parser.RunRepository{DB: db, Generation: generation}, fired
}
