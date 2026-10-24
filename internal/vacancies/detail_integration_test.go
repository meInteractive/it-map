//go:build mariadb

package vacancies

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Read-only contract smoke test. ITMAP_TEST_MARIADB_DSN must use a SELECT-only
// user on an isolated staging MariaDB restored from the private project dump.
// No vacancy contents or private personal data are printed in test output.
func TestMariaDBVacancyDetailReadOnlyIntegration(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required MariaDB DSN or business fixture unavailable")
		}
		t.Skip("set a read-only staging MariaDB DSN to exercise SQL queries")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("could not open test DB")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var id int64
	err = db.QueryRowContext(ctx, "SELECT id FROM company_vacancies ORDER BY id LIMIT 1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required MariaDB DSN or business fixture unavailable")
		}
		t.Skip("staging has no vacancies")
	}
	if err != nil {
		t.Fatal("failed to locate one test vacancy")
	}
	for _, user := range []string{"Eugene", "Demo"} {
		detail, err := (Repository{DB: db}).DetailByID(ctx, user, id)
		if err != nil {
			t.Fatalf("vacancy detail read failed for %s: %v", user, err)
		}
		if detail.Vacancy.ID != id || detail.Vacancy.Roles == nil || detail.Vacancy.TelegramSources == nil || detail.Vacancy.PublicationSources == nil || detail.Entities == nil {
			t.Fatalf("vacancy detail shape invalid for %s", user)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(detail.Vacancy.Revision) {
			t.Fatal("invalid revision token")
		}
	}
	_, err = (Repository{DB: db}).DetailByID(ctx, "Demo", 9223372036854775807)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing vacancy must return ErrNotFound, received %T", err)
	}
}
