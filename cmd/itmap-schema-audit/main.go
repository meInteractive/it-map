// Command itmap-schema-audit compares schema metadata without changing data.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"itmap/internal/dbschema"
	"itmap/internal/storage"
)

var errUnavailable = errors.New("schema audit unavailable")
var errIncompatible = errors.New("schema audit incompatible")

type auditFunc func(context.Context, string) error

func run(ctx context.Context, getenv func(string) string, audit auditFunc, out io.Writer) int {
	if ctx == nil || getenv == nil || audit == nil || out == nil || getenv("ITMAP_SCHEMA_AUDIT") != "1" {
		return 2
	}
	settings := storage.Settings{Host: getenv("DB_HOST"), Port: getenv("DB_PORT"), Database: getenv("DB_NAME"), User: getenv("DB_USER")}
	dsn, err := storage.MakeDSN(settings, getenv("DB_PASSWORD"))
	if err != nil {
		fmt.Fprintln(out, "schema_audit_invalid_configuration")
		return 2
	}
	if err = audit(ctx, dsn); err != nil {
		if errors.Is(err, errIncompatible) {
			fmt.Fprintln(out, "schema_audit_mismatch_or_incomplete")
		} else {
			fmt.Fprintln(out, "schema_audit_unavailable")
		}
		return 1
	}
	fmt.Fprintln(out, "schema_audit_compatible")
	return 0
}

func audit(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return errUnavailable
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return errUnavailable
	}
	defer tx.Rollback()
	var version string
	if tx.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version) != nil {
		return errUnavailable
	}
	if dbschema.CheckServerVersion(version) != nil {
		return errIncompatible
	}
	snapshot, err := dbschema.Inspect(ctx, tx)
	if err != nil {
		return errUnavailable
	}
	if dbschema.CheckFull(snapshot) != nil {
		return errIncompatible
	}
	if ctx.Err() != nil {
		return errUnavailable
	}
	// Always roll back: the audit does not commit or run DDL/DML.
	return nil
}

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "schema_audit_no_arguments")
		os.Exit(2)
	}
	os.Exit(run(context.Background(), os.Getenv, audit, os.Stdout))
}
