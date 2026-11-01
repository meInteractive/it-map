// Package apifyruns реализует бюджет и устойчивые операции Apify.
package apifyruns

import (
	"errors"
	"math"
)

const RunCapUSD = 0.25
const ReadReserveUSD = 0.01

var ErrBudgetInput = errors.New("некорректный снимок бюджета Apify")

type BudgetInput struct {
	Profile           string  `json:"profile"`
	PlanCreditsUSD    float64 `json:"planCreditsUsd"`
	ProviderLimitUSD  float64 `json:"providerLimitUsd"`
	ProviderUsedUSD   float64 `json:"providerUsedUsd"`
	HighWaterUSD      float64 `json:"highWaterUsd"`
	ConfirmedUSD      float64 `json:"confirmedUsd"`
	UnknownReserveUSD float64 `json:"unknownReserveUsd"`
	Runs              int64   `json:"runs"`
	UnknownRuns       int64   `json:"unknownRuns"`
}
type Budget struct {
	ProviderUsedUSD   float64 `json:"providerUsedUsd"`
	LimitUSD          float64 `json:"limitUsd"`
	LocalUpperUSD     float64 `json:"localUpperUsd"`
	EstimatedUsedUSD  float64 `json:"estimatedUsedUsd"`
	OwnConfirmedUSD   float64 `json:"ownConfirmedUsd"`
	UnknownReserveUSD float64 `json:"unknownReserveUsd"`
	UnknownRuns       int64   `json:"unknownRuns"`
	ReadReserveUSD    float64 `json:"readReserveUsd"`
	CoverageStatus    string  `json:"coverageStatus"`
	RemainingUSD      float64 `json:"remainingUsd"`
	MinimumReserveUSD float64 `json:"minimumReserveUsd"`
}

// Подтверждённые локальные списания и статистика аккаунта перекрываются:
// берётся максимум, затем добавляются неизвестные резервы и резерв чтения.
func CalculateBudget(in BudgetInput) (Budget, error) {
	cap := 5.0
	if in.Profile == "starter" {
		cap = 19
	} else if in.Profile != "free" {
		return Budget{}, ErrBudgetInput
	}
	for _, v := range []float64{in.PlanCreditsUSD, in.ProviderLimitUSD, in.ProviderUsedUSD, in.HighWaterUSD, in.ConfirmedUSD, in.UnknownReserveUSD} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return Budget{}, ErrBudgetInput
		}
	}
	if in.Runs < 0 || in.UnknownRuns < 0 || in.UnknownRuns > in.Runs || in.Runs > 9007199254740991 {
		return Budget{}, ErrBudgetInput
	}
	limit := math.Min(cap, math.Min(in.PlanCreditsUSD, in.ProviderLimitUSD))
	provider := math.Max(in.ProviderUsedUSD, in.HighWaterUSD)
	read := float64(in.Runs) * ReadReserveUSD
	local := in.ConfirmedUSD + in.UnknownReserveUSD + read
	used := math.Max(provider, in.ConfirmedUSD) + in.UnknownReserveUSD + read
	if math.IsInf(local, 0) || math.IsInf(used, 0) {
		return Budget{}, ErrBudgetInput
	}
	return Budget{ProviderUsedUSD: provider, LimitUSD: limit, LocalUpperUSD: local, EstimatedUsedUSD: used, OwnConfirmedUSD: in.ConfirmedUSD, UnknownReserveUSD: in.UnknownReserveUSD, UnknownRuns: in.UnknownRuns, ReadReserveUSD: read, CoverageStatus: "estimated_overlap", RemainingUSD: math.Max(0, limit-used), MinimumReserveUSD: RunCapUSD + ReadReserveUSD}, nil
}
