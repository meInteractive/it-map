package vacancies

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type deleteFakeState struct {
	archived, hasApps, hasTasks, notFound, failDelete, failDetach, agency bool
	began, committed, rolledBack                                          bool
	statements                                                            []string
}
type deleteDriver struct{ state *deleteFakeState }
type deleteConn struct{ state *deleteFakeState }
type deleteTx struct{ state *deleteFakeState }

func (d deleteDriver) Open(string) (driver.Conn, error)                            { return nil, errors.New("use connector") }
func (d deleteDriver) Connect(context.Context) (driver.Conn, error)                { return &deleteConn{d.state}, nil }
func (d deleteDriver) Driver() driver.Driver                                       { return d }
func (c *deleteConn) Prepare(string) (driver.Stmt, error)                          { return nil, errors.New("unsupported") }
func (c *deleteConn) Close() error                                                 { return nil }
func (c *deleteConn) Begin() (driver.Tx, error)                                    { c.state.began = true; return &deleteTx{c.state}, nil }
func (c *deleteConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) { return c.Begin() }
func (tx *deleteTx) Commit() error                                                 { tx.state.committed = true; return nil }
func (tx *deleteTx) Rollback() error                                               { tx.state.rolledBack = true; return nil }
func (c *deleteConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	s.statements = append(s.statements, q)
	switch {
	case strings.Contains(q, "FROM company_vacancies WHERE id=? FOR UPDATE"):
		if s.notFound {
			return &createSQLRows{columns: []string{"company_name", "entity_type", "archived"}}, nil
		}
		arch := int64(0)
		if s.archived {
			arch = 1
		}
		kind := "company"
		if s.agency {
			kind = "agency"
		}
		return &createSQLRows{columns: []string{"company_name", "entity_type", "archived"}, values: [][]driver.Value{{"Example", kind, arch}}}, nil
	case strings.Contains(q, "FROM company_applications WHERE vacancy_id"):
		n := int64(0)
		if s.hasApps {
			n = 2
		}
		return &createSQLRows{columns: []string{"count"}, values: [][]driver.Value{{n}}}, nil
	case strings.Contains(q, "FROM company_tasks WHERE vacancy_id"):
		n := int64(0)
		if s.hasTasks {
			n = 1
		}
		return &createSQLRows{columns: []string{"count"}, values: [][]driver.Value{{n}}}, nil
	}
	return nil, fmt.Errorf("unexpected query %s", q)
}
func (c *deleteConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.statements = append(s.statements, q)
	if strings.Contains(q, "DELETE FROM company_vacancies") && s.failDelete {
		return nil, errors.New("FK restrict")
	}
	if strings.Contains(q, "UPDATE company_tasks SET vacancy_id=NULL") && s.failDetach {
		return nil, errors.New("update failed")
	}
	return createSQLResult{affected: 1}, nil
}
func TestDeleteManualTransaction(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		init                     deleteFakeState
		wantArchived, wantCommit bool
		wantErr                  error
		statements               int
	}{
		{name: "archive application", init: deleteFakeState{hasApps: true}, wantArchived: true, wantCommit: true, statements: 4},
		{name: "archive task", init: deleteFakeState{hasTasks: true}, wantArchived: true, wantCommit: true, statements: 4},
		{name: "hard delete unused", wantCommit: true, statements: 6},
		{name: "hard delete archived with both links", init: deleteFakeState{archived: true, hasApps: true, hasTasks: true}, wantCommit: true, statements: 6},
		{name: "agency guarded", init: deleteFakeState{agency: true}, wantErr: ErrAgencySalaryProjectionPending, statements: 1},
		{name: "not found", init: deleteFakeState{notFound: true}, wantErr: ErrNotFound, statements: 1},
		{name: "delete constraint rollback", init: deleteFakeState{failDelete: true}, statements: 6},
		{name: "detach failure rollback", init: deleteFakeState{failDetach: true}, statements: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := tc.init
			db := sql.OpenDB(deleteDriver{state: &state})
			defer db.Close()
			got, err := (Repository{DB: db}).DeleteManual(context.Background(), 27)
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("wanted %v got %v", tc.wantErr, err)
			}
			if tc.wantErr == nil && !tc.wantCommit && err == nil {
				t.Fatal("expected error")
			}
			if tc.wantCommit && err != nil {
				t.Fatal(err)
			}
			if state.committed != tc.wantCommit || state.rolledBack == tc.wantCommit || !state.began {
				t.Fatalf("bad transaction %+v", state)
			}
			if got.Archived != tc.wantArchived {
				t.Fatalf("unexpected archiving %+v", got)
			}
			if len(state.statements) != tc.statements {
				t.Fatalf("queries=%d expected=%d: %v", len(state.statements), tc.statements, state.statements)
			}
			if tc.wantCommit && !got.OK {
				t.Fatalf("missing success %+v", got)
			}
			if state.committed && !got.Archived && tc.init.hasApps && !got.PreservedApplication {
				t.Fatal("application lost")
			}
			if state.committed && !got.Archived && tc.init.hasTasks && !got.PreservedTask {
				t.Fatal("task lost")
			}
		})
	}
}
