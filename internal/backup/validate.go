// Package backup implements the portable v39 data contract. SQL snapshots,
// HTTP download and transactional restore are separate migration gates.
package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"itmap/internal/v39"
)

const Format = "moscow-it-employers-backup"
const Version = 39
const MaxBytes = 25 * 1024 * 1024
const MaxRows = 200000
const MaxTables = 52
const maxSafeInteger = 9007199254740991

type InputError struct {
	Status  int
	Message string
}

func (e *InputError) Error() string { return e.Message }
func invalid(message string) error  { return &InputError{Status: 400, Message: message} }
func ErrorStatus(err error) int {
	if err == nil {
		return 200
	}
	var input *InputError
	if errors.As(err, &input) {
		return input.Status
	}
	return 503
}

// Scalar JSON is retained verbatim after JS canonicalization, including lone
// UTF-16 units. A Go string roundtrip would silently replace those units.
type Table struct {
	Name    string                       `json:"name"`
	Columns []json.RawMessage            `json:"columns"`
	Rows    []map[string]json.RawMessage `json:"rows"`
}
type ManifestEntry struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    int      `json:"rows"`
}
type Sequence struct {
	Name string  `json:"name"`
	Seq  float64 `json:"seq"`
}
type Payload struct {
	Format    string          `json:"format"`
	Version   float64         `json:"version"`
	CreatedAt json.RawMessage `json:"createdAt"`
	Catalog   json.RawMessage `json:"catalog"`
	Storage   struct {
		Tables []Table `json:"tables"`
	} `json:"storage"`
	Manifest json.RawMessage `json:"manifest"`
	Metadata struct {
		Schema        []json.RawMessage `json:"schema"`
		Sequences     []Sequence        `json:"sequences"`
		Migrations    []json.RawMessage `json:"migrations"`
		MissingTables json.RawMessage   `json:"missingTables"`
	} `json:"metadata"`
	Integrity struct {
		Algorithm string `json:"algorithm"`
		Digest    string `json:"digest"`
	} `json:"integrity"`
}

func columnNames(table Table) ([]string, error) {
	if table.Columns == nil {
		return nil, invalid("Некорректные таблицы бэкапа.")
	}
	names := make([]string, 0, len(table.Columns))
	for _, raw := range table.Columns {
		column, err := orderedJSON(raw)
		if err != nil || column.field("name") == nil || column.field("name").kind != 's' {
			return nil, invalid("Некорректная структура раздела.")
		}
		names = append(names, stringFromUnits(column.field("name").text))
	}
	return names, nil
}
func checkColumns(table Table, definition v39.Table) ([]string, error) {
	names, err := columnNames(table)
	if err != nil {
		return nil, err
	}
	if len(names) != len(definition.Columns) {
		return nil, invalid("Неполная или несовместимая структура раздела.")
	}
	allowed := map[string]bool{}
	for _, c := range definition.Columns {
		allowed[c.Name] = true
	}
	for _, name := range names {
		if !allowed[name] {
			return nil, invalid("Неполная или несовместимая структура раздела.")
		}
		delete(allowed, name)
	}
	return names, nil
}
func validCatalog(v *jsonValue) bool {
	companies := v.field("companies")
	if companies == nil || companies.kind != '[' {
		return false
	}
	for _, company := range companies.items {
		name := company.field("name")
		if name == nil || name.kind != 's' || trimName(stringFromUnits(name.text)) == "" {
			return false
		}
	}
	return true
}

