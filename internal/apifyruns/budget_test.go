package apifyruns

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"testing"
)

func TestBudgetMatchesLegacyOverlap(t *testing.T) {
	cases := []BudgetInput{
		{Profile: "free", PlanCreditsUSD: 5, ProviderLimitUSD: 5},
		{Profile: "free", PlanCreditsUSD: 5, ProviderLimitUSD: 5, ProviderUsedUSD: 2, HighWaterUSD: 3, ConfirmedUSD: 1, UnknownReserveUSD: 0.25, Runs: 2, UnknownRuns: 1},
		{Profile: "starter", PlanCreditsUSD: 30, ProviderLimitUSD: 19, ProviderUsedUSD: 2, ConfirmedUSD: 10, UnknownReserveUSD: 0.5, Runs: 3, UnknownRuns: 2},
		{Profile: "free", PlanCreditsUSD: 5, ProviderLimitUSD: 1, ProviderUsedUSD: 2},
	}
	node, e := exec.LookPath("node")
	if e != nil {
		t.Fatal(e)
	}
	script := `import {apifyBudget} from './server/services/apify-budget.js';let raw='';for await(const c of process.stdin)raw+=c;let out=[];for(const c of JSON.parse(raw)){let account={accountId:'CIAccount',periodStart:'2026-11-01',periodEnd:'2026-12-01',planCreditsUsd:c.planCreditsUsd,providerLimitUsd:c.providerLimitUsd,providerUsedUsd:c.providerUsedUsd};let row={provider_used:c.highWaterUsd,confirmed:c.confirmedUsd,unknown:c.unknownReserveUsd,runs:c.runs,unknown_count:c.unknownRuns};let db={prepare(){return {bind(){return {async first(){return row}}}}}};out.push(await apifyBudget(db,c.profile,account));}console.log(JSON.stringify(out));`
	input, _ := json.Marshal(cases)
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = bytes.NewReader(input)
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	var expected []Budget
	if e = json.Unmarshal(raw, &expected); e != nil {
		t.Fatal(e)
	}
	for i, in := range cases {
		got, e := CalculateBudget(in)
		if e != nil || got != expected[i] {
			t.Fatalf("case %d: %+v != %+v error=%v", i, got, expected[i], e)
		}
	}
}

func TestBudgetRejectsUntrustedSnapshots(t *testing.T) {
	for _, in := range []BudgetInput{
		{Profile: "other"},
		{Profile: "free", ProviderUsedUSD: -1},
		{Profile: "free", Runs: 1, UnknownRuns: 2},
		{Profile: "free", PlanCreditsUSD: math.NaN()},
		{Profile: "free", HighWaterUSD: math.Inf(1)},
		{Profile: "free", ConfirmedUSD: math.MaxFloat64, UnknownReserveUSD: math.MaxFloat64},
	} {
		if _, err := CalculateBudget(in); !errors.Is(err, ErrBudgetInput) {
			t.Fatal("invalid budget accepted", err)
		}
	}
}
