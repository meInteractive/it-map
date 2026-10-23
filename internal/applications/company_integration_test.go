//go:build mariadb

package applications

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestCompanyApplicationsStaging(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		t.Skip("isolated staging MariaDB DSN not configured")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("unable to initialize staging pool")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var company string
	err = db.QueryRowContext(ctx, `SELECT company_name FROM company_applications ORDER BY id LIMIT 1`).Scan(&company)
	if err == sql.ErrNoRows {
		t.Skip("no company applications")
	}
	if err != nil {
		t.Fatal("could not select company")
	}
	repo := Repository{DB: db}
	for _, user := range []string{"Eugene", "Demo"} {
		result, err := repo.CompanyApplications(ctx, user, company)
		if err != nil {
			t.Fatalf("company response %s: %v", user, err)
		}
		if result.Limit != 10 || result.Records == nil || result.Vacancies == nil || result.CompanyName != company {
			t.Fatal("incomplete company response")
		}
		for _, r := range result.Records {
			if r.CompanyName != company {
				t.Fatal("cross-company leakage")
			}
		}
	}
}
