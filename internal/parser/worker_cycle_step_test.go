package parser

import (
	"context"
	"errors"
	"testing"
)

func TestCycleStepCancellationDoesNotClaim(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	status, release := "completed", true
	result := WorkerResult{}
	phaseProcessed, hhStages := 0, 0
	processed, err := (Worker{}).cycleStep(context.Background(), ctx, "run", "pipeline", workerDispatch{}, "owner", &result, &phaseProcessed, &hhStages, &status, &release)
	if !errors.Is(err, context.Canceled) || processed || status != "interrupted" || !release || result.Processed != 0 || phaseProcessed != 0 || hhStages != 0 {
		t.Fatalf("cancelled step changed state: processed=%v err=%v status=%s release=%v result=%d phase=%d hh=%d", processed, err, status, release, result.Processed, phaseProcessed, hhStages)
	}
}
