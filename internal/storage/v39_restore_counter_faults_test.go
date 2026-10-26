//go:build mariadb && profilewrites && v39exports && restorecounters

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/backup"
)

var errCounterFault = errors.New("CI_COUNTER_LATE_FAULT")

type counterFault struct {
	mu               sync.Mutex
	prefix           string
	fired, fail      bool
	txs              int
	entered, release chan struct{}
}
type counterConnector struct {
	driver.Connector
	hook *counterFault
}

func (c counterConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &counterConn{Conn: conn, hook: c.hook}, nil
}

type counterConn struct {
	driver.Conn
	hook *counterFault
}

func (c *counterConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.hook.mu.Lock()
	c.hook.txs++
	c.hook.mu.Unlock()
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}
func (c *counterConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	result, e := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	if e != nil {
		return result, e
	}
	if strings.HasPrefix(q, c.hook.prefix) {
		c.hook.mu.Lock()
		first := !c.hook.fired
		if first {
			c.hook.fired = true
		}
		c.hook.mu.Unlock()
		if first {
			if c.hook.entered != nil {
				close(c.hook.entered)
				select {
				case <-c.hook.release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if c.hook.fail {
				return nil, errCounterFault
			}
		}
	}
	return result, nil
}
func counterFaultPool(f *profileSettingsFixture, hook *counterFault) *sql.DB {
	f.t.Helper()
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		f.t.Fatal("unsafe counter fault target")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		f.t.Fatal("cannot configure counter fault driver")
	}
	db := sql.OpenDB(counterConnector{Connector: connector, hook: hook})
	db.SetMaxOpenConns(1)
	f.t.Cleanup(func() { db.Close() })
	return db
}
func testCounterLateFault(t *testing.T, prefix string) {
	f := openProfileSettingsFixture(t)
	before := counterSnapshot(f)
	target := counterNext(f) + 7
	body := counterFile(f, target)
	hook := &counterFault{prefix: prefix, fail: true}
	pool := counterFaultPool(f, hook)
	session, e := (backup.Repository{DB: pool}).OpenV39RestoreSession(f.ctx, body)
	if session != nil || !errors.Is(e, errCounterFault) {
		if session != nil {
			session.Close()
		}
		t.Fatal("counter fault did not stop after actual SQL")
	}
	hook.mu.Lock()
	fired, txs := hook.fired, hook.txs
	hook.mu.Unlock()
	if !fired || txs != 0 {
		t.Fatal("preflight fault did not occur outside data transactions")
	}
	if !reflect.DeepEqual(before, counterSnapshot(f)) {
		t.Fatal("counter preflight fault changed business rows")
	}
	if strings.HasPrefix(prefix, "ALTER") && counterNext(f) < target+1 {
		t.Fatal("post-DDL fault unexpectedly lowered counter")
	}
	pool.Close()
	retry, e := (backup.Repository{DB: f.db}).OpenV39RestoreSession(f.ctx, body)
	if e != nil {
		t.Fatal("fault leaked ownership/table locks or prevented fresh retry")
	}
	if e = retry.Close(); e != nil {
		t.Fatal("retry leaked ownership")
	}
}
func testCounterDDLRetainsFence(t *testing.T) {
	f := openProfileSettingsFixture(t)
	target := counterNext(f) + 2
	body := counterFile(f, target)
	hook := &counterFault{prefix: "ALTER TABLE `agency_identity`", entered: make(chan struct{}), release: make(chan struct{})}
	pool := counterFaultPool(f, hook)
	type outcome struct {
		s *backup.RestoreSession
		e error
	}
	opened := make(chan outcome, 1)
	go func() {
		s, e := (backup.Repository{DB: pool}).OpenV39RestoreSession(f.ctx, body)
		opened <- outcome{s, e}
	}()
	released, received := false, false
	var result outcome
	t.Cleanup(func() {
		if !released {
			close(hook.release)
		}
		if !received {
			result = <-opened
		}
		if result.s != nil {
			_ = result.s.Close()
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, e := f.db.ExecContext(cleanup, `DELETE FROM agency_identity WHERE agency_name='CI counter concurrent allocation'`); e != nil {
			t.Error("cannot clean concurrent counter fixture")
		}
	})
	select {
	case <-hook.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("counter DDL did not reach after-execution barrier")
	}
	writer, e := f.db.Conn(f.ctx)
	if e != nil {
		t.Fatal("cannot open concurrent counter writer")
	}
	defer func() {
		if !released {
			close(hook.release)
			released = true
		}
		_ = writer.Close()
	}()
	var id int64
	if e = writer.QueryRowContext(f.ctx, `SELECT CONNECTION_ID()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	writeDone := make(chan error, 1)
	go func() {
		_, e := writer.ExecContext(f.ctx, `INSERT INTO agency_identity(agency_name) VALUES('CI counter concurrent allocation')`)
		writeDone <- e
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		var waiting int
		if e = f.db.QueryRowContext(f.ctx, `SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID=? AND INFO LIKE 'INSERT INTO agency_identity%'`, id).Scan(&waiting); e != nil {
			t.Fatal("cannot observe concurrent counter writer")
		}
		select {
		case <-writeDone:
			t.Fatal("ALTER released the table fence before explicit UNLOCK TABLES")
		default:
		}
		if waiting == 1 {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("concurrent allocation never reached held SQL table lock")
		}
	}
	close(hook.release)
	released = true
	result = <-opened
	received = true
	if result.e != nil || result.s == nil {
		t.Fatalf("fenced counter preflight failed: %T", result.e)
	}
	if e = <-writeDone; e != nil {
		t.Fatal("concurrent allocation failed after unlock")
	}
	var allocated int64
	if e = f.db.QueryRowContext(f.ctx, `SELECT id FROM agency_identity WHERE agency_name='CI counter concurrent allocation'`).Scan(&allocated); e != nil || allocated < target+1 {
		t.Fatal("concurrent allocation reused an ID below restored high-water")
	}
	if e = result.s.Close(); e != nil {
		t.Fatal("fenced session ownership leaked")
	}
}
func testCounterCancelWait(t *testing.T) {
	f := openProfileSettingsFixture(t)
	body := counterFile(f, counterNext(f)+4)
	holder, e := f.db.BeginTx(f.ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer holder.Rollback()
	if _, e = holder.ExecContext(f.ctx, `INSERT INTO agency_identity(agency_name) VALUES('CI counter cancelled allocation')`); e != nil {
		t.Fatal("cannot hold counter metadata lock")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	type outcome struct {
		s *backup.RestoreSession
		e error
	}
	done := make(chan outcome, 1)
	go func() {
		session, e := (backup.Repository{DB: f.db}).OpenV39RestoreSession(ctx, body)
		done <- outcome{session, e}
	}()
	received := false
	var result outcome
	defer func() {
		cancel()
		_ = holder.Rollback()
		if !received {
			result = <-done
		}
		if result.s != nil {
			_ = result.s.Close()
		}
	}()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting int
		if e = f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE INFO LIKE 'LOCK TABLES `agency_identity` WRITE%'").Scan(&waiting); e != nil {
			t.Fatal("cannot observe waiting counter lock")
		}
		if waiting > 0 {
			break
		}
		select {
		case <-timeout.C:
			t.Fatal("counter preflight never reached actual SQL table lock")
		case <-tick.C:
		}
	}
	cancel()
	result = <-done
	received = true
	if result.s != nil {
		result.s.Close()
		t.Fatal("preflight ignored cancellation while waiting for SQL lock")
	}
	if !errors.Is(result.e, context.Canceled) {
		t.Fatalf("counter cancellation was not preserved: %T", result.e)
	}
	if e = holder.Rollback(); e != nil {
		t.Fatal(e)
	}
	retry, e := (backup.Repository{DB: f.db}).OpenV39RestoreSession(f.ctx, body)
	if e != nil {
		t.Fatal("cancelled counter stage leaked a SQL lock")
	}
	retry.Close()
}
