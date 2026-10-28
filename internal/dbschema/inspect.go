package dbschema

import (
	"context"
	"database/sql"
	"errors"
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
	"foreign_keys": `SELECT k.TABLE_NAME,k.CONSTRAINT_NAME,k.COLUMN_NAME,k.ORDINAL_POSITION,k.POSITION_IN_UNIQUE_CONSTRAINT,k.REFERENCED_TABLE_NAME,k.REFERENCED_COLUMN_NAME,r.UPDATE_RULE,r.DELETE_RULE FROM information_schema.KEY_COLUMN_USAGE k JOIN information_schema.REFERENTIAL_CONSTRAINTS r ON r.CONSTRAINT_SCHEMA=k.CONSTRAINT_SCHEMA AND r.CONSTRAINT_NAME=k.CONSTRAINT_NAME AND r.TABLE_NAME=k.TABLE_NAME WHERE k.CONSTRAINT_SCHEMA=DATABASE()`,
	"triggers":     `SELECT EVENT_OBJECT_TABLE,TRIGGER_NAME,EVENT_MANIPULATION,ACTION_TIMING,ACTION_ORIENTATION,ACTION_STATEMENT,SQL_MODE FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()`,
}

func Inspect(ctx context.Context, reader Reader) (Snapshot, error) {
	if reader == nil {
		return nil, errors.New("schema reader unavailable")
	}
	out := Snapshot{}
	cells, bytes := 0, 0
	for _, section := range []string{"tables", "columns", "indexes", "checks", "foreign_keys", "triggers"} {
		rows, e := reader.QueryContext(ctx, queries[section])
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
	return out, nil
}
