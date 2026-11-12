package workersupervisor

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"itmap/internal/workerconfig"
	"itmap/internal/workerhealth"
	"itmap/internal/workerloop"
)

type fakeSession struct {
	probe    func(context.Context) error
	recover  func(context.Context) error
	pipeline func(context.Context) (bool, error)
	close    func(context.Context) error
	closes   atomic.Int32
}

func (s *fakeSession) Probe(ctx context.Context) error {
	if s.probe != nil {
		return s.probe(ctx)
	}
	return nil
}
func (s *fakeSession) Recover(ctx context.Context) error {
	if s.recover != nil {
		return s.recover(ctx)
	}
	return nil
}
func (s *fakeSession) Pipeline(ctx context.Context) (bool, error) {
	if s.pipeline != nil {
		return s.pipeline(ctx)
	}
	return false, nil
}
func (s *fakeSession) Close(ctx context.Context) error {
	s.closes.Add(1)
	if s.close != nil {
		return s.close(ctx)
	}
	return nil
}

func enabled(key string) string {
	if key == "ITMAP_WORKER_ENABLED" || key == "ITMAP_PARSER_EXECUTION_ENABLED" {
		return "1"
	}
	return ""
}
func newOptions(t *testing.T, open Factory) Options {
	t.Helper()
	health, err := workerhealth.New(time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return Options{Getenv: enabled, Open: open, Health: health, Counters: &workerloop.ProfileCounters{}, RecoveryInterval: time.Hour, OpenTimeout: time.Second, CloseTimeout: time.Second, Profile: workerloop.Profile{Name: workerloop.Pipeline, WorkDelay: 10 * time.Millisecond, IdleDelay: 10 * time.Millisecond, ErrorMin: 10 * time.Millisecond, ErrorMax: 10 * time.Millisecond, StepTimeout: time.Second}}
}
func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for state")
		case <-ticker.C:
		}
	}
}

func TestDisabledAndInvalidConfigurationNeverOpen(t *testing.T) {
	var opened atomic.Int32
	open := func(context.Context) (Session, error) { opened.Add(1); return &fakeSession{}, nil }
	for _, key := range []string{"ITMAP_WORKER_ENABLED", "ITMAP_PARSER_EXECUTION_ENABLED"} {
		opt := newOptions(t, open)
		opt.Getenv = func(k string) string {
			if k == key {
				return "0"
			}
			return enabled(k)
		}
		if err := Run(context.Background(), opt); !errors.Is(err, workerconfig.ErrDisabled) {
			t.Fatal(err)
		}
		if opt.Health.Snapshot().Reason != workerhealth.ReasonStopped {
			t.Fatal("health not stopped")
		}
	}
	for _, mutate := range []func(*Options){
		func(o *Options) { o.Profile.StepTimeout = 0 },
		func(o *Options) { o.Profile.Name = workerloop.Launch },
		func(o *Options) { o.RecoveryInterval = 0 },
		func(o *Options) { o.CloseTimeout = 0 },
		func(o *Options) { o.OpenTimeout = 0 },
		func(o *Options) { o.Profile.Step = func(context.Context) (bool, error) { return true, nil } },
	} {
		opt := newOptions(t, open)
		mutate(&opt)
		if err := Run(context.Background(), opt); !errors.Is(err, workerloop.ErrConfig) {
			t.Fatal("invalid config accepted", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, newOptions(t, open)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if opened.Load() != 0 {
		t.Fatal("opened before enable/config validation")
	}
}

func TestRecoveryFailureBacksOffThenRunsAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var probes, recoveries, sessions atomic.Int32
	s := &fakeSession{}
	s.probe = func(context.Context) error { probes.Add(1); return nil }
	s.recover = func(context.Context) error {
		if recoveries.Add(1) == 1 {
			return errors.New("private database detail")
		}
		return nil
	}
	s.pipeline = func(ctx context.Context) (bool, error) {
		if sessions.Add(1) == 1 {
			return true, nil
		}
		<-ctx.Done()
		return false, ctx.Err()
	}
	opt := newOptions(t, func(context.Context) (Session, error) { return s, nil })
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt) }()
	await(t, func() bool { stats := opt.Counters.Snapshot(); return stats[2].Worked >= 1 && stats[2].Failed >= 1 })
	if probes.Load() < 2 || recoveries.Load() != 2 || sessions.Load() < 1 {
		t.Fatal("recovery sequence", probes.Load(), recoveries.Load(), sessions.Load())
	}
	if got := opt.Health.Snapshot(); !got.Ready || got.Reason != workerhealth.ReasonReady {
		t.Fatal("success not ready", got)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked")
	}
	if s.closes.Load() != 1 || opt.Health.Snapshot().Reason != workerhealth.ReasonStopped {
		t.Fatal("close/health", s.closes.Load())
	}
}

