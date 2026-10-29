package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"itmap/internal/backup"
	"itmap/internal/dbretry"
)

type ErrorHistoryRepository struct{ DB *sql.DB }
type ErrorHistoryCounts struct {
	P1 int64 `json:"p1"`
	P2 int64 `json:"p2"`
}

// ErrorHistoryOperation сохраняет только серверные owner/generation и не держит TX.
type ErrorHistoryOperation struct {
	db                *sql.DB
	owner, generation string
}

func (r ErrorHistoryRepository) PrepareHistory(ctx context.Context, owner string) (ErrorHistoryOperation, error) {
	op := ErrorHistoryOperation{db: r.DB, owner: owner}
	if r.DB == nil {
		return op, ErrSettingsStorage
	}
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&op.generation); e != nil || !generationPattern.MatchString(op.generation) {
		return ErrorHistoryOperation{}, settingsFailure(ctx, ErrSettingsStorage)
	}
	return op, nil
}
func historyDay(now int64) (string, error) {
	at := time.UnixMilli(now).UTC().Add(3 * time.Hour)
	if at.Year() < 0 || at.Year() > 9999 {
		return "", ErrSettingsStorage
	}
	return at.Format("2006-01-02"), nil
}

// recordSourceCount повторяет Number(D1 count) перед INTEGER affinity SQLite.
func recordSourceCount(n int64) (int64, error) {
	f := float64(n)
	if n < 0 || f >= math.Ldexp(1, 63) {
		return 0, ErrSettingsStorage
	}
	return int64(f), nil
}
func (op ErrorHistoryOperation) Record(ctx context.Context, counts ErrorHistoryCounts, now int64) error {
	if op.db == nil || !generationPattern.MatchString(op.generation) {
		return ErrSettingsStorage
	}
	day, e := historyDay(now)
	if e != nil {
		return e
	}

	_, e = dbretry.Run(ctx, func() (struct{}, error) {
		tx, e := op.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if e != nil {
			return struct{}{}, e
		}
		defer tx.Rollback()
		var generation string
		if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); e != nil {
			return struct{}{}, e
		}
		if generation != op.generation {
			return struct{}{}, ErrSettingsGeneration
		}
		p1, e := recordSourceCount(counts.P1)
		if e != nil {
			return struct{}{}, e
		}
		p2, e := recordSourceCount(counts.P2)
		if e != nil {
			return struct{}{}, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO parser_error_daily(owner_name,day,p1_peak,p2_peak,first_sample_at,last_sample_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE p1_peak=GREATEST(p1_peak,VALUES(p1_peak)),p2_peak=GREATEST(p2_peak,VALUES(p2_peak)),first_sample_at=LEAST(first_sample_at,VALUES(first_sample_at)),last_sample_at=GREATEST(last_sample_at,VALUES(last_sample_at))`, op.owner, day, p1, p2, now, now)
		if e != nil {
			return struct{}{}, e
		}
		return struct{}{}, tx.Commit()
	})
	return settingsFailure(ctx, e)
}
func (op ErrorHistoryOperation) Read(ctx context.Context, now int64) (body json.RawMessage, failure error) {
	defer func() { failure = settingsFailure(ctx, failure) }()
	if op.db == nil || !generationPattern.MatchString(op.generation) {
		return nil, ErrSettingsStorage
	}
	keys := make([]string, 30)
	for i := range keys {
		var e error
		keys[i], e = historyDay(now - int64(29-i)*86400000)
		if e != nil {
			return nil, e
		}
	}
	tx, e := op.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var generation string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil {
		return nil, e
	}
	if generation != op.generation {
		return nil, ErrSettingsGeneration
	}
	rows, e := dashboardRows(ctx, tx, "SELECT day,p1_peak,p2_peak,last_sample_at FROM parser_error_daily WHERE BINARY owner_name=BINARY ? AND BINARY day>=BINARY ? AND BINARY day<=BINARY ? ORDER BY BINARY day", op.owner, keys[0], keys[29])
	if e != nil {
		return nil, e
	}
	byDay := map[string]map[string]any{}
	p1, p2 := float64(0), float64(0)
	for _, r := range rows {
		day, ok := r["day"].(string)
		if !ok {
			return nil, ErrSettingsStorage
		}
		byDay[day] = r
		p1 += dashboardNumber(r["p1_peak"])
		p2 += dashboardNumber(r["p2_peak"])
	}
	average := map[string]any{"p1": nil, "p2": nil}
	if len(rows) > 0 {
		average["p1"] = p1 / float64(len(rows))
		average["p2"] = p2 / float64(len(rows))
	}
	points := []map[string]any{}
	for _, day := range keys {
		p := map[string]any{"day": day, "p1": nil, "p2": nil, "observedAt": nil}
		if r := byDay[day]; r != nil {
			p["p1"], p["p2"], p["observedAt"] = r["p1_peak"], r["p2_peak"], r["last_sample_at"]
		}
		points = append(points, p)
	}
	raw, e := json.Marshal(map[string]any{"timezone": "Europe/Moscow", "metric": "daily_peak_active", "days": 30, "observedDays": len(rows), "average": average, "points": points})
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
