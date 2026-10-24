//go:build mariadb

package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Set ITMAP_TEST_MARIADB_DSN to a READ-ONLY account on an isolated staging
// database restored from the private source dump. No private data are logged.
func TestMariaDBVacancyListReadOnlyIntegration(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required MariaDB DSN unavailable")
		}
		t.Skip("set ITMAP_TEST_MARIADB_DSN for isolated staging integration")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("unable to initialize integration driver")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p := All
	for _, user := range []string{"Eugene", "Demo"} {
		result, err := (Repository{DB: db}).List(ctx, user, &p, time.Now())
		if err != nil {
			t.Fatalf("list failed for %s: %T (catalog/record missing: %t)", user, err, errors.Is(err, sql.ErrNoRows))
		}
		if result.Vacancies == nil || result.RoleTree == nil || result.Entities == nil || result.ApplicationSummary == nil {
			t.Fatalf("unexpected nil arrays for %s", user)
		}
		if os.Getenv("ITMAP_TEST_SYNTHETIC_READS") == "1" {
			if len(result.Vacancies) != 1 || len(result.Entities) != 1 {
				t.Fatal("synthetic vacancy/catalog fixture missing")
			}
			v := result.Vacancies[0]
			if v.ID != 1 || v.Title != "CI Synthetic Vacancy" || v.RealCompanyName != "" || v.SalaryFrom != nil {
				t.Fatal("synthetic vacancy or optional NULL handling mismatch")
			}
			if user == "Eugene" {
				if v.ApplicationID == nil || *v.ApplicationID != 1 {
					t.Fatal("synthetic application link missing")
				}
			} else if v.ApplicationID != nil {
				t.Fatal("synthetic application leaked to Demo")
			}
		}
	}
}
