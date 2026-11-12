package consumercontrol

import (
	"context"
	"database/sql"
	"errors"
)

var ErrStorage = errors.New("consumer control storage unavailable or incompatible")

// GuardNewRun блокирует generation, затем строку управления и проверяет допуск.
// Caller обязан предварительно проверить доверенную InnoDB-схему и singleton PK.
// Вызывающий обязан создать run/claim в той же транзакции и повторно проверить
// guard непосредственно перед commit: удержание строк не останавливает время.
// Сроки run/job не должны превышать owner lease или срок пилота.
// При любом отказе вызывающий обязан откатить транзакцию.
// Отсутствующая таблица, строка или несовместимые данные приводят к отказу.
// До версии схемы с проверенным backup/restore метод не подключён к runtime.
func GuardNewRun(ctx context.Context, tx *sql.Tx, req Request) error {
	if ctx == nil || tx == nil {
		return ErrState
	}
	var generation string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return guardStorageError(ctx)
	}
	if generation != req.Generation {
		return ErrStale
	}
	var state State
	var revision int64
	var mode string
	var digest sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT generation, revision, mode, owner_digest, lease_until FROM go_consumer_control WHERE id=1 FOR UPDATE").Scan(&state.Generation, &revision, &mode, &digest, &state.LeaseUntilMS); err != nil {
		return guardStorageError(ctx)
	}
	if revision <= 0 || (digest.Valid && len(digest.String) != len(state.OwnerDigest)) {
		return ErrState
	}
	state.Revision, state.Mode = uint64(revision), Mode(mode)
	copy(state.OwnerDigest[:], digest.String)
	if digest.Valid && state.OwnerDigest == ([32]byte{}) {
		return ErrState
	}
	if state.Generation != generation {
		return ErrStale
	}
	var now int64
	if err := tx.QueryRowContext(ctx, "SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)").Scan(&now); err != nil {
		return guardStorageError(ctx)
	}
	return CanStart(state, req, now)
}

func guardStorageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrStorage
}
