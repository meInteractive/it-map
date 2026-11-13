package paidrecovery

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"

	"itmap/internal/consumercontrol"
	"itmap/internal/dbschema"
)

const maxBindingAudit = 256

var ErrBindingAuditLimit = errors.New("HH binding audit exceeds diagnostic limit")
var ErrBindingAuditIncompatible = errors.New("HH binding evidence incompatible")

var auditGenerationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var auditOperationIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var auditOperationKeyPattern = regexp.MustCompile(`^go398:[0-9a-f]{64}$`)

// BindingAudit — непересекающиеся категории ссылок, имеющихся в этой БД.
// Для каждой связи учитывается только первое обнаруженное нарушение в порядке
// operation → attempt → job → run. OldGeneration выделяет устаревшие ссылки
// независимо от их категории. Даже
// отсутствие проблем не подтверждает полноту архивов или settled стоимость.
type BindingAudit struct {
	Total            int
	OldGeneration    int
	Consistent       int
	MissingOperation int
	MissingAttempt   int
	MissingJob       int
	MissingRun       int
	IdentityMismatch int
}

type bindingRow struct{ operationID, generation, operationKey, jobID, attemptID string }

// InspectBindings диагностически проверяет candidate metadata и ссылки на
// apify_runs/scheduler под generation → consumer → marker locks. Неполный
// marker, повреждённая metadata, невалидная строка или более 256 связей дают
// отказ без частичного результата. Это не проверка marker таблицы связей,
// архивов, provider или settlement и не разрешение POST или снятия draining.
// Внешние писатели вне протокола блокировок не сериализуются. В runtime не подключён.
func InspectBindings(ctx context.Context, db *sql.DB, generation string) (summary BindingAudit, outcome error) {
	if ctx == nil || db == nil || !auditGenerationPattern.MatchString(generation) {
		return BindingAudit{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return BindingAudit{}, snapshotStorageError(ctx)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			summary = BindingAudit{}
			outcome = snapshotStorageError(ctx)
		}
	}()
	if err = consumercontrol.GuardVersionedSingleton(ctx, tx, generation); err != nil {
		return BindingAudit{}, err
	}
	checkMetadata := func() error {
		snapshot, err := dbschema.Inspect(ctx, tx)
		if err != nil || dbschema.CheckHHBindingCandidate(snapshot, dbschema.HHBindingCandidateVersion) != nil {
			return ErrBindingAuditIncompatible
		}
		return nil
	}
	if err = checkMetadata(); err != nil {
		return BindingAudit{}, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT operation_id,generation,operation_key,job_id,attempt_id FROM go_hh_attempt_bindings ORDER BY operation_id LIMIT 257 FOR UPDATE")
	if err != nil {
		return BindingAudit{}, snapshotStorageError(ctx)
	}
	defer rows.Close()
	bindings := make([]bindingRow, 0)
	for rows.Next() {
		if len(bindings) >= maxBindingAudit {
			return BindingAudit{}, ErrBindingAuditLimit
		}
		var b bindingRow
		if err = rows.Scan(&b.operationID, &b.generation, &b.operationKey, &b.jobID, &b.attemptID); err != nil {
			return BindingAudit{}, snapshotStorageError(ctx)
		}
		if !auditOperationIDPattern.MatchString(b.operationID) || !auditGenerationPattern.MatchString(b.generation) || !auditOperationKeyPattern.MatchString(b.operationKey) || b.jobID == "" || b.attemptID == "" || !utf8.ValidString(b.jobID) || !utf8.ValidString(b.attemptID) {
			return BindingAudit{}, ErrBindingAuditIncompatible
		}
		bindings = append(bindings, b)
	}
	if rows.Err() != nil {
		return BindingAudit{}, snapshotStorageError(ctx)
	}
	if err = rows.Close(); err != nil {
		return BindingAudit{}, snapshotStorageError(ctx)
	}
	for _, b := range bindings {
		summary.Total++
		if b.generation != generation {
			summary.OldGeneration++
		}
		var actualID, actualGeneration, actualKey string
		err = tx.QueryRowContext(ctx, "SELECT id,generation,operation_key FROM apify_runs WHERE id=? FOR UPDATE", b.operationID).Scan(&actualID, &actualGeneration, &actualKey)
		if errors.Is(err, sql.ErrNoRows) {
			summary.MissingOperation++
			continue
		}
		if err != nil {
			return BindingAudit{}, snapshotStorageError(ctx)
		}
		if actualID != b.operationID || actualGeneration != b.generation || actualKey != b.operationKey {
			summary.IdentityMismatch++
			continue
		}
		var attemptID, attemptJob, runID string
		err = tx.QueryRowContext(ctx, "SELECT id,job_id,run_id FROM scheduler_attempts WHERE id=? FOR UPDATE", b.attemptID).Scan(&attemptID, &attemptJob, &runID)
		if errors.Is(err, sql.ErrNoRows) {
			summary.MissingAttempt++
			continue
		}
		if err != nil {
			return BindingAudit{}, snapshotStorageError(ctx)
		}
		if attemptID != b.attemptID || attemptJob != b.jobID || runID == "" {
			summary.IdentityMismatch++
			continue
		}
		var jobID, jobGeneration string
		err = tx.QueryRowContext(ctx, "SELECT id,generation FROM scheduler_jobs WHERE id=? FOR UPDATE", b.jobID).Scan(&jobID, &jobGeneration)
		if errors.Is(err, sql.ErrNoRows) {
			summary.MissingJob++
			continue
		}
		if err != nil {
			return BindingAudit{}, snapshotStorageError(ctx)
		}
		if jobID != b.jobID || jobGeneration != b.generation {
			summary.IdentityMismatch++
			continue
		}
		var actualRun, runGeneration string
		err = tx.QueryRowContext(ctx, "SELECT id,generation FROM scheduler_runs WHERE id=? FOR UPDATE", runID).Scan(&actualRun, &runGeneration)
		if errors.Is(err, sql.ErrNoRows) {
			summary.MissingRun++
			continue
		}
		if err != nil {
			return BindingAudit{}, snapshotStorageError(ctx)
		}
		if actualRun != runID || runGeneration != b.generation {
			summary.IdentityMismatch++
			continue
		}
		summary.Consistent++
	}
	if err = checkMetadata(); err != nil {
		return BindingAudit{}, err
	}
	if err = consumercontrol.GuardVersionedSingleton(ctx, tx, generation); err != nil {
		return BindingAudit{}, err
	}
	if err = ctx.Err(); err != nil {
		return BindingAudit{}, err
	}
	return summary, nil
}
