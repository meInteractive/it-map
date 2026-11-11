package workerhealth

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStateReadinessAndExpiry(t *testing.T) {
	s, err := New(10*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	s.now = func() time.Time { return now }
	check := func(live, ready bool, reason Reason) {
		t.Helper()
		if got := s.Snapshot(); got.Live != live || got.Ready != ready || got.Reason != reason {
			t.Fatalf("snapshot: %+v", got)
		}
	}
	check(false, false, ReasonStarting)
	s.Progress()
	check(true, false, ReasonDatabaseUnchecked)
	s.DatabaseCheck(false)
	check(true, false, ReasonDatabaseUnavailable)
	s.DatabaseCheck(true)
	check(true, true, ReasonReady)
	now = now.Add(5 * time.Second)
	check(true, true, ReasonReady)
	now = now.Add(time.Nanosecond)
	check(true, false, ReasonDatabaseStale)
	s.DatabaseCheck(true)
	now = now.Add(5*time.Second + time.Nanosecond)
	check(false, false, ReasonProgressStale)
	s.Progress()
	check(true, false, ReasonDatabaseStale)
	s.DatabaseCheck(true)
	check(true, true, ReasonReady)
	s.Stop()
	s.Progress()
	s.DatabaseCheck(true)
	check(false, false, ReasonStopped)
}

func TestStateRejectsInvalidFreshnessAndClockRollback(t *testing.T) {
	if _, err := New(0, time.Second); err == nil {
		t.Fatal("accepted zero progress freshness")
	}
	if _, err := New(time.Second, -1); err == nil {
		t.Fatal("accepted negative database freshness")
	}
	s, err := New(time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	s.now = func() time.Time { return now }
	s.Progress()
	s.DatabaseCheck(true)
	now = now.Add(-time.Second)
	if got := s.Snapshot(); got.Live || got.Ready || got.Reason != ReasonProgressStale {
		t.Fatalf("clock rollback: %+v", got)
	}
}

func TestConcurrentStateContainsOnlyFixedCodes(t *testing.T) {
	s, err := New(time.Minute, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				s.Progress()
				s.DatabaseCheck(j%2 == 0)
				b, err := json.Marshal(s.Snapshot())
				if err != nil || strings.Contains(string(b), "secret") {
					t.Errorf("unsafe snapshot: %s, %v", b, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
