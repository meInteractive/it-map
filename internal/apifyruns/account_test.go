package apifyruns

import (
	"errors"
	"testing"
	"time"
)

func TestAccountRequiresCurrentEligiblePlan(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	valid := `{"accountId":"CIAccount","planPriceUsd":0,"planCreditsUsd":5,"providerLimitUsd":5,"providerUsedUsd":0,"periodStart":"2026-11-01T00:00:00Z","periodEnd":"2026-12-01T00:00:00Z"}`
	if _, e := parseAccount([]byte(valid), "free", now); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{}`, `null`, `{"accountId":"CIAccount"}`, `{"accountId":"CIAccount","planPriceUsd":19,"planCreditsUsd":19,"providerLimitUsd":19,"providerUsedUsd":0,"periodStart":"2026-11-01T00:00:00Z","periodEnd":"2026-12-01T00:00:00Z"}`} {
		if _, e := parseAccount([]byte(raw), "free", now); !errors.Is(e, ErrBudget) {
			t.Fatal("unverified or paid account accepted as free", e)
		}
	}
	if _, e := parseAccount([]byte(valid), "starter", now); !errors.Is(e, ErrBudget) {
		t.Fatal("free account accepted as starter", e)
	}
	if _, e := parseAccount([]byte(valid), "free", now.AddDate(0, 1, 0)); !errors.Is(e, ErrBudget) {
		t.Fatal("expired account cycle accepted", e)
	}
}
