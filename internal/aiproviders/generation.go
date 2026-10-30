package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
	"regexp"
	"strings"
)

type Message struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content"`
}
type AIResult struct {
	Model     string          `json:"model"`
	Message   Message         `json:"message"`
	Usage     json.RawMessage `json:"usage,omitempty"`
	Fallback  bool            `json:"fallback_used"`
	Attempted []string        `json:"attempted_models"`
}
type GenerationRequest struct {
	Task            string
	Instructions    string
	State           any
	Roles           []LeafRole
	Messages        []Message
	MaxTokens       int
	ResponseFormat  json.RawMessage
	Validate        func(string) bool
	ValidateContent func(string) error
	CheckActive     func(context.Context) error
}

// parser.Attempt реализует этот интерфейс. Запуск automatic без ledger запрещён.
type Billing interface {
	Run(context.Context, json.RawMessage, func(context.Context) (json.RawMessage, error)) (json.RawMessage, error)
}
type CompletionTransport interface {
	Responses(context.Context, json.RawMessage) (json.RawMessage, error)
	ChatCompletions(context.Context, json.RawMessage) (json.RawMessage, error)
}
type Generator struct {
	OpenAI  *OpenAIService
	Router  *RouterService
	factory func(aihttp.Provider, string) (CompletionTransport, error)
}

