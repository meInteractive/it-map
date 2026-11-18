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
	processed, err := (Worker{}).cycleStep(context.Background(), ctx, nil, 0, "run", "pipeline", workerDispatch{}, "owner", &result, &phaseProcessed, &hhStages, &status, &release)
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

// GO-431 P0: an eligible job can turn ineligible while ClaimJob is holding
// SQL locks. The second check gates the first provider I/O, not just claims.
func TestGO431PostClaimAdmissionRejectsLostTimeAndShutdown(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(100 * time.Second)
	cutoff := deadline.Add(-pipelineCleanupReserve)
	stop := make(chan struct{})
	if !admittedAfterClaim(now, deadline, cutoff, 60*time.Second, stop) {
		t.Fatal("fresh paid work should be admitted")
	}
	if !admittedAfterClaim(now, deadline, cutoff, 0, nil) {
		t.Fatal("manual cycle with time must retain its existing budget policy")
	}
	for _, tc := range []struct {
		name       string
		now        time.Time
		minimum    time.Duration
		workEnd    time.Time
		stopSignal <-chan struct{}
	}{
		{"lock_wait_consumed_minimum", now.Add(40*time.Second + time.Nanosecond), 60 * time.Second, cutoff, nil},
		{"exact_minimum_still_valid", now.Add(40 * time.Second), 60 * time.Second, cutoff, stop},
		{"provider_cutoff_reached", cutoff, 0, cutoff, nil},
		{"past_work_cutoff", cutoff.Add(time.Nanosecond), 0, cutoff, nil},
		{"cycle_deadline_passed", deadline.Add(time.Second), 0, deadline, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admitted := admittedAfterClaim(tc.now, deadline, tc.workEnd, tc.minimum, tc.stopSignal)
			want := tc.name == "exact_minimum_still_valid"
			if admitted != want {
				t.Fatalf("post-claim admission=%v want=%v", admitted, want)
			}
		})
	}
	close(stop)
	if admittedAfterClaim(now, deadline, cutoff, 60*time.Second, stop) {
		t.Fatal("worker shutdown signaled during ClaimJob permitted a new POST")
	}
	if admittedAfterClaim(now, deadline, cutoff, 0, stop) {
		t.Fatal("shutdown was ignored in manual zero-minimum cycle")
	}
}
