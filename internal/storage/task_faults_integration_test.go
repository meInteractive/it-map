//go:build mariadb && taskwrites

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

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/tasks"
)

var errTaskWriteFault = errors.New("synthetic task write interruption")

type taskWriteFault struct {
	mu              sync.Mutex
	prefix          string
	fired           bool
	writes, commits int
}
type taskFaultConnector struct {
	driver.Connector
	fault *taskWriteFault
}

func (c taskFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &taskFaultConn{Conn: conn, fault: c.fault}, nil
}

type taskFaultConn struct {
	driver.Conn
	fault *taskWriteFault
}

func (c *taskFaultConn) Prepare(q string) (driver.Stmt, error) {
	s, e := c.Conn.Prepare(q)
	if e != nil {
		return nil, e
	}
	return &taskFaultStmt{Stmt: s, query: q, fault: c.fault}, nil
}
func (c *taskFaultConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return &taskFaultStmt{Stmt: s, query: q, fault: c.fault}, nil
}
func (c *taskFaultConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *taskFaultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return &taskFaultTx{Tx: tx, fault: c.fault}, nil
}

type taskFaultTx struct {
	driver.Tx
	fault *taskWriteFault
}

func (tx *taskFaultTx) Commit() error {
	e := tx.Tx.Commit()
	if e == nil {
		tx.fault.mu.Lock()
		tx.fault.commits++
		tx.fault.mu.Unlock()
	}
	return e
}

type taskFaultStmt struct {
	driver.Stmt
	query string
	fault *taskWriteFault
}

func (s *taskFaultStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}
func (s *taskFaultStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
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
			return nil, errTaskWriteFault
		}
	}
	return result, nil
}
func (f *taskFixture) faultRepo(prefix string) (tasks.Repository, *taskWriteFault, *sql.DB) {
	f.t.Helper()
	cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if err != nil {
		f.t.Fatal("cannot configure guarded task fault connection")
	}
	c, err := mysql.NewConnector(cfg)
	if err != nil {
		f.t.Fatal("cannot initialize guarded task fault connector")
	}
	fault := &taskWriteFault{prefix: prefix}
	db := sql.OpenDB(taskFaultConnector{Connector: c, fault: fault})
	f.t.Cleanup(func() { db.Close() })
	return tasks.Repository{DB: db}, fault, db
}
func testTaskWriteRollbacks(t *testing.T) {
	for _, stage := range []string{"create", "reschedule", "completion"} {
		t.Run(stage, func(t *testing.T) {
			f := openTaskFixture(t)
			var id int64
			var before tasks.Task
			if stage != "create" {
				id = f.create()
				before = f.row(id)
			}
			prefix := map[string]string{"create": "INSERT INTO company_tasks", "reschedule": "UPDATE company_tasks SET due_at", "completion": "UPDATE company_tasks SET completed"}[stage]
			repo, fault, db := f.faultRepo(prefix)
			var err error
			switch stage {
			case "create":
				_, err = repo.Create(f.ctx, taskUser, f.input())
			case "reschedule":
				_, err = repo.Reschedule(f.ctx, taskUser, id, "2026-11-01T12:00:00Z")
			case "completion":
				err = repo.SetCompleted(f.ctx, taskUser, id, true)
			}
			fault.mu.Lock()
			fired, writes, commits := fault.fired, fault.writes, fault.commits
			fault.mu.Unlock()
			if !errors.Is(err, errTaskWriteFault) || !fired || writes != 1 || commits != 0 {
				t.Fatalf("expected the armed error AFTER a real task write: %T", err)
			}
			if err := db.Close(); err != nil {
				t.Fatal("cannot close interrupted task pool")
			}
			if stage == "create" {
				if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 0 {
					t.Fatal("failed INSERT was not rolled back")
				}
			} else if !reflect.DeepEqual(before, f.row(id)) {
				t.Fatal("failed task UPDATE changed fields or completion timestamp")
			}
			switch stage {
			case "create":
				f.create()
			case "reschedule":
				if due, err := f.repo.Reschedule(f.ctx, taskUser, id, "2026-11-01T12:00:00Z"); err != nil || due != "2026-11-01T12:00:00.000Z" {
					t.Fatalf("task reschedule retry failed: %T", err)
				}
			case "completion":
				if err := f.repo.SetCompleted(f.ctx, taskUser, id, true); err != nil {
					t.Fatalf("task completion retry failed: %T", err)
				}
			}
			if f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 1 {
				t.Fatal("task retry left an unexpected number of rows")
			}
		})
	}
}
