// Package paidrecovery содержит только диагностику старых денежных записей.
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
	Apify     apifyruns.HistoricalEvidence
	Scheduler parser.HistoricalPaidAttempts
}

// Inspect блокирует generation и читает оба журнала в одной транзакции без
// изменения данных. Ошибка любой части возвращает пустой результат. Участники
// протокола обязаны блокировать generation перед записью; произвольные внешние
// писатели этим снимком не ограничены. Архивы и attempts без денежных полей не
// проверяются. Нулевой результат не разрешает worker или снятие draining.
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
	if err = ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Apify: apify, Scheduler: scheduler}, nil
}

func snapshotStorageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrUnavailable
}
