package parser

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/consumercontrol"
	"itmap/internal/dbschema"
	"itmap/internal/integrations/apify"
)

// ErrHHEvidenceAbsent не разрешает повторный POST или освобождение резерва.
var ErrHHEvidenceAbsent = errors.New("HH paid evidence not found")

// InspectHHRecovery читает возможное совпадение в существующем apify_runs под
// locks generation → consumer → run → job → attempt → operation. Ключ строится
// из стабильного job и шага; он не содержит lease owner. Проверка диагностическая:
// это не durable связь attempt с операцией, не проверка настроек/провайдера,
// не право на POST, settlement или освобождение резерва. Caller проверяет точную
// схему и происхождение v/step/options. После restore evidence старой generation
// требует отдельной проверки. В runtime метод не подключён и не вызывает сеть.
func (a *BoundAttempt) InspectHHRecovery(ctx context.Context, v apifysettings.OperationVersion, step string, options apify.HHOptions) (evidence apifyruns.RecoveryEvidence, outcome error) {
	return a.inspectHHRecovery(ctx, v, step, options, false)
}

// InspectBoundHHRecovery дополнительно проверяет сохранённую связь с конкретной
// attempt, точную candidate-схему и consumer marker. Это только чтение текущего
// владельца: caller подтверждает происхождение input, marker таблицы связей,
// backup/restore и provider evidence. Метод не разрешает POST, settlement,
// восстановление старой generation или освобождение резерва и не подключён к runtime.
func (a *BoundAttempt) InspectBoundHHRecovery(ctx context.Context, v apifysettings.OperationVersion, step string, options apify.HHOptions) (apifyruns.RecoveryEvidence, error) {
	return a.inspectHHRecovery(ctx, v, step, options, true)
}

func (a *BoundAttempt) inspectHHRecovery(ctx context.Context, v apifysettings.OperationVersion, step string, options apify.HHOptions, requireBinding bool) (evidence apifyruns.RecoveryEvidence, outcome error) {
	if ctx == nil || a == nil || a.bound == nil || a.lease == nil || a.id == "" {
		return "", ErrJobInput
	}
	if v.Generation != a.bound.repo.Generation {
		return "", consumercontrol.ErrStale
	}
	jobKey, err := apifyruns.HHJobKey(v.Generation, a.lease.jobID, step)
	if err != nil {
		return "", err
	}
	operationKey, err := apifyruns.HHOperationIdentity(v, jobKey, options)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, fence, err := a.bound.beginBoundCompletionMode(ctx, a.lease, requireBinding)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			evidence = ""
			outcome = storageError(ctx)
		}
	}()
	checkMetadata := func() error {
		if !requireBinding {
			return nil
		}
		snapshot, err := dbschema.Inspect(ctx, tx)
		if err != nil || dbschema.CheckHHBindingCandidate(snapshot, dbschema.HHBindingCandidateVersion) != nil {
			return ErrHHBindingConflict
		}
		return nil
	}
	if err = checkMetadata(); err != nil {
		return "", err
	}
	var job, run, status string
	var provider sql.NullString
	var reserved float64
	var cost sql.NullFloat64
	err = tx.QueryRowContext(ctx, "SELECT job_id,run_id,status,provider,reserved_usd,cost_usd FROM scheduler_attempts WHERE id=? FOR UPDATE", a.id).Scan(&job, &run, &status, &provider, &reserved, &cost)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrAttemptClosed
	}
	if err != nil {
		return "", storageError(ctx)
	}
	if job != a.lease.jobID || run != a.bound.runID || status != "running" {
		return "", ErrAttemptClosed
	}
	if !provider.Valid || provider.String != "apify" || !finiteMoney(reserved) || reserved <= 0 || cost.Valid {
		return "", ErrMoneyUncertain
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,state,run_id,dataset_id,account_id,charged_usd,profile,reserved_usd FROM apify_runs WHERE generation=? AND operation_key=? LIMIT 2 FOR UPDATE", v.Generation, operationKey)
	if err != nil {
		return "", storageError(ctx)
	}
	defer rows.Close()
	if !rows.Next() {
		if rows.Err() != nil {
			return "", storageError(ctx)
		}
		return "", ErrHHEvidenceAbsent
	}
	var row apifyruns.Record
	var charged sql.NullFloat64
	var profile string
	var providerReserve float64
	if err = rows.Scan(&row.ID, &row.State, &row.RunID, &row.DatasetID, &row.AccountID, &charged, &profile, &providerReserve); err != nil {
		return "", storageError(ctx)
	}
	if rows.Next() {
		return "", ErrMoneyUncertain
	}
	if rows.Err() != nil {
		return "", storageError(ctx)
	}
	if charged.Valid {
		row.ChargedUSD = &charged.Float64
	}
	evidence, err = apifyruns.ClassifyRecoveryEvidence(row)
	if err != nil {
		return "", ErrMoneyUncertain
	}
	if err = rows.Close(); err != nil {
		return "", storageError(ctx)
	}
	if requireBinding {
		if !hhOperationIDPattern.MatchString(row.ID) || profile != v.Profile || !finiteMoney(providerReserve) || providerReserve != apifyruns.RunCapUSD || reserved < providerReserve {
			return "", ErrHHBindingConflict
		}
		var id, generation, storedKey, storedJob, attempt string
		err = tx.QueryRowContext(ctx, "SELECT operation_id,generation,operation_key,job_id,attempt_id FROM go_hh_attempt_bindings WHERE operation_id=? FOR UPDATE", row.ID).Scan(&id, &generation, &storedKey, &storedJob, &attempt)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrHHEvidenceAbsent
		}
		if err != nil {
			return "", storageError(ctx)
		}
		if id != row.ID || generation != v.Generation || storedKey != operationKey || storedJob != a.lease.jobID || attempt != a.id {
			return "", ErrHHBindingConflict
		}
		if err = checkMetadata(); err != nil {
			return "", err
		}
		if err = consumercontrol.GuardVersionedSingleton(ctx, tx, v.Generation); err != nil {
			return "", err
		}
	}
	if err = a.bound.recheckBoundCompletion(ctx, tx, fence); err != nil {
		return "", err
	}
	return evidence, nil
}
