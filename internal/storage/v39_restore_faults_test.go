//go:build mariadb && v39restore

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"sync"

	mysql "github.com/go-sql-driver/mysql"
)

type restoreFault struct {
	mu     sync.Mutex
	prefix string
	fired  bool
}

func (h *restoreFault) after(q string, r driver.Result, e error) (driver.Result, error) {
	if e != nil {
		return r, e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.fired && strings.HasPrefix(q, h.prefix) {
		h.fired = true
		return nil, errors.New("CI_RESTORE_AFTER_SQL")
	}
	return r, nil
}

type restoreConnector struct {
	driver.Connector
	hook *restoreFault
}

func (c restoreConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &restoreConn{Conn: conn, hook: c.hook}, nil
}

type restoreConn struct {
	driver.Conn
	hook *restoreFault
}

func (c *restoreConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *restoreConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
}
func (c *restoreConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	r, e := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
	return c.hook.after(q, r, e)
}
func (c *restoreConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return &restoreStmt{Stmt: s, q: q, hook: c.hook}, nil
}

type restoreStmt struct {
	driver.Stmt
	q    string
	hook *restoreFault
}

func (s *restoreStmt) ExecContext(ctx context.Context, a []driver.NamedValue) (driver.Result, error) {
	r, e := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, a)
	return s.hook.after(s.q, r, e)
}
func restoreFaultPool(f *restoreFixture, h *restoreFault) *sql.DB {
	f.t.Helper()
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		f.t.Fatal("unsafe restore fault target")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		f.t.Fatal("restore connector failed")
	}
	db := sql.OpenDB(restoreConnector{Connector: connector, hook: h})
	db.SetMaxOpenConns(1)
	f.t.Cleanup(func() { db.Close() })
	return db
}
