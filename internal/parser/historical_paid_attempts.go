package parser

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const maxHistoricalPaidAttempts = 1024

var ErrHistoricalAttemptsLimit = errors.New("historical scheduler evidence exceeds diagnostic limit")
var ErrHistoricalAttemptsIncompatible = errors.New("historical scheduler evidence incompatible")

// HistoricalPaidAttempts содержит только агрегаты существующих строк. Orphan
// означает отсутствие run и не позволяет определить generation этой попытки.
// Нулевые счётчики не доказывают полноту истории и не разрешают новую работу.
type HistoricalPaidAttempts struct {
	HistoricalUnknown      int
	HistoricalCostRecorded int
	OrphanUnknown          int
	OrphanCostRecorded     int
}

// InspectHistoricalPaidAttempts кратковременно удерживает generation lock и
// читает ограниченный набор attempts с резервом или записанной стоимостью.
// Непривязанные к run строки считаются отдельно и никогда не игнорируются.
// Это не снимок apify_runs/архивов, не проверка оплаты и не разрешение POST,
// settlement, освобождения резерва или снятия draining. Runtime его не вызывает.
func (r RunRepository) InspectHistoricalPaidAttempts(ctx context.Context) (summary HistoricalPaidAttempts, outcome error) {
	if ctx == nil || r.DB == nil || !generationPattern.MatchString(r.Generation) {
		return HistoricalPaidAttempts{}, ErrRunInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.lockGeneration(ctx)
	if err != nil {
		return HistoricalPaidAttempts{}, err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			summary = HistoricalPaidAttempts{}
			outcome = storageError(ctx)
		}
	}()
	rows, err := tx.QueryContext(ctx, `SELECT a.status,a.reserved_usd,a.cost_usd,r.generation FROM scheduler_attempts a LEFT JOIN scheduler_runs r ON r.id=a.run_id WHERE a.reserved_usd>0 OR a.cost_usd IS NOT NULL ORDER BY a.id LIMIT 1025 FOR UPDATE`)
	if err != nil {
		return HistoricalPaidAttempts{}, storageError(ctx)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > maxHistoricalPaidAttempts {
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsLimit
		}
		var status string
		var reserved float64
		var cost sql.NullFloat64
		var generation sql.NullString
		if err = rows.Scan(&status, &reserved, &cost, &generation); err != nil {
			return HistoricalPaidAttempts{}, storageError(ctx)
		}
		if !finiteMoney(reserved) || (cost.Valid && !finiteMoney(cost.Float64)) {
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
		}
		switch status {
		case "running", "completed", "failed", "interrupted", "done":
		default:
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
		}
		if generation.Valid && !generationPattern.MatchString(generation.String) {
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
		}
		if generation.Valid && generation.String == r.Generation {
			continue
		}
		if !generation.Valid {
			if cost.Valid {
				summary.OrphanCostRecorded++
			} else {
				summary.OrphanUnknown++
			}
		} else if cost.Valid {
			summary.HistoricalCostRecorded++
		} else {
			summary.HistoricalUnknown++
		}
	}
	if rows.Err() != nil {
		return HistoricalPaidAttempts{}, storageError(ctx)
	}
	if err = rows.Close(); err != nil {
		return HistoricalPaidAttempts{}, storageError(ctx)
	}
	if err = ctx.Err(); err != nil {
		return HistoricalPaidAttempts{}, err
	}
	return summary, nil
}
