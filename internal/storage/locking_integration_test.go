//go:build mariadb

package storage

import (
	"database/sql"
	"errors"
	"os"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

// Verify the singleton generation lock used to coordinate CRM writers, using
// two physical connections. No rows are inserted, updated, deleted or committed.
// This test runs only on the guarded disposable CI database, before business seed.
func TestMariaDBDisposableLockingIntegration(t *testing.T) {
	ctx, db := openDisposableMariaDB(t)
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	first, err := db.Conn(ctx)
	if err != nil {
		t.Fatal("cannot reserve first CI connection")
	}
	defer first.Close()
	second, err := db.Conn(ctx)
	if err != nil {
		t.Fatal("cannot reserve second CI connection")
	}
	defer second.Close()
	var firstID, secondID int64
	if err := first.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&firstID); err != nil {
		t.Fatal("cannot identify first CI connection")
	}
	if err := second.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&secondID); err != nil || firstID == secondID {
		t.Fatal("locking test requires two distinct physical connections")
	}
	// The expected failure must come from InnoDB (1205), not cancellation or
	// waiting for a connection from the Go pool. Only this disposable session
	// is changed; the server/global configuration is untouched.
	if _, err := second.ExecContext(ctx, "SET SESSION innodb_lock_wait_timeout=1"); err != nil {
		t.Fatal("cannot bound CI lock wait")
	}
	opts := &sql.TxOptions{Isolation: sql.LevelRepeatableRead}
	holder, err := first.BeginTx(ctx, opts)
	if err != nil {
		t.Fatal("cannot start lock holder")
	}
	defer holder.Rollback()
	const lockRow = "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE"
	var generation string
	if err := holder.QueryRowContext(ctx, lockRow).Scan(&generation); err != nil || requireDisposableGeneration(os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION"), generation) != nil {
		t.Fatal("cannot lock current disposable generation")
	}
	contender, err := second.BeginTx(ctx, opts)
	if err != nil {
		t.Fatal("cannot start competing transaction")
	}
	defer contender.Rollback()
	err = contender.QueryRowContext(ctx, lockRow).Scan(&generation)
	var lockError *mysql.MySQLError
	if !errors.As(err, &lockError) || lockError.Number != 1205 || ctx.Err() != nil {
		t.Fatal("competing FOR UPDATE must fail with InnoDB lock-wait timeout 1205")
	}
	if err := contender.Rollback(); err != nil {
		t.Fatal("cannot roll back timed-out transaction")
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal("cannot release generation lock")
	}
	// Reuse the same contender connection to prove that rollback released the
	// row lock and left the connection usable, without changing the marker.
	retry, err := second.BeginTx(ctx, opts)
	if err != nil {
		t.Fatal("cannot start transaction after lock release")
	}
	defer retry.Rollback()
	if err := retry.QueryRowContext(ctx, lockRow).Scan(&generation); err != nil || requireDisposableGeneration(os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION"), generation) != nil {
		t.Fatal("generation must remain unchanged and lockable after rollback")
	}
	if err := retry.Rollback(); err != nil {
		t.Fatal("cannot finish lock-release verification")
	}
}
