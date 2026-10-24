//go:build mariadb

package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// This test uses SELECT-only credentials. It runs against the empty synthetic
// schema in CI or an explicitly approved, isolated staging database. It does
// not require, expose, or modify private dump data.
func TestMariaDBSchemaBaselineIntegration(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		t.Skip("isolated read-only MariaDB DSN not provided")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("cannot open integration database") // never print DSN
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("cannot reach isolated integration database")
	}
	if err := ValidateBaseline(ctx, db); err != nil {
		t.Fatalf("MariaDB baseline not satisfied: %v", err)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`).Scan(&tables); err != nil {
		t.Fatal("cannot count schema tables")
	}
	if tables != 72 {
		t.Fatalf("unexpected table count: %d, expected 72", tables)
	}
}
