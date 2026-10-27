//go:build mariadb && v39restore && historicalrestore

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/backup"
	"itmap/internal/profiles"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type preserveBarrier struct {
	mu               sync.Mutex
	fired            bool
	prefix, suffix   string
	entered, release chan struct{}
	once             sync.Once
}

func (h *preserveBarrier) unblock() { h.once.Do(func() { close(h.release) }) }
func (h *preserveBarrier) after(ctx context.Context, q string) error {
	if !strings.HasPrefix(q, h.prefix) || !strings.HasSuffix(q, h.suffix) {
		return nil
	}
	h.mu.Lock()
	first := !h.fired
	h.fired = true
	h.mu.Unlock()
	if first {
		close(h.entered)
		select {
		case <-h.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type preserveConnector struct {
	driver.Connector
	hook *preserveBarrier
}

func (c preserveConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &preserveConn{Conn: conn, hook: c.hook}, nil
}

type preserveConn struct {
	driver.Conn
	hook *preserveBarrier
}

func (c *preserveConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *preserveConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	rows, e := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
	if e != nil {
		return nil, e
	}
	if e = c.hook.after(ctx, q); e != nil {
		rows.Close()
		return nil, e
	}
	return rows, nil
}
func (c *preserveConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	r, e := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
	if e != nil {
		return r, e
	}
	return r, c.hook.after(ctx, q)
}
func (c *preserveConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	stmt, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return &preserveStmt{Stmt: stmt, q: q, hook: c.hook}, nil
}

type preserveStmt struct {
	driver.Stmt
	q    string
	hook *preserveBarrier
}

func (s *preserveStmt) ExecContext(ctx context.Context, a []driver.NamedValue) (driver.Result, error) {
	r, e := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, a)
	if e != nil {
		return r, e
	}
	return r, s.hook.after(ctx, s.q)
}
func preservePool(f *restoreFixture, prefix, suffix string) (*sql.DB, *preserveBarrier) {
	f.t.Helper()
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		f.t.Fatal("unsafe preserve race target")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		f.t.Fatal("preserve connector failed")
	}
	hook := &preserveBarrier{prefix: prefix, suffix: suffix, entered: make(chan struct{}), release: make(chan struct{})}
	db := sql.OpenDB(preserveConnector{Connector: connector, hook: hook})
	db.SetMaxOpenConns(1)
	f.t.Cleanup(func() { hook.unblock(); db.Close() })
	return db, hook
}
func waitPreserveBlocked(f *restoreFixture, pattern string) {
	f.t.Helper()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var n int
		if e := f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID<>CONNECTION_ID() AND INFO LIKE ?", pattern).Scan(&n); e != nil {
			f.t.Fatal("preserve wait inspection failed")
		}
		if n > 0 {
			return
		}
		select {
		case <-tick.C:
		case <-f.ctx.Done():
			f.t.Fatal("real preserve lock waiter not observed")
		}
	}
}
func TestMariaDBHistoricalPreserveRaceDisposable(t *testing.T) {
	for _, first := range []string{"writer", "restore"} {
		t.Run(first+" first", func(t *testing.T) {
			f, v := openHistoricalFixture(t, 16)
			old := f.generation()
			repo := backup.Repository{DB: f.db}
			writer := profiles.Repository{DB: f.db}
			prefix, suffix := "UPDATE user_profiles SET resume_md=", ""
			if first == "restore" {
				prefix = "SELECT `user_name`,`resume_md`"
				suffix = " FOR UPDATE"
			}
			pool, hook := preservePool(f, prefix, suffix)
			if first == "restore" {
				repo.DB = pool
			} else {
				writer.DB = pool
			}
			restored, written := make(chan error, 1), make(chan error, 1)
			startRestore := func() { go func() { _, e := repo.Restore(f.ctx, []byte(v.Body)); restored <- e }() }
			startWrite := func() {
				go func() {
					_, e := writer.SaveResume(f.ctx, "Demo", profiles.TextInput{Text: "CI concurrent preserved resume 😀", Revision: 7})
					written <- e
				}()
			}
			if first == "writer" {
				startWrite()
			} else {
				startRestore()
			}
			select {
			case <-hook.entered:
			case <-f.ctx.Done():
				t.Fatal("preserve barrier not reached")
			}
			if first == "writer" {
				startRestore()
				waitPreserveBlocked(f, "SELECT `user_name`,`resume_md`%FOR UPDATE")
			} else {
				startWrite()
				waitPreserveBlocked(f, "INSERT INTO user_profiles%")
			}
			select {
			case <-restored:
				t.Fatal("restore crossed held preserve lock")
			default:
			}
			select {
			case <-written:
				t.Fatal("profile writer crossed held preserve lock")
			default:
			}
			hook.unblock()
			restoreErr, writeErr := <-restored, <-written
			if restoreErr != nil || writeErr != nil {
				t.Fatalf("preserve race failed: restore=%v writer=%v", restoreErr, writeErr)
			}
			result, e := (profiles.Repository{DB: f.db}).ReadResume(f.ctx, "Demo")
			if e != nil || result.ResumeMD != "CI concurrent preserved resume 😀" || result.Revision != 8 || f.generation() == old {
				t.Fatal("restore lost a committed preserved profile write")
			}
		})
	}
}
