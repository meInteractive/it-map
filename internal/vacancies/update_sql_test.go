package vacancies

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type updateSQLState struct {
	writes       []string
	failRole     bool
	committed    int
	rolledBack   int
	lockAcquired int
	released     int
}
type updateDriver struct{ s *updateSQLState }
type updateConn struct{ s *updateSQLState }
type updateTx struct {
	s      *updateSQLState
	closed bool
}

func (d updateDriver) Open(string) (driver.Conn, error)                            { return nil, errors.New("connector only") }
func (d updateDriver) Connect(context.Context) (driver.Conn, error)                { return &updateConn{d.s}, nil }
func (d updateDriver) Driver() driver.Driver                                       { return d }
func (c *updateConn) Prepare(string) (driver.Stmt, error)                          { return nil, errors.New("unsupported") }
func (c *updateConn) Close() error                                                 { return nil }
func (c *updateConn) Begin() (driver.Tx, error)                                    { return &updateTx{s: c.s}, nil }
func (c *updateConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) { return c.Begin() }
func (t *updateTx) Commit() error                                                  { t.closed = true; t.s.committed++; return nil }
func (t *updateTx) Rollback() error {
	if !t.closed {
		t.closed = true
		t.s.rolledBack++
	}
	return nil
}
func (c *updateConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	ret := func(columns []string, values ...[]driver.Value) (driver.Rows, error) {
		return &createSQLRows{columns: columns, values: values}, nil
	}
	switch {
	case strings.Contains(q, "FROM vacancy_roles AS r"):
		return ret([]string{"id", "stable_key", "name", "parent_id", "sort_order", "usageCount", "childCount"}, []driver.Value{int64(3), "lead", "Lead", nil, int64(0), int64(0), int64(0)})
	case strings.Contains(q, "GET_LOCK"):
		c.s.lockAcquired++
		return ret([]string{"lock"}, []driver.Value{int64(1)})
	case strings.Contains(q, "SELECT id FROM company_vacancies WHERE id=? FOR UPDATE"):
		return ret([]string{"id"}, []driver.Value{int64(10)})
	case strings.Contains(q, "FROM company_vacancies v WHERE v.id=?"):
		return ret([]string{"id", "company_name", "entity_type", "real_company_name", "title", "salary_from", "salary_to", "role", "level", "relevance", "favorite", "hidden", "published_at", "loaded_at", "archived", "created_at", "app_id", "app_status", "url", "description", "letter_subject", "letter_md", "last_checked", "updated_at"}, []driver.Value{
			int64(10), "Example", "company", nil, "Lead", nil, nil, "Lead", "Head", nil, int64(0), int64(0), nil, nil, int64(0), "2026-01-01 00:00:00", nil, "", "https://example.com/old", "Description", "Shared", "Public", nil, "2026-01-01 00:00:00"})
	case strings.Contains(q, "FROM vacancy_cover_letters") && strings.Contains(q, "LIMIT 1"):
		return ret([]string{"subject", "markdown", "override_shared", "revision"})
	case strings.Contains(q, "SELECT role_id FROM vacancy_role_assignments"):
		return ret([]string{"role_id"}, []driver.Value{int64(3)})
	case strings.Contains(q, "FROM vacancy_telegram_sources s JOIN telegram_channels"):
		return ret([]string{"id", "channel_id", "name", "url", "username", "message_url", "telegram_contact", "created_at", "updated_at"})
	case strings.Contains(q, "FROM vacancy_publication_sources"):
		return ret([]string{"candidate_id", "url", "updated_at"})
	case strings.Contains(q, "SELECT data_json FROM app_catalog"):
		return ret([]string{"data_json"}, []driver.Value{`{"companies":[{"name":"Example"}],"agencies":[]}`})
	case strings.Contains(q, "SELECT r.id FROM vacancy_roles r"):
		return ret([]string{"id"}, []driver.Value{int64(3)})
	case strings.Contains(q, "SELECT id FROM company_vacancies WHERE url="):
		return ret([]string{"id"})
	case strings.Contains(q, "SELECT cover_letter_subject,cover_letter_md FROM company_vacancies"):
		return ret([]string{"subject", "markdown"}, []driver.Value{"Shared", "Public"})
	case strings.Contains(q, "SELECT COUNT(*) FROM vacancy_cover_letters"):
		return ret([]string{"count"}, []driver.Value{int64(0)})
	case strings.Contains(q, "SELECT COUNT(*) FROM company_vacancy_user_state"):
		return ret([]string{"count"}, []driver.Value{int64(0)})
	case strings.Contains(q, "FROM company_vacancies v"):
		return ret([]string{"id", "company_name", "entity_type", "real_company_name", "title", "salary_from", "salary_to", "role", "level", "relevance", "favorite", "hidden", "published_at", "loaded_at", "last_checked_at", "archived", "created_at", "updated_at", "application_id", "application_status", "url", "description", "letter_subject", "letter_md"})
	default:
		return nil, fmt.Errorf("unexpected query %q", q)
	}
}
func (c *updateConn) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(q, "RELEASE_LOCK") {
		c.s.released++
		return createSQLResult{affected: 1}, nil
	}
	c.s.writes = append(c.s.writes, q)
	if strings.Contains(q, "INSERT INTO vacancy_role_assignments") && c.s.failRole {
		return nil, errors.New("induced role failure")
	}
	return createSQLResult{affected: 1}, nil
}

