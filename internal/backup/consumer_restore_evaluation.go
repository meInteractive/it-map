//go:build consumerevaluation

package backup

import (
	"context"
	"errors"
	"time"
)

// RestoreConsumerCandidateForEvaluation доступен только в специальной тестовой
// сборке. Текущий runtime его не включает и не вызывает. Это проверка в disposable
// MariaDB без migration marker и доказательства остановки старых бинарников;
// она не является разрешением миграции, эксплуатации или production restore.
// Как и стандартный restore, этот метод разрушительно заменяет данные целевой БД.
func (r Repository) RestoreConsumerCandidateForEvaluation(ctx context.Context, data []byte) (RestoreResult, error) {
	return r.restoreConsumerForEvaluation(ctx, data, candidateRestore)
}

// RestoreVersionedConsumerForEvaluation дополнительно требует доверенный marker;
// доступен лишь в тестовой сборке и разрушительно заменяет данные целевой БД.
func (r Repository) RestoreVersionedConsumerForEvaluation(ctx context.Context, data []byte) (RestoreResult, error) {
	return r.restoreConsumerForEvaluation(ctx, data, versionedRestore)
}

func (r Repository) restoreConsumerForEvaluation(ctx context.Context, data []byte, mode consumerRestoreMode) (RestoreResult, error) {
	if ctx == nil || r.DB == nil {
		return RestoreResult{}, errors.New("consumer restore input invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	plan, err := PrepareV39Restore(data)
	if err != nil {
		return RestoreResult{}, err
	}
	session, err := r.openRestorePlanMode(ctx, plan, mode)
	if err != nil {
		return RestoreResult{}, err
	}
	defer session.Close()
	return applyRestore(ctx, session, nil)
}
