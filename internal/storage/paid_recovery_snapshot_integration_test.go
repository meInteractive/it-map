//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/paidrecovery"
	"itmap/internal/parser"
)

type paidSnapshotBarrier struct {
	entered, release chan struct{}
	once             sync.Once
}
type paidSnapshotConnector struct {
	driver.Connector
	barrier *paidSnapshotBarrier
}

func (c paidSnapshotConnector) Connect(ctx context.Context) (driver.Conn, error) {
	v, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return paidSnapshotConn{Conn: v, barrier: c.barrier}, nil
}

type paidSnapshotConn struct {
	driver.Conn
	barrier *paidSnapshotBarrier
}

func (c paidSnapshotConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c paidSnapshotConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT a.status,a.reserved_usd") {
		c.barrier.once.Do(func() { close(c.barrier.entered) })
		select {
		case <-c.barrier.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func TestMariaDBPaidRecoverySnapshotDisposable(t *testing.T) {
	f := billingFixture(t)
	billingReset(f)
	if f.count("SELECT COUNT(*) FROM apify_runs") != 0 {
		t.Fatal("paid snapshot fixture not empty")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DELETE FROM apify_runs WHERE account_id='CISnapshot'"); err != nil {
			t.Error("snapshot cleanup failed")
		}
		billingReset(f)
	})
	old := "11111111-2222-4333-8444-555555555555"
	if old == f.repo.Generation {
		old = "11111111-2222-4333-8444-555555555556"
	}
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_old_snapshot',?,'automatic',1,'interrupted')", old)
	f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd) VALUES('CI_old_paid','CIjob','CI_old_snapshot','diagnostic',1,'interrupted',0.25),('CI_orphan_paid','CIjob','CI_missing','diagnostic',1,'interrupted',0.25)")
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES('00000000000000000000000000000001',?,'CIkey','free','CISnapshot','CIhash','CIstart','CIend',0.25,'uncertain',1,1)`, old)
	got, err := paidrecovery.Inspect(f.ctx, f.db, f.repo.Generation)
	if err != nil || got.Apify.Unknown != 1 || got.Scheduler.HistoricalUnknown != 1 || got.Scheduler.OrphanUnknown != 1 {
		t.Fatal("combined snapshot mismatch", got, err)
	}
	f.exec("UPDATE scheduler_attempts SET status='CI_invalid' WHERE id='CI_old_paid'")
	if got, err = paidrecovery.Inspect(f.ctx, f.db, f.repo.Generation); !errors.Is(err, parser.ErrHistoricalAttemptsIncompatible) || got != (paidrecovery.Snapshot{}) {
		t.Fatal("partial snapshot returned", err)
	}
	f.exec("UPDATE scheduler_attempts SET status='interrupted' WHERE id='CI_old_paid'")
	cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		t.Fatal("unsafe snapshot target")
	}
	base, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	barrier := &paidSnapshotBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	wrapped := sql.OpenDB(paidSnapshotConnector{Connector: base, barrier: barrier})
	defer wrapped.Close()
	type result struct {
		snapshot paidrecovery.Snapshot
		err      error
	}
	done := make(chan result, 1)
	go func() { s, e := paidrecovery.Inspect(f.ctx, wrapped, f.repo.Generation); done <- result{s, e} }()
	select {
	case <-barrier.entered:
	case <-f.ctx.Done():
		close(barrier.release)
		t.Fatal("snapshot barrier not reached")
	}
	blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	contender, err := f.db.BeginTx(blocked, nil)
	if err == nil {
		var g string
		err = contender.QueryRowContext(blocked, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&g)
		contender.Rollback()
	}
	cancel()
	close(barrier.release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("generation lock not retained across ledgers", err)
	}
	out := <-done
	if out.err != nil || out.snapshot.Apify.Unknown != 1 || out.snapshot.Scheduler.HistoricalUnknown != 1 || out.snapshot.Scheduler.OrphanUnknown != 1 {
		t.Fatal("snapshot barrier result mismatch", out.err)
	}
}
