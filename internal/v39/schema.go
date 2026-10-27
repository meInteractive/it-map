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

//go:embed historical_policy.json
var historicalPolicy []byte

//go:embed catalog_policy.json
var catalogPolicy []byte

// CatalogPolicy contains public source defaults/options and synthetic embedded
// examples only. Runtime catalog records never populate this document.
func CatalogPolicy() json.RawMessage { return append(json.RawMessage(nil), catalogPolicy...) }

// HistoricalPolicy contains source role templates, warnings and Unicode metadata.
func HistoricalPolicy() json.RawMessage { return append(json.RawMessage(nil), historicalPolicy...) }

//go:embed restore_policy.json
var restorePolicy []byte

type RestoreRules struct {
	SchedulerDefaults json.RawMessage `json:"schedulerDefaults"`
	SupportedSources  []string        `json:"supportedSources"`
	Warnings          []string        `json:"warnings"`
}

func RestorePolicy() RestoreRules {
	var rules RestoreRules
	if err := json.Unmarshal(restorePolicy, &rules); err != nil {
		panic("invalid trusted restore policy")
	}
	return rules
}

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
	ID    int    `json:"id"`
	Seq   int    `json:"seq"`
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
