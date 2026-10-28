package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
)

// Глобальные расходы не ограничиваются поколением: restore не обнуляет историю.
// Потоковый расчёт сохраняет округление SQLite SUM вместо SUM MariaDB.
func readDashboardSpending(ctx context.Context, tx *sql.Tx, period BudgetPeriod) (map[string]any, error) {
	rows, e := tx.QueryContext(ctx, "SELECT type,started_at,reserved_usd,cost_usd,credits FROM scheduler_attempts WHERE started_at>=? ORDER BY started_at,type", period.Month)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	type group struct {
		usd, credits dashboardSum
		unknown      int64
	}
	groups := map[string]*group{}
	var day dashboardSum
	for rows.Next() {
		var kind string
		var at int64
		var reserved float64
		var cost, credits sql.NullFloat64
		if e = rows.Scan(&kind, &at, &reserved, &cost, &credits); e != nil {
			return nil, e
		}
		g := groups[kind]
		if g == nil {
			g = &group{}
			groups[kind] = g
		}
		usd := reserved
		if cost.Valid {
			usd = cost.Float64
		} else if reserved > 0 {
			g.unknown++
		}
		g.usd.add(false, 0, usd)
		if at >= period.Day {
			day.add(false, 0, usd)
		}
		if credits.Valid {
			g.credits.add(false, 0, credits.Float64)
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	keys := []string{}
	for kind := range groups {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	byType := []map[string]any{}
	month := float64(0)
	for _, kind := range keys {
		g := groups[kind]
		value, e := g.usd.sqlValue()
		if e != nil {
			return nil, e
		}
		month += dashboardNumber(value)
		usd, e := g.usd.jsonValue(false)
		if e != nil {
			return nil, e
		}
		credits, e := g.credits.jsonValue(false)
		if e != nil {
			return nil, e
		}
		byType = append(byType, map[string]any{"type": kind, "usd": usd, "unknown": g.unknown, "credits": credits})
	}
	var monthly json.RawMessage
	if math.IsNaN(month) || math.IsInf(month, 0) {
		monthly = json.RawMessage("null")
	} else {
		monthly, e = json.Marshal(month)
		if e != nil {
			return nil, e
		}
	}
	daily, e := day.coalescedJSON()
	if e != nil {
		return nil, e
	}
	return map[string]any{"dayUsd": daily, "monthUsd": monthly, "byType": byType, "timezone": "Europe/Moscow"}, nil
}
