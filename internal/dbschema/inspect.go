package dbschema

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Reader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Each metadata row starts with table and object identity. Counters, estimates,
// usernames/definers and business contents are intentionally outside the query.
var queries = map[string]string{
	"tables":       `SELECT TABLE_NAME,ENGINE,TABLE_COLLATION,ROW_FORMAT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()`,
	"columns":      `SELECT TABLE_NAME,COLUMN_NAME,ORDINAL_POSITION,COLUMN_TYPE,IS_NULLABLE,COLUMN_DEFAULT,EXTRA,GENERATION_EXPRESSION,CHARACTER_SET_NAME,COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE()`,
	"indexes":      `SELECT TABLE_NAME,INDEX_NAME,NON_UNIQUE,SEQ_IN_INDEX,COLUMN_NAME,COLLATION,SUB_PART,INDEX_TYPE,IGNORED FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE()`,
	"checks":       `SELECT TABLE_NAME,CONSTRAINT_NAME,CHECK_CLAUSE FROM information_schema.CHECK_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=DATABASE()`,
	"foreign_keys": `SELECT k.TABLE_NAME,k.CONSTRAINT_NAME,k.COLUMN_NAME,k.ORDINAL_POSITION,k.POSITION_IN_UNIQUE_CONSTRAINT,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.UPDATE_RULE,r.DELETE_RULE,CASE WHEN k.REFERENCED_TABLE_SCHEMA=DATABASE() THEN 'CURRENT' ELSE k.REFERENCED_TABLE_SCHEMA END FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME WHERE k.CONSTRAINT_SCHEMA=DATABASE()`,
	"triggers":     `SELECT EVENT_OBJECT_TABLE,TRIGGER_NAME,EVENT_MANIPULATION,ACTION_TIMING,ACTION_ORIENTATION,ACTION_STATEMENT,SQL_MODE FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()`,
}

func Inspect(ctx context.Context, reader Reader) (Snapshot, error) {
	if reader == nil {
		return nil, errors.New("schema reader unavailable")
	}
	versionRows, e := reader.QueryContext(ctx, "SELECT VERSION()")
	if e != nil {
		return nil, errors.New("cannot inspect schema server version")
	}
	var version string
	if !versionRows.Next() || versionRows.Scan(&version) != nil {
		versionRows.Close()
		return nil, errors.New("cannot read schema server version")
	}
	e = versionRows.Err()
	versionRows.Close()
	if e != nil || !supportedVersion(version) {
		return nil, errors.New("unsupported schema server version")
	}
	out := Snapshot{}
	cells, bytes := 0, 0
	for _, section := range []string{"tables", "columns", "indexes", "checks", "foreign_keys", "triggers"} {
		query := queries[section]
		if section == "indexes" && !mariaAtLeast(version, 10, 6, 0) {
			query = strings.Replace(query, ",IGNORED FROM", ",'NO' AS IGNORED FROM", 1)
		}
		rows, e := reader.QueryContext(ctx, query)
		if e != nil {
			return nil, errors.New("cannot inspect schema metadata")
		}
		columns, e := rows.Columns()
		if e != nil {
			rows.Close()
			return nil, e
		}
		out[section] = [][]*string{}
		for rows.Next() {
			values := make([]sql.NullString, len(columns))
			args := make([]any, len(columns))
			for i := range args {
				args[i] = &values[i]
			}
			if e = rows.Scan(args...); e != nil {
				rows.Close()
				return nil, errors.New("cannot scan schema metadata")
			}
			row := make([]*string, len(values))
			for i, v := range values {
				if v.Valid {
					s := v.String
					row[i] = &s
					bytes += len(s)
				}
			}
			cells += len(row)
			if cells > 50000 || bytes > 2<<20 {
				rows.Close()
				return nil, errors.New("schema metadata limit exceeded")
			}
			out[section] = append(out[section], row)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, errors.New("cannot read schema metadata")
		}
	}
	if !mariaAtLeast(version, 10, 8, 0) {
		normalizeLegacyIndexDirections(out)
	}
	return out, nil
}

// До 10.8 сервер принимает DESC, но хранит индекс в ASC. Приводим только
// известные DESC-позиции эталона; остальные свойства по-прежнему проверяет Preflight.
func normalizeLegacyIndexDirections(snapshot Snapshot) {
	expectedIndexes := Trusted().Metadata["indexes"]
	for _, row := range snapshot["indexes"] {
		if len(row) != 9 || cell(row, 5) != "A" {
			continue
		}
		for _, expected := range expectedIndexes {
			if cell(expected, 5) != "D" {
				continue
			}
			if cell(row, 0) == cell(expected, 0) && cell(row, 1) == cell(expected, 1) && cell(row, 3) == cell(expected, 3) && cell(row, 4) == cell(expected, 4) {
				direction := "D"
				row[5] = &direction
				break
			}
		}
	}
}
