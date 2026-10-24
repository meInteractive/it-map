//go:build mariadb

package vacancies

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// This test is opt-in and MUST use an isolated staging clone with SELECT-only
// privileges. Never point ITMAP_TEST_MARIADB_DSN at the live service.
func TestCompanyVacanciesStaging(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required MariaDB DSN or business fixture unavailable")
		}
		t.Skip("staging MariaDB DSN not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("unable to initialize staging test pool")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	repo := Repository{DB: db}
	for _, user := range []string{"Eugene", "Demo"} {
		// Use one catalog-supported company from the staging database. The
		// query is read-only and exercises roles, letters and Telegram links.
		var name string
		err = db.QueryRowContext(ctx, `SELECT company_name FROM company_vacancies WHERE company_name<>'' ORDER BY id LIMIT 1`).Scan(&name)
		if err == sql.ErrNoRows {
			if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
				t.Fatal("required MariaDB DSN or business fixture unavailable")
			}
			t.Skip("no staging vacancies")
		}
		if err != nil {
			t.Fatal("could not read staging company")
		}
		rows, err := repo.CompanyVacancies(ctx, user, name)
		if err != nil {
			t.Fatalf("company vacancies %s: %v", user, err)
		}
		if rows == nil || len(rows) > 100 {
			t.Fatalf("invalid result length: %d", len(rows))
		}
		for _, v := range rows {
			if v.ID <= 0 || v.CompanyName == "" || v.Roles == nil || v.TelegramSources == nil || v.CoverLetterScope == "" {
				t.Fatal("malformed company vacancy")
			}
		}
	}
}
