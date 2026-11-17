package parser

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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

func TestTimeBudgetKeepsFirstJobAndProtectsSubsequentClaim(t *testing.T) {
	for _, tc := range []struct {
		name               string
		processed          int
		remaining, minimum time.Duration
		want               bool
	}{
		{"first job even on short cycle", 0, time.Second, time.Minute, true},
		{"legacy disabled", 1, time.Second, 0, true},
		{"enough time", 1, time.Minute, time.Minute, true},
		{"just below budget", 1, time.Minute - time.Nanosecond, time.Minute, false},
		{"negative remaining", 1, -time.Second, time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := enoughTimeForNextJob(tc.processed, tc.remaining, tc.minimum); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestCycleRejectsInvalidTimeBudgetBeforeDatabaseAccess(t *testing.T) {
	db := &sql.DB{}
	worker := Worker{Runs: RunRepository{DB: db}, Settings: apifysettings.Repository{DB: db}}
	for _, budget := range []time.Duration{-time.Second, 2 * time.Second} {
		result, err := worker.Cycle(context.Background(), WorkerOptions{Run: RunOptions{Duration: time.Second}, MaxJobs: 1, MinRemaining: budget})
		if !errors.Is(err, ErrRunInput) || result.Processed != 0 || result.Run.ID != "" {
			t.Fatal("invalid budget reached run", err)
		}
	}
}
