//go:build mariadb && taskwrites

package storage

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"itmap/internal/tasks"
)

const taskUser = "Demo"
const taskCompany = "CI Synthetic Employer"
const taskCalendarMarker = "CI_SYNTHETIC_NO_TOKEN"

type taskFixture struct {
	t    *testing.T
	ctx  context.Context
	db   *sql.DB
	repo tasks.Repository
}

func openTaskFixture(t *testing.T) *taskFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("task writes require disposable GitHub CI")
		}
		t.Skip("task writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &taskFixture{t: t, ctx: ctx, db: db, repo: tasks.Repository{DB: db}}
	if f.count("SELECT COUNT(*) FROM company_crm WHERE company_name=?", taskCompany) != 1 ||
		f.count("SELECT COUNT(*) FROM company_tasks WHERE user_name=?", taskUser) != 0 ||
		f.count("SELECT COUNT(*) FROM google_calendar_user_connection WHERE user_name=?", taskUser) != 0 {
		t.Fatal("task writes require the known empty Demo task/calendar fixture")
	}
	original, err := f.repo.List(ctx, "Eugene", "", true, nil)
	if err != nil || len(original.Tasks) != 1 || original.Tasks[0].ID != 1 {
		t.Fatal("original synthetic task fixture unavailable")
	}
	var crmBefore string
	if err := db.QueryRowContext(ctx, "SELECT JSON_ARRAY(company_name,status,comment_md,updated_at,favorite) FROM company_crm WHERE company_name=?", taskCompany).Scan(&crmBefore); err != nil {
		t.Fatal("cannot inspect original synthetic CRM fixture")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []struct {
			query string
			args  []any
		}{
			{"DELETE FROM company_tasks WHERE user_name=? AND company_name=?", []any{taskUser, taskCompany}},
			{"DELETE FROM google_calendar_user_connection WHERE user_name=? AND refresh_token_encrypted=?", []any{taskUser, taskCalendarMarker}},
		} {
			if _, err := db.ExecContext(cleanup, q.query, q.args...); err != nil {
				t.Error("synthetic task cleanup failed")
			}
		}
		var remaining int
		if err := db.QueryRowContext(cleanup, `SELECT (SELECT COUNT(*) FROM company_tasks WHERE user_name=?) +
   (SELECT COUNT(*) FROM google_calendar_user_connection WHERE user_name=?)`, taskUser, taskUser).Scan(&remaining); err != nil || remaining != 0 {
			t.Error("task writes leaked into later readonly fixtures")
		}
		after, err := f.repo.List(cleanup, "Eugene", "", true, nil)
		if err != nil || !reflect.DeepEqual(original, after) {
			t.Error("another user's original tasks were changed")
		}
		var crmAfter, generation string
		if err := db.QueryRowContext(cleanup, "SELECT JSON_ARRAY(company_name,status,comment_md,updated_at,favorite) FROM company_crm WHERE company_name=?", taskCompany).Scan(&crmAfter); err != nil || crmAfter != crmBefore {
			t.Error("task writes changed the original shared CRM row")
		}
		if err := db.QueryRowContext(cleanup, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
			t.Error("task writes changed the disposable generation")
		}
	})
	return f
}
func (f *taskFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		f.t.Fatal("cannot inspect synthetic task fixture")
	}
	return n
}
func (f *taskFixture) input() tasks.CreateInput {
	vacancy := int64(1)
	return tasks.CreateInput{CompanyName: taskCompany, VacancyID: &vacancy, Type: "call", Note: "CI synthetic task roundtrip", DueAt: "2026-10-27T00:15:00.123+03:00"}
}
func (f *taskFixture) row(id int64) tasks.Task {
	f.t.Helper()
	out, err := f.repo.List(f.ctx, taskUser, "", true, nil)
	if err != nil {
		f.t.Fatal("cannot read synthetic task")
	}
	for _, row := range out.Tasks {
		if row.ID == id {
			return row
		}
	}
	f.t.Fatal("synthetic task not found")
	return tasks.Task{}
}
func (f *taskFixture) create() int64 {
	f.t.Helper()
	id, err := f.repo.Create(f.ctx, taskUser, f.input())
	if err != nil || id <= 1 {
		f.t.Fatalf("synthetic task create failed: %T", err)
	}
	return id
}
