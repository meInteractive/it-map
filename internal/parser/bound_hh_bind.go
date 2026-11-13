package parser

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/consumercontrol"
	"itmap/internal/dbschema"
	"itmap/internal/integrations/apify"
)

var ErrHHBindingConflict = errors.New("HH operation binding incompatible")
var hhOperationIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// BindHHReservation связывает уже существующий неотправленный резерв Apify с
// конкретной bound attempt. Денежные поля и provider не меняются, сеть не
// вызывается. Повтор при той же identity идемпотентен; другую attempt или
// job нельзя привязать к той же операции. Повтор допускается только пока
// starting-резерв остаётся свежим и все fence действуют. Ошибка/неизвестный commit требует
// повторного чтения, но не разрешает отправку POST. Caller обязан подтвердить
// источник v/step/options, совместимость backup/restore и миграционный marker
// таблицы связей; текущий consumer marker не доказывает её установку. Metadata
// проверяются до и после записи, но произвольный DDL вне протокола не исключён.
// Функция экспериментальная, не подключена к runtime и не разрешает paid work.
func (a *BoundAttempt) BindHHReservation(ctx context.Context, v apifysettings.OperationVersion, step string, options apify.HHOptions) error {
	if ctx == nil || a == nil || a.bound == nil || a.lease == nil || a.id == "" {
		return ErrJobInput
	}
	if v.Generation != a.bound.repo.Generation {
		return consumercontrol.ErrStale
	}
	key, err := apifyruns.HHJobKey(v.Generation, a.lease.jobID, step)
	if err != nil {
		return err
	}
	operationKey, err := apifyruns.HHOperationIdentity(v, key, options)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, fence, err := a.bound.beginBoundCompletionMode(ctx, a.lease, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = consumercontrol.GuardNewRun(ctx, tx, a.bound.consumer); err != nil {
		return err
	}
	checkMetadata := func() error {
		snapshot, err := dbschema.Inspect(ctx, tx)
		if err != nil || dbschema.CheckHHBindingCandidate(snapshot, dbschema.HHBindingCandidateVersion) != nil {
			return ErrHHBindingConflict
		}
		return nil
	}
	if err = checkMetadata(); err != nil {
		return err
	}
	var jobID, runID, status string
	var provider sql.NullString
	var reserve float64
	var cost sql.NullFloat64
	err = tx.QueryRowContext(ctx, "SELECT job_id,run_id,status,provider,reserved_usd,cost_usd FROM scheduler_attempts WHERE id=? FOR UPDATE", a.id).Scan(&jobID, &runID, &status, &provider, &reserve, &cost)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAttemptClosed
	}
	if err != nil {
		return storageError(ctx)
	}
	if jobID != a.lease.jobID || runID != a.bound.runID || status != "running" {
		return ErrAttemptClosed
	}
	if !provider.Valid || provider.String != "apify" || !finiteMoney(reserve) || reserve <= 0 || cost.Valid {
		return ErrMoneyUncertain
	}
	var operationID, profile, opState, run, dataset string
	var charged sql.NullFloat64
	var apifyReserve float64
	var createdAt int64
	err = tx.QueryRowContext(ctx, "SELECT id,profile,state,run_id,dataset_id,reserved_usd,charged_usd,created_at FROM apify_runs WHERE generation=? AND operation_key=? FOR UPDATE", v.Generation, operationKey).Scan(&operationID, &profile, &opState, &run, &dataset, &apifyReserve, &charged, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrHHEvidenceAbsent
	}
	if err != nil {
		return storageError(ctx)
	}
	if !hhOperationIDPattern.MatchString(operationID) || profile != v.Profile || opState != "starting" || run != "" || dataset != "" || charged.Valid || !finiteMoney(apifyReserve) || apifyReserve != apifyruns.RunCapUSD || reserve < apifyReserve || createdAt <= 0 || createdAt > fence.now || fence.now-createdAt >= 300000 {
		return ErrHHBindingConflict
	}
	rows, err := tx.QueryContext(ctx, "SELECT operation_id,generation,operation_key,job_id,attempt_id FROM go_hh_attempt_bindings WHERE operation_id=? OR (generation=? AND operation_key=?) LIMIT 2 FOR UPDATE", operationID, v.Generation, operationKey)
	if err != nil {
		return storageError(ctx)
	}
	found := false
	if rows.Next() {
		var id, generation, storedKey, storedJob, attempt string
		if err = rows.Scan(&id, &generation, &storedKey, &storedJob, &attempt); err != nil {
			rows.Close()
			return storageError(ctx)
		}
		if id != operationID || generation != v.Generation || storedKey != operationKey || storedJob != a.lease.jobID || attempt != a.id {
			rows.Close()
			return ErrHHBindingConflict
		}
		found = true
	}
	if rows.Next() {
		rows.Close()
		return ErrHHBindingConflict
	}
	if rows.Err() != nil {
		rows.Close()
		return storageError(ctx)
	}
	if err = rows.Close(); err != nil {
		return storageError(ctx)
	}
	if !found {
		if _, err = tx.ExecContext(ctx, "INSERT INTO go_hh_attempt_bindings(operation_id,generation,operation_key,job_id,attempt_id) VALUES(?,?,?,?,?)", operationID, v.Generation, operationKey, a.lease.jobID, a.id); err != nil {
			return storageError(ctx)
		}
	}
	if err = checkMetadata(); err != nil {
		return err
	}
	if err = consumercontrol.GuardVersionedSingleton(ctx, tx, v.Generation); err != nil {
		return err
	}
	if err = a.bound.recheckBoundCompletion(ctx, tx, fence); err != nil {
		return err
	}
	if err = consumercontrol.GuardNewRun(ctx, tx, a.bound.consumer); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}
