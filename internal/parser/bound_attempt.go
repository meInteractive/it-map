package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"itmap/internal/consumercontrol"
)

// BoundAttempt не предоставляет provider-вызовы и действует только в процессе,
// создавшем bound run. В runtime экспериментальный путь не подключён.
type BoundAttempt struct {
	bound *BoundRun
	lease *JobLease
	id    string
}

func (*BoundAttempt) String() string   { return "<redacted>" }
func (*BoundAttempt) GoString() string { return "<redacted>" }

// Reserve записывает один положительный резерв без вызова провайдера.
// Повторная попытка с уже записанным резервом отклоняется, включая случай
// неизвестного исхода commit. Денежные правила совпадают с обычным scheduler.
// Для сетевого запроса и settlement этот экспериментальный API не пригоден.
func (a *BoundAttempt) Reserve(ctx context.Context, request json.RawMessage) error {
	if a == nil || a.bound == nil || a.lease == nil || a.id == "" {
		return ErrJobInput
	}
	upper, err := UpperCostJSON(request)
	if err != nil {
		return err
	}
	if !finiteMoney(upper) || upper <= 0 {
		return ErrBudgetUnknown
	}
	var fields map[string]json.RawMessage
	var meta struct{ Provider, Model string }
	if json.Unmarshal(request, &fields) != nil || json.Unmarshal(fields["provider"], &meta.Provider) != nil || json.Unmarshal(fields["model"], &meta.Model) != nil || meta.Provider == "" || meta.Model == "" || !utf8.ValidString(meta.Provider) || !utf8.ValidString(meta.Model) {
		return ErrBudgetUnknown
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, fence, err := a.bound.beginBoundCompletion(ctx, a.lease)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = consumercontrol.GuardNewRun(ctx, tx, a.bound.consumer); err != nil {
		return err
	}
	var jobID, runID, status string
	var reserved float64
	var cost sql.NullFloat64
	err = tx.QueryRowContext(ctx, "SELECT job_id,run_id,status,reserved_usd,cost_usd FROM scheduler_attempts WHERE id=? FOR UPDATE", a.id).Scan(&jobID, &runID, &status, &reserved, &cost)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAttemptClosed
	}
	if err != nil {
		return storageError(ctx)
	}
	if jobID != a.lease.jobID || runID != a.bound.runID || status != "running" {
		return ErrAttemptClosed
	}
	if !finiteMoney(reserved) || reserved != 0 || cost.Valid {
		return ErrMoneyUncertain
	}
	if err = reserveAttemptLocked(ctx, tx, a.lease, a.id, fence.now, upper, meta.Provider, meta.Model); err != nil {
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

// StartBoundAttempt создаёт только receipt; внешние вызовы остаются запрещены.
// Draining не разрешает начинать новые attempts. Commit при ошибке не повторяется.
func (b *BoundRun) StartBoundAttempt(ctx context.Context, j *JobLease) (*BoundAttempt, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, fence, err := b.beginBoundCompletion(ctx, j)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = consumercontrol.GuardNewRun(ctx, tx, b.consumer); err != nil {
		return nil, err
	}
	id := attemptIdentity(j)
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_attempts WHERE id=?", id).Scan(&exists); err != nil {
		return nil, storageError(ctx)
	}
	if exists != 0 {
		return nil, ErrAttemptReplay
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO scheduler_attempts(id,job_id,run_id,type,company_id,source_id,started_at,status) VALUES(?,?,?,?,?,?,?,'running')", id, j.jobID, b.runID, j.job.Type, j.job.CompanyID, j.job.SourceID, fence.now); err != nil {
		return nil, storageError(ctx)
	}
	if err = b.recheckBoundCompletion(ctx, tx, fence); err != nil {
		return nil, err
	}
	if err = consumercontrol.GuardNewRun(ctx, tx, b.consumer); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, storageError(ctx)
	}
	return &BoundAttempt{bound: b, lease: j, id: id}, nil
}

// Interrupt сохраняет reservation, стоимость и result как есть, помечает
// attempt interrupted, а job failed. Автоматического повторного вызова нет.
// Допустим в draining для исходного владельца и живых вложенных leases.
func (a *BoundAttempt) Interrupt(ctx context.Context) error {
	if a == nil || a.bound == nil || a.lease == nil || a.id == "" {
		return ErrJobInput
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, fence, err := a.bound.beginBoundCompletion(ctx, a.lease)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var jobID, runID, status string
	err = tx.QueryRowContext(ctx, "SELECT job_id,run_id,status FROM scheduler_attempts WHERE id=? FOR UPDATE", a.id).Scan(&jobID, &runID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAttemptClosed
	}
	if err != nil {
		return storageError(ctx)
	}
	if jobID != a.lease.jobID || runID != a.bound.runID || status != "running" {
		return ErrAttemptClosed
	}
	if _, err = tx.ExecContext(ctx, "UPDATE scheduler_attempts SET finished_at=?,status='interrupted',reason_code='interrupted',message='Обработчик прерван; денежный резерв сохранён.' WHERE id=?", fence.now, a.id); err != nil {
		return storageError(ctx)
	}
	options, payload, err := a.lease.prepareFinish(FinishOptions{Status: "failed", Code: "interrupted", Message: "Обработчик прерван; денежный резерв сохранён."})
	if err != nil {
		return err
	}
	if err = a.lease.finishLocked(ctx, tx, fence.now, options, payload); err != nil {
		return err
	}
	if err = a.bound.recheckBoundCompletion(ctx, tx, fence); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return storageError(ctx)
	}
	return nil
}
