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

// Read-only integration. ITMAP_TEST_MARIADB_DSN must point to an isolated
// staging clone with SELECT-only credentials, NEVER production.
func TestReadApplicationsOnStagingMariaDB(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
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
			t.Fatalf("CRM %s: %T", name, err)
		}
		if result.Records == nil || result.Entities == nil {
			t.Fatal("CRM arrays must be non-nil")
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
