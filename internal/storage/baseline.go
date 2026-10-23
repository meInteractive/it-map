// Package storage provides MariaDB access without coupling repositories to a specific ORM.
package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed baseline.json
var baselineJSON []byte

type schemaBaseline struct {
	Tables map[string]map[string]string `json:"tables"`
}

type column struct{ Table, Name, SQLType string }

// Baseline is a schema-only contract; it includes no contents from the D1 export.
// Additional future columns are tolerated, but removed or retyped legacy columns
// block readiness to prevent silent data corruption.
func ValidateBaseline(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("database is not configured")
	}
	var baseline schemaBaseline
	if err := json.Unmarshal(baselineJSON, &baseline); err != nil {
		return fmt.Errorf("invalid embedded DB baseline: %w", err)
	}
	if len(baseline.Tables) != 72 {
		return errors.New("incorrect embedded DB baseline table count")
	}
	var schema string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		return fmt.Errorf("read active schema: %w", err)
	}
	if schema == "" {
		return errors.New("no active database selected")
	}
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE
        FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ?`, schema)
	if err != nil {
		return fmt.Errorf("read DB schema: %w", err)
	}
	actual := make(map[string]map[string]string)
	for rows.Next() {
		var t, n, sqlType string
		if err := rows.Scan(&t, &n, &sqlType); err != nil {
			rows.Close()
			return fmt.Errorf("scan DB schema: %w", err)
		}
		if actual[t] == nil {
			actual[t] = make(map[string]string)
		}
		actual[t][n] = strings.ToLower(sqlType)
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return fmt.Errorf("read DB schema rows: %w", scanErr)
	}
	return compareBaseline(baseline.Tables, actual)
}

// compareBaseline is independent from SQL execution and is unit-testable offline.
func compareBaseline(expectedTables, actual map[string]map[string]string) error {
	for table, columns := range expectedTables {
		received, exists := actual[table]
		if !exists {
			return fmt.Errorf("DB schema missing table %q", table)
		}
		for name, expected := range columns {
			got, found := received[name]
			if !found {
				return fmt.Errorf("DB schema missing column %s.%s", table, name)
			}
			if expected != "*" && expected != got {
				return fmt.Errorf("DB schema type mismatch for %s.%s: expected %s got %s", table, name, expected, got)
			}
		}
	}
	return nil
}
