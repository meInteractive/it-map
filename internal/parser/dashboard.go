package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"itmap/internal/backup"
)

var ErrDashboardStorage = errors.New("scheduler dashboard unavailable")

// DashboardRepository читает только согласованный снимок. Now задаёт время
// в тестах; HTTP-клиент не может выбрать время, поколение или пользователя.
type DashboardRepository struct {
	DB  *sql.DB
	Now func() time.Time
}

func dashboardRows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]map[string]any, error) {
	rows, e := tx.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	columns, e := rows.Columns()
	if e != nil {
		return nil, e
	}
	out := []map[string]any{}
	for rows.Next() {
		values, dest := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if e = rows.Scan(dest...); e != nil {
			return nil, e
		}
		row := map[string]any{}
		for i, name := range columns {
			value := values[i]
			if b, ok := value.([]byte); ok {
				switch name {
				case "total", "sources", "found", "accepted", "imported", "usd", "unknown", "credits", "at":
					value, e = strconv.ParseFloat(string(b), 64)
					if e != nil {
						return nil, e
					}
				default:
					value = string(b)
				}
			}
			row[name] = value
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
func dashboardNumber(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func (r DashboardRepository) ReadDashboard(ctx context.Context) (body json.RawMessage, failure error) {
	defer func() {
		if failure != nil {
			if ctx.Err() != nil {
				failure = ctx.Err()
			} else {
				failure = ErrDashboardStorage
			}
		}
	}()
	if r.DB == nil {
		return nil, ErrDashboardStorage
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var generation string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || !generationPattern.MatchString(generation) {
		return nil, ErrDashboardStorage
	}
	stored, e := readSettingsRow(ctx, tx, false)
	if e != nil {
		return nil, e
	}
	settings, e := settingsView(ctx, tx, stored)
	if e != nil {
		return nil, e
	}
	countsRows, e := dashboardRows(ctx, tx, "SELECT status,count(*) total FROM scheduler_jobs WHERE generation=? GROUP BY status ORDER BY status", generation)
	if e != nil {
		return nil, e
	}
	counts := map[string]any{}
	for _, row := range countsRows {
		status, ok := row["status"].(string)
		if !ok {
			return nil, ErrDashboardStorage
		}
		counts[status] = row["total"]
	}
	runs, e := dashboardRows(ctx, tx, "SELECT id,generation,`trigger`,started_at,finished_at,status,ready,processed,succeeded,errors,remaining,reason,cursor_id,lease_until FROM scheduler_runs WHERE generation=? ORDER BY started_at DESC LIMIT 20", generation)
	if e != nil {
		return nil, e
	}
	attemptRows, e := dashboardRows(ctx, tx, "SELECT a.id,a.job_id,a.run_id,a.type,a.company_id,j.agency_id,a.source_id,j.candidate_id,a.started_at,a.finished_at,a.status,a.reason_code,a.message,a.provider,a.model,a.reserved_usd,a.cost_usd,a.credits,substr(a.result_json,1,2048) result_json FROM scheduler_attempts a LEFT JOIN scheduler_jobs j ON j.id=a.job_id ORDER BY a.started_at DESC LIMIT 50")
	if e != nil {
		return nil, e
	}
	attempts := []json.RawMessage{}
	for _, row := range attemptRows {
		raw, e := json.Marshal(row)
		if e != nil {
			return nil, e
		}
		projected, e := backup.PrepareSchedulerAttemptJSON(raw)
		if e != nil {
			return nil, e
		}
		attempts = append(attempts, projected)
	}
	signals, e := dashboardRows(ctx, tx, "SELECT started_at FROM scheduler_runs WHERE generation=? AND `trigger`='automatic' ORDER BY started_at DESC LIMIT 1", generation)
	if e != nil {
		return nil, e
	}
	// Счётчики сохраняют первое совпадение JSON1 и строгое сравнение строк.
	statistics, e := dashboardRows(ctx, tx, `SELECT count(CASE WHEN type='scan_source' AND status='completed' THEN 1 END) sources,0 found,count(CASE WHEN type='classify_title' AND (JSON_TYPE(JSON_EXTRACT(result_json,'$.accepted'))='BOOLEAN' AND JSON_EXTRACT(result_json,'$.accepted')='true' OR JSON_TYPE(JSON_EXTRACT(result_json,'$.accepted')) IN ('INTEGER','DOUBLE') AND JSON_EXTRACT(result_json,'$.accepted')=1) THEN 1 END) accepted,count(CASE WHEN type='import_vacancy' AND status='completed' AND JSON_TYPE(JSON_EXTRACT(result_json,'$.action'))='STRING' AND BINARY JSON_UNQUOTE(JSON_EXTRACT(result_json,'$.action'))=BINARY 'create' THEN 1 END) imported FROM scheduler_attempts`)
	if e != nil || len(statistics) != 1 {
		return nil, ErrDashboardStorage
	}
	// SUM над JSON нельзя заменить SUM текстового значения MariaDB: различаются
	// целое переполнение, вещественная аффинность и компенсированное округление SQLite.
	foundRows, e := tx.QueryContext(ctx, "SELECT CASE WHEN type='scan_source' AND status='completed' THEN JSON_EXTRACT(result_json,'$.found') ELSE 0 END FROM scheduler_attempts")
	if e != nil {
		return nil, e
	}
	var found dashboardSum
	for foundRows.Next() {
		var raw []byte
		if e = foundRows.Scan(&raw); e == nil {
			e = found.addJSON(raw)
		}
		if e != nil {
			break
		}
	}
	if e == nil {
		e = foundRows.Err()
	}
	foundRows.Close()
	if e != nil {
		return nil, e
	}
	statistics[0]["found"], e = found.coalescedJSON()
	if e != nil {
		return nil, e
	}
	last, e := dashboardRows(ctx, tx, "SELECT max(finished_at) at FROM scheduler_runs WHERE generation=? AND status='completed' AND succeeded>0 AND `trigger`<>'probe'", generation)
	if e != nil || len(last) != 1 {
		return nil, ErrDashboardStorage
	}
	var lastSuccess any
	if dashboardNumber(last[0]["at"]) != 0 {
		lastSuccess = last[0]["at"]
	}
	spending, e := readDashboardSpending(ctx, tx, SchedulerPeriod(now))
	if e != nil {
		return nil, e
	}
	var public map[string]json.RawMessage
	if json.Unmarshal(settings.PublicJSON(), &public) != nil {
		return nil, ErrDashboardStorage
	}
	cloud := "owner_binding_required"
	if string(public["cloudConnected"]) == "true" {
		cloud = "ready_to_connect"
	}
	var signalAt, nextSignal any
	if len(signals) != 0 {
		at := dashboardNumber(signals[0]["started_at"])
		if at != 0 {
			signalAt = signals[0]["started_at"]
		}
		if float64(now.UnixMilli())-at < 75*60000 {
			cloud = "signal_received"
			nextSignal = math.Ceil(float64(now.UnixMilli())/3600000) * 3600000
		} else {
			cloud = "signal_overdue"
		}
	}
	value := map[string]any{"apifyPolicy": map[string]any{"priorityPenalty": 50, "maxStagesPerCycle": 1, "maxHhStagesPerCycle": 5, "pollDelayMs": 60000}, "sourceOptions": SourceOptions(), "settings": settings.PublicJSON(), "counts": counts, "runs": runs, "attempts": attempts, "statistics": statistics[0], "spending": spending, "lastSuccessAt": lastSuccess, "lastSignalAt": signalAt, "nextSignalAt": nextSignal, "cloudStatus": cloud}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	body, e = backup.StringifyParsedJSON(raw)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return body, nil
}