func TestTimeoutPreventsReadinessAndNextStage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var recovery, pipeline atomic.Int32
	s := &fakeSession{probe: func(ctx context.Context) error { <-ctx.Done(); return nil }, recover: func(context.Context) error { recovery.Add(1); return nil }, pipeline: func(context.Context) (bool, error) { pipeline.Add(1); return true, nil }}
	opt := newOptions(t, func(context.Context) (Session, error) { return s, nil })
	opt.Profile.StepTimeout = 20 * time.Millisecond
	opt.Profile.ErrorMin = time.Hour
	opt.Profile.ErrorMax = time.Hour
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt) }()
	await(t, func() bool { return opt.Counters.Snapshot()[2].TimedOut == 1 })
	if got := opt.Health.Snapshot(); got.Ready || got.Reason != workerhealth.ReasonDatabaseUnavailable {
		t.Fatal("late probe treated as ready", got)
	}
	if recovery.Load() != 0 || pipeline.Load() != 0 {
		t.Fatal("advanced after timeout")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPipelineTimeoutAndErrorsAreRedacted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &fakeSession{pipeline: func(ctx context.Context) (bool, error) { <-ctx.Done(); return true, nil }, close: func(context.Context) error { return errors.New("private credential") }}
	opt := newOptions(t, func(context.Context) (Session, error) { return s, nil })
	opt.Profile.StepTimeout = 20 * time.Millisecond
	opt.Profile.ErrorMin = time.Hour
	opt.Profile.ErrorMax = time.Hour
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt) }()
	await(t, func() bool { return opt.Counters.Snapshot()[2].TimedOut == 1 })
	if got := opt.Health.Snapshot(); got.Ready || got.Reason != workerhealth.ReasonWorkTimeout {
		t.Fatal("timeout marked ready", got)
	}
	cancel()
	err := <-done
	if !errors.Is(err, ErrClose) || strings.Contains(err.Error(), "private") {
		t.Fatal("close error leaked", err)
	}
	if s.closes.Load() != 1 || opt.Health.Snapshot().Reason != workerhealth.ReasonStopped {
		t.Fatal("not stopped")
	}
	opt2 := newOptions(t, func(context.Context) (Session, error) { return nil, errors.New("private credential") })
	if err := Run(context.Background(), opt2); !errors.Is(err, ErrOpen) || strings.Contains(err.Error(), "private") {
		t.Fatal("open error leaked", err)
	}
}

func TestFactoryFailureClosesPartialSession(t *testing.T) {
	s := &fakeSession{close: func(context.Context) error { return errors.New("private close detail") }}
	opt := newOptions(t, func(context.Context) (Session, error) { return s, errors.New("private open detail") })
	err := Run(context.Background(), opt)
	if !errors.Is(err, ErrOpen) || !errors.Is(err, ErrClose) || strings.Contains(err.Error(), "private") {
		t.Fatal("unredacted or missing failure", err)
	}
	if s.closes.Load() != 1 || opt.Health.Snapshot().Reason != workerhealth.ReasonStopped {
		t.Fatal("partial session not closed")
	}
}

func TestCloseDeadlineFailureIsReported(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &fakeSession{pipeline: func(context.Context) (bool, error) { cancel(); return false, nil }, close: func(ctx context.Context) error { <-ctx.Done(); return nil }}
	opt := newOptions(t, func(context.Context) (Session, error) { return s, nil })
	opt.CloseTimeout = 20 * time.Millisecond
	if err := Run(ctx, opt); !errors.Is(err, ErrClose) {
		t.Fatal("ignored close deadline", err)
	}
	if s.closes.Load() != 1 || opt.Health.Snapshot().Reason != workerhealth.ReasonStopped {
		t.Fatal("not stopped")
	}
}

func TestLateRecoveryCannotStartPipeline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var pipeline atomic.Int32
	s := &fakeSession{recover: func(ctx context.Context) error { <-ctx.Done(); return nil }, pipeline: func(context.Context) (bool, error) { pipeline.Add(1); return true, nil }}
	opt := newOptions(t, func(context.Context) (Session, error) { return s, nil })
	opt.Profile.StepTimeout = 20 * time.Millisecond
	opt.Profile.ErrorMin = time.Hour
	opt.Profile.ErrorMax = time.Hour
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt) }()
	await(t, func() bool { return opt.Counters.Snapshot()[2].TimedOut == 1 })
	if pipeline.Load() != 0 {
		t.Fatal("pipeline started after recovery deadline")
	}
	if got := opt.Health.Snapshot(); got.Ready || got.Reason != workerhealth.ReasonWorkTimeout {
		t.Fatal(got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLateFactoryClosesSessionWithoutStarting(t *testing.T) {
	var pipeline atomic.Int32
	s := &fakeSession{pipeline: func(context.Context) (bool, error) { pipeline.Add(1); return true, nil }}
	opt := newOptions(t, func(ctx context.Context) (Session, error) { <-ctx.Done(); return s, nil })
	opt.OpenTimeout = 20 * time.Millisecond
	if err := Run(context.Background(), opt); !errors.Is(err, ErrOpen) {
		t.Fatal("late open accepted", err)
	}
	if s.closes.Load() != 1 || pipeline.Load() != 0 || opt.Health.Snapshot().Reason != workerhealth.ReasonStopped {
		t.Fatal("late session not closed")
	}
}

func TestHealthStateCannotStartTwoSupervisors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var opens atomic.Int32
	s := &fakeSession{pipeline: func(ctx context.Context) (bool, error) { close(started); <-ctx.Done(); return false, ctx.Err() }}
	opt := newOptions(t, func(context.Context) (Session, error) { opens.Add(1); return s, nil })
	done := make(chan error, 1)
	go func() { done <- Run(ctx, opt) }()
	<-started
	if err := Run(context.Background(), opt); !errors.Is(err, workerloop.ErrConfig) {
		t.Fatal("second supervisor accepted", err)
	}
	if opt.Health.Snapshot().Reason == workerhealth.ReasonStopped {
		t.Fatal("rejected supervisor stopped active health")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := Run(context.Background(), opt); !errors.Is(err, workerloop.ErrConfig) {
		t.Fatal("stopped state reused", err)
	}
	if opens.Load() != 1 || s.closes.Load() != 1 {
		t.Fatal("duplicate open/close", opens.Load(), s.closes.Load())
	}
}
