//go:build mariadb

package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// openDisposableMariaDB validates the target BEFORE connecting, then verifies
// the current CI generation BEFORE allowing any row locks or write operations.
// It is test-only and never accepts a real staging/production database.
func openDisposableMariaDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required disposable MariaDB write DSN not provided")
		}
		t.Skip("disposable MariaDB write DSN not provided")
	}
	// Refuse even to CONNECT if the supplied DSN points outside the disposable,
	// loopback-only CI database. Database name alone is not a sufficient guard.
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil || requireDisposableTarget(parsed.Net, parsed.Addr, parsed.DBName, parsed.User) != nil {
		t.Fatal("refusing non-disposable MariaDB write DSN")
	}
	nonce := os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")
	if err := requireDisposableGeneration(nonce, nonce); err != nil {
		t.Fatal("required disposable database generation is missing or malformed")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("cannot initialize disposable MariaDB connection")
	}
	t.Cleanup(func() { db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("cannot reach disposable MariaDB")
	}
	var name string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal("cannot resolve database name")
	}
	// Fail closed even if someone mistakenly supplies production credentials.
	if name != "itmap_ci" {
		t.Fatalf("refusing write integration test outside disposable itmap_ci database")
	}
	// A random value is inserted into this schema by this workflow run only.
	// Reject similarly named local/shared databases, including stale CI copies.
	var recordedNonce string
	if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&recordedNonce); err != nil || requireDisposableGeneration(nonce, recordedNonce) != nil {
		t.Fatal("disposable CI database generation did not match the current run")
	}
	return ctx, db
}
