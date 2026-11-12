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
// Сроки run/job не должны превышать owner lease.
// При любом отказе вызывающий обязан откатить транзакцию.
// Отсутствующая таблица, строка или несовместимые данные приводят к отказу.
// До версии схемы с проверенным backup/restore метод не подключён к runtime.
func GuardNewRun(ctx context.Context, tx *sql.Tx, req Request) error {
	state, now, err := lockControl(ctx, tx, req.Generation)
	if err != nil {
		return err
	}
	return CanStart(state, req, now)
}

// BeginDrain переводит legacy или worker в draining с CAS под теми же locks.
// Авторизация оператора, схема, commit/rollback и отключение старых бинарников —
// ответственность вызывающего слоя. При ошибке commit исход неизвестен: caller
// обязан повторно прочитать состояние под lock, а не считать rollback доказанным.
// В runtime метод не подключён.
func BeginDrain(ctx context.Context, tx *sql.Tx, generation string, revision uint64) (State, error) {
	state, now, err := lockControl(ctx, tx, generation)
	if err != nil {
		return State{}, err
	}
	next, err := Transition(state, Change{Generation: generation, Revision: revision, To: Draining}, now)
	if err != nil {
		return State{}, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE go_consumer_control SET mode=?, revision=? WHERE id=1 AND generation=? AND revision=? AND mode=?", string(next.Mode), int64(next.Revision), generation, int64(state.Revision), string(state.Mode))
	if err != nil {
		return State{}, guardStorageError(ctx)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return State{}, guardStorageError(ctx)
	}
	if changed != 1 {
		return State{}, ErrStale
	}
	return next, nil
}

// RenewOwner продлевает живого владельца без смены revision. Транзакцией и
// проверкой доверенной схемы управляет вызывающий слой, как у GuardNewRun.
// Не возрождает истёкший lease и не продлевает draining; commit здесь нет.
// При неизвестном исходе commit требуется перечитать состояние под lock.
// В runtime метод не подключён и сроки существующих run/job не изменяет.
func RenewOwner(ctx context.Context, tx *sql.Tx, req Request, untilMS int64) (State, error) {
	state, now, err := lockControl(ctx, tx, req.Generation)
	if err != nil {
		return State{}, err
	}
	next, err := Renew(state, req, untilMS, now)
	if err != nil {
		return State{}, err
	}
	// Проверка времени повторяется непосредственно в UPDATE: SQL-lock не
	// останавливает часы, а истёкшее владение нельзя восстановить heartbeat.
	result, err := tx.ExecContext(ctx, `UPDATE go_consumer_control SET lease_until=?
WHERE id=1 AND generation=? AND revision=? AND mode='worker' AND owner_digest=? AND lease_until=?
AND lease_until>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)
AND ?>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)
AND ?-CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)<=?`,
		next.LeaseUntilMS, req.Generation, int64(req.Revision), state.OwnerDigest[:], state.LeaseUntilMS,
		untilMS, untilMS, MaxOwnerLeaseMS)
	if err != nil {
		return State{}, guardStorageError(ctx)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return State{}, guardStorageError(ctx)
	}
	if changed != 1 {
		return State{}, ErrDenied
	}
	return next, nil
}

func lockControl(ctx context.Context, tx *sql.Tx, expectedGeneration string) (State, int64, error) {
	if ctx == nil || tx == nil {
		return State{}, 0, ErrState
	}
	var generation string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return State{}, 0, guardStorageError(ctx)
	}
	if generation != expectedGeneration {
		return State{}, 0, ErrStale
	}
	var state State
	var revision int64
	var mode string
	var digest sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT generation, revision, mode, owner_digest, lease_until FROM go_consumer_control WHERE id=1 FOR UPDATE").Scan(&state.Generation, &revision, &mode, &digest, &state.LeaseUntilMS); err != nil {
		return State{}, 0, guardStorageError(ctx)
	}
	if revision <= 0 || (digest.Valid && len(digest.String) != len(state.OwnerDigest)) {
		return State{}, 0, ErrState
	}
	state.Revision, state.Mode = uint64(revision), Mode(mode)
	copy(state.OwnerDigest[:], digest.String)
	if digest.Valid && state.OwnerDigest == ([32]byte{}) {
		return State{}, 0, ErrState
	}
	if state.Generation != generation {
		return State{}, 0, ErrStale
	}
	var now int64
	if err := tx.QueryRowContext(ctx, "SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)").Scan(&now); err != nil {
		return State{}, 0, guardStorageError(ctx)
	}
	return state, now, nil
}

func guardStorageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrStorage
}
