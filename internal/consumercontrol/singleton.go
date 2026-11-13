package consumercontrol

import (
	"context"
	"database/sql"
)

// GuardSingleton проверяет ровно одну строку id=1 и соответствие generation
// под блокировкой generation, затем таблицы consumer. Это только чтение; caller
// обязан отдельно проверить точную доверенную схему и migration marker в той
// же защищённой процедуре. Все участники записи должны соблюдать общий порядок
// блокировок. Не создаёт строки, не исправляет данные и не подключён к runtime.
func GuardSingleton(ctx context.Context, tx *sql.Tx, expectedGeneration string) error {
	if ctx == nil || tx == nil || !generationPattern.MatchString(expectedGeneration) {
		return ErrState
	}
	var generation string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return guardStorageError(ctx)
	}
	if generation != expectedGeneration {
		return ErrStale
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,generation,revision,mode,owner_digest,lease_until FROM go_consumer_control ORDER BY id LIMIT 2 FOR UPDATE")
	if err != nil {
		return guardStorageError(ctx)
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return guardStorageError(ctx)
		}
		return ErrState
	}
	var id, revision int64
	var state State
	var mode string
	var digest sql.NullString
	if err = rows.Scan(&id, &state.Generation, &revision, &mode, &digest, &state.LeaseUntilMS); err != nil {
		return guardStorageError(ctx)
	}
	if rows.Next() {
		return ErrState
	}
	if rows.Err() != nil {
		return guardStorageError(ctx)
	}
	if id != 1 || revision <= 0 || (digest.Valid && len(digest.String) != len(state.OwnerDigest)) {
		return ErrState
	}
	state.Revision = uint64(revision)
	state.Mode = Mode(mode)
	if digest.Valid {
		copy(state.OwnerDigest[:], digest.String)
		if state.OwnerDigest == ([32]byte{}) {
			return ErrState
		}
	}
	if state.Generation != generation {
		return ErrStale
	}
	return validate(state)
}
