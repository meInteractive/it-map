package applications

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// A deterministic SQL ordering/rollback test double. It intentionally does
// NOT claim to implement MariaDB SQL semantics or engine constraints.
type receiptDriver struct{ st *receiptState }
type receiptState struct {
	generation                     string
	receipt                        bool
	receiptHash, receiptGeneration string
	receiptID                      int64
	exists                         bool
	creates, commits, rollbacks    int
	failHistory                    bool
	limit                          int
	pending                        bool
	pendingHash, pendingGen        string
}
type receiptConn struct{ st *receiptState }
type receiptTx struct{ st *receiptState }
type receiptRows struct {
	names []string
	data  [][]driver.Value
	at    int
}
type receiptResult struct{ id, affected int64 }

func (r receiptResult) LastInsertId() (int64, error)     { return r.id, nil }
func (r receiptResult) RowsAffected() (int64, error)     { return r.affected, nil }
func (d receiptDriver) Open(string) (driver.Conn, error) { return &receiptConn{st: d.st}, nil }
func (d receiptDriver) Connect(context.Context) (driver.Conn, error) {
	return &receiptConn{st: d.st}, nil
}
func (d receiptDriver) Driver() driver.Driver              { return d }
func (c *receiptConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c *receiptConn) Close() error                        { return nil }
func (c *receiptConn) Begin() (driver.Tx, error)           { return &receiptTx{c.st}, nil }
func (c *receiptConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &receiptTx{c.st}, nil
}
func (t *receiptTx) Commit() error {
	t.st.commits++
	if t.st.pending {
		t.st.receipt = true
		t.st.exists = true
		t.st.receiptID = 41
		t.st.receiptHash = t.st.pendingHash
		t.st.receiptGeneration = t.st.pendingGen
		t.st.creates++
	}
	t.st.pending = false
	return nil
}
func (t *receiptTx) Rollback() error     { t.st.rollbacks++; t.st.pending = false; return nil }
func (r *receiptRows) Columns() []string { return r.names }
func (r *receiptRows) Close() error      { return nil }
func (r *receiptRows) Next(dst []driver.Value) error {
	if r.at >= len(r.data) {
		return io.EOF
	}
	copy(dst, r.data[r.at])
	r.at++
	return nil
}
func (c *receiptConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	s := c.st
	rows := func(names []string, data ...[]driver.Value) (driver.Rows, error) {
		return &receiptRows{names: names, data: data}, nil
	}
	switch {
	case strings.Contains(q, "FROM app_runtime_generation"):
		return rows([]string{"generation"}, []driver.Value{s.generation})
	case strings.Contains(q, "FROM application_creation_receipts"):
		if !s.receipt {
			return rows([]string{"generation", "request_hash", "created_application_id"})
		}
		return rows([]string{"generation", "request_hash", "created_application_id"}, []driver.Value{s.receiptGeneration, s.receiptHash, s.receiptID})
	case strings.Contains(q, "SELECT id FROM company_applications WHERE id=?"):
		if s.exists {
			return rows([]string{"id"}, []driver.Value{int64(41)})
		}
		return rows([]string{"id"})
	case strings.Contains(q, "SELECT data_json FROM app_catalog"):
		if s.commits > 0 {
			return nil, errors.New("post-commit read unavailable")
		}
		return rows([]string{"data_json"}, []driver.Value{`{"companies":[{"name":"Example"}],"agencies":[]}`})
	case strings.Contains(q, "SELECT company_name FROM company_crm"):
		return rows([]string{"company_name"}, []driver.Value{"Example"})
	case strings.Contains(q, "SELECT COUNT(*) FROM company_applications"):
		return rows([]string{"count"}, []driver.Value{int64(s.limit)})
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (c *receiptConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	s := c.st
	switch {
	case strings.Contains(q, "INSERT IGNORE INTO company_crm"):
		return receiptResult{affected: 1}, nil
	case strings.Contains(q, "INSERT INTO company_applications"):
		s.pending = true
		return receiptResult{id: 41, affected: 1}, nil
	case strings.Contains(q, "INSERT INTO company_application_status_history"):
		if s.failHistory {
			return nil, errors.New("history write failed")
		}
		return receiptResult{affected: 1}, nil
	case strings.Contains(q, "INSERT INTO application_creation_receipts"):
		if len(args) != 5 {
			return nil, errors.New("wrong receipt arguments")
		}
		s.pendingGen = args[2].Value.(string)
		s.pendingHash = args[3].Value.(string)
		return receiptResult{affected: 1}, nil
	default:
		return nil, errors.New("unexpected exec: " + q)
	}
}
func newReceiptDB(t *testing.T, s *receiptState) *sql.DB {
	t.Helper()
	db := sql.OpenDB(receiptDriver{st: s})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func TestCreateReceiptCommitRetryAndConflict(t *testing.T) {
	state := &receiptState{generation: testGeneration}
	repo := Repository{DB: newReceiptDB(t, state), CreationSecret: []byte(strings.Repeat("k", 64))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	token, err := repo.CreationContext(ctx, "Demo")
	if err != nil {
		t.Fatal(err)
	}
	input := creationInput()
	input.CreationContext = token
	// Simulate an unavailable response read AFTER commit. The write is still
	// durable, and retrying it must NEVER create a second application.
	_, err = repo.CreateWithReceipt(ctx, "Demo", input)
	if err == nil || state.creates != 1 || !state.receipt || !state.exists {
		t.Fatalf("expected committed write and read error: %v %+v", err, state)
	}
	_, err = repo.CreateWithReceipt(ctx, "Demo", input)
	if err == nil || state.creates != 1 {
		t.Fatalf("retry inserted duplicate: %v %+v", err, state)
	}
	changed := input
	changed.Comment = "different request"
	if _, err = repo.CreateWithReceipt(ctx, "Demo", changed); !errors.Is(err, ErrIntentConflict) || state.creates != 1 {
		t.Fatalf("same key different input: %v", err)
	}
	state.exists = false
	if _, err = repo.CreateWithReceipt(ctx, "Demo", input); !errors.Is(err, ErrApplicationDeleted) {
		t.Fatalf("deleted replay: %v", err)
	}
	state.generation = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err = repo.CreateWithReceipt(ctx, "Demo", input); !errors.Is(err, ErrCreationContextExpired) {
		t.Fatalf("old generation: %v", err)
	}
}
func TestCreateReceiptRollback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failHistory bool
		limit       int
	}{
		{"history write fails", true, 0}, {"ten applications", false, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &receiptState{generation: testGeneration, failHistory: tc.failHistory, limit: tc.limit}
			repo := Repository{DB: newReceiptDB(t, state), CreationSecret: []byte(strings.Repeat("k", 64))}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			token, _ := repo.CreationContext(ctx, "Demo")
			input := creationInput()
			input.CreationContext = token
			_, err := repo.CreateWithReceipt(ctx, "Demo", input)
			if err == nil || state.creates != 0 || state.receipt || state.commits != 0 || state.rollbacks == 0 {
				t.Fatalf("unexpected write: %v %+v", err, state)
			}
			if tc.limit == 10 && !errors.Is(err, ErrApplicationLimit) {
				t.Fatal(err)
			}
		})
	}
}
