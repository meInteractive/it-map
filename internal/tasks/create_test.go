package tasks

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type taskCreateState struct {
	connected, invalidCompany, invalidVacancy, failInsert, failCRM bool
	commits, rollbacks, writes                                     int
	user                                                           string
	taskDueAt                                                      string
}
type taskCreateDriver struct{ state *taskCreateState }
type taskCreateConn struct{ state *taskCreateState }
type taskCreateTx struct {
	state  *taskCreateState
	closed bool
}
type taskCreateResult struct{ id int64 }

func (d taskCreateDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (d taskCreateDriver) Driver() driver.Driver            { return d }
func (d taskCreateDriver) Connect(context.Context) (driver.Conn, error) {
	return &taskCreateConn{state: d.state}, nil
}
func (c *taskCreateConn) Close() error { return nil }
func (c *taskCreateConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not supported")
}
func (c *taskCreateConn) Begin() (driver.Tx, error) { return &taskCreateTx{state: c.state}, nil }
func (c *taskCreateConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (t *taskCreateTx) Commit() error { t.state.commits++; t.closed = true; return nil }
func (t *taskCreateTx) Rollback() error {
	if !t.closed {
		t.state.rollbacks++
		t.closed = true
	}
	return nil
}
func (r taskCreateResult) LastInsertId() (int64, error) { return r.id, nil }
func (r taskCreateResult) RowsAffected() (int64, error) { return 1, nil }
func (c *taskCreateConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	mk := func(cols []string, values ...[]driver.Value) driver.Rows {
		return &rescheduleRows{columns: cols, data: values}
	}
	switch {
	case strings.Contains(q, "FROM app_catalog WHERE id=1"):
		if len(args) != 0 {
			return nil, errors.New("unexpected catalog args")
		}
		if s.invalidCompany {
			return mk([]string{"data_json"}, []driver.Value{`{"companies":[{"name":"Other"}],"agencies":[]}`}), nil
		}
		return mk([]string{"data_json"}, []driver.Value{`{"companies":[{"name":"Example"}],"agencies":[]}`}), nil
	case strings.Contains(q, "FROM google_calendar_user_connection"):
		if len(args) != 1 || args[0].Value != s.user {
			return nil, errors.New("calendar user not scoped")
		}
		if s.connected {
			return mk([]string{"1"}, []driver.Value{int64(1)}), nil
		}
		return mk([]string{"1"}), nil
	case strings.Contains(q, "FROM company_vacancies WHERE id=?"):
		if len(args) != 3 || args[0].Value != int64(7) || args[1].Value != "Example" || args[2].Value != "Example" {
			return nil, errors.New("wrong vacancy validation")
		}
		if s.invalidVacancy {
			return mk([]string{"count"}, []driver.Value{int64(0)}), nil
		}
		return mk([]string{"count"}, []driver.Value{int64(1)}), nil
	}
	return nil, fmt.Errorf("unexpected query: %s", q)
}
func (c *taskCreateConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	s := c.state
	s.writes++
	if strings.Contains(q, "INSERT INTO company_crm") {
		if len(args) != 1 || args[0].Value != "Example" {
			return nil, errors.New("incorrect company CRM")
		}
		if s.failCRM {
			return nil, errors.New("CRM unavailable")
		}
		return taskCreateResult{id: 0}, nil
	}
	if strings.Contains(q, "INSERT INTO company_tasks") {
		if len(args) != 6 || args[0].Value != s.user || args[1].Value != "Example" || args[2].Value != int64(7) || args[3].Value != "call" {
			return nil, errors.New("wrong task data")
		}
		s.taskDueAt = args[5].Value.(string)
		if s.failInsert {
			return nil, errors.New("task INSERT unavailable")
		}
		return taskCreateResult{id: 42}, nil
	}
	return nil, fmt.Errorf("unexpected exec: %s", q)
}
func TestTaskCreateTransaction(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   taskCreateState
		expect  error
		success bool
	}{
		{"created", taskCreateState{}, nil, true},
		{"unknown company", taskCreateState{invalidCompany: true}, ErrCompanyNotFound, false},
		{"calendar connected", taskCreateState{connected: true}, ErrCalendarSyncRequired, false},
		{"unrelated vacancy", taskCreateState{invalidVacancy: true}, ErrVacancyNotInCompany, false},
		{"CRM failure", taskCreateState{failCRM: true}, nil, false},
		{"task INSERT failure", taskCreateState{failInsert: true}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.state
			s.user = "Demo"
			db := sql.OpenDB(taskCreateDriver{&s})
			defer db.Close()
			db.SetMaxOpenConns(1)
			v := int64(7)
			id, err := (Repository{DB: db}).Create(context.Background(), "Demo", CreateInput{CompanyName: "Example", VacancyID: &v, Type: "call", DueAt: "2026-10-24T14:45:00+03:00"})
			if tc.success {
				if err != nil || id != 42 || s.taskDueAt != "2026-10-24T11:45:00.000Z" || s.commits != 1 || s.writes != 2 || s.rollbacks != 0 {
					t.Fatalf("id=%d err=%v state=%+v", id, err, s)
				}
				return
			}
			if err == nil || (tc.expect != nil && !errors.Is(err, tc.expect)) || s.commits != 0 || s.rollbacks != 1 {
				t.Fatalf("id=%d err=%v state=%+v", id, err, s)
			}
			if (s.connected || s.invalidCompany || s.invalidVacancy) && s.writes != 0 {
				t.Fatalf("unexpected write %+v", s)
			}
		})
	}
}
func TestTaskCreateRejectsValidationBeforeSQL(t *testing.T) {
	for _, v := range []CreateInput{{CompanyName: "", Type: "call", DueAt: "2026-10-24T12:00:00Z"}, {CompanyName: "Example", Type: "other", DueAt: "2026-10-24T12:00:00Z"}, {CompanyName: "Example", Type: "call", DueAt: "bad"}, {CompanyName: "Example", Type: "call", Note: strings.Repeat("x", 1201), DueAt: "2026-10-24T12:00:00Z"}} {
		if _, err := (Repository{DB: &sql.DB{}}).Create(context.Background(), "Demo", v); !errors.Is(err, ErrInvalidTask) {
			t.Fatalf("err=%v input=%+v", err, v)
		}
	}
}
