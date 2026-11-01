package apifyruns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

// StoreAccount сохраняет проверенную адаптером статистику. Опоздавший ответ
// не уменьшает расход аккаунта и не переживает смену ключа или поколения.
func (r Repository) StoreAccount(ctx context.Context, v apifysettings.OperationVersion, a apify.BillingAccount) (apify.BillingAccount, error) {
	if r.DB == nil || r.Settings.DB != r.DB {
		return apify.BillingAccount{}, ErrStorage
	}
	raw, e := json.Marshal(a)
	if e != nil {
		return apify.BillingAccount{}, ErrBudget
	}
	if _, e = parseAccount(raw, v.Profile, time.Now()); e != nil {
		return apify.BillingAccount{}, e
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return apify.BillingAccount{}, ErrStorage
	}
	defer tx.Rollback()
	var generation string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation) != nil {
		return apify.BillingAccount{}, ErrStorage
	}
	if generation != v.Generation {
		return apify.BillingAccount{}, apifysettings.ErrGeneration
	}
	if _, e = r.Settings.ResolveOperationTx(ctx, tx, v); e != nil {
		return apify.BillingAccount{}, e
	}
	column := "free_account_json"
	if v.Profile == "starter" {
		column = "starter_account_json"
	}
	var previous []byte
	if tx.QueryRowContext(ctx, "SELECT "+column+" FROM apify_settings WHERE id=1").Scan(&previous) != nil {
		return apify.BillingAccount{}, ErrStorage
	}
	var old apify.BillingAccount
	if json.Unmarshal(previous, &old) == nil && old.AccountID == a.AccountID && old.PeriodStart == a.PeriodStart && old.PeriodEnd == a.PeriodEnd {
		a.ProviderUsedUSD = math.Max(a.ProviderUsedUSD, old.ProviderUsedUSD)
	}
	id, _ := json.Marshal([]string{a.AccountID, a.PeriodStart, a.PeriodEnd})
	var high float64
	e = tx.QueryRowContext(ctx, "SELECT provider_used_usd FROM apify_account_usage WHERE id=?", string(id)).Scan(&high)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return apify.BillingAccount{}, ErrStorage
	}
	a.ProviderUsedUSD = math.Max(a.ProviderUsedUSD, high)
	if _, e = tx.ExecContext(ctx, "INSERT INTO apify_account_usage(id,account_id,period_start,period_end,provider_used_usd,updated_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE provider_used_usd=GREATEST(provider_used_usd,VALUES(provider_used_usd)),updated_at=GREATEST(updated_at,VALUES(updated_at))", string(id), a.AccountID, a.PeriodStart, a.PeriodEnd, a.ProviderUsedUSD, a.CheckedAt); e != nil {
		return apify.BillingAccount{}, ErrStorage
	}
	raw, e = json.Marshal(a)
	if e != nil {
		return apify.BillingAccount{}, ErrBudget
	}
	errorColumn := "free_error"
	if v.Profile == "starter" {
		errorColumn = "starter_error"
	}
	result, e := tx.ExecContext(ctx, "UPDATE apify_settings SET "+column+"=?,"+errorColumn+"='' WHERE id=1 AND revision=?", string(raw), v.SettingsRevision)
	if e != nil {
		return apify.BillingAccount{}, ErrStorage
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return apify.BillingAccount{}, apifysettings.ErrChanged
	}
	if tx.Commit() != nil {
		return apify.BillingAccount{}, ErrStorage
	}
	return a, nil
}