func NewGenerator(a *OpenAIService, r *RouterService) *Generator {
	return &Generator{OpenAI: a, Router: r, factory: func(p aihttp.Provider, key string) (CompletionTransport, error) { return aihttp.New(p, key) }}
}
func generationError(code, msg string, status int) error {
	return &ServiceError{Code: code, Message: msg, Status: status}
}
func active(ctx context.Context, r GenerationRequest) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if r.CheckActive != nil {
		return r.CheckActive(ctx)
	}
	return nil
}
func (g *Generator) Automatic(ctx context.Context, r GenerationRequest, c profiles.ModelInput, b Billing) (AIResult, error) {
	if b == nil {
		return AIResult{}, generationError("billing_required", "Учёт расходов автоматического запроса не подключён.", 503)
	}
	return g.run(ctx, r, c, b)
}
func (g *Generator) ConnectionTest(ctx context.Context, c profiles.ModelInput) (AIResult, error) {
	return g.run(ctx, GenerationRequest{Task: "connection_test", Messages: []Message{{Role: "user", Content: "Ответь одним словом: готово."}}, MaxTokens: 1024}, c, nil)
}
func (g *Generator) run(ctx context.Context, r GenerationRequest, c profiles.ModelInput, b Billing) (AIResult, error) {
	if e := active(ctx, r); e != nil {
		return AIResult{}, e
	}
	if g == nil || g.factory == nil {
		return AIResult{}, ErrStorage
	}
	if c.Provider == "openai" {
		return g.openAI(ctx, r, c, b)
	}
	if c.Provider == "openrouter" && c.OpenRouterModel == "jev" {
		if g.Router == nil {
			return AIResult{}, ErrStorage
		}
		return g.Router.runJEV(ctx, r, b)
	}
	if c.Provider != "openrouter" {
		return AIResult{}, generationError("model_unavailable", "Выбранный провайдер ещё не подключён.", 409)
	}
	return g.router(ctx, r, b)
}
func (g *Generator) invoke(ctx context.Context, p aihttp.Provider, model string, payload map[string]any, b Billing, key string) (json.RawMessage, error) {
	raw, e := json.Marshal(payload)
	if e != nil {
		return nil, generationError("input", "Некорректный запрос к модели.", 400)
	}
	client, e := g.factory(p, key)
	if e != nil {
		return nil, e
	}
	work := func(ctx context.Context) (json.RawMessage, error) {
		if p == aihttp.OpenAI {
			return client.Responses(ctx, raw)
		}
		return client.ChatCompletions(ctx, raw)
	}
	if b == nil {
		return work(ctx)
	}
	meta := map[string]any{"provider": p, "model": model, "payload": payload}
	if p == aihttp.OpenRouter {
		meta["free"] = true
	}
	request, e := json.Marshal(meta)
	if e != nil {
		return nil, ErrStorage
	}
	return b.Run(ctx, request, work)
}
func (g *Generator) openAI(ctx context.Context, r GenerationRequest, c profiles.ModelInput, b Billing) (AIResult, error) {
	if g.OpenAI == nil || g.OpenAI.Store == nil {
		return AIResult{}, ErrStorage
	}
	models, e := g.OpenAI.Models(ctx, false)
	if e != nil {
		return AIResult{}, e
	}
	id := c.OpenAIModel
	if id == "" {
		id = DefaultModel(models)
	}
	var selected *Model
	for i := range models {
		if models[i].ID == id {
			selected = &models[i]
			break
		}
	}
	if selected == nil {
		return AIResult{}, generationError("model_unavailable", "Выбранная модель OpenAI недоступна. Администратору нужно обновить список и выбрать модель.", 409)
	}
	effort := c.ReasoningEffort
	if c.OpenAIModel == "" {
		effort = "auto"
		for _, v := range selected.ReasoningEfforts {
			if v == "medium" {
				effort = "medium"
			}
		}
	}
	if effort != "auto" {
		valid := false
		for _, v := range ModelProfile(id).ReasoningEfforts {
			if v == effort {
				valid = true
			}
		}
		if !valid {
			return AIResult{}, &InputError{Status: 400, Message: "Этот уровень рассуждений не подтверждён для выбранной модели. Выберите «По умолчанию модели»."}
		}
	}
	messages := make([]Message, len(r.Messages))
	for i, m := range r.Messages {
		messages[i] = m
		if m.Role == "system" {
			messages[i].Role = "developer"
		}
	}
	tokens := r.MaxTokens
	if tokens < 32768 {
		tokens = 32768
	}
	payload := map[string]any{"model": id, "input": messages, "max_output_tokens": tokens, "store": false}
	if effort != "auto" {
		payload["reasoning"] = map[string]any{"effort": effort}
	}
	if len(r.ResponseFormat) > 0 {
		var format map[string]json.RawMessage
		if json.Unmarshal(r.ResponseFormat, &format) != nil {
			return AIResult{}, &InputError{Status: 400, Message: "Некорректная схема ответа."}
		}
		if string(format["type"]) == `"json_schema"` {
			var spec map[string]any
			if json.Unmarshal(format["json_schema"], &spec) != nil || spec == nil {
				return AIResult{}, &InputError{Status: 400, Message: "Некорректная схема ответа."}
			}
			mapped := map[string]any{"type": "json_schema"}
			for k, v := range spec {
				mapped[k] = v
			}
			payload["text"] = map[string]any{"format": mapped}
		}
	}
	if e = active(ctx, r); e != nil {
		return AIResult{}, e
	}
	key, e := g.OpenAI.Store.Resolve(ctx, aihttp.OpenAI)
	if e != nil {
		return AIResult{}, e
	}
	raw, e := g.invoke(ctx, aihttp.OpenAI, id, payload, b, key)
	if e != nil {
		return AIResult{}, e
	}
	if e = active(ctx, r); e != nil {
		return AIResult{}, e
	}
	var data struct {
		Status, Model string
		Output        []struct {
			Type, Role string
			Content    []struct{ Type, Text string }
		}
		Usage json.RawMessage
	}
	if json.Unmarshal(raw, &data) != nil {
		return AIResult{}, generationError("invalid_output", "OpenAI не вернул полный ответ в нужном формате. Результат не сохранён.", 502)
	}
	parts := []string{}
	refusal := false
	for _, m := range data.Output {
		if m.Type == "message" && m.Role == "assistant" {
			for _, v := range m.Content {
				if v.Type == "refusal" {
					refusal = true
				}
				if v.Type == "output_text" {
					parts = append(parts, v.Text)
				}
			}
		}
	}
	content := strings.Join(parts, "\n")
	if data.Status != "completed" || refusal || strings.TrimSpace(content) == "" || r.Validate != nil && !r.Validate(content) {
		return AIResult{}, generationError("invalid_output", "OpenAI не вернул полный ответ в нужном формате. Результат не сохранён.", 502)
	}
	if r.ValidateContent != nil {
		if e := r.ValidateContent(content); e != nil {
			return AIResult{}, e
		}
	}
	if data.Model != id && !regexp.MustCompile("^"+regexp.QuoteMeta(id)+`-\d{4}-\d{2}-\d{2}$`).MatchString(data.Model) {
		return AIResult{}, generationError("model_mismatch", "OpenAI вернул ответ другой модели. Результат не сохранён.", 502)
	}
	return AIResult{Model: data.Model, Message: Message{Content: content}, Usage: data.Usage, Attempted: []string{id}}, nil
}
func (g *Generator) router(ctx context.Context, r GenerationRequest, b Billing) (AIResult, error) {
	if g.Router == nil || g.Router.Store == nil {
		return AIResult{}, ErrStorage
	}
	if _, e := g.Router.Public(ctx, false); e != nil {
		return AIResult{}, e
	}
	s, e := g.Router.Store.ReadRouter(ctx)
	if e != nil {
		return AIResult{}, e
	}
	if !s.Configured {
		return AIResult{}, ErrMissing
	}
	if len(s.Models) == 0 {
		return AIResult{}, generationError("no_models", "Нет бесплатных текстовых моделей с контекстом от 262 144 токенов и поддержкой рассуждений.", 503)
	}
	key, e := g.Router.Store.ResolveRouter(ctx, s.Revision)
	if e != nil {
		return AIResult{}, e
	}
	candidates := s.Models
	structured := r.Task == "vacancy_extraction" || r.Task == "vacancy_role"
	if structured {
		filtered := []map[string]any{}
		for _, m := range candidates {
			if m["supports_structured_outputs"] == true {
				filtered = append(filtered, m)
			}
		}
		candidates = filtered
		if len(candidates) == 0 {
			return AIResult{}, generationError("no_structured_models", "Нет доступной бесплатной модели с поддержкой JSON Schema. Обновите модели или выберите OpenAI.", 409)
		}
		candidates = candidates[:1]
	}
	if (r.Task == "cover_letter" || r.Task == "relevance") && s.ActiveTest != nil {
		for i, m := range candidates {
			if text(m["id"]) == *s.ActiveTest {
				candidates = candidates[i:]
				break
			}
		}
	}
	primary := text(candidates[0]["id"])
	attempted := []string{}
	invalid := false
	for _, m := range candidates {
		id := text(m["id"])
		attempted = append(attempted, id)
		effort := "high"
		if r.Task == "cover_letter" || structured {
			effort = "medium"
		}
		reasonings := []map[string]any{{"effort": effort, "exclude": true}}
		if !structured {
			reasonings = append(reasonings, map[string]any{"enabled": true, "exclude": true})
		}
		for n, reasoning := range reasonings {
			if e = active(ctx, r); e != nil {
				return AIResult{Attempted: attempted}, e
			}
			tokens := r.MaxTokens
			if tokens == 0 {
				tokens = 4096
			}
			payload := map[string]any{"model": id, "messages": r.Messages, "reasoning": reasoning, "provider": map[string]any{"max_price": map[string]any{"prompt": 0, "completion": 0, "request": 0}, "require_parameters": true}, "max_tokens": tokens, "stream": false}
			if len(r.ResponseFormat) > 0 {
				payload["response_format"] = r.ResponseFormat
			}
			raw, e := g.invoke(ctx, aihttp.OpenRouter, id, payload, b, key)
			if e != nil {
				if ctx.Err() != nil {
					return AIResult{Attempted: attempted}, ctx.Err()
				}
				var p *aihttp.Error
				if errors.As(e, &p) {
					if p.Code == "invalid_key" || structured {
						return AIResult{Attempted: attempted}, e
					}
					if p.Code == "reasoning_parameter" && n == 0 {
						continue
					}
				}
				break
			}
			if e = active(ctx, r); e != nil {
				return AIResult{Attempted: attempted}, e
			}
			var result struct {
				Model   string
				Choices []struct {
					FinishReason string `json:"finish_reason"`
					Error        json.RawMessage
					Message      struct {
						Content string
						Role    string
						Refusal json.RawMessage
					}
				}
				Usage json.RawMessage
			}
			if json.Unmarshal(raw, &result) != nil || len(result.Choices) == 0 {
				break
			}
			choice := result.Choices[0]
			if baseID(result.Model) != baseID(id) && baseID(result.Model) != baseID(text(m["canonical_slug"])) {
				break
			}
			if truthyRaw(choice.Error) {
				break
			}
			if choice.FinishReason == "error" || choice.FinishReason == "content_filter" || choice.FinishReason == "length" {
				break
			}
			if truthyRaw(choice.Message.Refusal) {
				break
			}
			content := choice.Message.Content
			if strings.TrimSpace(content) == "" {
				break
			}
			if r.ValidateContent != nil {
				if e := r.ValidateContent(content); e != nil {
					if structured {
						return AIResult{Attempted: attempted}, e
					}
					break
				}
			}
			if r.Validate != nil && !r.Validate(content) {
				invalid = true
				break
			}
			return AIResult{Model: result.Model, Message: Message{Role: choice.Message.Role, Content: content}, Usage: result.Usage, Fallback: id != primary, Attempted: attempted}, nil
		}
	}
	if structured {
		code := "unavailable"
		if invalid {
			code = "invalid_output"
		}
		return AIResult{Attempted: attempted}, generationError(code, "Модель не вернула полный структурированный ответ. Markdown сохранён.", 502)
	}
	if r.Task == "relevance" && invalid {
		return AIResult{Attempted: attempted}, generationError("invalid_output", "Модели OpenRouter не вернули корректную оценку от 0 до 100. Прежняя оценка сохранена.", 502)
	}
	code, status, msg := "unavailable", 503, "Все подходящие модели сейчас недоступны. Повторите запрос позже."
	if invalid {
		code, status, msg = "invalid_output", 502, "Ни одна доступная модель не вернула письмо в нужном формате. Повторите попытку позже."
	}
	return AIResult{Attempted: attempted}, generationError(code, msg, status)
}

func truthyRaw(raw json.RawMessage) bool {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return false
	}
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != ""
	default:
		return true
	}
}
