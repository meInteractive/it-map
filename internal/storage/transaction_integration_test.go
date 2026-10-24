//go:build mariadb

package storage

import (
	"context"
	"database/sql"
	"errors"

	mysql "github.com/go-sql-driver/mysql"
	"os"
	"testing"
	"time"
)

// This test is deliberately restricted to the disposable CI database.
// Do not accept a production/staging schema override: the test performs an INSERT.
// The transaction is rolled back, and an error path must roll back as well.
func TestMariaDBDisposableTransactionIntegration(t *testing.T) {
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
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
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
	var original int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agency_identity").Scan(&original); err != nil {
		t.Fatal("cannot count synthetic table rows")
	}
	if original != 0 {
		t.Fatal("disposable integration database must start with zero agency identities")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("begin transaction failed")
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "INSERT INTO agency_identity (agency_name) VALUES (?)", "__CI_SYNTHETIC_ROLLBACK_ONLY__")
	if err != nil {
		t.Fatal("synthetic insert failed")
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal("cannot resolve synthetic row ID")
	}
	result, err = tx.ExecContext(ctx, "UPDATE agency_identity SET agency_name=agency_name WHERE id=?", id)
	if err != nil {
		t.Fatal("synthetic idempotent UPDATE failed")
	}
	count, err := result.RowsAffected()
	if err != nil {
		t.Fatal("cannot determine matched rows")
	}
	if count != 1 {
		t.Fatal("clientFoundRows=true required: unchanged UPDATE must report one matched row")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal("rollback failed")
	}

	sentinel := errors.New("intentionally reject transaction")
	err = WithinTx(ctx, db, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO agency_identity (agency_name) VALUES (?)", "__CI_SYNTHETIC_ERROR_ROLLBACK__"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal("transaction error was not propagated")
	}
	var remaining int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agency_identity").Scan(&remaining); err != nil {
		t.Fatal("cannot verify transactional rollback")
	}
	if remaining != 0 {
		t.Fatal("rollback leaked synthetic rows")
	}
}
