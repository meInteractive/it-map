package workerengine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/workerloop"
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

// Готовность всех профилей при пустой очереди не означает публикацию вакансий.
// В частности /activityz не выдаёт счётчики import операций за newVacancies.
func TestReadyAndEmptyQueueDoNotClaimNewVacancies(t *testing.T) {
	m := NewMonitor(time.Minute)
	for _, name := range []workerloop.ProfileName{workerloop.Launch, workerloop.Reconcile, workerloop.Pipeline} {
		m.Record(name, workerloop.ProfileIdle)
	}
	h := m.HandlerWithActivity(
		queueFunc(func(context.Context) (QueueSnapshot, error) { return QueueSnapshot{}, nil }),
		nil, nil,
		activityFunc(func(context.Context) (ActivitySnapshot, error) { return ActivitySnapshot{}, nil }),
	)
	read := func(path string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatal("unexpected diagnostic status", path, w.Code)
		}
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal("invalid diagnostic response", path, err)
		}
		return got
	}
	health := read("/readyz")
	if health["ready"] != true || health["reason"] != "ready" {
		t.Fatal("idle worker was not ready", health)
	}
	queue := read("/queuez")
	scan, okScan := queue["scan"].(map[string]any)
	pipe, okPipe := queue["pipeline"].(map[string]any)
	if !okScan || !okPipe || scan["due"] != float64(0) || pipe["running"] != float64(0) {
		t.Fatal("empty queue changed readiness semantics", queue)
	}
	activity := read("/activityz")
	if activity["pipelineCompleted"] != float64(0) ||
		activity["importCreateOperations"] != float64(0) ||
		activity["importUpdateOperations"] != float64(0) ||
		activity["importUnchangedOperations"] != float64(0) {
		t.Fatal("idle worker invented import or pipeline success", activity)
	}
	if _, ok := activity["newVacancies"]; ok {
		t.Fatal("activity falsely claims unique published vacancies")
	}
	if _, ok := activity["published"]; ok {
		t.Fatal("completed jobs must not be labelled publications")
	}
}

// GO-431: локальная готовность профилей, состояние очереди, историческая
// активность и неизвестный платный исход имеют разные области счёта.
// Их нельзя складывать в общий сигнал «новые вакансии опубликованы».
func TestGO431CrossEndpointDiagnosticsNeverInventImport(t *testing.T) {
	m := NewMonitor(time.Minute)
	for _, name := range []workerloop.ProfileName{workerloop.Launch, workerloop.Reconcile, workerloop.Pipeline} {
		m.Record(name, workerloop.ProfileIdle)
	}
	h := m.HandlerWithActivity(
		queueFunc(func(context.Context) (QueueSnapshot, error) {
			return QueueSnapshot{Scan: QueueGroup{Delayed: 1}}, nil
		}),
		recoveryFunc(func(context.Context) (RecoverySnapshot, error) {
			return RecoverySnapshot{CurrentAIZeroReserveProviderUnknown: 1}, nil
		}),
		hhScheduleFunc(func(context.Context) (HHScheduleSnapshot, error) {
			return HHScheduleSnapshot{Enabled: true, PendingHHScans: 1}, nil
		}),
		activityFunc(func(context.Context) (ActivitySnapshot, error) {
			return ActivitySnapshot{
				ScanCompleted: 2, PipelineCompleted: 4, PipelineFailed: 1,
				ImportUpdateOperations: 1,
			}, nil
		}),
	)
	read := func(path string, expectedStatus int) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != expectedStatus || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: unexpected status/cache: %d", path, response.Code)
		}
		var value map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatalf("%s: invalid JSON: %v", path, err)
		}
		return value
	}
	if ready := read("/readyz", http.StatusOK); ready["ready"] != true {
		t.Fatal("all recently idle profiles must remain ready", ready)
	}
	queue := read("/queuez", http.StatusOK)
	scan, ok := queue["scan"].(map[string]any)
	if !ok || scan["due"] != float64(0) || scan["delayed"] != float64(1) {
		t.Fatal("not-due HH scan misrepresented as ready work", queue)
	}
	if recovery := read("/recoveryz", http.StatusOK); recovery["currentAIZeroReserveProviderUnknown"] != float64(1) {
		t.Fatal("unknown paid outcome must remain visible independently of readiness", recovery)
	}
	if schedule := read("/hh-schedulez", http.StatusOK); schedule["pendingHHScans"] != float64(1) {
		t.Fatal("HH pending scan disappeared from its own diagnostic scope", schedule)
	}
	activity := read("/activityz", http.StatusOK)
	if activity["scanCompleted"] != float64(2) ||
		activity["pipelineCompleted"] != float64(4) ||
		activity["pipelineFailed"] != float64(1) ||
		activity["importCreateOperations"] != float64(0) ||
		activity["importUpdateOperations"] != float64(1) {
		t.Fatal("job completion was incorrectly attributed to new imports", activity)
	}
	for _, forbidden := range []string{"newVacancies", "published"} {
		if _, exists := activity[forbidden]; exists {
			t.Fatalf("activity advertised unsupported publication metric %q", forbidden)
		}
	}
	// Последняя ошибка профиля меняет readiness, но не переписывает
	// агрегаты событий и денежные свидетельства другого окна.
	m.Record(workerloop.Pipeline, workerloop.ProfileFailed)
	if ready := read("/readyz", http.StatusServiceUnavailable); ready["ready"] != false {
		t.Fatal("failed profile incorrectly reported ready", ready)
	}
	if after := read("/activityz", http.StatusOK); after["pipelineCompleted"] != float64(4) || after["importCreateOperations"] != float64(0) {
		t.Fatal("health state changed historical activity", after)
	}
}
