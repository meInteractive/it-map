//go:build mariadb

package applications

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Read-only integration. ITMAP_TEST_MARIADB_DSN must point to an isolated
// staging clone with SELECT-only credentials, NEVER production.
func TestReadApplicationsOnStagingMariaDB(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required MariaDB DSN or business fixture unavailable")
		}
		t.Skip("isolated MariaDB staging DSN unavailable")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("could not initialize staging pool")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	repo := Repository{DB: db}
	for _, name := range []string{"Eugene", "Demo"} {
		result, err := repo.CRM(ctx, name)
		if err != nil {
			t.Fatalf("CRM %s: %T (catalog/record missing: %t)", name, err, errors.Is(err, sql.ErrNoRows))
		}
		if result.Records == nil || result.Entities == nil {
			t.Fatal("CRM arrays must be non-nil")
		}
		if os.Getenv("ITMAP_TEST_SYNTHETIC_READS") == "1" {
			want := 0
			if name == "Eugene" {
				want = 1
			}
			if len(result.Records) != want || len(result.Entities) != want {
				t.Fatal("synthetic CRM records/entities missing or leaked between users")
			}
			if want == 1 && (result.Records[0].ID != 1 || result.Records[0].VacancyTitle != "CI Synthetic Vacancy" || len(result.Records[0].History) != 1 || result.Records[0].ApplicationSources[0] != "site") {
				t.Fatal("synthetic application join/history/sources mismatch")
			}
			if _, err := repo.ByID(ctx, "Demo", 1); !errors.Is(err, ErrNotFound) {
				t.Fatal("Demo must not read Eugene's synthetic application")
			}
		}
		for _, rec := range result.Records {
			if len(rec.Revision) != 64 || rec.CompanyName == "" || rec.History == nil || rec.ApplicationSources == nil {
				t.Fatal("malformed CRM record")
			}
			detail, err := repo.ByID(ctx, name, rec.ID)
			if err != nil || detail.Revision != rec.Revision {
				t.Fatal("inconsistent application snapshot")
			}
		}
	}
}
