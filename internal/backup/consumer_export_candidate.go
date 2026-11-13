package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/dbschema"
)

// ExportConsumerCandidateForEvaluation проверяет экспериментальный экспорт v39
// из candidate-схемы. Текущие public API и runtime его не вызывают. Таблица
// consumer исключается только после точной проверки полной trusted metadata и
// singleton; digest, lease и режим не сериализуются. Для проверки singleton
// нужны права блокировки строк. Общий advisory lock защищает только участников
// протокола, а не произвольный DDL. Migration marker и совместимость restore
// здесь не подтверждаются: результат не разрешает миграцию или запуск worker.
func (r Repository) ExportConsumerCandidateForEvaluation(ctx context.Context, generation string) (result Download, outcome error) {
	return r.exportConsumerForEvaluation(ctx, generation, false)
}

// ExportVersionedConsumerForEvaluation дополнительно проверяет точный marker.
// Этот экспериментальный путь не подключён к API и не подтверждает остановку
// старых процессов или готовность production restore.
func (r Repository) ExportVersionedConsumerForEvaluation(ctx context.Context, generation string) (Download, error) {
	return r.exportConsumerForEvaluation(ctx, generation, true)
}

func (r Repository) exportConsumerForEvaluation(ctx context.Context, generation string, versioned bool) (result Download, outcome error) {
	if ctx == nil || r.DB == nil || generation == "" {
		return Download{}, errors.New("consumer export input invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return Download{}, errors.New("consumer export connection unavailable")
	}
	discard := func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			result = Download{}
			outcome = errors.Join(outcome, errors.New("consumer export cleanup failed"))
		}
	}()
	var database string
	var transaction, autocommit int
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE(),@@in_transaction,@@autocommit").Scan(&database, &transaction, &autocommit); err != nil || database == "" || transaction != 0 || autocommit != 1 {
		discard()
		return Download{}, errors.New("consumer export requires idle autocommit session")
	}
	h := sha256.Sum256([]byte(database))
	lock := "itmap.restore.v39:" + hex.EncodeToString(h[:16])
	var connectionID int64
	var owner sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(),IS_USED_LOCK(?)", lock).Scan(&connectionID, &owner); err != nil {
		discard()
		return Download{}, errors.New("consumer export lock unavailable")
	}
	if owner.Valid && owner.Int64 == connectionID {
		discard()
		return Download{}, errors.New("consumer export session already owns lock")
	}
	var claimed sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&claimed); err != nil || !claimed.Valid || (claimed.Int64 != 0 && claimed.Int64 != 1) {
		discard()
		return Download{}, errors.New("consumer export lock unavailable")
	}
	if claimed.Int64 == 0 {
		return Download{}, ErrRestoreBusy
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			discard()
			result = Download{}
			outcome = errors.Join(outcome, errors.New("consumer export lock cleanup failed"))
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		discard()
		return Download{}, errors.New("consumer export transaction unavailable")
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			discard()
			result = Download{}
			outcome = errors.Join(outcome, errors.New("consumer export transaction cleanup failed"))
		}
	}()
	guard := func(ctx context.Context, tx *sql.Tx) error {
		var id, owner int64
		if err := tx.QueryRowContext(ctx, "SELECT CONNECTION_ID(),IS_USED_LOCK(?)", lock).Scan(&id, &owner); err != nil || id != owner {
			return errors.New("consumer export lock lost")
		}
		snapshot, err := dbschema.Inspect(ctx, tx)
		if err != nil {
			return errors.New("consumer export metadata unavailable")
		}
		if versioned {
			err = dbschema.CheckVersionedConsumerOverlay(snapshot, dbschema.ConsumerSchemaVersion)
		} else {
			err = dbschema.CheckConsumerOverlay(snapshot, dbschema.ConsumerOverlayVersion)
		}
		if err != nil {
			return errors.New("consumer export schema incompatible")
		}
		if versioned {
			err = consumercontrol.GuardVersionedSingleton(ctx, tx, generation)
		} else {
			err = consumercontrol.GuardSingleton(ctx, tx, generation)
		}
		if err != nil {
			return errors.New("consumer export singleton incompatible")
		}
		return nil
	}
	result, err = exportSnapshot(ctx, tx, &candidateGuard{check: guard, versioned: versioned})
	if err != nil {
		return Download{}, err
	}
	if err = tx.Commit(); err != nil {
		discard()
		return Download{}, errors.New("consumer export transaction failed")
	}
	return result, nil
}
