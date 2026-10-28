// Package dbschema describes trusted MariaDB baseline DDL and its complete
// metadata contract. It never executes uploaded SQL or reads business rows.
package dbschema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
)

//go:embed baseline_v1.json
var document []byte

type Action struct{ Kind, Table, Name, SQL string }

// Snapshot contains information_schema rows only; cells preserve SQL NULL.
// It deliberately excludes table counters/cardinality, definers and row data.
type Snapshot map[string][][]*string
type Policy struct {
	Version      int
	SourceSHA256 string
	Actions      []Action
	Metadata     Snapshot
}

var ErrUnverified = errors.New("trusted schema metadata has not been verified")

// Trusted returns owned allocations so callers cannot change the next policy.
func Trusted() Policy {
	var p Policy
	if json.Unmarshal(document, &p) != nil || p.Version != 1 || len(p.Actions) != 217 {
		panic("invalid trusted schema policy")
	}
	return p
}
func cell(row []*string, i int) string {
	if i >= len(row) || row[i] == nil {
		return ""
	}
	return *row[i]
}
func rowKey(row []*string) string { b, _ := json.Marshal(row); return string(b) }
func ordered(rows [][]*string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowKey(r))
	}
	sort.Strings(out)
	return out
}
func equalRows(a, b [][]*string) bool {
	aa, bb := ordered(a), ordered(b)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}
func identity(kind, table, name string) string { return kind + ":" + table + ":" + name }

// Preflight refuses unknown or changed objects and incomplete existing table
// definitions. Missing whole tables/additive objects become trusted actions in
// the original reviewed order. It does no SQL and cannot prove rows are empty;
// the separate offline initializer must check that before executing any DDL.
func Preflight(current Snapshot) ([]Action, error) {
	p := Trusted()
	if len(p.Metadata) == 0 {
		return nil, ErrUnverified
	}
	expectedCounts := map[string]int{"tables": 4, "columns": 10, "indexes": 9, "checks": 3, "foreign_keys": 9, "triggers": 7}
	if len(current) != len(expectedCounts) || len(p.Metadata) != len(expectedCounts) {
		return nil, errors.New("incomplete schema metadata")
	}
	actual := map[string]map[string]bool{}
	presentTables := map[string]bool{}
	for section, width := range expectedCounts {
		expected, ok := p.Metadata[section]
		if !ok {
			return nil, ErrUnverified
		}
		allowed := map[string]bool{}
		for _, r := range expected {
			if len(r) != width {
				return nil, ErrUnverified
			}
			allowed[rowKey(r)] = true
		}
		actual[section] = map[string]bool{}
		for _, r := range current[section] {
			key := rowKey(r)
			if len(r) != width || !allowed[key] || actual[section][key] {
				return nil, errors.New("incompatible schema metadata")
			}
			actual[section][key] = true
			if section == "tables" {
				presentTables[cell(r, 0)] = true
			}
		}
	}
	addableIndexes := map[string]bool{}
	for _, a := range p.Actions {
		if a.Kind == "index" || a.Kind == "foreign_key" {
			addableIndexes[a.Table+":"+a.Name] = true
		}
	}
	// Column/default/generated/check definitions are created atomically with a
	// table. Their omission in an existing table cannot be repaired by CREATE.
	for _, section := range []string{"columns", "checks", "indexes"} {
		for _, r := range p.Metadata[section] {
			if !presentTables[cell(r, 0)] || actual[section][rowKey(r)] {
				continue
			}
			if section != "indexes" || cell(r, 1) == "PRIMARY" || !addableIndexes[cell(r, 0)+":"+cell(r, 1)] {
				return nil, errors.New("incomplete existing table definition")
			}
		}
	}
	objects := map[string]bool{}
	for name := range presentTables {
		objects[identity("table", name, name)] = true
	}
	for _, spec := range []struct{ section, kind string }{{"indexes", "index"}, {"foreign_keys", "foreign_key"}, {"triggers", "trigger"}} {
		for _, r := range current[spec.section] {
			objects[identity(spec.kind, cell(r, 0), cell(r, 1))] = true
		}
	}
	actions := []Action{}
	for _, a := range p.Actions {
		if !objects[identity(a.Kind, a.Table, a.Name)] {
			actions = append(actions, a)
		}
	}
	if len(actions) == 0 {
		for section, expected := range p.Metadata {
			if !equalRows(current[section], expected) {
				return nil, errors.New("schema is missing required objects")
			}
		}
	}
	return actions, nil
}

// CheckFull is stricter than preflight and accepts only a complete baseline.
func CheckFull(current Snapshot) error {
	actions, e := Preflight(current)
	if e != nil {
		return e
	}
	if len(actions) != 0 {
		return errors.New("schema is incomplete")
	}
	return nil
}
func CanonicalJSON(snapshot Snapshot) []byte {
	// Canonical row ordering makes CI captures reproducible across query plans.
	owned := Snapshot{}
	for section, rows := range snapshot {
		keys := ordered(rows)
		for _, key := range keys {
			var r []*string
			if json.Unmarshal([]byte(key), &r) != nil {
				panic("invalid schema row")
			}
			owned[section] = append(owned[section], r)
		}
		if len(rows) == 0 {
			owned[section] = [][]*string{}
		}
	}
	data, _ := json.Marshal(owned)
	return bytes.Clone(data)
}
