//go:build mariadb

package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// schemaObjectManifest records structure ONLY from the disposable MariaDB fixture.
// It contains no table rows, credentials, or production data.
//
//go:embed schema-objects.json
var schemaObjectsJSON []byte

type schemaObjectManifest struct {
	ForeignKeys []struct {
		Table string `json:"table"`
		Name  string `json:"name"`
	} `json:"foreign_keys"`
	Triggers []struct {
		Table  string `json:"table"`
		Name   string `json:"name"`
		Timing string `json:"timing"`
		Event  string `json:"event"`
	} `json:"triggers"`
}

// TestMariaDBSchemaObjectsIntegration detects missing/replaced FK constraints,
// triggers, and non-InnoDB tables, even when the *total* counts still match.
// Queries are SELECT-only. Use the disposable CI writer identity because
// MariaDB may hide trigger metadata from accounts lacking TRIGGER privileges;
// granting TRIGGER to the read-only application identity would be unsafe.
func TestMariaDBSchemaObjectsIntegration(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("required disposable MariaDB metadata DSN not provided")
		}
		t.Skip("disposable MariaDB metadata DSN not provided")
	}
	var expected schemaObjectManifest
	if err := json.Unmarshal(schemaObjectsJSON, &expected); err != nil {
		t.Fatalf("invalid embedded schema-object manifest: %v", err)
	}
	if len(expected.ForeignKeys) != 50 || len(expected.Triggers) != 22 {
		t.Fatal("schema-object manifest does not match the expected baseline")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("cannot initialize disposable MariaDB") // Never log the DSN.
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("cannot reach disposable MariaDB")
	}
	// Do not allow privileged schema inspection against arbitrary databases.
	// This check is a staging CI gate, never a production operations command.
	var databaseName string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&databaseName); err != nil || databaseName != "itmap_ci" {
		t.Fatal("schema-object metadata check is restricted to itmap_ci")
	}

	var engineCount, unexpected int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*),
	SUM(CASE WHEN ENGINE <> 'InnoDB' OR ENGINE IS NULL THEN 1 ELSE 0 END)
	FROM information_schema.TABLES
	WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'`).Scan(&engineCount, &unexpected); err != nil {
		t.Fatal("cannot inspect MariaDB table engines")
	}
	if engineCount != 72 || unexpected != 0 {
		t.Fatalf("unexpected table engines: tables=%d nonInnoDB=%d", engineCount, unexpected)
	}

	fkRows, err := db.QueryContext(ctx, `SELECT TABLE_NAME, CONSTRAINT_NAME
	FROM information_schema.TABLE_CONSTRAINTS
	WHERE CONSTRAINT_SCHEMA = DATABASE() AND CONSTRAINT_TYPE = 'FOREIGN KEY'`)
	if err != nil {
		t.Fatal("cannot inspect MariaDB foreign keys")
	}
	foreignKeys := make(map[string]bool)
	for fkRows.Next() {
		var table, name string
		if err := fkRows.Scan(&table, &name); err != nil {
			fkRows.Close()
			t.Fatal("cannot read MariaDB foreign key")
		}
		foreignKeys[table+"\x00"+name] = true
	}
	if err := fkRows.Err(); err != nil {
		fkRows.Close()
		t.Fatal("cannot read all MariaDB foreign keys")
	}
	fkRows.Close()
	if len(foreignKeys) != len(expected.ForeignKeys) {
		t.Fatalf("wrong foreign key count: got %d, want %d", len(foreignKeys), len(expected.ForeignKeys))
	}
	for _, fk := range expected.ForeignKeys {
		if !foreignKeys[fk.Table+"\x00"+fk.Name] {
			t.Errorf("missing foreign key %s on table %s", fk.Name, fk.Table)
		}
	}

	triggerRows, err := db.QueryContext(ctx, `SELECT EVENT_OBJECT_TABLE, TRIGGER_NAME, ACTION_TIMING, EVENT_MANIPULATION
	FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE()`)
	if err != nil {
		t.Fatal("cannot inspect MariaDB triggers")
	}
	triggers := make(map[string]bool)
	for triggerRows.Next() {
		var table, name, timing, event string
		if err := triggerRows.Scan(&table, &name, &timing, &event); err != nil {
			triggerRows.Close()
			t.Fatal("cannot read MariaDB trigger")
		}
		triggers[fmt.Sprintf("%s\x00%s\x00%s\x00%s", table, name, timing, event)] = true
	}
	if err := triggerRows.Err(); err != nil {
		triggerRows.Close()
		t.Fatal("cannot read all MariaDB triggers")
	}
	triggerRows.Close()
	if len(triggers) != len(expected.Triggers) {
		t.Fatalf("wrong trigger count: got %d, want %d", len(triggers), len(expected.Triggers))
	}
	for _, trigger := range expected.Triggers {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s", trigger.Table, trigger.Name, trigger.Timing, trigger.Event)
		if !triggers[key] {
			t.Errorf("missing or changed trigger %s on table %s (%s %s)", trigger.Name, trigger.Table, trigger.Timing, trigger.Event)
		}
	}
}
