package apifyruns

import (
	"testing"
	"time"
)

func TestHardLimitAdmission(t *testing.T) {
	now := time.Unix(1800000000, 0)
	base := BudgetInput{Profile: "free", PlanCreditsUSD: 5, ProviderLimitUSD: 5, ProviderUsedUSD: 4.4665, HighWaterUSD: 4.4665, ConfirmedUSD: 2, Runs: 59}
	sample := account{Price: 0, Credits: 5, Limit: 5, CheckedAt: now.UnixMilli()}
	cases := []struct {
		name   string
		change func(*BudgetInput, *account, *int64)
		want   bool
	}{
		{"settled account with hard cap", func(_ *BudgetInput, _ *account, _ *int64) {}, true},
		{"stale provider snapshot", func(_ *BudgetInput, a *account, _ *int64) { a.CheckedAt -= 30001 }, false},
		{"future snapshot", func(_ *BudgetInput, a *account, _ *int64) { a.CheckedAt++ }, false},
		{"missing snapshot", func(_ *BudgetInput, a *account, _ *int64) { a.CheckedAt = 0 }, false},
		{"unresolved actor", func(i *BudgetInput, _ *account, _ *int64) { i.UnknownRuns = 1; i.UnknownReserveUSD = .25 }, false},
		{"unsettled row", func(_ *BudgetInput, _ *account, n *int64) { *n-- }, false},
		{"inadequate remaining", func(i *BudgetInput, _ *account, _ *int64) { i.HighWaterUSD = 4.8 }, false},
		{"local high water", func(i *BudgetInput, _ *account, _ *int64) { i.ConfirmedUSD = 4.8 }, false},
		{"paid profile", func(i *BudgetInput, _ *account, _ *int64) { i.Profile = "starter" }, false},
		{"paid plan", func(_ *BudgetInput, a *account, _ *int64) { a.Price = 19 }, false},
		{"hard cap over consent", func(_ *BudgetInput, a *account, _ *int64) { a.Limit = 5.01 }, false},
		{"cap above credits", func(_ *BudgetInput, a *account, _ *int64) { a.Credits = 4 }, false},
		{"missing cap", func(_ *BudgetInput, a *account, _ *int64) { a.Limit = 0 }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, a, n := base, sample, int64(59)
			tc.change(&i, &a, &n)
			if got := hardLimitAdmission(i, a, now, n); got != tc.want {
				t.Fatalf("admission=%v want %v", got, tc.want)
			}
		})
	}
}
