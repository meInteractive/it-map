// Package v39 exposes trusted structure copied from the unchanged legacy
// generated schema. It contains DDL and column metadata only, never table rows.
package v39

import (
	_ "embed"
	"encoding/json"
)

//go:embed schema.json
var document []byte

//go:embed default_catalog.json
var defaultCatalog []byte

// DefaultCatalog matches normalizeCatalog(DATA) when no stored catalog exists.
func DefaultCatalog() json.RawMessage { return append(json.RawMessage(nil), defaultCatalog...) }

type Column struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	NotNull      bool    `json:"notNull"`
	DefaultValue *string `json:"defaultValue"`
	PrimaryKey   bool    `json:"primaryKey"`
}
type ForeignKey struct {
	Table string `json:"table"`
	From  string `json:"from"`
	To    string `json:"to"`
}
type Table struct {
	Name         string       `json:"name"`
	Columns      []Column     `json:"columns"`
	ForeignKeys  []ForeignKey `json:"foreignKeys"`
	ParentTables []string     `json:"parentTables"`
}
type Object struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Table string `json:"tbl_name"`
	SQL   string `json:"sql"`
}
type Schema struct {
	Objects           []Object `json:"objects"`
	Tables            []Table  `json:"tables"`
	OperationalTables []Table  `json:"operationalTables"`
}

// Fresh allocations prevent response mutation from changing trusted metadata.
func Trusted() Schema {
	var result Schema
	if err := json.Unmarshal(document, &result); err != nil {
		panic("invalid trusted v39 structure")
	}
	return result
}
