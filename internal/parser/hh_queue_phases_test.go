package parser

import "testing"

func TestHHQueuePhaseUsesReservedOperationIdentity(t *testing.T) {
	source := "hh"
	job := Job{ID: "CI_SCAN", Type: "scan_source", Status: "pending", SourceID: &source}
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
	view.lastListingStart = now - 3600001
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); !ok || err != nil {
		t.Fatal("launch not released after cooldown", err)
	}
	view.blockedHHScans[job.ID] = true
	if ok, _, err := claimEligible(job, now, settings, launch, types, view); ok || err != nil {
		t.Fatal("unverified old cursor reached new launch", err)
	}
}

func TestQueuePhaseInputValidation(t *testing.T) {
	r := RunRepository{}
	for _, phase := range []string{"start", "Launch", "all"} {
		if _, err := r.ClaimJob(nil, "", ClaimOptions{HHScanPhase: phase, VerifiedHHOnly: true}); err != ErrJobInput {
			t.Fatalf("invalid phase %s was not rejected: %v", phase, err)
		}
	}
	if _, err := r.ClaimJob(nil, "", ClaimOptions{HHScanPhase: "launch", VerifiedHHOnly: false}); err != ErrJobInput {
		t.Fatal("unverified launch phase accepted", err)
	}
}
