package parser

import (
	"errors"
	"math"
	"sort"
	"strings"
)

// GO-434 audit helper: this is NOT a production price calculator. Its input
// must come from a reviewed, read-only ledger extract, never from user payloads.
// Stage cost fields are observations, not additional billable operations.
type expenseAuditObservation struct {
	Ledger       string // apify_runs, scheduler_attempts, stage, firecrawl_snapshot
	LocalID      string
	Provider     string
	AccountScope string // stable provider account scope, only for apify_runs
	ExternalID   string // actual Apify run ID, NOT dataset or scheduler run ID
	CostUSD      *float64
	ReservedUSD  *float64
	Credits      *float64
}

type expenseAuditTotals struct {
	ConfirmedUSD      float64
	UnknownReserveUSD float64
	ObservedCredits   float64
	Confirmed         int
	Unknown           int
	Unpriced          int
	Observations      int
	Deduplicated      int
	Ambiguous         int
	EvidenceOnly      int
	// True means SQL evidence cannot establish a non-overlapping total.
	OverlapUnresolved bool
}

type expenseAuditGroup struct {
	Ledger string
	Provider string
	Cost *float64
	Reserve float64
	HasReserve bool
	Credits *float64
	Conflict bool
}

var errExpenseAuditInput = errors.New("invalid expense audit evidence")

func expenseAuditAmount(n *float64) bool {
	return n == nil || (*n >= 0 && !math.IsNaN(*n) && !math.IsInf(*n, 0))
}

func expenseAuditKey(v expenseAuditObservation) string {
	if v.Ledger == "apify_runs" && v.AccountScope != "" && v.ExternalID != "" {
		return "apify-run\x00" + v.AccountScope + "\x00" + v.ExternalID
	}
	// Never JOIN scheduler_attempts.run_id (scheduler cycle ID) with
	// apify_runs.run_id (provider Actor ID). They are unrelated domains.
	return v.Ledger + "\x00" + v.LocalID
}

func expenseAuditReconcile(items []expenseAuditObservation) (expenseAuditTotals, error) {
	totals := expenseAuditTotals{}
	groups := map[string]*expenseAuditGroup{}
	localIdentity := map[string]string{}
	hasApify, hasSchedulerApify := false, false
	for _, item := range items {
		if item.LocalID == "" || strings.ContainsRune(item.LocalID, '\x00') ||
			!expenseAuditAmount(item.CostUSD) || !expenseAuditAmount(item.ReservedUSD) ||
			!expenseAuditAmount(item.Credits) {
			return expenseAuditTotals{}, errExpenseAuditInput
		}
		switch item.Ledger {
		case "apify_runs":
			hasApify = true
			if item.AccountScope == "" && item.ExternalID != "" {
				return expenseAuditTotals{}, errExpenseAuditInput
			}
		case "scheduler_attempts":
			if item.Provider == "apify" {
				hasSchedulerApify = true
			}
		case "stage", "firecrawl_snapshot":
			// The provider invoice cannot be inferred from these rows.
		default:
			return expenseAuditTotals{}, errExpenseAuditInput
		}
		if item.Ledger != "apify_runs" && (item.AccountScope != "" || item.ExternalID != "") {
			return expenseAuditTotals{}, errExpenseAuditInput
		}
		localKey := item.Ledger + "\x00" + item.LocalID
		key := expenseAuditKey(item)
		if prior, ok := localIdentity[localKey]; ok && prior != key {
			return expenseAuditTotals{}, errExpenseAuditInput
		}
		localIdentity[localKey] = key
		totals.Observations++
		g := groups[key]
		if g == nil {
			g = &expenseAuditGroup{Ledger: item.Ledger, Provider: item.Provider}
			groups[key] = g
		} else {
			totals.Deduplicated++
			if g.Provider != item.Provider {
				g.Conflict = true
			}
		}
		if item.CostUSD != nil {
			if g.Cost != nil && *g.Cost != *item.CostUSD {
				// Two incompatible prices for one provider operation must
				// be reconciled manually, not summed or silently maxed.
				g.Conflict = true
			}
			v := *item.CostUSD
			g.Cost = &v
		}
		if item.ReservedUSD != nil {
			if !g.HasReserve || *item.ReservedUSD > g.Reserve {
				g.Reserve = *item.ReservedUSD
			}
			g.HasReserve = true
		}
		if item.Credits != nil {
			if g.Credits != nil && *g.Credits != *item.Credits {
				g.Conflict = true
			}
			v := *item.Credits
			g.Credits = &v
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups { keys = append(keys, key) }
	sort.Strings(keys)
	for _, key := range keys {
		g := groups[key]
		if g.Conflict {
			totals.Ambiguous++
			continue
		}
		switch g.Ledger {
		case "stage":
			totals.EvidenceOnly++
		case "firecrawl_snapshot":
			// Observed snapshot credits are NOT provider-billed USD and
			// cannot be projected to a paid request without a usage ID.
			totals.EvidenceOnly++
			if g.Credits != nil {
				totals.ObservedCredits += *g.Credits
			}
		case "apify_runs", "scheduler_attempts":
			if g.Cost != nil {
				totals.Confirmed++
				totals.ConfirmedUSD += *g.Cost
			} else if g.HasReserve && g.Reserve > 0 {
				totals.Unknown++
				totals.UnknownReserveUSD += g.Reserve
			} else {
				// NULL cost with 0 reserve is NOT proved free.
				totals.Unpriced++
			}
		}
	}
	// With no provider-operation ID on scheduler_attempts, two native
	// ledgers potentially describing Apify cannot be combined reliably.
	totals.OverlapUnresolved = hasApify && hasSchedulerApify
	return totals, nil
}
