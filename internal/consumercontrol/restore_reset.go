package consumercontrol

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"
)

// ResetForRestore создаёт новую случайную generation и сбрасывает singleton в
// draining внутри транзакции восстановления. Сначала блокирует generation,
// затем проверяет и блокирует ровно одну строку consumer. При любой ошибке
// caller обязан откатить всю транзакцию; commit с неизвестным исходом требует
// отдельной повторной проверки. Caller обязан заранее проверить точную схему,
// migration marker, advisory lock, отсутствие старых бинарников и применять
// данные backup в этой же транзакции. Функция не выполняет эти проверки, не
// управляет commit и не подключена к существующему restore или runtime.
func ResetForRestore(ctx context.Context, tx *sql.Tx, expectedGeneration string) (string, error) {
	if ctx == nil || tx == nil || !generationPattern.MatchString(expectedGeneration) {
		return "", ErrState
	}
	if err := GuardSingleton(ctx, tx, expectedGeneration); err != nil {
		return "", err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", ErrStorage
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	generation := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
	if generation == expectedGeneration {
		return "", ErrState
	}
	result, err := tx.ExecContext(ctx, "UPDATE app_runtime_generation SET generation=?,updated_at=? WHERE id=1 AND generation=?", generation, time.Now().UTC().Format(time.RFC3339Nano), expectedGeneration)
	if err != nil {
		return "", guardStorageError(ctx)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return "", ErrStorage
	}
	result, err = tx.ExecContext(ctx, "UPDATE go_consumer_control SET generation=?,revision=1,mode='draining',owner_digest=NULL,lease_until=0 WHERE id=1 AND generation=?", generation, expectedGeneration)
	if err != nil {
		return "", guardStorageError(ctx)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return "", ErrStorage
	}
	return generation, nil
}
