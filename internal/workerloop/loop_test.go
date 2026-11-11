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

func TestWakeupInterruptsIdleAndClosedChannelDoesNotSpin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wake := make(chan struct{}, 1)
	calls := make(chan struct{}, 3)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{IdleDelay: time.Hour, ErrorMin: time.Second, ErrorMax: time.Second, Wakeup: wake, WakeupMinInterval: time.Millisecond}, func(context.Context) (bool, error) {
			calls <- struct{}{}
			return false, nil
		})
	}()
	<-calls
	wake <- struct{}{}
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("wakeup did not interrupt idle wait")
	}
	close(wake)
	select {
	case <-calls:
		t.Fatal("closed wakeup caused an extra iteration")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWakeupDoesNotBypassErrorBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	wake := make(chan struct{}, 1)
	called := make(chan struct{}, 2)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Config{IdleDelay: time.Hour, ErrorMin: time.Hour, ErrorMax: time.Hour, Wakeup: wake, WakeupMinInterval: time.Millisecond}, func(context.Context) (bool, error) {
			called <- struct{}{}
			return false, errors.New("unavailable")
		})
	}()
	<-called
	wake <- struct{}{}
	select {
	case <-called:
		t.Fatal("wakeup bypassed error backoff")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWakeupBurstIsRateLimited(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	wake := make(chan struct{})
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case wake <- struct{}{}:
			case <-done:
				return
			}
		}
	}()
	calls := 0
	err := Run(ctx, Config{IdleDelay: time.Hour, ErrorMin: time.Second, ErrorMax: time.Second, Wakeup: wake, WakeupMinInterval: 20 * time.Millisecond}, func(context.Context) (bool, error) {
		calls++
		return false, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || calls < 2 || calls > 9 {
		t.Fatalf("wakeup burst: err=%v calls=%d", err, calls)
	}
}

func TestWakeupRequiresPositiveInterval(t *testing.T) {
	wake := make(chan struct{})
	err := Run(context.Background(), Config{IdleDelay: time.Second, ErrorMin: time.Second, ErrorMax: time.Second, Wakeup: wake}, func(context.Context) (bool, error) { return false, nil })
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("missing wakeup interval: %v", err)
	}
}

func TestWakeupLimitDoesNotBlockPolling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wake := make(chan struct{}, 1)
	wake <- struct{}{}
	calls := 0
	err := Run(ctx, Config{IdleDelay: 20 * time.Millisecond, ErrorMin: time.Second, ErrorMax: time.Second, Wakeup: wake, WakeupMinInterval: time.Hour}, func(context.Context) (bool, error) {
		calls++
		if calls == 3 {
			cancel()
		}
		return false, nil
	})
	if !errors.Is(err, context.Canceled) || calls != 3 {
		t.Fatalf("polling stopped: err=%v calls=%d", err, calls)
	}
}
