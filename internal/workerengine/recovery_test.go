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

type recoveryFunc func(context.Context) (RecoverySnapshot, error)

func (f recoveryFunc) ReadRecovery(ctx context.Context) (RecoverySnapshot, error) { return f(ctx) }

func TestRecoveryHandlerReturnsAggregateAndHidesErrors(t *testing.T) {
	m := NewMonitor(time.Minute)
	handler := m.HandlerWithDiagnostics(nil, recoveryFunc(func(context.Context) (RecoverySnapshot, error) {
		return RecoverySnapshot{ActorUnknown: 2, SchedulerOrphanUnknown: 1, CurrentAIReservedOther: 3}, nil
	}))
	call := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	w := call(http.MethodGet, "/recoveryz")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"actorUnknown":2`) || !strings.Contains(w.Body.String(), `"schedulerOrphanUnknown":1`) || !strings.Contains(w.Body.String(), `"currentAIReservedOther":3`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call(http.MethodPost, "/recoveryz"); w.Code != http.StatusMethodNotAllowed {
		t.Fatal(w.Code)
	}
	if w = call(http.MethodGet, "/queuez"); w.Code != http.StatusNotFound {
		t.Fatal(w.Code)
	}
	handler = m.HandlerWithDiagnostics(nil, recoveryFunc(func(context.Context) (RecoverySnapshot, error) { return RecoverySnapshot{}, errors.New("private-dsn") }))
	w = call(http.MethodGet, "/recoveryz")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), "recovery_unavailable") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestDiagnosticsShareQueryLimit(t *testing.T) {
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
	recovery := recoveryFunc(func(context.Context) (RecoverySnapshot, error) {
		t.Error("recovery ran during queue query")
		return RecoverySnapshot{}, nil
	})
	handler := NewMonitor(time.Minute).HandlerWithDiagnostics(queue, recovery)
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/queuez", nil))
		close(done)
	}()
	<-entered
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/recoveryz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("overlapping snapshot accepted", w.Code)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queue query stuck")
	}
}
func TestRecoveryReaderRejectsMissingConfiguration(t *testing.T) {
	if _, err := NewRecoveryReader(nil, "synthetic").ReadRecovery(context.Background()); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := NewRecoveryReader(nil, "").ReadRecovery(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}
