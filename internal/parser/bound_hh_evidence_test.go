package parser

import (
	"context"
	"errors"
	"testing"

	"itmap/internal/apifysettings"
	"itmap/internal/consumercontrol"
	"itmap/internal/integrations/apify"
)

func TestBoundHHEvidenceRejectsUnboundOrStaleInput(t *testing.T) {
	var attempt *BoundAttempt
	v := apifysettings.OperationVersion{Generation: "11111111-2222-4333-8444-555555555555"}
	if _, err := attempt.InspectHHRecovery(context.Background(), v, "step", apify.HHOptions{}); !errors.Is(err, ErrJobInput) {
		t.Fatal("unbound attempt accepted", err)
	}
	attempt = &BoundAttempt{bound: &BoundRun{repo: RunRepository{Generation: "11111111-2222-4333-8444-555555555556"}}, lease: &JobLease{jobID: "CI"}, id: "CI"}
	if _, err := attempt.InspectHHRecovery(context.Background(), v, "step", apify.HHOptions{}); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("stale generation accepted", err)
	}
	if _, err := attempt.InspectHHRecovery(nil, v, "step", apify.HHOptions{}); !errors.Is(err, ErrJobInput) {
		t.Fatal("missing context accepted", err)
	}
}
