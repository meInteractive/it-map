//go:build mariadb && v39restore && schemainit

package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// The fresh-schema job has no generation table yet. Its run-only nonce is
// instead bound by CI root to the database COMMENT, outside application tables.
// Every reset checks that comment before dropping this disposable database.
func openBlankSchemaFixture(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("schema initialization requires disposable GitHub CI")
		}
		t.Skip("schema initialization requires disposable GitHub CI")
	}
	dsn := os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required disposable MariaDB write DSN not provided")
		}
		t.Skip("disposable MariaDB write DSN not provided")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		t.Fatal("refusing non-disposable MariaDB write DSN")
	}
	nonce := os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")
	if requireDisposableGeneration(nonce, nonce) != nil {
		t.Fatal("required disposable database generation is missing or malformed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminCfg := *cfg
	adminCfg.DBName = ""
	admin, e := sql.Open("mysql", adminCfg.FormatDSN())
	if e != nil {
		t.Fatal("cannot open disposable schema owner")
	}
	defer admin.Close()
	admin.SetMaxOpenConns(1)
	var recorded string
	if e = admin.QueryRowContext(ctx, "SELECT SCHEMA_COMMENT FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='itmap_ci'").Scan(&recorded); e != nil || requireDisposableGeneration(nonce, recorded) != nil {
		t.Fatal("disposable schema comment did not match current run")
	}
	if _, e = admin.ExecContext(ctx, "DROP DATABASE `itmap_ci`"); e != nil {
		t.Fatal("cannot reset owned disposable schema")
	}
	// nonce has already passed the strict UUID guard; database name is constant.
	if _, e = admin.ExecContext(ctx, "CREATE DATABASE `itmap_ci` CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin COMMENT '"+nonce+"'"); e != nil {
		t.Fatal("cannot recreate owned disposable schema")
	}
	db, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal("cannot open blank disposable database")
	}
	db.SetMaxOpenConns(5)
	t.Cleanup(func() { db.Close() })
	return ctx, db
}
