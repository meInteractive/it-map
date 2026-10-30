package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"strings"
	"testing"
)

type serviceStore struct {
	key        string
	err        error
	configured bool
}

func (s *serviceStore) Read(context.Context, aihttp.Provider) (Settings, error) {
	return Settings{Configured: s.configured, KeyMask: "••••xxxx"}, nil
}
func (s *serviceStore) Resolve(context.Context, aihttp.Provider) (string, error)       { return s.key, s.err }
func (s *serviceStore) Generation(context.Context) (string, error)                     { return "", nil }
func (s *serviceStore) SaveKey(context.Context, aihttp.Provider, string, string) error { return nil }

type modelClient func(context.Context) (json.RawMessage, error)

func (c modelClient) Models(ctx context.Context) (json.RawMessage, error) { return c(ctx) }
func TestOpenAIServiceCacheRotationAndErrors(t *testing.T) {
	store := &serviceStore{key: "CI_KEY_1", configured: true}
	s := NewOpenAIService(store)
	calls := 0
	s.factory = func(key string) (ModelTransport, error) {
		return modelClient(func(context.Context) (json.RawMessage, error) {
			calls++
			return []byte(`{"data":[{"id":"gpt-6-luna"}]}`), nil
		}), nil
	}
	for i := 0; i < 2; i++ {
		m, e := s.Models(context.Background(), false)
		if e != nil || len(m) != 1 {
			t.Fatal(e)
		}
		m[0].Name = "caller mutated"
		m[0].ReasoningEfforts[0] = "caller mutated"
	}
	if calls != 1 {
		t.Fatal("cache not reused")
	}
	m, e := s.Models(context.Background(), false)
	if e != nil || m[0].Name == "caller mutated" || m[0].ReasoningEfforts[0] == "caller mutated" {
		t.Fatal("caller corrupted shared cache")
	}
	store.key = "CI_KEY_2"
	_, e = s.Models(context.Background(), false)
	if e != nil || calls != 2 {
		t.Fatal("rotated key reused old cache")
	}
	_, e = s.Models(context.Background(), true)
	if e != nil || calls != 3 {
		t.Fatal("explicit refresh ignored")
	}
	store.err = ErrDecrypt
	out, e := s.Public(context.Background(), false)
	if e != nil || !strings.Contains(out["warning"].(string), "расшифровать") {
		t.Fatal("stored decrypt failure hidden")
	}
	if _, e = s.Public(context.Background(), true); !errors.Is(e, ErrDecrypt) {
		t.Fatal("refresh swallowed decrypt failure")
	}
	store.err = nil
	store.configured = false
	out, e = s.Public(context.Background(), false)
	if e != nil || out["configured"] != false || calls != 3 {
		t.Fatal("unconfigured GET called provider")
	}
}
