package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
	"strings"
)

const JEVModelID = "typesafe/jev-1.13"

type DecisionTransport interface {
	DecisionModels(context.Context) (json.RawMessage, error)
	Decisions(context.Context, json.RawMessage) (json.RawMessage, error)
}

func (s *RouterService) DecisionCatalog(ctx context.Context) (map[string]any, error) {
	if s == nil || s.Store == nil {
		return nil, ErrStorage
	}
	old, e := s.Store.ReadRouter(ctx)
	if e != nil {
		return nil, e
	}
	key, e := s.Store.ResolveRouter(ctx, old.Revision)
	if e != nil {
		return nil, e
	}
	client, e := s.factory(key)
	if e != nil {
		return nil, e
	}
	decisions, ok := client.(DecisionTransport)
	if !ok {
		return nil, ErrStorage
	}
	raw, e := decisions.DecisionModels(ctx)
	if e != nil {
		return nil, e
	}
	rows, e := routerData(raw)
	if e != nil {
		return nil, e
	}
	for _, row := range rows {
		if row["id"] != JEVModelID {
			continue
		}
		price := object(row["pricing"])
		prompt := number(price["prompt"])
		if prompt == nil || *prompt <= 0 {
			break
		}
		name := text(row["name"])
		if name == "" {
			name = "JEV"
		}
		var length any
		if n := number(row["context_length"]); n != nil {
			length = *n
		}
		return map[string]any{"id": JEVModelID, "name": name, "contextLength": length, "pricing": price, "source": "https://openrouter.ai/" + JEVModelID, "fetchedAt": s.now().UTC().Format("2006-01-02T15:04:05.000Z")}, nil
	}
	return nil, generationError("jev_unavailable", "Платная JEV отсутствует в каталоге OpenRouter. Выберите «Лучшая бесплатная» или повторите позже.", 409)
}

type ModelCatalog struct {
	OpenAI *OpenAIService
	Router *RouterService
}

func (c ModelCatalog) ValidateChoice(ctx context.Context, kind string, input profiles.ModelInput) error {
	if e := profiles.ValidateModel(kind, input); e != nil {
		return e
	}
	if input.Provider == "openai" {
		return c.OpenAI.ValidateChoice(ctx, kind, input)
	}
	if input.OpenRouterModel != "jev" {
		return nil
	}
	_, e := c.Router.DecisionCatalog(ctx)
	if e == nil {
		return nil
	}
	status, message := 503, routerWarning(e)
	var service *ServiceError
	var provider *aihttp.Error
	switch {
	case errors.As(e, &service):
		status, message = service.Status, service.Message
	case errors.As(e, &provider):
		status = provider.Status
	case errors.Is(e, ErrMissing), errors.Is(e, ErrDecrypt), errors.Is(e, ErrChanged), errors.Is(e, ErrGeneration):
		status = 409
	}
	return &profiles.InputError{Status: status, Message: message}
}

// Автоматический paid Decisions вызов обязательно проходит существующий ledger.
func (s *RouterService) AutomaticDecisions(ctx context.Context, b Billing, state any, questions map[string]any) (json.RawMessage, error) {
	return s.automaticDecisions(ctx, b, state, questions, nil, false)
}

func (s *RouterService) automaticDecisions(ctx context.Context, b Billing, state any, questions map[string]any, check func(context.Context) error, bounded bool) (json.RawMessage, error) {
	if b == nil {
		return nil, generationError("billing_required", "Учёт расходов JEV не подключён.", 503)
	}
	if len(questions) == 0 {
		return nil, &InputError{400, "Передайте вопросы JEV."}
	}
	if e := active(ctx, GenerationRequest{CheckActive: check}); e != nil {
		return nil, e
	}
	g, e := s.Generation(ctx)
	if e != nil {
		return nil, e
	}
	old, e := s.Store.ReadRouter(ctx)
	if e != nil {
		return nil, e
	}
	catalog, e := s.DecisionCatalog(ctx)
	if e != nil {
		return nil, e
	}
	key, e := s.Store.ResolveRouter(ctx, old.Revision)
	if e != nil {
		return nil, e
	}
	client, e := s.factory(key)
	if e != nil {
		return nil, e
	}
	transport, ok := client.(DecisionTransport)
	if !ok {
		return nil, ErrStorage
	}
	payload := map[string]any{"model": JEVModelID, "state": state, "questions": questions}
	raw, e := json.Marshal(payload)
	if e != nil {
		return nil, &InputError{400, "Некорректные данные JEV."}
	}
	if bounded {
		n := number(catalog["contextLength"])
		if n == nil || *n <= 0 || float64(len(raw)+4096) > *n {
			return nil, generationError("input_too_large", "Полный текст и вопросы ролей превышают безопасный размер запроса JEV. Данные сохранены без усечения.", 422)
		}
	}
	fence := func(ctx context.Context) error {
		if e := active(ctx, GenerationRequest{CheckActive: check}); e != nil {
			return e
		}
		latest, e := s.Generation(ctx)
		if e != nil {
			return e
		}
		if latest != g {
			return ErrGeneration
		}
		current, e := s.Store.ReadRouter(ctx)
		if e != nil {
			return e
		}
		if current.Revision != old.Revision {
			return ErrChanged
		}
		return nil
	}
	if e = fence(ctx); e != nil {
		return nil, e
	}
	meta, _ := json.Marshal(map[string]any{"provider": "openrouter", "model": JEVModelID, "pricing": catalog["pricing"], "contextLength": catalog["contextLength"], "payload": payload})
	response, e := b.Run(ctx, meta, func(ctx context.Context) (json.RawMessage, error) {
		if e := fence(ctx); e != nil {
			return nil, e
		}
		return transport.Decisions(ctx, raw)
	})
	if e != nil {
		var provider *aihttp.Error
		if errors.As(e, &provider) && provider.ProviderStatus == 402 {
			return nil, generationError("credits", "Недостаточно средств на балансе OpenRouter для JEV.", 402)
		}
		return nil, e
	}
	var v struct{ Model string }
	if json.Unmarshal(response, &v) != nil || v.Model != JEVModelID && !strings.HasPrefix(v.Model, JEVModelID+"-") {
		return nil, generationError("model_mismatch", "OpenRouter вернул другую модель вместо JEV.", 502)
	}
	if e = fence(ctx); e != nil {
		return nil, e
	}
	return response, nil
}
