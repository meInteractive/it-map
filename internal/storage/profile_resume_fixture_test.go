//go:build mariadb && profilewrites

package storage

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"itmap/internal/profiles"
)

type profileResumeFixture struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	repo     profiles.Repository
	original profiles.Resume
}

func openProfileResumeFixture(t *testing.T) *profileResumeFixture {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("profile writes require disposable GitHub CI")
		}
		t.Skip("profile writes require disposable GitHub CI")
	}
	ctx, db := openDisposableMariaDB(t)
	f := &profileResumeFixture{t: t, ctx: ctx, db: db, repo: profiles.Repository{DB: db}}
	if f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name IN ('Demo','Nastya')`) != 0 {
		t.Fatal("profile fixture requires unused synthetic owners")
	}
	f.original = f.read("Eugene")
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanup, `DELETE FROM user_profiles WHERE user_name IN ('Demo','Nastya')`); err != nil {
			t.Error("cannot clean synthetic profiles")
		}
		after, err := f.repo.ReadResume(cleanup, "Eugene")
		if err != nil || !reflect.DeepEqual(f.original, after) {
			t.Error("profile writes changed another user's original profile")
		}
		var count int
		var generation string
		if err = db.QueryRowContext(cleanup, `SELECT COUNT(*) FROM user_profiles WHERE user_name IN ('Demo','Nastya')`).Scan(&count); err != nil || count != 0 {
			t.Error("profile fixture leaked private rows")
		}
		if err = db.QueryRowContext(cleanup, `SELECT generation FROM app_runtime_generation WHERE id=1`).Scan(&generation); err != nil || generation != os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION") {
			t.Error("profile writes changed runtime generation")
		}
	})
	return f
}
func (f *profileResumeFixture) count(q string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, q, args...).Scan(&n); err != nil {
		f.t.Fatalf("profile fixture query failed: %T", err)
	}
	return n
}
func (f *profileResumeFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("profile fixture statement failed: %T", err)
	}
}
func (f *profileResumeFixture) read(user string) profiles.Resume {
	f.t.Helper()
	value, err := f.repo.ReadResume(f.ctx, user)
	if err != nil {
		f.t.Fatalf("profile read failed: %T", err)
	}
	return value
}
func (f *profileResumeFixture) save(user, text string, revision int64) profiles.Resume {
	f.t.Helper()
	value, err := f.repo.SaveResume(f.ctx, user, profiles.TextInput{Text: text, Revision: revision})
	if err != nil {
		f.t.Fatalf("profile save failed: %T", err)
	}
	return value
}
