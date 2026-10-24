//go:build mariadb

package storage

import (
	"database/sql"
	"errors"
	"testing"
)

// This test is deliberately restricted to the disposable CI database.
// Do not accept a production/staging schema override: the test performs an INSERT.
// The transaction is rolled back, and an error path must roll back as well.
func TestMariaDBDisposableTransactionIntegration(t *testing.T) {
	ctx, db := openDisposableMariaDB(t)
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
