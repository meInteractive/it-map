// Package paidrecovery содержит диагностику сохранённых денежных записей.
package paidrecovery

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/parser"
)

var ErrUnavailable = errors.New("paid recovery snapshot unavailable")

// Snapshot — агрегаты обеих таблиц внутри одной транзакции. Он не доказывает
// полноту архивов, связь с attempt или подтверждение оплаты и не разрешает POST.
type Snapshot struct {
	Apify                    apifyruns.HistoricalEvidence
	Scheduler                parser.HistoricalPaidAttempts
	CurrentActorUnresolved   int
	CurrentAIReservedRunning int
	CurrentAIReservedOther   int
}

// Inspect блокирует generation и читает оба журнала в одной транзакции без
// изменения данных. Ошибка любой части возвращает пустой результат. Участники
// протокола обязаны блокировать generation перед записью; произвольные внешние
// писатели этим снимком не ограничены. Текущие AI attempts учитываются отдельно по ненулевому резерву и отсутствию стоимости;
// это не подтверждённые расходы. Архивы и attempts без денежных полей не проверяются. Нулевой результат не разрешает worker или
// снятие draining.
func Inspect(ctx context.Context, db *sql.DB, generation string) (snapshot Snapshot, outcome error) {
	if ctx == nil || db == nil {
		return Snapshot{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Snapshot{}, snapshotStorageError(ctx)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			snapshot = Snapshot{}
			outcome = snapshotStorageError(ctx)
		}
	}()
	apify, err := apifyruns.InspectHistoricalEvidenceTx(ctx, tx, generation)
	if err != nil {
		return Snapshot{}, err
	}
	scheduler, err := parser.InspectHistoricalPaidAttemptsTx(ctx, tx, generation)
	if err != nil {
		return Snapshot{}, err
	}
	current, err := apifyruns.CountCurrentUnresolvedTx(ctx, tx, generation)
	if err != nil {
		return Snapshot{}, err
	}
	var aiRunning, aiOther int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN a.status='running' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN a.status<>'running' THEN 1 ELSE 0 END),0) FROM scheduler_attempts a JOIN scheduler_runs r ON r.id=a.run_id WHERE BINARY r.generation=BINARY ? AND a.provider IN ('openai','openrouter') AND a.reserved_usd>0 AND a.cost_usd IS NULL`, generation).Scan(&aiRunning, &aiOther); err != nil {
		return Snapshot{}, snapshotStorageError(ctx)
	}
	if err = ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Apify: apify, Scheduler: scheduler, CurrentActorUnresolved: current, CurrentAIReservedRunning: aiRunning, CurrentAIReservedOther: aiOther}, nil
}

func snapshotStorageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrUnavailable
}
