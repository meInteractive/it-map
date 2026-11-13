package workerengine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"itmap/internal/workerloop"
)

func TestMonitorNeedsAllFreshProfilesAndDoesNotMaskFailure(t *testing.T) {
	m := NewMonitor(time.Minute)
	now := time.Unix(100, 0)
	m.now = func() time.Time { return now }
	if s := m.Snapshot(); s.Live || s.Ready || s.Reason != "starting" {
		t.Fatal(s)
	}
	m.Record(workerloop.Launch, workerloop.ProfileWorked)
	m.Record(workerloop.Reconcile, workerloop.ProfileIdle)
	if s := m.Snapshot(); !s.Live || s.Ready {
		t.Fatal(s)
	}
	m.Record(workerloop.Pipeline, workerloop.ProfileIdle)
	if s := m.Snapshot(); !s.Live || !s.Ready {
		t.Fatal(s)
	}
	m.Record(workerloop.Launch, workerloop.ProfileFailed)
	m.Record(workerloop.Pipeline, workerloop.ProfileWorked)
	if s := m.Snapshot(); s.Ready || s.Reason != "profile_failed" {
		t.Fatal(s)
	}
	m.Record(workerloop.Launch, workerloop.ProfileIdle)
	if !m.Snapshot().Ready {
		t.Fatal("profile did not recover")
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if s := m.Snapshot(); s.Live || s.Ready || s.Reason != "stale" {
		t.Fatal(s)
	}
	m.Stop()
	m.Record(workerloop.Launch, workerloop.ProfileIdle)
	if s := m.Snapshot(); s.Live || s.Ready || s.Reason != "stopped" {
		t.Fatal(s)
	}
}

func TestMonitorConcurrentUpdatesAndStop(t *testing.T) {
	m := NewMonitor(time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				m.Record(workerloop.Launch, workerloop.ProfileIdle)
				_ = m.Snapshot()
			}
		}()
	}
	m.Stop()
	wg.Wait()
	if s := m.Snapshot(); s.Ready || s.Live || s.Reason != "stopped" {
		t.Fatal(s)
	}
}

func TestMonitorHandlerExposesOnlyFixedStates(t *testing.T) {
	m := NewMonitor(time.Minute)
	call := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		m.Handler().ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	if w := call(http.MethodGet, "/readyz"); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"reason":"starting"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, "/readyz"); w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code)
	}
	if w := call(http.MethodGet, "/other"); w.Code != http.StatusNotFound {
		t.Fatal(w.Code)
	}
	for _, name := range []workerloop.ProfileName{workerloop.Launch, workerloop.Reconcile, workerloop.Pipeline} {
		m.Record(name, workerloop.ProfileIdle)
	}
	if w := call(http.MethodGet, "/readyz"); w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
}
