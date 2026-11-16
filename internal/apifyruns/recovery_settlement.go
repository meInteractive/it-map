package apifyruns

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"time"

	"itmap/internal/apifysettings"
)

var ErrRecoveryChanged = errors.New("apify recovery state changed")
var ErrRecoveryCommitUnknown = errors.New("apify recovery commit outcome unknown")
var recoveryID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var recoveryKeyHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var recoveryOperationKey = regexp.MustCompile(`^go398:[0-9a-f]{64}$`)

type recoverySnapshot struct {
	id, generation, operationKey, profile, accountID, keyHash, periodStart, periodEnd string
	state, runID, datasetID, errorCode                                                string
	reserved                                                                          float64
	charged                                                                           sql.NullFloat64
	created, updated, revision                                                        int64
}

func (s recoverySnapshot) record() Record {
	return Record{ID: s.id, State: s.state, RunID: s.runID, DatasetID: s.datasetID, AccountID: s.accountID}
}

// lockRecovery holds generation, settings and row locks in the same order as
// reservation. The caller must end the transaction before any provider I/O.
func (r Repository) lockRecovery(ctx context.Context, generation, id string) (*sql.Tx, recoverySnapshot, error) {
	var s recoverySnapshot
	if r.DB == nil || r.Settings.DB != r.DB {
		return nil, s, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, s, ErrStorage
	}
	fail := func(e error) (*sql.Tx, recoverySnapshot, error) { _ = tx.Rollback(); return nil, recoverySnapshot{}, e }
	var actual string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual) != nil {
		return fail(ErrStorage)
	}
	if actual != generation {
		return fail(apifysettings.ErrGeneration)
	}
	if tx.QueryRowContext(ctx, "SELECT revision FROM apify_settings WHERE id=1 FOR UPDATE").Scan(&s.revision) != nil {
		return fail(ErrStorage)
	}
	err = tx.QueryRowContext(ctx, `SELECT id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,state,run_id,dataset_id,error_code,reserved_usd,charged_usd,created_at,updated_at FROM apify_runs WHERE id=? FOR UPDATE`, id).Scan(&s.id, &s.generation, &s.operationKey, &s.profile, &s.accountID, &s.keyHash, &s.periodStart, &s.periodEnd, &s.state, &s.runID, &s.datasetID, &s.errorCode, &s.reserved, &s.charged, &s.created, &s.updated)
	if errors.Is(err, sql.ErrNoRows) {
		return fail(ErrRecoveryEvidence)
	}
	if err != nil {
		return fail(ErrStorage)
	}
	if s.id != id || s.generation != generation || (s.profile != "free" && s.profile != "starter") || s.charged.Valid || !recoveryOperationKey.MatchString(s.operationKey) || !recoveryKeyHash.MatchString(s.keyHash) || s.revision < 0 || s.periodStart == "" || s.periodEnd == "" || s.created <= 0 || s.updated <= 0 || s.reserved <= 0 || math.IsNaN(s.reserved) || math.IsInf(s.reserved, 0) || s.datasetID == "" || validateStoredIdentity(s.record()) != nil {
		return fail(ErrRecoveryEvidence)
	}
	return tx, s, nil
}

// ReconcileKnownRecovery confirms a previously identified operation using
// authenticated GETs, even if its token has rotated. It never sends POST,
// changes the token hash, fetches datasets or creates a job. Only a terminal
// provider response with matching account/run/dataset and confirmed cost can
// be saved; generation, settings revision and every ledger field are rechecked
// after I/O under locks. This method is not connected to an HTTP endpoint or
// automatic worker. Caller must supply an ID from trusted local inventory.
func (s Service) ReconcileKnownRecovery(ctx context.Context, generation, id string) error {
	if ctx == nil || !jobGenerationPattern.MatchString(generation) || !recoveryID.MatchString(id) {
		return ErrRecoveryEvidence
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, original, err := s.Repository.lockRecovery(ctx, generation, id)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return ErrStorage
	}
	token, err := s.Repository.Settings.Resolve(ctx, original.profile)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || token == "" {
		return ErrRecoveryEvidence
	}
	// Confirm the credential and ledger remained unchanged while it was resolved.
	tx, stable, err := s.Repository.lockRecovery(ctx, generation, id)
	if err != nil {
		return err
	}
	if stable != original {
		_ = tx.Rollback()
		return ErrRecoveryChanged
	}
	if err = tx.Commit(); err != nil {
		return ErrStorage
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.checkActive(ctx); err != nil {
		return err
	}
	client, err := s.client(token)
	if err != nil || client == nil {
		return ErrRecoveryEvidence
	}
	account, err := client.Account(ctx)
	if err != nil {
		return ErrRecoveryEvidence
	}
	if account.AccountID == "" || account.AccountID != original.accountID {
		return ErrRecoveryEvidence
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.checkActive(ctx); err != nil {
		return err
	}
	run, err := client.Run(ctx, original.runID)
	if err != nil {
		return ErrRecoveryEvidence
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.checkActive(ctx); err != nil {
		return err
	}
	candidate, err := VerifyRecoveryObservation(original.record(), account.AccountID, run, time.Now())
	if err != nil {
		return err
	}
	tx, current, err := s.Repository.lockRecovery(ctx, generation, id)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if current != original {
		return ErrRecoveryChanged
	}
	code := ""
	if candidate.State == "failed" {
		code = "apify_failed"
	}
	result, err := tx.ExecContext(ctx, `UPDATE apify_runs SET state=?,charged_usd=?,error_code=?,updated_at=GREATEST(updated_at,CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)) WHERE id=? AND charged_usd IS NULL`, candidate.State, *candidate.ChargedUSD, code, id)
	if err != nil {
		return ErrStorage
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return ErrRecoveryChanged
	}
	if err = tx.Commit(); err != nil {
		return ErrRecoveryCommitUnknown
	}
	return nil
}