func TestUpdateManualUsesGETRevisionAndAtomicCommit(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		failRole, stale, personal bool
		wantErr                   error
	}{
		{name: "update passes with exact detail revision"},
		{name: "stale revision rejected", stale: true, wantErr: ErrVersionConflict},
		{name: "role write failure rolls back", failRole: true},
		{name: "personal letter remains personal", personal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &updateSQLState{failRole: tc.failRole}
			db := sql.OpenDB(updateDriver{s: state})
			db.SetMaxOpenConns(1)
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			repo := Repository{DB: db}
			detail, err := repo.DetailByID(ctx, "Eugene", 10)
			if err != nil {
				t.Fatalf("GET baseline: %v", err)
			}
			revision := detail.Vacancy.Revision
			if tc.stale {
				revision = strings.Repeat("b", 64)
			}
			state.committed = 0
			state.rolledBack = 0
			req := UpdateRequest{Input: Input{CompanyName: "Example", EntityType: "company", URL: "https://example.com/old", Title: "Updated Lead", DescriptionMD: "New description", Level: "Head", RoleIDs: []int64{3}}, Revision: revision}
			if tc.personal {
				req.LetterProvided = true
				req.CoverLetterScope = "personal"
				req.CoverLetterSubject = "Private subject"
				req.CoverLetterMD = "Private content"
			}
			_, err = repo.UpdateManual(ctx, "Eugene", 10, req)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("expected %v got %v", tc.wantErr, err)
				}
			case tc.failRole:
				if err == nil {
					t.Fatal("expected failure on role insert")
				}
			case err != nil:
				t.Fatal(err)
			}
			if state.lockAcquired != 1 || state.released != 1 {
				t.Fatalf("named lock leaked: %+v", state)
			}
			if tc.stale || tc.failRole {
				if state.committed != 0 || state.rolledBack != 1 {
					t.Fatalf("expected rollback: %+v", state)
				}
			} else if state.committed != 2 || state.rolledBack != 0 { // update + readback
				t.Fatalf("expected successful transactions: %+v", state)
			}
			if tc.stale && len(state.writes) != 0 {
				t.Fatalf("stale edit wrote SQL: %v", state.writes)
			}
			if tc.personal {
				found := false
				for _, q := range state.writes {
					if strings.Contains(q, "INSERT INTO vacancy_cover_letters") {
						found = true
					}
				}
				if !found {
					t.Fatal("personal letter not stored separately")
				}
			}
		})
	}
}
