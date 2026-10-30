package aiproviders

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"itmap/internal/integrations/aihttp"
)

type KeyStore interface {
	Read(context.Context, aihttp.Provider) (Settings, error)
	Resolve(context.Context, aihttp.Provider) (string, error)
	Generation(context.Context) (string, error)
	SaveKey(context.Context, aihttp.Provider, string, string) error
}
type ModelTransport interface {
	Models(context.Context) (json.RawMessage, error)
}
type OpenAIService struct {
	Store    KeyStore
	factory  func(string) (ModelTransport, error)
	mu       sync.Mutex
	cacheKey [32]byte
	cacheAt  time.Time
	cache    []Model
}

func NewOpenAIService(store KeyStore) *OpenAIService {
	return &OpenAIService{Store: store, factory: func(key string) (ModelTransport, error) { return aihttp.New(aihttp.OpenAI, key) }}
}
func (s *OpenAIService) Models(ctx context.Context, refresh bool) ([]Model, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStorage
	}
	key, err := s.Store.Resolve(ctx, aihttp.OpenAI)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(key))
	s.mu.Lock()
	if !refresh && s.cache != nil && s.cacheKey == hash && time.Since(s.cacheAt) < 6*time.Hour {
		b, _ := json.Marshal(s.cache)
		var copy []Model
		_ = json.Unmarshal(b, &copy)
		s.mu.Unlock()
		return copy, nil
	}
	s.mu.Unlock()
	factory := s.factory
	if factory == nil {
		factory = func(key string) (ModelTransport, error) { return aihttp.New(aihttp.OpenAI, key) }
	}
	client, err := factory(key)
	if err != nil {
		return nil, err
	}
	raw, err := client.Models(ctx)
	if err != nil {
		return nil, err
	}
	models, err := OpenAIModels(raw)
	if err != nil {
		return nil, &aihttp.Error{Code: "invalid_models", Status: 502}
	}
	s.mu.Lock()
	s.cacheKey = hash
	s.cacheAt = time.Now()
	b, _ := json.Marshal(models)
	_ = json.Unmarshal(b, &s.cache)
	s.mu.Unlock()
	return models, nil
}
func providerWarning(err error) string {
	switch {
	case errors.Is(err, ErrDecrypt):
		return "Не удалось расшифровать ключ OpenAI. Сохраните его заново."
	case errors.Is(err, ErrMissing):
		return "Сохраните API-ключ OpenAI во вкладке «Нейросети» → «ChatGPT»."
	}
	var e *aihttp.Error
	if errors.As(err, &e) {
		return e.Error()
	}
	return "Не удалось загрузить модели OpenAI."
}
func (s *OpenAIService) Public(ctx context.Context, refresh bool) (map[string]any, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStorage
	}
	settings, err := s.Store.Read(ctx, aihttp.OpenAI)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"configured": settings.Configured, "key_mask": settings.KeyMask, "requestedDefault": RequestedDefault, "models": []Model{}, "defaultModel": ""}
	if !settings.Configured {
		out["warning"] = "Сохраните API-ключ OpenAI, чтобы загрузить доступные модели."
		return out, nil
	}
	models, err := s.Models(ctx, refresh)
	if err != nil {
		if refresh {
			return nil, err
		}
		out["warning"] = providerWarning(err)
		return out, nil
	}
	out["models"] = models
	def := DefaultModel(models)
	out["defaultModel"] = def
	warning := "GPT-6.1 Luna отсутствует в доступных API-моделях."
	if def != "" {
		warning += " По умолчанию: " + ModelProfile(def).Name + "."
	}
	for _, m := range models {
		if m.ID == RequestedDefault {
			warning = ""
			break
		}
	}
	out["warning"] = warning
	return out, nil
}

func (s *OpenAIService) Generation(ctx context.Context) (string, error) {
	if s == nil || s.Store == nil {
		return "", ErrStorage
	}
	return s.Store.Generation(ctx)
}
func (s *OpenAIService) Save(ctx context.Context, generation string, body []byte) (map[string]any, error) {
	key, err := ParseKey(aihttp.OpenAI, body)
	if err != nil {
		return nil, err
	}
	if s == nil || s.Store == nil {
		return nil, ErrStorage
	}
	if err = s.Store.SaveKey(ctx, aihttp.OpenAI, generation, key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
	return map[string]any{"configured": true, "key_mask": "••••" + key[len(key)-4:]}, nil
}
