package apifyruns

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

type UsageProfile struct {
	Budget
	AccountID      string  `json:"accountId"`
	PlanPriceUSD   float64 `json:"planPriceUsd"`
	PlanCreditsUSD float64 `json:"planCreditsUsd"`
	PeriodStart    string  `json:"periodStart"`
	PeriodEnd      string  `json:"periodEnd"`
	CheckedAt      int64   `json:"checkedAt"`
}
type ActiveRun struct {
	ID        string `json:"id"`
	Profile   string `json:"profile"`
	State     string `json:"state"`
	RunID     string `json:"run_id"`
	CreatedAt int64  `json:"created_at"`
}
type UsageView struct {
	Profiles          map[string]*UsageProfile `json:"profiles"`
	Active            []ActiveRun              `json:"active"`
	ProviderLimitMode bool                     `json:"providerLimitMode"`
}

var safeRunID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ReadUsage is a local snapshot: it does not call Apify or change its ledger.
func (r Repository) ReadUsage(ctx context.Context) (UsageView, error) {
	out := UsageView{Profiles: map[string]*UsageProfile{"free": nil, "starter": nil}, Active: []ActiveRun{}, ProviderLimitMode: r.TrustFreeProviderLimit}
	if r.DB == nil {
		return out, ErrStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, ErrStorage
	}
	defer tx.Rollback()
	var free, starter string
	err = tx.QueryRowContext(ctx, "SELECT free_account_json,starter_account_json FROM apify_settings WHERE id=1").Scan(&free, &starter)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, ErrStorage
	}
	if err == nil {
		for profile, raw := range map[string]string{"free": free, "starter": starter} {
			a, e := parseAccount([]byte(raw), profile, time.Now())
			if e != nil {
				continue
			}
			in := BudgetInput{Profile: profile, PlanCreditsUSD: a.Credits, ProviderLimitUSD: a.Limit, ProviderUsedUSD: a.Used}
			usageID, _ := json.Marshal([]string{a.ID, a.Start, a.End})
			e = tx.QueryRowContext(ctx, "SELECT provider_used_usd FROM apify_account_usage WHERE id=?", string(usageID)).Scan(&in.HighWaterUSD)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return out, ErrStorage
			}
			e = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(charged_usd),0),COALESCE(SUM(CASE WHEN charged_usd IS NULL THEN reserved_usd ELSE 0 END),0),COUNT(*),COALESCE(SUM(CASE WHEN charged_usd IS NULL THEN 1 ELSE 0 END),0) FROM apify_runs WHERE account_id=? AND period_start=? AND period_end=?", a.ID, a.Start, a.End).Scan(&in.ConfirmedUSD, &in.UnknownReserveUSD, &in.Runs, &in.UnknownRuns)
			if e != nil {
				return out, ErrStorage
			}
			b, e := CalculateBudget(in)
			if e != nil {
				return out, ErrStorage
			}
			out.Profiles[profile] = &UsageProfile{Budget: b, AccountID: a.ID, PlanPriceUSD: a.Price, PlanCreditsUSD: a.Credits, PeriodStart: a.Start, PeriodEnd: a.End, CheckedAt: a.CheckedAt}
		}
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,profile,state,run_id,created_at FROM apify_runs WHERE state IN ('starting','running','uncertain') ORDER BY created_at DESC LIMIT 5")
	if e != nil {
		return out, ErrStorage
	}
	for rows.Next() {
		var v ActiveRun
		if rows.Scan(&v.ID, &v.Profile, &v.State, &v.RunID, &v.CreatedAt) != nil {
			rows.Close()
			return out, ErrStorage
		}
		if !safeRunID.MatchString(v.ID) {
			v.ID = ""
		}
		if !safeRunID.MatchString(v.RunID) {
			v.RunID = ""
		}
		out.Active = append(out.Active, v)
	}
	if rows.Err() != nil {
		rows.Close()
		return out, ErrStorage
	}
	rows.Close()
	if tx.Commit() != nil {
		return out, ErrStorage
	}
	return out, nil
}
