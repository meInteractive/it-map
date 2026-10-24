//go:build mariadb

package tasks

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Select-only smoke test on an isolated staging database. Real recruiter
// contacts, task notes and other private data must not be logged.
func TestMariaDBTaskListReadOnlyIntegration(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required MariaDB DSN or business fixture unavailable")
		}
		t.Skip("staging MariaDB with SELECT-only credentials not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("test DB open failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, user := range []string{"Eugene", "Demo"} {
		result, err := (Repository{DB: db}).List(ctx, user, "", true, nil)
		if err != nil {
			t.Fatalf("task list read failed for %s: %v", user, err)
		}
		if result.Tasks == nil || result.Entities == nil {
			t.Fatal("nil tasks or entities")
		}
		if os.Getenv("ITMAP_TEST_SYNTHETIC_READS") == "1" {
			want := 0
			if user == "Eugene" {
				want = 1
			}
			if len(result.Tasks) != want || len(result.Entities) != want {
				t.Fatal("synthetic tasks/entities missing or leaked between users")
			}
			if want == 1 && (result.Tasks[0].ID != 1 || result.Tasks[0].VacancyTitle != "CI Synthetic Vacancy" || result.Tasks[0].GoogleEventID != "" || result.Tasks[0].CompletedAt != nil) {
				t.Fatal("synthetic task join or optional NULL handling mismatch")
			}
		}
	}
	_, err = (Repository{DB: db}).List(ctx, "Demo", "___NONEXISTENT_TEST_COMPANY___", false, nil)
	if !errors.Is(err, ErrCompanyNotFound) {
		t.Fatal("company lookup failed to reject unknown organization")
	}
}
