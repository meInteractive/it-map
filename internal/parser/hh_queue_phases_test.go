package parser

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestHHQueuePhaseUsesReservedOperationIdentity(t *testing.T) {
	source := "hh"
	job := Job{ID: "CI_SCAN", Type: "scan_source", Status: "pending", SourceID: &source, Payload: json.RawMessage("{}")}
	now := int64(1780000000000)
	settings := DefaultSettings()
	settings.Enabled = true
	types, err := claimTypes([]string{"scan_source"})
	if err != nil {
		t.Fatal(err)
	}
	view := claimView{
		busy: map[int64]bool{}, pages: map[string]string{},
		operations: map[string]bool{}, goOperationKeys: map[string]string{},
		verifiedHH: map[hhCandidateKey]bool{}, blockedHHScans: map[string]bool{},
	}
	launch := ClaimOptions{VerifiedHHOnly: true, HHScanPhase: "launch"}
	reconcile := ClaimOptions{VerifiedHHOnly: true, HHScanPhase: "reconcile"}
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); !ok || err != nil {
		t.Fatal("unstarted scan must be launchable", err)
	}
	if ok, _, err := claimEligible(job, now, settings, reconcile, types, view); ok || err != nil {
		t.Fatal("unstarted scan cannot be reconciled", err)
	}
	// Нельзя определять оплаченный запуск по строке listing:job:page:
	// в БД сохранён только go398:<hash>, вычисленный от точного input.
	view.goOperationKeys[job.ID] = "go398:CI_HASH"
	view.operations["go398:CI_HASH"] = true
	view.lastListingStart = now - 1000
	if ok, page, err := claimEligible(job, now, settings, reconcile, types, view); !ok || !page || err != nil {
		t.Fatal("paid operation must be GET/reconcile only", err)
	}
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); ok || err != nil {
		t.Fatal("paid operation was eligible for another start", err)
	}
	delete(view.operations, "go398:CI_HASH")
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); ok || err != nil {
		t.Fatal("recent HH start must throttle the next scan", err)
	}
	view.lastListingStart = now - int64(hhActorLaunchCooldown/time.Millisecond) - 1
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); !ok || err != nil {
		t.Fatal("launch not released after cooldown", err)
	}
	view.lastListingStart = now - int64(hhActorLaunchCooldown/time.Millisecond)
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); !ok || err != nil {
		t.Fatal("ровно через 15 минут новый HH запуск должен допускаться", err)
	}
	view.lastListingStart++
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); ok || err != nil {
		t.Fatal("за миллисекунду до конца 15 минут запуск недопустим", err)
	}
	view.lastListingStart = now - int64(time.Hour/time.Millisecond)
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); !ok || err != nil {
		t.Fatal("не сохранять старый часовой cooldown после смены политики", err)
	}
	view.blockedHHScans[job.ID] = true
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); ok || err != nil {
		t.Fatal("unverified old cursor reached new launch", err)
	}
}

func TestQueuePhaseInputValidation(t *testing.T) {
	r := RunRepository{}
	for _, phase := range []string{"start", "Launch", "all"} {
		if _, err := r.ClaimJob(context.Background(), "", ClaimOptions{HHScanPhase: phase, VerifiedHHOnly: true}); err != ErrJobInput {
			t.Fatalf("invalid phase %s was not rejected: %v", phase, err)
		}
	}
	if _, err := r.ClaimJob(context.Background(), "", ClaimOptions{HHScanPhase: "launch", VerifiedHHOnly: false}); err != ErrJobInput {
		t.Fatal("unverified launch phase accepted", err)
	}
}