// ValidateV39 handles the current export layout only. Historical v14–38 import
// transformations, destination planning and restore are not accepted here.
// Uploaded metadata.schema is never executed or used as trusted structure.
func ValidateV39(data []byte) (*Payload, error) {
	if len(data) > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Файл бэкапа больше допустимых 25 МБ."}
	}
	root, err := orderedJSON(data)
	if err != nil {
		return nil, err
	}
	// Read exact property names. encoding/json struct matching is case-insensitive
	// and would let an unrelated "Format" or "Name" override the real field.
	if textField(root, "format") != Format || root.field("version") == nil || root.field("version").kind != 'f' || root.field("version").number != Version {
		return nil, invalid("Поддерживается формат бэкапа v39.")
	}
	integrity := root.field("integrity")
	if textField(integrity, "algorithm") != "SHA-256" || textField(integrity, "digest") != digest(root) {
		return nil, invalid("Контрольная сумма не совпадает. Файл изменён или повреждён.")
	}
	p := Payload{Format: Format, Version: Version, CreatedAt: rawValue(root.field("createdAt")), Catalog: rawValue(root.field("catalog")), Manifest: rawValue(root.field("manifest"))}
	p.Integrity.Algorithm, p.Integrity.Digest = "SHA-256", textField(integrity, "digest")
	tableNodes := root.field("storage").field("tables")
	if tableNodes == nil || tableNodes.kind != '[' {
		return nil, invalid("Некорректные таблицы бэкапа.")
	}
	for _, node := range tableNodes.items {
		t := Table{Name: textField(node, "name")}
		columns, rows := node.field("columns"), node.field("rows")
		if columns == nil || columns.kind != '[' || rows == nil || rows.kind != '[' {
			return nil, invalid("Некорректные таблицы бэкапа.")
		}
		t.Columns = make([]json.RawMessage, 0, len(columns.items))
		for _, c := range columns.items {
			t.Columns = append(t.Columns, rawValue(c))
		}
		t.Rows = make([]map[string]json.RawMessage, 0, len(rows.items))
		for _, r := range rows.items {
			if r.kind != '{' {
				return nil, invalid("Неполная или некорректная запись в разделе.")
			}
			fields := map[string]json.RawMessage{}
			for _, m := range r.members {
				v := m.value
				if v.kind != 'n' && v.kind != 's' && (v.kind != 'f' || math.IsInf(v.number, 0) || math.IsNaN(v.number)) {
					return nil, invalid("Неполная или некорректная запись в разделе.")
				}
				fields[stringFromUnits(m.key)] = rawValue(v)
			}
			t.Rows = append(t.Rows, fields)
		}
		p.Storage.Tables = append(p.Storage.Tables, t)
	}
	metadata := root.field("metadata")
	for name, target := range map[string]*[]json.RawMessage{"schema": &p.Metadata.Schema, "migrations": &p.Metadata.Migrations} {
		a := metadata.field(name)
		if a == nil || a.kind != '[' {
			return nil, invalid("Отсутствуют метаданные бэкапа.")
		}
		*target = make([]json.RawMessage, 0, len(a.items))
		for _, v := range a.items {
			*target = append(*target, rawValue(v))
		}
	}
	p.Metadata.MissingTables = rawValue(metadata.field("missingTables"))
	seq := metadata.field("sequences")
	if seq == nil || seq.kind != '[' {
		return nil, invalid("Отсутствуют метаданные бэкапа.")
	}
	p.Metadata.Sequences = make([]Sequence, 0, len(seq.items))
	for _, v := range seq.items {
		n := v.field("seq")
		if n == nil || n.kind != 'f' || math.IsInf(n.number, 0) || math.IsNaN(n.number) {
			return nil, invalid("Некорректные счётчики идентификаторов.")
		}
		p.Metadata.Sequences = append(p.Metadata.Sequences, Sequence{textField(v, "name"), n.number})
	}
	trusted := v39.Trusted()
	definitions := map[string]v39.Table{}
	for _, d := range trusted.Tables {
		definitions[d.Name] = d
	}
	if len(p.Storage.Tables) != MaxTables {
		return nil, invalid("В бэкапе отсутствуют таблицы или присутствуют неизвестные разделы.")
	}
	byName := map[string]Table{}
	manifest := make([]ManifestEntry, 0, MaxTables)
	total := 0
	for _, t := range p.Storage.Tables {
		d, known := definitions[t.Name]
		if _, duplicate := byName[t.Name]; !known || duplicate {
			return nil, invalid("В бэкапе отсутствуют таблицы или присутствуют неизвестные разделы.")
		}
		names, e := checkColumns(t, d)
		if e != nil {
			return nil, e
		}
		if t.Rows == nil {
			return nil, invalid("Некорректные таблицы бэкапа.")
		}
		total += len(t.Rows)
		if total > MaxRows {
			return nil, invalid("В бэкапе слишком много записей.")
		}
		for _, r := range t.Rows {
			if r == nil || len(r) != len(names) {
				return nil, invalid("Неполная или некорректная запись в разделе.")
			}
			for _, name := range names {
				_, ok := r[name]
				if !ok {
					return nil, invalid("Неполная или некорректная запись в разделе.")
				}
			}
		}
		manifest = append(manifest, ManifestEntry{t.Name, names, len(t.Rows)})
		byName[t.Name] = t
	}
	expected, _ := json.Marshal(manifest)
	expectedNode, _ := orderedJSON(expected)
	actualManifest := root.field("manifest")
	if actualManifest == nil || !bytes.Equal(expectedNode.stringify(nil, false), actualManifest.stringify(nil, false)) {
		return nil, invalid("Манифест не соответствует содержимому бэкапа.")
	}
	if p.Metadata.Schema == nil || p.Metadata.Sequences == nil || p.Metadata.Migrations == nil {
		return nil, invalid("Отсутствуют метаданные бэкапа.")
	}
	auto := map[string]bool{}
	for _, o := range trusted.Objects {
		if o.Type == "table" && strings.Contains(strings.ToUpper(o.SQL), "AUTOINCREMENT") {
			auto[o.Name] = true
		}
	}
	seen := map[string]bool{}
	for _, s := range p.Metadata.Sequences {
		if !auto[s.Name] || seen[s.Name] || s.Seq < 0 || s.Seq > maxSafeInteger || s.Seq != math.Trunc(s.Seq) {
			return nil, invalid("Некорректные счётчики идентификаторов.")
		}
		seen[s.Name] = true
	}
	for _, d := range trusted.Tables {
		for _, fk := range d.ForeignKeys {
			parents := map[string]bool{}
			for _, r := range byName[fk.Table].Rows {
				parents[string(r[fk.To])] = true
			}
			for _, r := range byName[d.Name].Rows {
				v := r[fk.From]
				if string(v) != "null" && !parents[string(v)] {
					return nil, invalid("Нарушена связь разделов бэкапа.")
				}
			}
		}
	}
	catalog := root.field("catalog")
	if !validCatalog(catalog) {
		return nil, invalid("Каталог не соответствует исходным данным.")
	}
	for _, r := range byName["app_catalog"].Rows {
		if string(r["id"]) != "1" {
			continue
		}
		cell, e := orderedJSON(r["data_json"])
		if e != nil || cell.kind != 's' {
			return nil, invalid("Каталог не соответствует исходным данным.")
		}
		text, e := jsonSourceText(cell.text)
		if e != nil {
			return nil, e
		}
		raw, e := orderedJSON(text)
		if e != nil || !bytes.Equal(raw.stringify(nil, false), catalog.stringify(nil, false)) {
			return nil, invalid("Каталог не соответствует исходным данным.")
		}
		break
	}
	return &p, nil
}
