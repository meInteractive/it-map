package backup

import (
	"testing"

	"itmap/internal/v39"
)

// Новая операционная схема должна получить версионированный backup-контракт,
// а не молча исчезнуть из экспорта или восстановления.
func TestBackupInventoryRejectsUnversionedConsumerSchema(t *testing.T) {
	trusted := v39.Trusted()
	var definition v39.Table
	for _, table := range trusted.Tables {
		if table.Name == "firecrawl_settings" {
			definition = table
			break
		}
	}
	if len(definition.Columns) == 0 {
		t.Fatal("missing trusted scheduler settings table")
	}
	table := sqlTable{Name: definition.Name, Kind: "BASE TABLE", Engine: "InnoDB"}
	for _, column := range definition.Columns {
		kind := map[string]string{"INTEGER": "bigint", "REAL": "double", "TEXT": "longtext"}[column.Type]
		if kind == "" {
			t.Fatalf("unexpected trusted type %q", column.Type)
		}
		table.Columns = append(table.Columns, sqlColumn{Name: column.Name, Type: kind})
	}
	if err := validateInventory(map[string]sqlTable{table.Name: table}, trusted); err != nil {
		t.Fatalf("trusted table rejected: %v", err)
	}
	t.Run("unknown operational table", func(t *testing.T) {
		actual := map[string]sqlTable{table.Name: table, "ci_consumer_control": {Name: "ci_consumer_control", Kind: "BASE TABLE", Engine: "InnoDB"}}
		if err := validateInventory(actual, trusted); err == nil {
			t.Fatal("unversioned operational table accepted")
		}
	})
	t.Run("unknown scheduler column", func(t *testing.T) {
		changed := table
		changed.Columns = append(append([]sqlColumn(nil), table.Columns...), sqlColumn{Name: "ci_consumer_mode", Type: "longtext"})
		if err := validateInventory(map[string]sqlTable{changed.Name: changed}, trusted); err == nil {
			t.Fatal("unversioned scheduler column accepted")
		}
	})
}
