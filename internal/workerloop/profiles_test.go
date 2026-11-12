package workerloop

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func testProfile(name ProfileName) Profile {
	return Profile{Name: name, Step: func(context.Context) (bool, error) { return true, nil }, WorkDelay: time.Second, IdleDelay: 10 * time.Second, ErrorMin: 2 * time.Second, ErrorMax: 8 * time.Second}
}

func TestProfilePlanFairnessAndIndependentBackoff(t *testing.T) {
	p, err := newProfilePlan([]Profile{testProfile(Launch), testProfile(Reconcile), testProfile(Pipeline)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	for _, want := range []ProfileName{Launch, Reconcile, Pipeline} {
		i, _ := p.nextReady(now)
		if i < 0 || p.slots[i].profile.Name != want {
			t.Fatalf("expected %s", want)
		}
		p.complete(i, now, true, want == Reconcile)
	}
	if i, wait := p.nextReady(now); i != -1 || wait != time.Second {
		t.Fatalf("unexpected wait %d %v", i, wait)
	}
	now = now.Add(time.Second)
	for _, want := range []ProfileName{Launch, Pipeline} {
		i, _ := p.nextReady(now)
		if i < 0 || p.slots[i].profile.Name != want {
			t.Fatalf("error backoff blocked %s", want)
		}
		p.complete(i, now, true, false)
	}
	now = now.Add(time.Second)
	// При постоянной работе launch очередь reconcile не голодает.
	seen := map[ProfileName]bool{}
	for range 3 {
		i, _ := p.nextReady(now)
		if i < 0 {
			t.Fatal("ready queue missed")
		}
		name := p.slots[i].profile.Name
		seen[name] = true
		p.complete(i, now, true, name == Reconcile)
	}
	if len(seen) != 3 {
		t.Fatal("ready profile starved", seen)
	}
	if p.slots[1].backoff != 8*time.Second {
		t.Fatal("backoff did not double", p.slots[1].backoff)
	}
	// Третья ошибка ограничивается ErrorMax; успех сбрасывает backoff.
	p.complete(1, now, true, true)
	if p.slots[1].backoff != 8*time.Second {
		t.Fatal("backoff exceeded maximum")
	}
	p.complete(1, now, false, false)
	if p.slots[1].backoff != 2*time.Second || !p.slots[1].next.Equal(now.Add(10*time.Second)) {
		t.Fatal("idle/reset failed")
	}
}

func TestProfilesRejectInvalidConfiguration(t *testing.T) {
	valid := testProfile(Launch)
	for _, profiles := range [][]Profile{nil, {valid, valid}, {{Name: "other", Step: valid.Step, WorkDelay: time.Second, IdleDelay: time.Second, ErrorMin: time.Second, ErrorMax: time.Second}}, {{Name: Launch, Step: valid.Step, WorkDelay: 0, IdleDelay: time.Second, ErrorMin: time.Second, ErrorMax: time.Second}}, {{Name: Launch, Step: valid.Step, WorkDelay: time.Second, IdleDelay: time.Second, ErrorMin: time.Second, ErrorMax: time.Millisecond}}} {
		if _, err := newProfilePlan(profiles); !errors.Is(err, ErrConfig) {
			t.Fatal("invalid config accepted", err)
		}
	}
	if err := RunProfiles(nil, []Profile{valid}, nil); !errors.Is(err, ErrConfig) {
		t.Fatal(err)
	}
	for _, change := range []func(*Profile){
		func(p *Profile) { p.WorkDelay = time.Nanosecond },
		func(p *Profile) { p.IdleDelay = time.Nanosecond },
		func(p *Profile) { p.ErrorMin = time.Nanosecond },
		func(p *Profile) { p.ErrorMax = 25 * time.Hour },
	} {
		p := valid
		change(&p)
		if _, err := newProfilePlan([]Profile{p}); !errors.Is(err, ErrConfig) {
			t.Fatal("unbounded polling accepted", err)
		}
	}
}

func TestRunProfilesSequentialAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active atomic.Int32
	var count atomic.Int32
	forProfile := func(name ProfileName) Profile {
		p := testProfile(name)
		p.Step = func(ctx context.Context) (bool, error) {
			if active.Add(1) != 1 {
				t.Error("steps overlap")
			}
			defer active.Add(-1)
			if count.Add(1) == 3 {
				cancel()
			}
			return true, nil
		}
		return p
	}
	err := RunProfiles(ctx, []Profile{forProfile(Launch), forProfile(Reconcile), forProfile(Pipeline)}, nil)
	if !errors.Is(err, context.Canceled) || count.Load() != 3 {
		t.Fatal("cancellation/sequence", err, count.Load())
	}
}

func TestProfilesCancellationDuringIdleAndFailureIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := make(chan ProfileName, 3)
	fail := testProfile(Launch)
	fail.ErrorMin, fail.ErrorMax = time.Hour, time.Hour
	fail.Step = func(context.Context) (bool, error) {
		called <- Launch
		return false, errors.New("sensitive provider error")
	}
	idle := testProfile(Reconcile)
	idle.IdleDelay = time.Hour
	idle.Step = func(context.Context) (bool, error) { called <- Reconcile; return false, nil }
	reported := make(chan ProfileName, 1)
	done := make(chan error, 1)
	go func() { done <- RunProfiles(ctx, []Profile{fail, idle}, func(name ProfileName) { reported <- name }) }()
	for _, want := range []ProfileName{Launch, Reconcile} {
		select {
		case got := <-called:
			if got != want {
				t.Fatal("unexpected order", got)
			}
		case <-time.After(time.Second):
			t.Fatal("profile blocked")
		}
	}
	select {
	case got := <-reported:
		if got != Launch {
			t.Fatal(got)
		}
	default:
		t.Fatal("missing failure")
	}
	select {
	case <-called:
		t.Fatal("busy loop")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle wait ignored cancellation")
	}
}

func TestProfilesCancellationReachesActiveStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan error, 1)
	p := testProfile(Launch)
	p.Step = func(ctx context.Context) (bool, error) {
		close(started)
		<-ctx.Done()
		return false, ctx.Err()
	}
	go func() { done <- RunProfiles(ctx, []Profile{p}, nil) }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("active step ignored cancellation")
	}
}
