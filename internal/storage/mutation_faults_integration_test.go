//go:build mariadb && (crmwrites || vacancywrites || vacancycreates || vacancyedits || vacancydeletes || profilewrites || channelwrites || contactwrites)

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

	mysql "github.com/go-sql-driver/mysql"
)

var errMutationFault = errors.New("CI_LATE_WRITE_DETAILS_NOT_PUBLIC")

type mutationFault struct {
	mu               sync.Mutex
	prefix           string
	suffix           string
	fired            bool
	writes, commits  int
	queries          int
	entered, release chan struct{}
}
type mutationFaultConnector struct {
	driver.Connector
	fault *mutationFault
}

func (c mutationFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &mutationFaultConn{Conn: conn, fault: c.fault}, nil
}

type mutationFaultConn struct {
	driver.Conn
	fault *mutationFault
}

func (c *mutationFaultConn) Prepare(q string) (driver.Stmt, error) {
	s, e := c.Conn.Prepare(q)
	if e != nil {
		return nil, e
	}
	return &mutationFaultStmt{Stmt: s, query: q, fault: c.fault}, nil
}
func (c *mutationFaultConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return &mutationFaultStmt{Stmt: s, query: q, fault: c.fault}, nil
}
func (c *mutationFaultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(c.fault.prefix, "BEFORE:") && strings.HasPrefix(q, strings.TrimPrefix(c.fault.prefix, "BEFORE:")) {
		if e := c.fault.queryEvent(ctx, q); e != nil {
			return nil, e
		}
	}
	rows, e := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if e != nil {
		return rows, e
	}
	if e = c.fault.queryEvent(ctx, q); e != nil {
		rows.Close()
		return nil, e
	}
	return rows, nil
}
func (c *mutationFaultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return &mutationFaultTx{Tx: tx, fault: c.fault}, nil
}

type mutationFaultTx struct {
	driver.Tx
	fault *mutationFault
}

func (tx *mutationFaultTx) Commit() error {
	e := tx.Tx.Commit()
	if e == nil {
		tx.fault.mu.Lock()
		tx.fault.commits++
		tx.fault.mu.Unlock()
	}
	return e
}

type mutationFaultStmt struct {
	driver.Stmt
	query string
	fault *mutationFault
}

func (s *mutationFaultStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	rows, e := s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
	if e != nil {
		return rows, e
	}
	if e = s.fault.queryEvent(ctx, s.query); e != nil {
		rows.Close()
		return nil, e
	}
	return rows, nil
}

func (f *mutationFault) queryEvent(ctx context.Context, q string) error {
	// Empty prefix is the existing read-only probe: fault on any Exec, but
	// permit all Query calls without marking a fictitious write.
	if f.prefix == "" {
		return nil
	}
	prefix := strings.TrimPrefix(f.prefix, "BEFORE:")
	if !strings.HasPrefix(q, prefix) || (f.suffix != "" && !strings.HasSuffix(q, f.suffix)) {
		return nil
	}
	f.mu.Lock()
	f.queries++
	if f.fired {
		f.mu.Unlock()
		return nil
	}
	f.fired = true
	entered, release := f.entered, f.release
	f.mu.Unlock()
	if entered == nil {
		return errMutationFault
	}
	close(entered)
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *mutationFaultStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	result, err := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
	if err != nil {
		return result, err
	}
	s.fault.mu.Lock()
	if strings.HasPrefix(s.query, s.fault.prefix) {
		s.fault.writes++
		if !s.fault.fired {
			s.fault.fired = true
			entered, release := s.fault.entered, s.fault.release
			s.fault.mu.Unlock()
			if entered != nil {
				close(entered)
				select {
				case <-release:
					return result, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return nil, errMutationFault
		}
	}
	s.fault.mu.Unlock()
	return result, nil
}

// openMutationFaultPool is test-only and refuses unsafe targets before connect.
func openMutationFaultPool(t *testing.T, prefix string) (*mutationFault, *sql.DB) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatal("mutation faults require disposable GitHub CI")
	}
	_, guard := openDisposableMariaDB(t)
	if err := guard.Close(); err != nil {
		t.Fatal("cannot close disposable fault guard pool")
	}
	cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil {
		t.Fatal("cannot configure guarded mutation fault connection")
	}
	c, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal("cannot initialize guarded mutation fault connector")
	}
	fault := &mutationFault{prefix: prefix}
	db := sql.OpenDB(mutationFaultConnector{Connector: c, fault: fault})
	t.Cleanup(func() { db.Close() })

	return fault, db
}
