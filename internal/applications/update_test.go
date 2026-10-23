package applications

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

// Local in-memory SQL driver verifies transaction boundaries without needing
// a live server. SQL syntax and constraints still need staging MariaDB tests.
type updateConnector struct{ state *updateState }
type updateState struct {
	existing      bool
	vacancyExists bool
	linked        bool
	snapshot      string
	status        string
	queries       []string
	updateCount   int
	historyCount  int
	committed     bool
	rolledBack    bool
	failHistory   bool
}
type updateConn struct{ s *updateState }
type updateTx struct{ s *updateState }
type updateRows struct {
	columns []string
	values  [][]driver.Value
	pos     int
}
type updateResult int64

func (c updateConnector) Connect(context.Context) (driver.Conn, error) {
	return updateConn{c.state}, nil
}
func (c updateConnector) Driver() driver.Driver { return updateDriver{} }

type updateDriver struct{}

func (updateDriver) Open(string) (driver.Conn, error)    { return nil, errors.New("connector only") }
func (c updateConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not supported") }
func (c updateConn) Close() error                        { return nil }
func (c updateConn) Begin() (driver.Tx, error)           { return updateTx{c.s}, nil }
func (c updateConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return updateTx{c.s}, nil
}
func (t updateTx) Commit() error        { t.s.committed = true; return nil }
func (t updateTx) Rollback() error      { t.s.rolledBack = true; return nil }
func (r *updateRows) Columns() []string { return r.columns }
func (r *updateRows) Close() error      { return nil }
func (r *updateRows) Next(dst []driver.Value) error {
	if r.pos == len(r.values) {
		return io.EOF
	}
	copy(dst, r.values[r.pos])
	r.pos++
	return nil
}
func (n updateResult) LastInsertId() (int64, error) { return 0, nil }
func (n updateResult) RowsAffected() (int64, error) { return int64(n), nil }
func (c updateConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	s := c.s
	s.queries = append(s.queries, q)
	switch {
	case strings.Contains(q, "FROM company_applications a WHERE a.id"):
		values := [][]driver.Value{}
		if s.existing {
			values = append(values, []driver.Value{"Example", s.status, s.snapshot})
		}
		return &updateRows{columns: []string{"company_name", "status", "snapshot"}, values: values}, nil
	case strings.Contains(q, "SELECT company_name FROM company_applications"):
		values := [][]driver.Value{}
		if s.existing {
			values = append(values, []driver.Value{"Example"})
		}
		return &updateRows{columns: []string{"company_name"}, values: values}, nil
	case strings.Contains(q, "FROM company_vacancies"):
		values := [][]driver.Value{}
		if s.vacancyExists {
			values = append(values, []driver.Value{int64(5)})
		}
		return &updateRows{columns: []string{"id"}, values: values}, nil
	case strings.Contains(q, "FROM company_applications"):
		values := [][]driver.Value{}
		if s.linked {
			values = append(values, []driver.Value{int64(9)})
		}
		return &updateRows{columns: []string{"id"}, values: values}, nil
	default:
		return nil, errors.New("unexpected query")
	}
}
func (c updateConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	s := c.s
	s.queries = append(s.queries, q)
	switch {
	case strings.Contains(q, "UPDATE company_applications a SET"):
		if len(args) != 11 {
			return nil, errors.New("update arguments mismatch")
		}
		if args[8].Value != "Demo" || args[9].Value != int64(4) || args[10].Value != s.snapshot {
			return nil, errors.New("update not scoped to owner, id, state")
		}
		s.updateCount++
		return updateResult(1), nil
	case strings.Contains(q, "DELETE FROM company_application_status_history"):
		return updateResult(1), nil
	case strings.Contains(q, "DELETE FROM company_applications"):
		if len(args) != 2 || args[0].Value != "Demo" || args[1].Value != int64(4) {
			return nil, errors.New("unscoped delete")
		}
		return updateResult(1), nil
	case strings.Contains(q, "INSERT INTO company_application_status_history"):
		if s.failHistory {
			return nil, errors.New("history failed")
		}
		if len(args) != 3 || args[0].Value != int64(4) || args[1].Value != s.status || args[2].Value != "Скрининг" {
			return nil, errors.New("history arguments mismatch")
		}
		s.historyCount++
		return updateResult(1), nil
	default:
		return nil, errors.New("unexpected exec")
	}
}
func testRepo(s *updateState) (Repository, func()) {
	db := sql.OpenDB(updateConnector{s})
	db.SetMaxOpenConns(1)
	return Repository{DB: db}, func() { _ = db.Close() }
}
func validUpdate(s *updateState) UpdateInput {
	return UpdateInput{Revision: Fingerprint(s.snapshot), Status: "Скрининг", Comment: "Обсуждаем проект", RecruiterContactType: "direct", ApplicationSources: []string{"hh", "telegram", "hh"}}
}
func TestUpdateAtomicRevisionAndHistory(t *testing.T) {
	s := &updateState{existing: true, snapshot: `[4,"Demo","Example",null,"Откликнулся"]`, status: "Откликнулся"}
	r, closeDB := testRepo(s)
	defer closeDB()
	company, err := r.Update(context.Background(), "Demo", 4, validUpdate(s))
	if err != nil || company != "Example" || s.updateCount != 1 || s.historyCount != 1 || !s.committed || s.rolledBack {
		t.Fatalf("update: %q %v %+v", company, err, s)
	}
	if len(s.queries) != 3 {
		t.Fatalf("queries: %d", len(s.queries))
	}
	for _, q := range s.queries {
		if strings.Contains(q, "FROM company_vacancies") {
			t.Fatal("unexpected vacancy lookup")
		}
	}
}
func TestUpdateRejectsStaleRevision(t *testing.T) {
	s := &updateState{existing: true, snapshot: "live JSON snapshot", status: "Откликнулся"}
	r, closeDB := testRepo(s)
	defer closeDB()
	input := validUpdate(s)
	input.Revision = strings.Repeat("0", 64)
	_, err := r.Update(context.Background(), "Demo", 4, input)
	if !errors.Is(err, ErrConflict) || s.updateCount != 0 || s.historyCount != 0 || s.committed == true || !s.rolledBack {
		t.Fatal(err, s)
	}
}
func TestUpdateVacancyLinkedToOnlyOneApplication(t *testing.T) {
	s := &updateState{existing: true, snapshot: "snapshot", status: "Откликнулся", vacancyExists: true, linked: true}
	r, closeDB := testRepo(s)
	defer closeDB()
	input := validUpdate(s)
	id := int64(5)
	input.VacancyID = &id
	_, err := r.Update(context.Background(), "Demo", 4, input)
	if !errors.Is(err, ErrDuplicateVacancy) || s.committed || !s.rolledBack || s.updateCount != 0 {
		t.Fatal(err, s)
	}
	if len(s.queries) != 3 || !strings.Contains(s.queries[1], "FOR UPDATE") || !strings.Contains(s.queries[2], "user_name=?") {
		t.Fatal(s.queries)
	}
}
func TestUpdateForeignOrMissingVacancy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		vacancy bool
		want    error
	}{{"missing", false, ErrInvalidVacancy}, {"valid", true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			s := &updateState{existing: true, snapshot: "snapshot", status: "Скрининг", vacancyExists: tc.vacancy}
			r, closeDB := testRepo(s)
			defer closeDB()
			input := validUpdate(s)
			id := int64(5)
			input.VacancyID = &id
			_, err := r.Update(context.Background(), "Demo", 4, input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("%v, want %v", err, tc.want)
			}
			if tc.want == nil && (!s.committed || s.updateCount != 1 || s.historyCount != 0) {
				t.Fatal(s)
			}
		})
	}
}
func TestUpdateRollsBackWhenHistoryWriteFails(t *testing.T) {
	s := &updateState{existing: true, snapshot: "snapshot", status: "Откликнулся", failHistory: true}
	r, closeDB := testRepo(s)
	defer closeDB()
	_, err := r.Update(context.Background(), "Demo", 4, validUpdate(s))
	if err == nil || s.committed || !s.rolledBack || s.historyCount != 0 {
		t.Fatal(err, s)
	}
}
func TestUpdateValidation(t *testing.T) {
	s := &updateState{snapshot: "snapshot"}
	good := validUpdate(s)
	for _, tc := range []struct {
		name   string
		change func(*UpdateInput)
		want   error
	}{
		{"no revision", func(v *UpdateInput) { v.Revision = "" }, ErrRevisionRequired},
		{"invalid revision", func(v *UpdateInput) { v.Revision = "abc" }, ErrRevisionRequired},
		{"invalid status", func(v *UpdateInput) { v.Status = "В работе" }, ErrInvalidInput},
		{"long comment UTF16", func(v *UpdateInput) { v.Comment = strings.Repeat("🚀", 501) }, ErrInvalidInput},
		{"invalid mail", func(v *UpdateInput) { v.RecruiterEmail = "invalid" }, ErrInvalidInput},
		{"invalid source", func(v *UpdateInput) { v.ApplicationSources = []string{"fake"} }, ErrInvalidInput},
		{"empty source", func(v *UpdateInput) { v.ApplicationSources = []string{} }, ErrInvalidInput},
		{"bad contact type", func(v *UpdateInput) { v.RecruiterContactType = "other" }, ErrInvalidInput},
		{"negative vacancy", func(v *UpdateInput) { i := int64(-1); v.VacancyID = &i }, ErrInvalidInput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := good
			tc.change(&v)
			_, err := v.normalize()
			if !errors.Is(err, tc.want) {
				t.Fatalf("%v", err)
			}
		})
	}
	norm, err := good.normalize()
	if err != nil || len(norm.ApplicationSources) != 2 {
		t.Fatal(norm, err)
	}
	good.ApplicationSources = nil
	norm, err = good.normalize()
	if err != nil || len(norm.ApplicationSources) != 1 || norm.ApplicationSources[0] != "hh" {
		t.Fatal(norm, err)
	}
}

func TestDeleteUserScopedAtomic(t *testing.T) {
	s := &updateState{existing: true}
	r, closeDB := testRepo(s)
	defer closeDB()
	company, err := r.Delete(context.Background(), "Demo", 4)
	if err != nil || company != "Example" || !s.committed || s.rolledBack || len(s.queries) != 3 {
		t.Fatal(company, err, s)
	}
	if !strings.Contains(s.queries[0], "FOR UPDATE") || !strings.Contains(s.queries[1], "DELETE FROM company_application_status_history") || !strings.Contains(s.queries[2], "user_name=?") {
		t.Fatal(s.queries)
	}
}
func TestDeleteUnknownIsNotFound(t *testing.T) {
	s := &updateState{}
	r, closeDB := testRepo(s)
	defer closeDB()
	_, err := r.Delete(context.Background(), "Demo", 4)
	if !errors.Is(err, ErrNotFound) || !s.rolledBack || s.committed || len(s.queries) != 1 {
		t.Fatal(err, s)
	}
}
