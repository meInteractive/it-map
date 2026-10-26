//go:build mariadb && receiptwrites

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
	"itmap/internal/applications"
)

var errReceiptInterruption = errors.New("synthetic receipt interruption")

// The wrapper only injects a single client error. All other statements,
// transactions and rollbacks execute through the real pinned MariaDB driver.
// It is compiled exclusively into guarded receiptwrites integration tests.
type receiptFault struct {
	mu                    sync.Mutex
	stage                 string
	fired, committed      bool
	applications, history int
}

func (f *receiptFault) interrupt(query string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fired {
		return false
	}
	hit := (f.stage == "history" && strings.HasPrefix(query, "INSERT INTO company_application_status_history")) ||
		(f.stage == "receipt" && strings.HasPrefix(query, "INSERT INTO application_creation_receipts")) ||
		(f.stage == "response" && f.committed && query == "SELECT generation FROM app_runtime_generation WHERE id=1")
	if hit {
		f.fired = true
	}
	return hit
}
func (f *receiptFault) executed(query string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(query, "INSERT INTO company_applications(") {
		f.applications++
	}
	if strings.HasPrefix(query, "INSERT INTO company_application_status_history") {
		f.history++
	}
}

type receiptFaultConnector struct {
	driver.Connector
	fault *receiptFault
}

func (c receiptFaultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &receiptFaultConn{Conn: conn, fault: c.fault}, nil
}

type receiptFaultConn struct {
	driver.Conn
	fault *receiptFault
}

func (c *receiptFaultConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &receiptFaultStmt{Stmt: stmt, query: query, fault: c.fault}, nil
}
func (c *receiptFaultConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &receiptFaultStmt{Stmt: stmt, query: query, fault: c.fault}, nil
}
func (c *receiptFaultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.fault.interrupt(query) {
		return nil, errReceiptInterruption
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}
func (c *receiptFaultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &receiptFaultTx{Tx: tx, fault: c.fault}, nil
}

type receiptFaultTx struct {
	driver.Tx
	fault *receiptFault
}

func (tx *receiptFaultTx) Commit() error {
	err := tx.Tx.Commit()
	if err == nil {
		tx.fault.mu.Lock()
		tx.fault.committed = true
		tx.fault.mu.Unlock()
	}
	return err
}

type receiptFaultStmt struct {
	driver.Stmt
	query string
	fault *receiptFault
}

func (s *receiptFaultStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.fault.interrupt(s.query) {
		return nil, errReceiptInterruption
	}
	result, err := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
	if err == nil {
		s.fault.executed(s.query)
	}
	return result, err
}
func (s *receiptFaultStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}

func TestMariaDBApplicationFaultsDisposable(t *testing.T) {
	for _, stage := range []string{"history", "receipt", "response"} {
		t.Run(stage, func(t *testing.T) {
			f := openReceiptFixture(t) // CI/parsed-target/current-generation guards BEFORE connecting the wrapper.
			cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
			if err != nil {
				t.Fatal("cannot configure guarded synthetic fault connection")
			}
			connector, err := mysql.NewConnector(cfg)
			if err != nil {
				t.Fatal("cannot initialize guarded synthetic fault connector")
			}
			fault := &receiptFault{stage: stage}
			interruptedDB := sql.OpenDB(receiptFaultConnector{Connector: connector, fault: fault})
			interruptedDB.SetMaxOpenConns(1)
			t.Cleanup(func() { interruptedDB.Close() })
			repo := applications.Repository{DB: interruptedDB, CreationSecret: f.repo.CreationSecret}
			_, err = repo.CreateWithReceipt(f.ctx, receiptUser, f.input)
			if !errors.Is(err, errReceiptInterruption) || !fault.fired || fault.applications != 1 {
				t.Fatalf("expected the armed error after a real application INSERT: %T", err)
			}
			wantHistory := 1
			if stage == "history" {
				wantHistory = 0
			}
			if fault.history != wantHistory || fault.committed != (stage == "response") {
				t.Fatal("interruption did not occur at the required real transaction boundary")
			}
			// Dispose the entire failed pool. A new repository/physical connection must
			// recover solely from MariaDB state, never a cached application ID/receipt.
			if err := interruptedDB.Close(); err != nil {
				t.Fatal("cannot close interrupted writer pool")
			}
			if stage == "response" {
				f.assertCounts(1, 1, 1)
			} else {
				f.assertCounts(0, 0, 0)
			}
			_, freshDB := openDisposableMariaDB(t)
			fresh := applications.Repository{DB: freshDB, CreationSecret: f.repo.CreationSecret}
			recovered, err := fresh.CreateWithReceipt(f.ctx, receiptUser, f.input)
			if err != nil || recovered.Replayed != (stage == "response") || recovered.CreatedApplicationID <= 1 {
				t.Fatalf("fresh connection could not recover the original intent: %T", err)
			}
			f.assertCounts(1, 1, 1)
			retry, err := fresh.CreateWithReceipt(f.ctx, receiptUser, f.input)
			if err != nil || !retry.Replayed || retry.CreatedApplicationID != recovered.CreatedApplicationID {
				t.Fatalf("recovered intent lost durable identity on retry: %T", err)
			}
			row, err := fresh.ByID(f.ctx, receiptUser, recovered.CreatedApplicationID)
			if err != nil || row.Comment != f.input.Comment || row.Status != f.input.Status || len(row.History) != 1 {
				t.Fatal("recovered fields/history differ from the original intent")
			}
			changed := f.input
			changed.Comment = "CI synthetic changed retry"
			if _, err := fresh.CreateWithReceipt(f.ctx, receiptUser, changed); !errors.Is(err, applications.ErrIntentConflict) {
				t.Fatalf("changed recovered intent must still conflict: %T", err)
			}
			f.assertCounts(1, 1, 1)
		})
	}
}
