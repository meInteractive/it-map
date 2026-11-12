package workerloop

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
)

func TestProfileCountersConcurrentAndSafeSnapshot(t *testing.T) {
	var c ProfileCounters
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Record(Launch, ProfileWorked)
				_ = c.Snapshot()
			}
		}()
	}
	wg.Wait()
	c.Record(Pipeline, ProfileTimeout)
	c.Record(Reconcile, ProfileFailed)
	c.Record(Reconcile, ProfileIdle)
	c.Record("sensitive payload", ProfileWorked)
	c.Record(Launch, "sensitive error")
	s := c.Snapshot()
	if len(s) != 3 || s[0].Name != Launch || s[0].Worked != 1000 || s[1].Failed != 1 || s[1].Idle != 1 || s[2].TimedOut != 1 {
		t.Fatal("unexpected counters", s)
	}
	s[0].Worked = 0
	if c.Snapshot()[0].Worked != 1000 {
		t.Fatal("snapshot aliases state")
	}
	data, err := json.Marshal(c.Snapshot())
	if err != nil || strings.Contains(string(data), "sensitive") {
		t.Fatal("unsafe snapshot", err)
	}
}

func TestProfileCountersReceiveLoopOutcomes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var counters ProfileCounters
	launch := testProfile(Launch)
	launch.Step = func(context.Context) (bool, error) { return true, nil }
	launch.OnResult = counters.Record
	reconcile := testProfile(Reconcile)
	reconcile.Step = func(context.Context) (bool, error) { return false, nil }
	reconcile.OnResult = counters.Record
	pipeline := testProfile(Pipeline)
	pipeline.Step = func(context.Context) (bool, error) { return false, errors.New("private provider error") }
	pipeline.OnResult = func(name ProfileName, outcome ProfileOutcome) { counters.Record(name, outcome); cancel() }
	if err := RunProfiles(ctx, []Profile{launch, reconcile, pipeline}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	s := counters.Snapshot()
	if s[0].Worked != 1 || s[1].Idle != 1 || s[2].Failed != 1 {
		t.Fatal("incorrect outcomes", s)
	}
}

func TestCounterSaturates(t *testing.T) {
	v := uint64(math.MaxUint64)
	increment(&v)
	if v != math.MaxUint64 {
		t.Fatal("counter overflow")
	}
}
