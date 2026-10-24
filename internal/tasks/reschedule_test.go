package tasks

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type rescheduleState struct {
	linked, connected, missing, failUpdate, failConnectionCheck bool
	updates, commits, rollbacks                                 int
	deadline, user                                              string
}
type rescheduleDriver struct{ state *rescheduleState }
type rescheduleConn struct{ state *rescheduleState }
type rescheduleTx struct {
	state  *rescheduleState
	closed bool
}
type rescheduleRows struct {
	columns []string
	data    [][]driver.Value
	idx     int
}
type rescheduleResult struct{}

func (d rescheduleDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (d rescheduleDriver) Connect(context.Context) (driver.Conn, error) {
	return &rescheduleConn{d.state}, nil
}
func (d rescheduleDriver) Driver() driver.Driver              { return d }
func (c *rescheduleConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (c *rescheduleConn) Close() error                        { return nil }
func (c *rescheduleConn) Begin() (driver.Tx, error)           { return &rescheduleTx{state: c.state}, nil }
func (c *rescheduleConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}
func (t *rescheduleTx) Commit() error { t.closed = true; t.state.commits++; return nil }
func (t *rescheduleTx) Rollback() error {
	if !t.closed {
		t.state.rollbacks++
		t.closed = true
	}
	return nil
}
func (r *rescheduleRows) Columns() []string { return r.columns }
func (r *rescheduleRows) Close() error      { return nil }
func (r *rescheduleRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.idx])
	r.idx++
	return nil
}
func (rescheduleResult) LastInsertId() (int64, error) { return 0, nil }
func (rescheduleResult) RowsAffected() (int64, error) { return 1, nil }
func (c *rescheduleConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.state
	if len(args) != 1 && len(args) != 2 {
		return nil, fmt.Errorf("wrong argument count: %d", len(args))
	}
	if args[0].Value != s.user {
		return nil, fmt.Errorf("wrong user in query")
	}
	switch {
	case strings.Contains(query, "FROM company_tasks WHERE user_name=? AND id=? FOR UPDATE"):
		if args[1].Value != int64(10) {
			return nil, errors.New("wrong task")
		}
		rows := &rescheduleRows{columns: []string{"google_event_id", "google_sync_status"}}
		if !s.missing {
			event := ""
			if s.linked {
				event = "external-event"
			}
			rows.data = [][]driver.Value{{event, ""}}
		}
		return rows, nil
	case strings.Contains(query, "FROM google_calendar_user_connection WHERE user_name=?"):
		if s.failConnectionCheck {
			return nil, errors.New("calendar database lookup failed")
		}
		rows := &rescheduleRows{columns: []string{"1"}}
		if s.connected {
			rows.data = [][]driver.Value{{int64(1)}}
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected query %s", query)
	}
}
func (c *rescheduleConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s := c.state
	if !strings.Contains(query, "UPDATE company_tasks SET due_at=? WHERE user_name=? AND id=?") {
		return nil, fmt.Errorf("unexpected update: %s", query)
	}
	if len(args) != 3 || args[1].Value != s.user || args[2].Value != int64(10) {
		return nil, errors.New("incorrect update scope")
	}
	s.updates++
	if s.failUpdate {
		return nil, errors.New("write failed")
	}
	s.deadline = args[0].Value.(string)
	return rescheduleResult{}, nil
}
func TestRescheduleRejectsCalendarAndPreservesUserScope(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   rescheduleState
		wantErr error
		success bool
	}{
		{"unconnected", rescheduleState{}, nil, true},
		{"linked event", rescheduleState{linked: true}, ErrCalendarSyncRequired, false},
		{"connected user", rescheduleState{connected: true}, ErrCalendarSyncRequired, false},
		{"no task", rescheduleState{missing: true}, ErrTaskNotFound, false},
		{"calendar check failure", rescheduleState{failConnectionCheck: true}, nil, false},
		{"update failure", rescheduleState{failUpdate: true}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.state
			s.user = "Demo"
			db := sql.OpenDB(rescheduleDriver{&s})
			defer db.Close()
			db.SetMaxOpenConns(1)
			date, err := (Repository{DB: db}).Reschedule(context.Background(), "Demo", 10, "2026-10-24T14:45:00+03:00")
			if tc.success {
				if err != nil || date != "2026-10-24T11:45:00.000Z" || s.deadline != date || s.commits != 1 || s.updates != 1 || s.rollbacks != 0 {
					t.Fatalf("date=%s err=%v state=%+v", date, err, s)
				}
			} else {
				if err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) || s.commits != 0 || s.rollbacks != 1 {
					t.Fatalf("error=%v state=%+v", err, s)
				}
				if !tc.state.failUpdate && s.updates != 0 {
					t.Fatalf("unexpected write %+v", s)
				}
			}
		})
	}
}
func TestRescheduleRejectsInvalidInputBeforeSQL(t *testing.T) {
	for _, v := range []string{"", "not-date", "2026-02-30T12:00:00Z"} {
		if _, err := (Repository{}).Reschedule(context.Background(), "Demo", 10, v); err == nil {
			t.Fatal(v)
		}
	}
}
