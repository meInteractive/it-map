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
	return InspectHistoricalPaidAttemptsTx(ctx, tx, r.Generation)
}

// InspectHistoricalPaidAttemptsTx удерживает generation lock в транзакции
// вызывающего; caller ограничивает время и откатывает её при ошибке. Это только
// чтение evidence, не допуск к provider или восстановлению owner.
func InspectHistoricalPaidAttemptsTx(ctx context.Context, tx *sql.Tx, generation string) (summary HistoricalPaidAttempts, outcome error) {
	if ctx == nil || tx == nil || !generationPattern.MatchString(generation) {
		return HistoricalPaidAttempts{}, ErrRunInput
	}
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&current); err != nil {
		return HistoricalPaidAttempts{}, storageError(ctx)
	}
	if current != generation {
		return HistoricalPaidAttempts{}, ErrRunGeneration
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.status,a.reason_code,a.reserved_usd,a.cost_usd,r.generation FROM scheduler_attempts a LEFT JOIN scheduler_runs r ON r.id=a.run_id WHERE a.reserved_usd>0 OR a.cost_usd IS NOT NULL OR a.status='pending' ORDER BY a.id LIMIT 1025 FOR UPDATE`)
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
		var status, reason string
		var reserved float64
		var cost sql.NullFloat64
		var generation sql.NullString
		if err = rows.Scan(&status, &reason, &reserved, &cost, &generation); err != nil {
			return HistoricalPaidAttempts{}, storageError(ctx)
		}
		if !finiteMoney(reserved) || (cost.Valid && !finiteMoney(cost.Float64)) {
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
		}
		switch status {
		case "running", "completed", "failed", "interrupted", "done":
		case "pending":
			// Отложенная попытка может иметь нулевой scheduler receipt. Это
			// не доказывает отсутствие расходов провайдера: Apify проверяется отдельно.
			if (reason != "budget" && reason != "deferred" && reason != "paid_start_disabled") || reserved != 0 || !cost.Valid || cost.Float64 != 0 {
				return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
			}
		default:
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
		}
		if generation.Valid && !generationPattern.MatchString(generation.String) {
			return HistoricalPaidAttempts{}, ErrHistoricalAttemptsIncompatible
		}
		if generation.Valid && generation.String == current {
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
