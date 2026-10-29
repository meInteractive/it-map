package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"

	"itmap/internal/backup"
)

type QueueRepository struct{ DB *sql.DB }

// PrepareQueue фиксирует поколение до query validation и fallback HTTP метода.
// До вызова возвращённой операции соединение и транзакция освобождены.
func (r QueueRepository) PrepareQueue(ctx context.Context) (func(context.Context, json.RawMessage) (json.RawMessage, error), error) {
	if r.DB == nil {
		return nil, ErrSettingsStorage
	}
	var expected string
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&expected); e != nil || !generationPattern.MatchString(expected) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, query json.RawMessage) (json.RawMessage, error) {
		return r.read(ctx, query, expected)
	}, nil
}
func (r QueueRepository) ReadQueue(ctx context.Context, query json.RawMessage) (json.RawMessage, error) {
	read, e := r.PrepareQueue(ctx)
	if e != nil {
		return nil, e
	}
	return read(ctx, query)
}

var queueDecimal = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// После source Number validation SQLite применяет affinity к исходной строке ID.
// Hex/binary и Unicode whitespace проходят Number, но не SQL INTEGER affinity.
func queueOwnerID(text string) (int64, bool) {
	text = strings.Trim(text, " \t\r\n\v\f")
	if !queueDecimal.MatchString(text) {
		return 0, false
	}
	text = strings.TrimPrefix(text, "+")
	n, e := sqliteDecimal(text)
	if e != nil || math.Trunc(n) != n || n < 1 || n > 9007199254740991 {
		return 0, false
	}
	return int64(n), true
}
func (r QueueRepository) read(ctx context.Context, raw json.RawMessage, expected string) (body json.RawMessage, failure error) {
	defer func() { failure = settingsFailure(ctx, failure) }()
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var generation string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil {
		return nil, e
	}
	if generation != expected {
		return nil, ErrSettingsGeneration
	}
	f, e := backup.PrepareSchedulerQueueFilters(raw)
	if e != nil {
		message := "Некорректные фильтры очереди."
		if errors.Is(e, backup.ErrSchedulerQueueKeys) {
			message = "Неизвестные параметры запроса."
		}
		return nil, &SettingsError{Message: message}
	}
	where := "j.generation=?"
	args := []any{generation}
	if f.Status != "" {
		where += " AND BINARY j.status=BINARY ?"
		args = append(args, f.Status)
	}
	if f.Type != "" {
		where += " AND BINARY j.type=BINARY ?"
		args = append(args, f.Type)
	}
	for _, owner := range []struct{ column, text string }{{"company_id", f.CompanyID}, {"agency_id", f.AgencyID}} {
		if owner.text != "" {
			id, ok := queueOwnerID(owner.text)
			if !ok {
				where += " AND 0=1"
			} else {
				where += " AND j." + owner.column + "=?"
				args = append(args, id)
			}
		}
	}
	if f.HasSearch() {
		owners, e := dashboardRows(ctx, tx, "SELECT DISTINCT 'company' AS kind,i.id,i.company_name AS name FROM scheduler_jobs j JOIN company_identity i ON i.id=j.company_id WHERE j.generation=? UNION ALL SELECT DISTINCT 'agency' AS kind,i.id,i.agency_name AS name FROM scheduler_jobs j JOIN agency_identity i ON i.id=j.agency_id WHERE j.generation=?", generation, generation)
		if e != nil {
			return nil, e
		}
		companies, agencies := []any{}, []any{}
		for _, owner := range owners {
			name, ok := owner["name"].(string)
			if !ok {
				return nil, ErrSettingsStorage
			}
			if !f.MatchOwner(name) {
				continue
			}
			switch owner["kind"] {
			case "company":
				companies = append(companies, owner["id"])
			case "agency":
				agencies = append(agencies, owner["id"])
			default:
				return nil, ErrSettingsStorage
			}
		}
		// D1 Number → JSON.stringify применяется до json_each, в том числе к большим ID.
		// DECIMAL сохраняет точное сравнение целых, а два JSON bindings не зависят от числа owners.
		lists := []string{}
		for _, ids := range [][]any{companies, agencies} {
			encoded, e := json.Marshal(ids)
			if e != nil {
				return nil, e
			}
			canonical, e := backup.StringifyParsedJSON(encoded)
			if e != nil {
				return nil, e
			}
			lists = append(lists, string(canonical))
		}
		where += " AND (j.company_id IN (SELECT q.owner_id FROM JSON_TABLE(?,'$[*]' COLUMNS(owner_id DECIMAL(65,0) PATH '$')) AS q) OR j.agency_id IN (SELECT q.owner_id FROM JSON_TABLE(?,'$[*]' COLUMNS(owner_id DECIMAL(65,0) PATH '$')) AS q))"
		args = append(args, lists[0], lists[1])
	}
	var total int64
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) AS total FROM scheduler_jobs j WHERE "+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	// Payload и невидимые служебные столбцы не читаются и не попадают в DTO.
	columns := "j.id,j.generation,j.dedupe_key,j.type,j.company_id,j.agency_id,j.candidate_id,j.source_id,j.status,j.priority,j.run_after,j.attempts,j.max_attempts,j.locked_by,j.locked_until,j.last_error,j.reason_code,j.created_at,j.updated_at,j.completed_at"
	query := "SELECT " + columns + ",CASE WHEN j.agency_id IS NOT NULL THEN (SELECT agency_name FROM agency_identity WHERE id=j.agency_id) ELSE (SELECT company_name FROM company_identity WHERE id=j.company_id) END AS companyName,(SELECT title_raw FROM vacancy_candidates WHERE id=j.candidate_id) AS candidateTitle FROM scheduler_jobs j WHERE " + where + " ORDER BY j.created_at DESC,BINARY j.id LIMIT 30 OFFSET ?"
	pageArgs := append(append([]any{}, args...), (f.Page-1)*30)
	jobs, e := dashboardRows(ctx, tx, query, pageArgs...)
	if e != nil {
		return nil, e
	}
	rawBody, e := json.Marshal(map[string]any{"jobs": jobs, "total": total, "page": f.Page, "pageSize": 30})
	if e != nil {
		return nil, e
	}
	body, e = backup.StringifyParsedJSON(rawBody)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return body, nil
}
