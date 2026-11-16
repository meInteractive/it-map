package workerengine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type activityFunc func(context.Context) (ActivitySnapshot, error)

func (f activityFunc) ReadActivity(ctx context.Context) (ActivitySnapshot, error) { return f(ctx) }
func TestActivityEndpointShowsCountsAndRedactsErrors(t *testing.T) {
	m := NewMonitor(time.Minute)
	handler := m.HandlerWithActivity(nil, nil, nil, activityFunc(func(context.Context) (ActivitySnapshot, error) {
		return ActivitySnapshot{ActorStarted: 2, PipelineCompleted: 3}, nil
	}))
	call := func(method string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, "/activityz", nil))
		return w
	}
	w := call(http.MethodGet)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"actorStarted":2`) || !strings.Contains(w.Body.String(), `"pipelineCompleted":3`) {
		t.Fatal("unexpected response", w.Code, w.Body.String())
	}
	if w = call(http.MethodPost); w.Code != http.StatusMethodNotAllowed {
		t.Fatal("unexpected method accepted")
	}
	handler = m.HandlerWithActivity(nil, nil, nil, activityFunc(func(context.Context) (ActivitySnapshot, error) { return ActivitySnapshot{}, errors.New("private-dsn") }))
	if w = call(http.MethodGet); w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "activity_unavailable") {
		t.Fatal("error not redacted", w.Code, w.Body.String())
	}
}
func TestActivitySharesDiagnosticLimit(t *testing.T) {
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
	handler := NewMonitor(time.Minute).HandlerWithActivity(queue, nil, nil, activityFunc(func(context.Context) (ActivitySnapshot, error) {
		t.Error("concurrent activity query")
		return ActivitySnapshot{}, nil
	}))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/queuez", nil))
		close(done)
	}()
	<-entered
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/activityz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Error("concurrent query accepted", w.Code)
	}
	close(release)
	<-done
}
func TestActivityReaderRejectsMissingConfiguration(t *testing.T) {
	if _, err := NewActivityReader(nil, "test").ReadActivity(context.Background()); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := NewActivityReader(nil, "").ReadActivity(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}
