package workerloop

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunCancelsIdleWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	called := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{IdleDelay: time.Hour, ErrorMin: time.Second, ErrorMax: time.Minute}, func(context.Context) (bool, error) { close(called); return false, nil })
	}()
	<-called
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt idle wait")
	}
}

func TestRunBacksOffErrorsAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	starts := make([]time.Time, 0, 3)
	boom := errors.New("unavailable")
	var reported int
	err := Run(ctx, Config{IdleDelay: time.Second, ErrorMin: 20 * time.Millisecond, ErrorMax: 40 * time.Millisecond, OnError: func(err error) {
		if errors.Is(err, boom) {
			reported++
		}
	}}, func(context.Context) (bool, error) {
		starts = append(starts, time.Now())
		if len(starts) == 3 {
			cancel()
			return false, nil
		}
		return false, boom
	})
	if !errors.Is(err, context.Canceled) || reported != 2 || len(starts) != 3 || starts[1].Sub(starts[0]) < 15*time.Millisecond || starts[2].Sub(starts[1]) < 30*time.Millisecond {
		t.Fatalf("backoff/cancellation failed: err=%v reported=%d starts=%v", err, reported, starts)
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	if !errors.Is(Run(context.Background(), Config{}, func(context.Context) (bool, error) { return false, nil }), ErrConfig) {
		t.Fatal("expected invalid config")
	}
}

func TestJitterStaysWithinBounds(t *testing.T) {
	for _, delay := range []time.Duration{time.Nanosecond, time.Second, time.Hour} {
		for i := 0; i < 1000; i++ {
			got := jitter(delay)
			if got < delay-delay/4 || got > delay {
				t.Fatalf("jitter(%v) = %v", delay, got)
			}
		}
	}
}
