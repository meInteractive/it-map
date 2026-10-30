package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/profiles"
	"testing"
)

func TestProfileOpenAIModelChoice(t *testing.T) {
	store := &serviceStore{key: "CI_KEY", configured: true}
	s := NewOpenAIService(store)
	calls := 0
	catalog := `{"data":[{"id":"gpt-6-luna"},{"id":"gpt-5-custom"}]}`
	s.factory = func(string) (ModelTransport, error) {
		return modelClient(func(context.Context) (json.RawMessage, error) { calls++; return []byte(catalog), nil }), nil
	}
	cfg := profiles.ModelInput{Provider: "openai", OpenAIModel: "gpt-6-luna", OpenRouterModel: "best_free", ReasoningEffort: "medium"}
	if e := s.ValidateChoice(context.Background(), "cover_letter", cfg); e != nil {
		t.Fatal(e)
	}
	cfg.OpenAIModel = "gpt-5-custom"
	before := calls
	if e := s.ValidateChoice(context.Background(), "cover_letter", cfg); e == nil || calls != before {
		t.Fatal("unknown effort was invented or called provider")
	}
	cfg.ReasoningEffort = "auto"
	if e := s.ValidateChoice(context.Background(), "cover_letter", cfg); e != nil {
		t.Fatal(e)
	}
	cfg.OpenAIModel = "gpt-5-unavailable"
	var input *profiles.InputError
	if e := s.ValidateChoice(context.Background(), "cover_letter", cfg); !errors.As(e, &input) || input.Status != 400 {
		t.Fatal("missing catalog model accepted")
	}
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	catalog = `{"data":[]}`
	cfg.OpenAIModel = ""
	if e := s.ValidateChoice(context.Background(), "cover_letter", cfg); !errors.As(e, &input) || input.Status != 409 {
		t.Fatal("empty catalog accepted")
	}
	cfg.Provider = "openrouter"
	cfg.OpenRouterModel = "jev"
	if e := s.ValidateChoice(context.Background(), "relevance", cfg); !errors.As(e, &input) || input.Status != 503 {
		t.Fatal("unported JEV accepted")
	}
}
