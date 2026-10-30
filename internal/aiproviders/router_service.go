package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"time"
)

type ServiceError struct {
	Code    string
	Status  int
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

var routerSyncError = &ServiceError{Code: "sync", Status: 502, Message: "Не удалось обновить модели. Сохранён предыдущий список."}

type RouterStore interface {
	KeyStore
	ReadRouter(context.Context) (RouterSnapshot, error)
	ResolveRouter(context.Context, int64) (string, error)
	CacheRouter(context.Context, string, int64, []map[string]any, time.Time) error
	RouterConnection(context.Context, string, int64, string, string) error
	RouterLogs(context.Context) ([]map[string]any, error)
}
type RouterTransport interface {
	RankedModels(context.Context) (json.RawMessage, error)
	Benchmarks(context.Context) (json.RawMessage, error)
	Key(context.Context) (json.RawMessage, error)
}
type RouterService struct {
	Store   RouterStore
	factory func(string) (RouterTransport, error)
	now     func() time.Time
}

func NewRouterService(store RouterStore) *RouterService {
	return &RouterService{Store: store, factory: func(key string) (RouterTransport, error) { return aihttp.New(aihttp.OpenRouter, key) }, now: time.Now}
}
func routerWarning(e error) string {
	switch {
	case errors.Is(e, ErrDecrypt):
		return "Не удалось расшифровать ключ. Сохраните API-ключ заново."
	case errors.Is(e, ErrMissing):
		return "Укажите и сохраните API-ключ OpenRouter."
	case errors.Is(e, ErrChanged):
		return "Настройки изменились во время запроса. Повторите действие."
	}
	var provider *aihttp.Error
	if errors.As(e, &provider) {
		return provider.Error()
	}
	return "Не удалось обновить модели. Сохранён предыдущий список."
}
func (s *RouterService) Generation(ctx context.Context) (string, error) {
	if s == nil || s.Store == nil {
		return "", ErrStorage
	}
	return s.Store.Generation(ctx)
}
func routerData(raw json.RawMessage) ([]map[string]any, error) {
	var v struct {
		Data []map[string]any `json:"data"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Data == nil {
		return nil, routerSyncError
	}
	return v.Data, nil
}
func (s *RouterService) refresh(ctx context.Context, g string, old RouterSnapshot) error {
	key, e := s.Store.ResolveRouter(ctx, old.Revision)
	if e != nil {
		return e
	}
	client, e := s.factory(key)
	if e != nil {
		return e
	}
	type result struct {
		raw json.RawMessage
		e   error
	}
	models, bench := make(chan result, 1), make(chan result, 1)
	go func() { r, e := client.RankedModels(ctx); models <- result{r, e} }()
	go func() { r, e := client.Benchmarks(ctx); bench <- result{r, e} }()
	a, b := <-models, <-bench
	e = a.e
	if e == nil {
		e = b.e
	}
	var rows, benchmarks []map[string]any
	if e == nil {
		rows, e = routerData(a.raw)
	}
	if e == nil {
		benchmarks, e = routerData(b.raw)
	}
	if e == nil {
		now := s.now()
		return s.Store.CacheRouter(ctx, g, old.Revision, RankRouterModels(rows, benchmarks, now.UTC().Format("2006-01-02T15:04:05.000Z")), now)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	status, warning := old.Status, "Не удалось обновить модели. Сохранён предыдущий список."
	var provider *aihttp.Error
	if errors.As(e, &provider) && provider.Code == "invalid_key" {
		status = "error"
		warning = provider.Error()
	}
	if write := s.Store.RouterConnection(ctx, g, old.Revision, status, warning); write != nil {
		return write
	}
	return e
}
func (s *RouterService) public(ctx context.Context, g string, refresh bool) (map[string]any, error) {
	row, e := s.Store.ReadRouter(ctx)
	if e != nil {
		return nil, e
	}
	expired := row.SyncedAt == nil || *row.SyncedAt == ""
	if !expired {
		at, err := time.Parse(time.RFC3339Nano, *row.SyncedAt)
		expired = err == nil && s.now().Sub(at) > 6*time.Hour
	}
	if row.Configured && (refresh || expired) {
		if e = s.refresh(ctx, g, row); e != nil && (errors.Is(e, ErrChanged) || errors.Is(e, ErrGeneration) || ctx.Err() != nil) {
			return nil, e
		}
		row, e = s.Store.ReadRouter(ctx)
		if e != nil {
			return nil, e
		}
	}
	return row.Public(), nil
}
func (s *RouterService) Public(ctx context.Context, refresh bool) (map[string]any, error) {
	g, e := s.Generation(ctx)
	if e != nil {
		return nil, e
	}
	return s.public(ctx, g, refresh)
}
func (s *RouterService) Refresh(ctx context.Context) (map[string]any, error) {
	g, e := s.Generation(ctx)
	if e != nil {
		return nil, e
	}
	row, e := s.Store.ReadRouter(ctx)
	if e != nil {
		return nil, e
	}
	if e = s.refresh(ctx, g, row); e != nil {
		return nil, e
	}
	return s.public(ctx, g, false)
}
func (s *RouterService) Save(ctx context.Context, g string, body []byte) (map[string]any, error) {
	key, e := ParseKey(aihttp.OpenRouter, body)
	if e != nil {
		return nil, e
	}
	if e = s.Store.SaveKey(ctx, aihttp.OpenRouter, g, key); e != nil {
		return nil, e
	}
	return s.public(ctx, g, true)
}
func (s *RouterService) Check(ctx context.Context) (map[string]any, error) {
	g, e := s.Generation(ctx)
	if e != nil {
		return nil, e
	}
	old, e := s.Store.ReadRouter(ctx)
	if e != nil {
		return nil, e
	}
	key, e := s.Store.ResolveRouter(ctx, old.Revision)
	if e == nil {
		var client RouterTransport
		client, e = s.factory(key)
		if e == nil {
			_, e = client.Key(ctx)
		}
	}
	status, warning := "connected", ""
	if e != nil {
		status, warning = "error", routerWarning(e)
	}
	if !old.Configured && errors.Is(e, ErrMissing) {
		return nil, e
	}
	if write := s.Store.RouterConnection(ctx, g, old.Revision, status, warning); write != nil {
		return nil, write
	}
	if e != nil {
		return nil, e
	}
	return s.public(ctx, g, false)
}
func (s *RouterService) Logs(ctx context.Context) (map[string]any, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStorage
	}
	v, e := s.Store.RouterLogs(ctx)
	if e != nil {
		return nil, e
	}
	return map[string]any{"logs": v}, nil
}
