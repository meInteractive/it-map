package workerengine

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"itmap/internal/workerloop"
)

// Monitor отражает только локальный прогресс профилей; он не доказывает
// состояние внешних провайдеров, бюджет или полноту данных.
type Monitor struct {
	mu       sync.Mutex
	last     map[workerloop.ProfileName]profileHealth
	stopped  bool
	now      func() time.Time
	maxAge   time.Duration
	counters workerloop.ProfileCounters
}
type profileHealth struct {
	at      time.Time
	outcome workerloop.ProfileOutcome
}

type MonitorSnapshot struct {
	Live   bool   `json:"live"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason"`
}

// NewMonitor начинает в состоянии not ready; таймер сам по себе не отмечает прогресс.
func NewMonitor(maxAge time.Duration) *Monitor {
	if maxAge <= 0 {
		return nil
	}
	return &Monitor{last: make(map[workerloop.ProfileName]profileHealth), now: time.Now, maxAge: maxAge}
}

// Record принимает только ограниченные профили и исходы без пользовательских данных.
func (m *Monitor) Record(name workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
	if m == nil || (name != workerloop.Launch && name != workerloop.Reconcile && name != workerloop.Pipeline) ||
		(outcome != workerloop.ProfileWorked && outcome != workerloop.ProfileIdle && outcome != workerloop.ProfileFailed && outcome != workerloop.ProfileTimeout) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.last[name] = profileHealth{at: m.now(), outcome: outcome}
		m.counters.Record(name, outcome)
	}
}

// Stop необратимо снимает готовность после завершения worker.
func (m *Monitor) Stop() {
	if m != nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.stopped = true
	}
}

// Snapshot не выполняет сетевые проверки и не раскрывает ошибки адаптеров.
// Ошибка одного профиля не скрывается успехом другого.
func (m *Monitor) Snapshot() MonitorSnapshot {
	if m == nil {
		return MonitorSnapshot{Reason: "unavailable"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return MonitorSnapshot{Reason: "stopped"}
	}
	now := m.now()
	live := false
	for _, v := range m.last {
		age := now.Sub(v.at)
		if age >= 0 && age <= m.maxAge {
			live = true
		}
	}
	if !live {
		if len(m.last) == 0 {
			return MonitorSnapshot{Reason: "starting"}
		}
		return MonitorSnapshot{Reason: "stale"}
	}
	for _, name := range []workerloop.ProfileName{workerloop.Launch, workerloop.Reconcile, workerloop.Pipeline} {
		v, ok := m.last[name]
		if !ok {
			return MonitorSnapshot{Live: true, Reason: "starting"}
		}
		age := now.Sub(v.at)
		if age < 0 || age > m.maxAge {
			return MonitorSnapshot{Live: true, Reason: "stale"}
		}
		if v.outcome == workerloop.ProfileFailed || v.outcome == workerloop.ProfileTimeout {
			return MonitorSnapshot{Live: true, Reason: "profile_failed"}
		}
	}
	return MonitorSnapshot{Live: true, Ready: true, Reason: "ready"}
}

// Handler предназначен для loopback диагностики и возвращает только фиксированные поля.
func (m *Monitor) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && r.URL.Path != "/profilez" {
			http.NotFound(w, r)
			return
		}
		snapshot := m.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/profilez" {
			if m == nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("{\"error\":\"profile_unavailable\"}\n"))
				return
			}
			_ = json.NewEncoder(w).Encode(struct {
				Profiles []workerloop.ProfileStats `json:"profiles"`
			}{Profiles: m.counters.Snapshot()})
			return
		}
		if (r.URL.Path == "/healthz" && !snapshot.Live) || (r.URL.Path == "/readyz" && !snapshot.Ready) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
}

// HandlerWithQueue добавляет агрегаты только к уже ограниченному loopback HTTP.
// Не допускает одновременных тяжёлых снимков и не раскрывает ошибки БД.
func (m *Monitor) HandlerWithQueue(reader QueueReader) http.Handler {
	base := m.Handler()
	semaphore := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/queuez" {
			base.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if reader == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"error\":\"queue_unavailable\"}\n"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		result, err := reader.ReadQueue(ctx)
		if err != nil || ctx.Err() != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"error\":\"queue_unavailable\"}\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}
