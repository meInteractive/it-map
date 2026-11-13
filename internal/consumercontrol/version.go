package consumercontrol

import (
	"context"
	"database/sql"
)

// GuardVersionedSingleton блокирует generation, consumer и marker в этом
// порядке. Только полный marker id=1/version=1/status=complete принимается;
// отсутствие, лишняя строка и промежуточное состояние отклоняются. Caller
// проверяет полную metadata под общей advisory-блокировкой, обязан откатывать
// транзакцию при отказе и самостоятельно подтверждать backup и старые процессы.
// Метод не является миграцией и не подключён к runtime или restore.
func GuardVersionedSingleton(ctx context.Context, tx *sql.Tx, generation string) error {
	if err := GuardSingleton(ctx, tx, generation); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,version,status FROM go_consumer_schema_version ORDER BY id LIMIT 2 FOR UPDATE")
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
	var id, version int64
	var status string
	if err = rows.Scan(&id, &version, &status); err != nil {
		return guardStorageError(ctx)
	}
	if rows.Next() {
		return ErrState
	}
	if rows.Err() != nil {
		return guardStorageError(ctx)
	}
	if id != 1 || version != 1 || status != "complete" {
		return ErrState
	}
	return nil
}
