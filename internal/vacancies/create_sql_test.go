package vacancies

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

// In-memory SQL contract driver for transaction ordering and rollback. It does
// not emulate the MariaDB SQL parser or foreign keys; that is a staging gate.
type createSQLDriver struct{ state *createSQLState }
type createSQLState struct {
	duplicate      bool
	badOwner       bool
	failRoleInsert bool
	committed      bool
	rolledBack     bool
	released       bool
	inserted       int
	assigned       int
	lockCount      int
	lastConnClosed bool
}
type createConn struct{ state *createSQLState }
type createTx struct{ state *createSQLState }
type createSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}
type createSQLResult struct{ lastID, affected int64 }

func (r createSQLResult) LastInsertId() (int64, error) { return r.lastID, nil }
func (r createSQLResult) RowsAffected() (int64, error) { return r.affected, nil }
func (d createSQLDriver) Connect(context.Context) (driver.Conn, error) {
	return &createConn{d.state}, nil
}
func (d createSQLDriver) Driver() driver.Driver            { return d }
func (d createSQLDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector only") }
func (c *createConn) Prepare(string) (driver.Stmt, error)  { return nil, errors.New("unsupported") }
func (c *createConn) Close() error                         { c.state.lastConnClosed = true; return nil }
func (c *createConn) Begin() (driver.Tx, error)            { return &createTx{c.state}, nil }
func (c *createConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &createTx{c.state}, nil
}
func (tx *createTx) Commit() error         { tx.state.committed = true; return nil }
func (tx *createTx) Rollback() error       { tx.state.rolledBack = true; return nil }
func (r *createSQLRows) Columns() []string { return r.columns }
func (r *createSQLRows) Close() error      { return nil }
func (r *createSQLRows) Next(dst []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dst, r.values[r.index])
	r.index++
	return nil
}
func (c *createConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	reply := func(cols []string, values ...[]driver.Value) (driver.Rows, error) {
		return &createSQLRows{columns: cols, values: values}, nil
	}
	switch {
	case strings.Contains(q, "FROM vacancy_roles AS r"):
		return reply([]string{"id", "stable_key", "name", "parent_id", "sort_order", "usageCount", "childCount"}, []driver.Value{int64(3), "lead", "Lead", nil, int64(0), int64(0), int64(0)})
	case strings.Contains(q, "GET_LOCK"):
		s.lockCount++
		return reply([]string{"lock"}, []driver.Value{int64(1)})
	case strings.Contains(q, "SELECT data_json FROM app_catalog"):
		if s.badOwner {
			return reply([]string{"data_json"}, []driver.Value{`{"companies":[],"agencies":[]}`})
		}
		return reply([]string{"data_json"}, []driver.Value{`{"companies":[{"name":"Example"}],"agencies":[]}`})
	case strings.Contains(q, "SELECT r.id FROM vacancy_roles r"):
		return reply([]string{"id"}, []driver.Value{int64(3)})
	case strings.Contains(q, "SELECT id FROM company_vacancies WHERE url"):
		if s.duplicate {
			return reply([]string{"id"}, []driver.Value{int64(7)})
		}
		return reply([]string{"id"})
	case strings.Contains(q, "FROM company_vacancies v"):
		return reply([]string{"none"}) // no seed rows in the isolated fake
	default:
		return nil, errors.New("unexpected SQL query: " + q)
	}
}
func (c *createConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	s := c.state
	switch {
	case strings.Contains(q, "RELEASE_LOCK"):
		s.released = true
	case strings.Contains(q, "INSERT INTO company_crm"):
	case strings.Contains(q, "INSERT INTO company_vacancies"):
		s.inserted++
		return createSQLResult{lastID: 42, affected: 1}, nil
	case strings.Contains(q, "INSERT INTO vacancy_role_assignments"):
		if s.failRoleInsert {
			return nil, errors.New("injected role write error")
		}
		s.assigned++
	default:
		return nil, errors.New("unexpected SQL exec: " + q)
	}
	return createSQLResult{affected: 1}, nil
}

func TestCreateManualSQLTransactionAndLock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		modify  func(*createSQLState)
		want    error
		success bool
	}{
		{name: "complete create", success: true},
		{name: "duplicate", modify: func(s *createSQLState) { s.duplicate = true }, want: ErrDuplicateURL},
		{name: "unknown company", modify: func(s *createSQLState) { s.badOwner = true }, want: ErrInvalidOwner},
		{name: "broken role write rolls back", modify: func(s *createSQLState) { s.failRoleInsert = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &createSQLState{}
			if tc.modify != nil {
				tc.modify(state)
			}
			db := sql.OpenDB(createSQLDriver{state: state})
			db.SetMaxOpenConns(1)
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			input := Input{CompanyName: "Example", URL: "https://example.com/abc", Title: "Lead", DescriptionMD: "Some vacancy details", Level: "Head", RoleIDs: []int64{3}}
			got, err := (Repository{DB: db}).CreateManual(ctx, "Eugene", input)
			if tc.success {
				if err != nil || got == nil || !state.committed || state.rolledBack || state.inserted != 1 || state.assigned != 1 {
					t.Fatalf("bad success: rows=%v err=%v state=%+v", got, err, state)
				}
			} else {
				if err == nil || (tc.want != nil && !errors.Is(err, tc.want)) || state.committed || !state.rolledBack {
					t.Fatalf("bad failure: err=%v state=%+v", err, state)
				}
			}
			if state.lockCount != 1 || !state.released {
				t.Fatalf("unreleased or missing lock: %+v", state)
			}
		})
	}
}
