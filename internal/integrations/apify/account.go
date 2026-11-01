package apify

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"time"
)

type BillingAccount struct {
	AccountID        string  `json:"accountId"`
	PlanPriceUSD     float64 `json:"planPriceUsd"`
	PlanCreditsUSD   float64 `json:"planCreditsUsd"`
	ProviderUsedUSD  float64 `json:"providerUsedUsd"`
	ProviderLimitUSD float64 `json:"providerLimitUsd"`
	PeriodStart      string  `json:"periodStart"`
	PeriodEnd        string  `json:"periodEnd"`
	CheckedAt        int64   `json:"checkedAt"`
}

var billingID = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// Account читает только статистику. Несвязанные секреты /users/me не выходят
// из адаптера: возвращается явная проекция полей расчётного периода и лимитов.
func (c *Client) Account(ctx context.Context) (BillingAccount, error) {
	var out BillingAccount
	user, _, e := c.request(ctx, "/users/me", requestOptions{maxBytes: 64000})
	if e != nil {
		return out, e
	}
	var u struct {
		Data struct {
			ID   string `json:"id"`
			Plan struct {
				Price   *float64 `json:"monthlyBasePriceUsd"`
				Credits *float64 `json:"monthlyUsageCreditsUsd"`
				Enabled *bool    `json:"isEnabled"`
			} `json:"plan"`
		} `json:"data"`
	}
	if json.Unmarshal(user, &u) != nil || !billingID.MatchString(u.Data.ID) || u.Data.Plan.Enabled != nil && !*u.Data.Plan.Enabled {
		return out, apiError("apify_data", 502)
	}
	limits, _, e := c.request(ctx, "/users/me/limits", requestOptions{maxBytes: 64000})
	if e != nil {
		return out, e
	}
	var l struct {
		Data struct {
			Cycle struct {
				Start string `json:"startAt"`
				End   string `json:"endAt"`
			} `json:"monthlyUsageCycle"`
			Current struct {
				Used *float64 `json:"monthlyUsageUsd"`
			} `json:"current"`
			Limits struct {
				Max *float64 `json:"maxMonthlyUsageUsd"`
			} `json:"limits"`
		} `json:"data"`
	}
	if json.Unmarshal(limits, &l) != nil {
		return out, apiError("apify_data", 502)
	}
	for _, v := range []*float64{u.Data.Plan.Price, u.Data.Plan.Credits, l.Data.Current.Used, l.Data.Limits.Max} {
		if v == nil || *v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0) {
			return out, apiError("apify_data", 502)
		}
	}
	now := time.Now()
	start, e := time.Parse(time.RFC3339Nano, l.Data.Cycle.Start)
	if e != nil {
		return out, apiError("apify_data", 502)
	}
	end, e := time.Parse(time.RFC3339Nano, l.Data.Cycle.End)
	if e != nil || now.Before(start) || !now.Before(end) {
		return out, apiError("apify_data", 502)
	}
	return BillingAccount{AccountID: u.Data.ID, PlanPriceUSD: *u.Data.Plan.Price, PlanCreditsUSD: *u.Data.Plan.Credits, ProviderUsedUSD: *l.Data.Current.Used, ProviderLimitUSD: *l.Data.Limits.Max, PeriodStart: l.Data.Cycle.Start, PeriodEnd: l.Data.Cycle.End, CheckedAt: now.UnixMilli()}, nil
}
