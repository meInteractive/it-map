package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"reflect"
	"sync"
	"testing"
	"time"
)

type routerMemory struct {
	mu                                   sync.Mutex
	row                                  RouterSnapshot
	generation                           string
	key                                  string
	cacheWrites, connectionWrites, saves int
	stale                                bool
}

func (m *routerMemory) Read(context.Context, aihttp.Provider) (Settings, error) {
	return Settings{}, nil
}
func (m *routerMemory) Resolve(context.Context, aihttp.Provider) (string, error) { return m.key, nil }
func (m *routerMemory) Generation(context.Context) (string, error)               { return m.generation, nil }
func (m *routerMemory) ReadRouter(context.Context) (RouterSnapshot, error)       { return m.row, nil }
func (m *routerMemory) ResolveRouter(_ context.Context, v int64) (string, error) {
	if v != m.row.Revision {
		return "", ErrChanged
	}
	if m.key == "" {
		return "", ErrMissing
	}
	return m.key, nil
}
func (m *routerMemory) CacheRouter(_ context.Context, g string, v int64, rows []map[string]any, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stale || v != m.row.Revision {
		return ErrChanged
	}
	if g != m.generation {
		return ErrGeneration
	}
	m.row.Models = rows
	t := at.Format(time.RFC3339Nano)
	m.row.SyncedAt = &t
	m.row.Status = "connected"
	m.row.Warning = ""
	m.cacheWrites++
	return nil
}
func (m *routerMemory) RouterConnection(_ context.Context, g string, v int64, status, warning string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stale || v != m.row.Revision {
		return ErrChanged
	}
	if g != m.generation {
		return ErrGeneration
	}
	m.row.Status = status
	m.row.Warning = warning
	m.connectionWrites++
	return nil
}
func (m *routerMemory) SaveKey(_ context.Context, p aihttp.Provider, g, key string) error {
	if p != aihttp.OpenRouter || g != m.generation {
		return ErrGeneration
	}
	m.key = key
	m.row.Configured = true
	m.row.Revision++
	m.saves++
	return nil
}
func (m *routerMemory) RouterLogs(context.Context) ([]map[string]any, error) {
	return []map[string]any{}, nil
}

type routerMock struct {
	fail  bool
	calls *int
	mu    *sync.Mutex
}

func (m routerMock) RankedModels(context.Context) (json.RawMessage, error) {
	m.mu.Lock()
	*m.calls++
	m.mu.Unlock()
	if m.fail {
		return nil, errors.New("CI_PRIVATE_MARKER")
	}
	return []byte(`{"data":[{"id":"vendor/model","context_length":262144,"pricing":{"prompt":0,"completion":0},"supported_parameters":["reasoning"],"architecture":{"output_modalities":["text"]}}]}`), nil
}
func (m routerMock) Benchmarks(context.Context) (json.RawMessage, error) {
	return []byte(`{"data":[]}`), nil
}
func (m routerMock) Key(context.Context) (json.RawMessage, error) { return []byte(`{"data":{}}`), nil }
func TestRouterRefreshFailureAndFences(t *testing.T) {
	at := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	stamp := at.Format(time.RFC3339)
	m := &routerMemory{generation: "CI_GENERATION", key: "CI_KEY", row: RouterSnapshot{Configured: true, Status: "connected", Revision: 1, SyncedAt: &stamp, Models: []map[string]any{{"id": "old"}}}}
	s := NewRouterService(m)
	s.now = func() time.Time { return at }
	calls := 0
	var mu sync.Mutex
	fail := false
	s.factory = func(string) (RouterTransport, error) { return routerMock{fail: fail, calls: &calls, mu: &mu}, nil }
	if _, e := s.Public(context.Background(), false); e != nil || calls != 0 {
		t.Fatal("fresh GET called provider")
	}
	if _, e := s.Refresh(context.Background()); e != nil || calls != 1 || m.cacheWrites != 1 || m.row.Models[0]["id"] != "vendor/model" {
		t.Fatal("refresh failed", e)
	}
	old := m.row.Models
	fail = true
	if _, e := s.Refresh(context.Background()); e == nil || !reflect.DeepEqual(old, m.row.Models) || m.connectionWrites != 1 || m.row.Warning == "CI_PRIVATE_MARKER" {
		t.Fatal("provider failure replaced cache or leaked diagnostic")
	}
	m.stale = true
	fail = false
	if _, e := s.Refresh(context.Background()); !errors.Is(e, ErrChanged) || m.cacheWrites != 1 {
		t.Fatal("stale refresh wrote cache")
	}
	m.stale = false
	m.key = ""
	if _, e := s.Check(context.Background()); !errors.Is(e, ErrMissing) {
		t.Fatal("missing key check accepted")
	}
}
