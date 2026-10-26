//go:build mariadb && crmwrites

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
	"itmap/internal/crm"
)

var errCRMFlagFault = errors.New("CI_LATE_WRITE_DETAILS_NOT_PUBLIC")

type crmFlagFault struct {
	mu              sync.Mutex
	prefix          string
	fired           bool
	writes, commits int
}
type crmFlagFaultConnector struct {
	driver.Connector
	fault *crmFlagFault
}

func (c crmFlagFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &crmFlagFaultConn{Conn: conn, fault: c.fault}, nil
}

type crmFlagFaultConn struct {
	driver.Conn
	fault *crmFlagFault
}

func (c *crmFlagFaultConn) Prepare(q string) (driver.Stmt, error) {
	s, e := c.Conn.Prepare(q)
	if e != nil {
		return nil, e
	}
	return &crmFlagFaultStmt{Stmt: s, query: q, fault: c.fault}, nil
}
func (c *crmFlagFaultConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return &crmFlagFaultStmt{Stmt: s, query: q, fault: c.fault}, nil
}
func (c *crmFlagFaultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *crmFlagFaultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return &crmFlagFaultTx{Tx: tx, fault: c.fault}, nil
}

type crmFlagFaultTx struct {
	driver.Tx
	fault *crmFlagFault
}

func (tx *crmFlagFaultTx) Commit() error {
	e := tx.Tx.Commit()
	if e == nil {
		tx.fault.mu.Lock()
		tx.fault.commits++
		tx.fault.mu.Unlock()
	}
	return e
}

type crmFlagFaultStmt struct {
	driver.Stmt
	query string
	fault *crmFlagFault
}

func (s *crmFlagFaultStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}
func (s *crmFlagFaultStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	result, err := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
	if err != nil {
		return result, err
	}
	s.fault.mu.Lock()
	defer s.fault.mu.Unlock()
	if strings.HasPrefix(s.query, s.fault.prefix) {
		s.fault.writes++
		if !s.fault.fired {
			s.fault.fired = true
			return nil, errCRMFlagFault
		}
	}
	return result, nil
}
func (f *crmFlagFixture) faultRepo(prefix string) (crm.Repository, *crmFlagFault, *sql.DB) {
	f.t.Helper()
	cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil {
		f.t.Fatal("cannot configure guarded CRM fault connection")
	}
	c, err := mysql.NewConnector(cfg)
	if err != nil {
		f.t.Fatal("cannot initialize guarded CRM fault connector")
	}
	fault := &crmFlagFault{prefix: prefix}
	db := sql.OpenDB(crmFlagFaultConnector{Connector: c, fault: fault})
	f.t.Cleanup(func() { db.Close() })
	return crm.Repository{DB: db}, fault, db
}
