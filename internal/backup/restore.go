package backup

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf16"

	"itmap/internal/v39"
)

// RestoreResult is returned only after the data and generation commit together.
// Operational rows are never imported. Existing FK cascades remain enabled.
type RestoreResult struct {
	Generation string
	Rows       int
	Warnings   []string
	Catalog    json.RawMessage
}

// restoreScalar forbids implicit MariaDB conversion or UTF16 replacement before
// any counter DDL. SQLite's permissive affinity is not losslessly representable
// in the prepared MariaDB schema for these values.
func restoreScalar(raw json.RawMessage, c v39.Column) (any, error) {
	v, e := orderedJSON(raw)
	if e != nil {
		return nil, e
	}
	if v.kind == 'n' {
		if c.NotNull || c.PrimaryKey {
			return nil, invalid("NULL в обязательном поле бэкапа.")
		}
		return nil, nil
	}
	switch c.Type {
	case "TEXT":
		if v.kind != 's' {
			break
		}
		for i := 0; i < len(v.text); i++ {
			u := v.text[i]
			if u >= 0xd800 && u <= 0xdbff {
				if i+1 >= len(v.text) || v.text[i+1] < 0xdc00 || v.text[i+1] > 0xdfff {
					return nil, invalid("Текст бэкапа нельзя сохранить без потери UTF-16.")
				}
				i++
			} else if u >= 0xdc00 && u <= 0xdfff {
				return nil, invalid("Текст бэкапа нельзя сохранить без потери UTF-16.")
			}
		}
		return string(utf16.Decode(v.text)), nil
	case "INTEGER":
		if v.kind == 'f' && math.Abs(v.number) <= maxSafeInteger && math.Trunc(v.number) == v.number {
			return int64(v.number), nil
		}
	case "REAL":
		if v.kind == 'f' && !math.IsNaN(v.number) && !math.IsInf(v.number, 0) {
			return v.number, nil
		}
	}
	return nil, invalid("Значение бэкапа несовместимо с типом MariaDB.")
}
func validateRestoreScalars(plan *RestorePlan) error {
	definitions := map[string]v39.Table{}
	for _, t := range v39.Trusted().Tables {
		definitions[t.Name] = t
	}
	for _, t := range plan.Tables {
		for _, row := range t.Rows {
			for _, c := range definitions[t.Name].Columns {
				if _, e := restoreScalar(row[c.Name], c); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func restoreColumns(t v39.Table) []string {
	names := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		names[i] = c.Name
	}
	return names
}
func quotedColumns(names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quote(n)
	}
	return strings.Join(out, ",")
}
func completeRestoreInventory(ctx context.Context, reader metadataReader) (map[string]sqlTable, error) {
	actual, e := inventory(ctx, reader)
	if e != nil {
		return nil, e
	}
	trusted := v39.Trusted()
	if e = validateInventory(actual, trusted); e != nil {
		return nil, e
	}
	for _, t := range append(trusted.Tables, trusted.OperationalTables...) {
		if _, ok := actual[t.Name]; !ok {
			return nil, invalid("Сначала подготовьте полную доверенную схему базы.")
		}
	}
	return actual, nil
}
func insertRestoreTable(ctx context.Context, tx *sql.Tx, definition v39.Table, t Table) error {
	if len(t.Rows) == 0 {
		return nil
	}
	query := "INSERT INTO " + quote(t.Name) + " (" + quotedColumns(restoreColumns(definition)) + ") VALUES (" + strings.TrimSuffix(strings.Repeat("?,", len(definition.Columns)), ",") + ")"
	stmt, e := tx.PrepareContext(ctx, query)
	if e != nil {
		return e
	}
	defer stmt.Close()
	for _, row := range t.Rows {
		args := make([]any, len(definition.Columns))
		for i, c := range definition.Columns {
			args[i], e = restoreScalar(row[c.Name], c)
			if e != nil {
				return e
			}
		}
		if _, e = stmt.ExecContext(ctx, args...); e != nil {
			return e
		}
	}
	return nil
}
func restoreRowKey(row map[string]json.RawMessage, definition v39.Table) (string, error) {
	// Canonical scalar arrays retain every value and multiplicity. No collation,
	// hash collision, row order or map iteration can conceal changed data.
	out := []byte{'['}
	for i, c := range definition.Columns {
		v, e := orderedJSON(row[c.Name])
		if e != nil {
			return "", e
		}
		if i > 0 {
			out = append(out, ',')
		}
		out = v.stringify(out, false)
	}
	return string(append(out, ']')), nil
}
func verifyRestoreValues(ctx context.Context, tx *sql.Tx, actual map[string]sqlTable, plan *RestorePlan) error {
	defs := map[string]v39.Table{}
	for _, t := range v39.Trusted().Tables {
		defs[t.Name] = t
	}
	budget := &exportBudget{}
	for _, t := range plan.Tables {
		d := defs[t.Name]
		counts := map[string]int{}
		for _, r := range t.Rows {
			k, e := restoreRowKey(r, d)
			if e != nil {
				return e
			}
			counts[k]++
		}
		rows, _, e := readRows(ctx, tx, actual[t.Name], restoreColumns(d), budget, true)
		if e != nil {
			return e
		}
		if len(rows) != len(t.Rows) {
			return invalid("Проверка восстановленных записей не пройдена.")
		}
		for _, r := range rows {
			var row map[string]json.RawMessage
			if e = json.Unmarshal(r, &row); e != nil {
				return e
			}
			k, e := restoreRowKey(row, d)
			if e != nil {
				return e
			}
			if counts[k] == 0 {
				return invalid("Значения изменились при восстановлении.")
			}
			counts[k]--
		}
	}
	return nil
}
func verifyRestoreForeignKeys(ctx context.Context, tx *sql.Tx) error {
	for _, t := range v39.Trusted().Tables {
		groups := map[int][]v39.ForeignKey{}
		for _, fk := range t.ForeignKeys {
			groups[fk.ID] = append(groups[fk.ID], fk)
		}
		for _, group := range groups {
			required, matching := []string{}, []string{}
			for _, fk := range group {
				required = append(required, "c."+quote(fk.From)+" IS NOT NULL")
				matching = append(matching, "p."+quote(fk.To)+"=c."+quote(fk.From))
			}
			var invalidRows int
			q := "SELECT EXISTS(SELECT 1 FROM " + quote(t.Name) + " c WHERE " + strings.Join(required, " AND ") + " AND NOT EXISTS(SELECT 1 FROM " + quote(group[0].Table) + " p WHERE " + strings.Join(matching, " AND ") + "))"
			if e := tx.QueryRowContext(ctx, q).Scan(&invalidRows); e != nil {
				return e
			}
			if invalidRows != 0 {
				return invalid("Нарушены связи восстановленных записей.")
			}
		}
	}
	return nil
}

// RestoreV39 replaces all application data in one transaction on an already
// prepared schema. Counter DDL is completed before BeginTx; only safe high-water
// gaps can remain after rollback. Uploaded metadata SQL is never executed.
func (r Repository) RestoreV39(ctx context.Context, data []byte) (RestoreResult, error) {
	// Bound lock waits and row work even for callers with no deadline.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	session, e := r.OpenV39RestoreSession(ctx, data)
	if e != nil {
		return RestoreResult{}, e
	}
	defer session.Close()
	tx, e := session.conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return RestoreResult{}, e
	}
	defer tx.Rollback()
	// Same first lock as receipt-backed application creation: old contexts cannot
	// cross a restore generation boundary while retaining the generation lock.
	var oldGeneration string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&oldGeneration); e != nil {
		return RestoreResult{}, e
	}
	var foreignKeys, checks int
	if e = tx.QueryRowContext(ctx, "SELECT @@foreign_key_checks,@@check_constraint_checks").Scan(&foreignKeys, &checks); e != nil {
		return RestoreResult{}, e
	}
	if foreignKeys != 1 || checks != 1 {
		return RestoreResult{}, errors.New("restore requires FK and CHECK enforcement")
	}
	// Pin table metadata until COMMIT, then inspect again before destructive SQL.
	for _, t := range v39.Trusted().Tables {
		rows, err := tx.QueryContext(ctx, "SELECT "+quotedColumns(restoreColumns(t))+" FROM "+quote(t.Name)+" WHERE 1=0")
		if err != nil {
			return RestoreResult{}, err
		}
		rows.Close()
	}
	actual, e := completeRestoreInventory(ctx, tx)
	if e != nil {
		return RestoreResult{}, e
	}
	plan := session.plan
	for _, name := range plan.DeleteOrder {
		if name == "vacancy_roles" {
			if _, e = tx.ExecContext(ctx, "UPDATE `vacancy_roles` SET `parent_id`=NULL"); e != nil {
				return RestoreResult{}, e
			}
		}
		if _, e = tx.ExecContext(ctx, "DELETE FROM "+quote(name)); e != nil {
			return RestoreResult{}, e
		}
	}
	defs := map[string]v39.Table{}
	for _, t := range v39.Trusted().Tables {
		defs[t.Name] = t
	}
	tables := map[string]Table{}
	for _, t := range plan.Tables {
		tables[t.Name] = t
	}
	for _, name := range plan.InsertOrder {
		if e = insertRestoreTable(ctx, tx, defs[name], tables[name]); e != nil {
			return RestoreResult{}, e
		}
	}
	if e = verifyRestoreValues(ctx, tx, actual, plan); e != nil {
		return RestoreResult{}, e
	}
	if e = verifyRestoreForeignKeys(ctx, tx); e != nil {
		return RestoreResult{}, e
	}
	var uuid [16]byte
	if _, e = rand.Read(uuid[:]); e != nil {
		return RestoreResult{}, e
	}
	uuid[6] = (uuid[6] & 15) | 64
	uuid[8] = (uuid[8] & 63) | 128
	generation := fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	result, e := tx.ExecContext(ctx, "UPDATE app_runtime_generation SET generation=?,updated_at=? WHERE id=1 AND generation=?", generation, time.Now().UTC().Format(time.RFC3339Nano), oldGeneration)
	if e != nil {
		return RestoreResult{}, e
	}
	changed, e := result.RowsAffected()
	if e != nil {
		return RestoreResult{}, e
	}
	if changed != 1 {
		return RestoreResult{}, errors.New("restore generation conflict")
	}
	if e = tx.Commit(); e != nil {
		return RestoreResult{}, e
	}
	return RestoreResult{Generation: generation, Rows: plan.Rows, Warnings: plan.Warnings, Catalog: plan.Catalog}, nil
}
