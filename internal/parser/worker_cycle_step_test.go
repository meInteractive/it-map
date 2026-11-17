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

func TestTimeBudgetProtectsEveryClaim(t *testing.T) {
	for _, tc := range []struct {
		name               string
		remaining, minimum time.Duration
		want               bool
	}{
		{"first claim without budget", time.Second, time.Minute, false},
		{"manual profile without budget rule", time.Second, 0, true},
		{"enough time for first claim", time.Minute, time.Minute, true},
		{"subsequent claim just below budget", time.Minute - time.Nanosecond, time.Minute, false},
		{"expired cycle does not claim", -time.Second, time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := enoughTimeForNextJob(tc.remaining, tc.minimum); got != tc.want {
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


func TestPipelineWorkDeadlineLeavesCleanupReserve(t *testing.T) {
	now := time.Date(2026, time.November, 17, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		remaining  time.Duration
		admissible bool
	}{
		{"fresh_cycle", 100 * time.Second, true},
		{"just_above_reserve", pipelineCleanupReserve + time.Nanosecond, true},
		{"exact_reserve", pipelineCleanupReserve, false},
		{"less_than_reserve", pipelineCleanupReserve - time.Nanosecond, false},
		{"expired", -time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deadline := now.Add(tc.remaining)
			cutoff, ok := pipelineWorkDeadline(now, deadline)
			if ok != tc.admissible || !cutoff.Equal(deadline.Add(-pipelineCleanupReserve)) {
				t.Fatal("pipeline deadline did not preserve cleanup interval", cutoff, ok)
			}
		})
	}
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	cutoff, ok := pipelineWorkDeadline(time.Now(), parentDeadline)
	if !ok {
		t.Fatal("fresh cycle incorrectly rejected")
	}
	child, stopWork := context.WithDeadline(parent, cutoff)
	stopWork()
	if !errors.Is(child.Err(), context.Canceled) || parent.Err() != nil {
		t.Fatal("worker execution cancellation leaked to parent context")
	}
}
