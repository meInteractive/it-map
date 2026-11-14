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

type queueFunc func(context.Context) (QueueSnapshot, error)

func (f queueFunc) ReadQueue(ctx context.Context) (QueueSnapshot, error) { return f(ctx) }

func TestQueueHandlerReturnsOnlyAggregatesAndRedactsErrors(t *testing.T) {
	m := NewMonitor(time.Minute)
	r := queueFunc(func(context.Context) (QueueSnapshot, error) {
		return QueueSnapshot{Scan: QueueGroup{Due: 2, Delayed: 1, OldestDueSeconds: 17}, Pipeline: QueueGroup{Running: 3, Failed: 4}}, nil
	})
	handler := m.HandlerWithQueue(r)
	call := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	w := call(http.MethodGet, "/queuez")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"due":2`) || !strings.Contains(w.Body.String(), `"oldestDueSeconds":17`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call(http.MethodPost, "/queuez"); w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code)
	}
	if w = call(http.MethodGet, "/readyz"); w.Code != http.StatusServiceUnavailable {
		t.Fatal(w.Code)
	}
	handler = m.HandlerWithQueue(queueFunc(func(context.Context) (QueueSnapshot, error) {
		return QueueSnapshot{}, errors.New("private dsn and payload")
	}))
	w = call(http.MethodGet, "/queuez")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "queue_unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
	handler = m.HandlerWithQueue(nil)
	if w = call(http.MethodGet, "/queuez"); w.Code != http.StatusNotFound {
		t.Fatal(w.Code)
	}
}

func TestQueueHandlerBoundsConcurrentQueries(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	handler := NewMonitor(time.Minute).HandlerWithQueue(queueFunc(func(ctx context.Context) (QueueSnapshot, error) {
		close(entered)
		select {
		case <-release:
			return QueueSnapshot{}, nil
		case <-ctx.Done():
			return QueueSnapshot{}, ctx.Err()
		}
	}))
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/queuez", nil))
		first <- w
	}()
	<-entered
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/queuez", nil))
	if second.Code != http.StatusServiceUnavailable {
		t.Fatal("overlapping snapshot accepted", second.Code)
	}
	close(release)
	select {
	case w := <-first:
		if w.Code != http.StatusOK {
			t.Fatal(w.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("first query stuck")
	}
}

func TestQueueReaderRejectsMissingConfiguration(t *testing.T) {
	if _, err := NewQueueReader(nil, "synthetic").ReadQueue(context.Background()); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := NewQueueReader(nil, "").ReadQueue(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}
