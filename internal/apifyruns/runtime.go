package apifyruns

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

var errNoOperation = errors.New("операция Apify ещё не зарезервирована")

type operation struct {
	Record
	keyHash    string
	profile    string
	generation string
	createdAt  int64
}

// Одинаковый порядок блокировок применяется перед стартом и при записи ответа.
// Транзакция никогда не удерживается на время внешнего запроса.
func (r Repository) lockOperation(ctx context.Context, v apifysettings.OperationVersion, key string) (*sql.Tx, operation, string, error) {
	var row operation
	if r.DB == nil || r.Settings.DB != r.DB {
		return nil, row, "", ErrStorage
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return nil, row, "", ErrStorage
	}
	fail := func(e error) (*sql.Tx, operation, string, error) { tx.Rollback(); return nil, operation{}, "", e }
	var generation string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation) != nil {
		return fail(ErrStorage)
	}
	if generation != v.Generation {
		return fail(apifysettings.ErrGeneration)
	}
	token, e := r.Settings.ResolveOperationTx(ctx, tx, v)
	if e != nil {
		return fail(e)
	}
	var cost sql.NullFloat64
	e = tx.QueryRowContext(ctx, "SELECT id,state,run_id,dataset_id,account_id,charged_usd,key_hash,profile,generation,created_at FROM apify_runs WHERE generation=? AND operation_key=? FOR UPDATE", v.Generation, key).Scan(&row.ID, &row.State, &row.RunID, &row.DatasetID, &row.AccountID, &cost, &row.keyHash, &row.profile, &row.generation, &row.createdAt)
	if errors.Is(e, sql.ErrNoRows) {
		return fail(errNoOperation)
	}
	if e != nil {
		return fail(ErrStorage)
	}
	hash := sha256.Sum256([]byte(token))
	if row.keyHash != hex.EncodeToString(hash[:]) || row.profile != v.Profile {
		return fail(apifysettings.ErrChanged)
	}
	if cost.Valid {
		row.ChargedUSD = &cost.Float64
	}
	return tx, row, token, nil
}

func (r Repository) readOperation(ctx context.Context, v apifysettings.OperationVersion, key string) (Record, error) {
	tx, row, _, e := r.lockOperation(ctx, v, key)
	if e != nil {
		return Record{}, e
	}
	defer tx.Rollback()
	if tx.Commit() != nil {
		return Record{}, ErrStorage
	}
	return row.Record, nil
}

// Только успешный COMMIT перехода starting→uncertain разрешает единственный POST.
// Crash или неизвестный COMMIT оставляет резерв без права повторной отправки.
func (r Repository) claimStart(ctx context.Context, v apifysettings.OperationVersion, key string) (Record, string, error) {
	tx, row, token, e := r.lockOperation(ctx, v, key)
	if e != nil {
		return Record{}, "", e
	}
	defer tx.Rollback()
	if row.State != "starting" || row.RunID != "" || row.DatasetID != "" || row.ChargedUSD != nil || time.Now().UnixMilli()-row.createdAt >= 300000 {
		return row.Record, "", ErrUncertain
	}
	result, e := tx.ExecContext(ctx, "UPDATE apify_runs SET state='uncertain',error_code='apify_uncertain',updated_at=? WHERE id=? AND state='starting' AND run_id='' AND dataset_id=''", time.Now().UnixMilli(), row.ID)
	if e != nil {
		return Record{}, "", ErrStorage
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return Record{}, "", ErrUncertain
	}
	if tx.Commit() != nil {
		return Record{}, "", ErrStorage
	}
	row.State = "uncertain"
	return row.Record, token, nil
}

func (r Repository) record(ctx context.Context, v apifysettings.OperationVersion, key string, run apify.ActorRun) (Record, error) {
	tx, row, _, e := r.lockOperation(ctx, v, key)
	if e != nil {
		return Record{}, e
	}
	defer tx.Rollback()
	if row.State == "starting" {
		return Record{}, ErrUncertain
	}
	updated, e := mergeRun(row.Record, run, time.Now())
	if e != nil {
		return Record{}, e
	}
	code := ""
	if updated.State == "failed" {
		code = "apify_failed"
	}
	var cost any
	if updated.ChargedUSD != nil {
		cost = *updated.ChargedUSD
	}
	result, e := tx.ExecContext(ctx, "UPDATE apify_runs SET state=?,run_id=?,dataset_id=?,charged_usd=?,error_code=?,updated_at=? WHERE id=?", updated.State, updated.RunID, updated.DatasetID, cost, code, time.Now().UnixMilli(), row.ID)
	if e != nil {
		return Record{}, ErrStorage
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return Record{}, ErrStorage
	}
	if tx.Commit() != nil {
		return Record{}, ErrStorage
	}
	return updated, nil
}
