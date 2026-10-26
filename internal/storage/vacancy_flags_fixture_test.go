//go:build mariadb && vacancywrites

package storage

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"itmap/internal/vacancies"
)

type vacancyFlagFixture struct {
	t    *testing.T
	ctx  context.Context
	db   *sql.DB
	repo vacancies.Repository
}

func openVacancyFlagFixture(t *testing.T) *vacancyFlagFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("vacancy writes require disposable GitHub CI")
		}
		t.Skip("vacancy writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &vacancyFlagFixture{t: t, ctx: ctx, db: db, repo: vacancies.Repository{DB: db}}
	if f.count(`SELECT COUNT(*) FROM company_vacancies WHERE id=1 AND company_name='CI Synthetic Employer' AND title='CI Synthetic Vacancy'`) != 1 || f.count(`SELECT COUNT(*) FROM company_vacancy_user_state WHERE vacancy_id=1 AND user_name IN ('Demo','Nastya')`) != 0 {
		t.Fatal("vacancy flag fixture requires unused synthetic owners")
	}
	original := f.detail("Eugene")
	generation := os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, `DELETE FROM company_vacancy_user_state WHERE vacancy_id=1 AND user_name IN ('Demo','Nastya')`); err != nil {
			t.Error("cannot clean synthetic vacancy flags")
		}
		after, err := f.repo.DetailByID(cleanup, "Eugene", 1)
		if err != nil || !reflect.DeepEqual(original, after) {
			t.Error("vacancy flag writes changed shared vacancy or another user")
		}
		var remaining int
		var afterGeneration string
		if err = db.QueryRowContext(cleanup, `SELECT COUNT(*) FROM company_vacancy_user_state WHERE vacancy_id=1 AND user_name IN ('Demo','Nastya')`).Scan(&remaining); err != nil || remaining != 0 {
			t.Error("vacancy flags leaked into later smoke")
		}
		if err = db.QueryRowContext(cleanup, `SELECT generation FROM app_runtime_generation WHERE id=1`).Scan(&afterGeneration); err != nil || afterGeneration != generation {
			t.Error("vacancy flags changed runtime generation")
		}
	})
	return f
}
func (f *vacancyFlagFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("vacancy fixture statement failed: %T", err)
	}
}
func (f *vacancyFlagFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, q, args...).Scan(&n); err != nil {
		f.t.Fatalf("vacancy fixture query failed: %T", err)
	}
	return n
}
func (f *vacancyFlagFixture) detail(user string) vacancies.DetailResponse {
	f.t.Helper()
	v, err := f.repo.DetailByID(f.ctx, user, 1)
	if err != nil {
		f.t.Fatalf("vacancy detail unavailable: %T", err)
	}
	return v
}
func (f *vacancyFlagFixture) set(user, key string, value bool) vacancies.UserFlags {
	f.t.Helper()
	v, err := f.repo.SetUserFlag(f.ctx, user, 1, key, value)
	if err != nil {
		f.t.Fatalf("vacancy flag write failed: %T", err)
	}
	return v
}
func (f *vacancyFlagFixture) seed(user string, relevance int) {
	f.t.Helper()
	f.exec(`INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at) VALUES(?,1,?,'2026-10-26 00:00:00')`, user, relevance)
}
