package workerengine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/parser"
)

type hhScheduleFunc func(context.Context) (HHScheduleSnapshot, error)

func (f hhScheduleFunc) ReadHHSchedule(ctx context.Context) (HHScheduleSnapshot, error) {
	return f(ctx)
}

func TestHHScheduleEndpointAggregateAndRedactedErrors(t *testing.T) {
	m := NewMonitor(time.Minute)
	handler := m.HandlerWithHHSchedule(nil, nil, hhScheduleFunc(func(context.Context) (HHScheduleSnapshot, error) {
		return HHScheduleSnapshot{PendingBreakdown: parser.HHPendingBreakdown{LaunchCooldown: 4, UnverifiedCursor: 1}}, nil
	}))
	call := func(method string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "/hh-schedulez", nil))
		return w
	}
	w := call(http.MethodGet)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"launchCooldown":4`) || !strings.Contains(w.Body.String(), `"unverifiedCursor":1`) {
		t.Fatal("missing aggregate", w.Code, w.Body.String())
	}
	if w = call(http.MethodPost); w.Code != http.StatusMethodNotAllowed {
		t.Fatal("method accepted", w.Code)
	}
	handler = m.HandlerWithHHSchedule(nil, nil, hhScheduleFunc(func(context.Context) (HHScheduleSnapshot, error) {
		return HHScheduleSnapshot{}, errors.New("private credential")
	}))
	w = call(http.MethodGet)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "hh_schedule_unavailable") {
		t.Fatal("error not redacted", w.Code, w.Body.String())
	}
}

func TestHHScheduleSharesDiagnosticsLimit(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	queue := queueFunc(func(ctx context.Context) (QueueSnapshot, error) {
		close(entered)
		select {
		case <-release:
			return QueueSnapshot{}, nil
		case <-ctx.Done():
			return QueueSnapshot{}, ctx.Err()
		}
	})
	handler := NewMonitor(time.Minute).HandlerWithHHSchedule(queue, nil, hhScheduleFunc(func(context.Context) (HHScheduleSnapshot, error) {
		t.Error("concurrent schedule query")
		return HHScheduleSnapshot{}, nil
	}))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/queuez", nil))
		close(done)
	}()
	<-entered
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hh-schedulez", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("concurrent query accepted", w.Code)
	}
	close(release)
	<-done
}

func TestHHScheduleReaderRejectsMissingConfiguration(t *testing.T) {
	if _, err := NewHHScheduleReader(nil, "test").ReadHHSchedule(context.Background()); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := NewHHScheduleReader(nil, "").ReadHHSchedule(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}
