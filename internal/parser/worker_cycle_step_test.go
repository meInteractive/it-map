package parser

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"itmap/internal/apifysettings"
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

func TestStopClaimsSignal(t *testing.T) {
	if claimsStopped(nil) {
		t.Fatal("nil stop channel must allow work")
	}
	stop := make(chan struct{})
	if claimsStopped(stop) {
		t.Fatal("open channel stopped work")
	}
	close(stop)
	if !claimsStopped(stop) {
		t.Fatal("closed channel allowed work")
	}
}

func TestStoppedCycleDoesNotOpenRun(t *testing.T) {
	db := &sql.DB{}
	stop := make(chan struct{})
	close(stop)
	worker := Worker{Runs: RunRepository{DB: db}, Settings: apifysettings.Repository{DB: db}}
	result, err := worker.Cycle(context.Background(), WorkerOptions{MaxJobs: 1, StopClaims: stop})
	if !errors.Is(err, context.Canceled) || result.Processed != 0 || result.Run.ID != "" {
		t.Fatal("stopped cycle opened work", err)
	}
}
