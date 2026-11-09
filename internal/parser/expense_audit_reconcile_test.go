package parser

import (
	"errors"
	"math"
	"testing"
)

func auditFloat(v float64) *float64 { return &v }

func TestExpenseAuditReconcilesDistinctPaymentOperations(t *testing.T) {
	rows := []expenseAuditObservation{
		// One real Actor, three local observations, two sources and
		// two discovered candidates: provider run ID is the billing unit.
		{Ledger:"apify_runs",LocalID:"receipt-a",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-actor-1",CostUSD:auditFloat(0.40)},
		{Ledger:"apify_runs",LocalID:"receipt-b",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-actor-1",CostUSD:auditFloat(0.40)},
		{Ledger:"apify_runs",LocalID:"receipt-a",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-actor-1",CostUSD:auditFloat(0.40)},
		// Paid empty scan still counts in the spend numerator.
		{Ledger:"apify_runs",LocalID:"empty-scan",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-empty-run",CostUSD:auditFloat(0.10)},
		{Ledger:"scheduler_attempts",LocalID:"jev-1",Provider:"openrouter",CostUSD:auditFloat(0.02)},
		{Ledger:"scheduler_attempts",LocalID:"jev-2",Provider:"openrouter",CostUSD:auditFloat(0.02)},
		{Ledger:"scheduler_attempts",LocalID:"rejected",Provider:"openrouter",CostUSD:auditFloat(0.01)},
		{Ledger:"scheduler_attempts",LocalID:"failed-billed",Provider:"openai",CostUSD:auditFloat(0.03)},
		// Lost ACK: reserve only, NOT a confirmed charge.
		{Ledger:"scheduler_attempts",LocalID:"unknown-ack",Provider:"openai",ReservedUSD:auditFloat(0.20)},
		{Ledger:"scheduler_attempts",LocalID:"unpriced",Provider:"openai",ReservedUSD:auditFloat(0)},
		// Successful free receipt must be an explicitly known numeric 0.
		{Ledger:"scheduler_attempts",LocalID:"free",Provider:"openrouter",CostUSD:auditFloat(0)},
		// Stage receipts corroborate, but never add to the payment ledger.
		{Ledger:"stage",LocalID:"classify-1",Provider:"openrouter",CostUSD:auditFloat(0.02)},
		{Ledger:"stage",LocalID:"late-extraction",Provider:"openai",CostUSD:auditFloat(0.03)},
		// Snapshot credits carry no current-or-historical USD price.
		{Ledger:"firecrawl_snapshot",LocalID:"snapshot-1",Provider:"firecrawl",Credits:auditFloat(2)},
		{Ledger:"firecrawl_snapshot",LocalID:"snapshot-1",Provider:"firecrawl",Credits:auditFloat(2)},
	}
	got,err:=expenseAuditReconcile(rows)
	if err!=nil {t.Fatal(err)}
	if math.Abs(got.ConfirmedUSD-0.58)>1e-10 || got.Confirmed!=7 ||
		got.Unknown!=1 || got.UnknownReserveUSD!=0.20 ||
		got.Unpriced!=1 || got.Deduplicated!=3 || got.ObservedCredits!=2 ||
		got.EvidenceOnly!=3 || got.Ambiguous!=0 || got.OverlapUnresolved {
		t.Fatalf("incorrect expense reconciliation: %+v",got)
	}
	if got.Observations!=len(rows){t.Fatal("observations were dropped")}
}

func TestExpenseAuditCannotConcealUnknownOrConflictingMoney(t *testing.T) {
	// NULL cost does not mean zero; later confirmed receipt replaces an
	// unknown reserve for the SAME provider operation.
	got,err:=expenseAuditReconcile([]expenseAuditObservation{
		{Ledger:"apify_runs",LocalID:"starting",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-run",ReservedUSD:auditFloat(0.26)},
		{Ledger:"apify_runs",LocalID:"settled",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-run",CostUSD:auditFloat(0.17)},
	})
	if err!=nil||got.Confirmed!=1||got.ConfirmedUSD!=0.17||got.Unknown!=0||got.UnknownReserveUSD!=0{
		t.Fatalf("known settlement not reconciled: %+v %v",got,err)
	}
	// Different confirmations for the same Actor may represent a growing
	// charge snapshot, not two independent invoices. Block attribution.
	got,err=expenseAuditReconcile([]expenseAuditObservation{
		{Ledger:"apify_runs",LocalID:"first",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-run",CostUSD:auditFloat(0.17)},
		{Ledger:"apify_runs",LocalID:"later",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-run",CostUSD:auditFloat(0.19)},
	})
	if err!=nil||got.Ambiguous!=1||got.Confirmed!=0||got.ConfirmedUSD!=0{
		t.Fatalf("conflicting provider charge silently counted: %+v %v",got,err)
	}
	// Scheduler run_id is a scheduler cycle, never an Apify Actor ID.
	// Without an explicit cross-ledger provider identity there can be overlap.
	got,err=expenseAuditReconcile([]expenseAuditObservation{
		{Ledger:"apify_runs",LocalID:"actor",Provider:"apify",AccountScope:"ci-account",ExternalID:"ci-run",CostUSD:auditFloat(0.12)},
		{Ledger:"scheduler_attempts",LocalID:"job-attempt",Provider:"apify",CostUSD:auditFloat(0.12)},
	})
	if err!=nil||!got.OverlapUnresolved {t.Fatalf("missing cross-ledger warning: %+v %v",got,err)}
	// Distinct provider accounts are distinct paid operations even when the
	// Actor gives identical run IDs in synthetic history.
	got,err=expenseAuditReconcile([]expenseAuditObservation{
		{Ledger:"apify_runs",LocalID:"actor-A",Provider:"apify",AccountScope:"ci-a",ExternalID:"ci-run",CostUSD:auditFloat(0.12)},
		{Ledger:"apify_runs",LocalID:"actor-B",Provider:"apify",AccountScope:"ci-b",ExternalID:"ci-run",CostUSD:auditFloat(0.12)},
	})
	if err!=nil||got.Confirmed!=2||math.Abs(got.ConfirmedUSD-0.24)>1e-10{
		t.Fatalf("independent accounts collapsed: %+v %v",got,err)
	}
}

func TestExpenseAuditFailsClosedOnInvalidEvidence(t *testing.T) {
	cases:=[][]expenseAuditObservation{
		{{Ledger:"apify_runs",LocalID:"",Provider:"apify",CostUSD:auditFloat(0.1)}},
		{{Ledger:"scheduler_attempts",LocalID:"ci",CostUSD:auditFloat(-1)}},
		{{Ledger:"scheduler_attempts",LocalID:"ci",ReservedUSD:auditFloat(math.NaN())}},
		{{Ledger:"scheduler_attempts",LocalID:"ci",Credits:auditFloat(math.Inf(1))}},
		{{Ledger:"other",LocalID:"ci"}},
		{{Ledger:"scheduler_attempts",LocalID:"ci",ExternalID:"actor"}},
		{{Ledger:"apify_runs",LocalID:"ci",ExternalID:"actor",CostUSD:auditFloat(1)}},
		{
			{Ledger:"apify_runs",LocalID:"same-local",Provider:"apify",AccountScope:"ci",ExternalID:"actor-one"},
			{Ledger:"apify_runs",LocalID:"same-local",Provider:"apify",AccountScope:"ci",ExternalID:"actor-two"},
		},
	}
	for i,c:=range cases {
		_,err:=expenseAuditReconcile(c)
		if !errors.Is(err,errExpenseAuditInput){t.Errorf("fixture %d: accepted bad data: %v",i,err)}
	}
}
