// Package aihttp содержит изолированные HTTP-клиенты OpenAI и OpenRouter.
// Конструктор не читает env/БД и не запускает провайдеров или фоновые задания.
package aihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Provider string

const (
	OpenAI     Provider = "openai"
	OpenRouter Provider = "openrouter"
)
const maxResponseBytes = 8 << 20
const maxRequestBytes = 2 << 20

type Error struct {
	Code           string
	Status         int
	ProviderStatus int
	provider       Provider
}

func (e *Error) Error() string {
	if e.provider == OpenRouter {
		switch e.Code {
		case "missing_key":
			return "Укажите и сохраните API-ключ OpenRouter."
		case "invalid_key":
			return "OpenRouter отклонил API-ключ. Проверьте ключ и сохраните его заново."
		case "network":
			return "OpenRouter не ответил вовремя. Повторите попытку."
		}
		return "Ошибка подключения к OpenRouter."
	}
	switch e.Code {
	case "provider_401":
		return "OpenAI отклонил API-ключ. Проверьте серверное подключение."
	case "provider_403":
		return "Доступ к модели OpenAI запрещён для этого проекта."
	case "provider_404":
		return "Выбранная модель OpenAI недоступна. Обновите список моделей."
	case "provider_429":
		return "OpenAI ограничил запрос: проверьте лимиты и баланс проекта."
	case "provider_400":
		return "OpenAI отклонил параметры запроса. Проверьте модель и уровень рассуждений."
	case "invalid_response":
		return "OpenAI вернул некорректный ответ."
	case "invalid_models":
		return "OpenAI вернул некорректный список моделей."
	case "network":
		return "OpenAI не ответил за 5 минут или соединение прервано. Повторите запрос."
	case "missing_key":
		return "Сохраните API-ключ OpenAI во вкладке «Нейросети» → «ChatGPT»."
	}
	return "OpenAI сейчас недоступен. Повторите запрос позже."
}

// Неподтверждённый POST нельзя повторять: резерв расхода сохраняет вызывающий ledger.
type uncertain struct{ cause error }

func (e *uncertain) Error() string { return e.cause.Error() }
func (e *uncertain) Unwrap() error { return e.cause }
func IsUncertain(err error) bool   { var e *uncertain; return errors.As(err, &e) }

type Client struct {
	provider                  Provider
	key                       string
	http                      *http.Client
	readTimeout, writeTimeout time.Duration
}

func (c *Client) String() string   { return "aihttp.Client{credentials:redacted}" }
func (c *Client) GoString() string { return c.String() }
func New(provider Provider, key string) (*Client, error) {
	if provider != OpenAI && provider != OpenRouter {
		return nil, &Error{Code: "configuration", Status: 503}
	}
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
		return nil, &Error{Code: "missing_key", Status: 409, provider: provider}
	}
	timeout := 300 * time.Second
	if provider == OpenRouter {
		timeout = 210 * time.Second
	}
	return &Client{provider: provider, key: key, http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, readTimeout: 25 * time.Second, writeTimeout: timeout}, nil
}
func (c *Client) Models(ctx context.Context) (json.RawMessage, error) {
	return c.request(ctx, "models", nil)
}
func (c *Client) DecisionModels(ctx context.Context) (json.RawMessage, error) {
	return c.routerRead(ctx, "models?output_modalities=decisions")
}
func (c *Client) Decisions(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.provider != OpenRouter {
		return nil, &Error{Code: "configuration", Status: 503}
	}
	return c.post(ctx, "@alpha/decisions", body)
}
func (c *Client) RankedModels(ctx context.Context) (json.RawMessage, error) {
	return c.routerRead(ctx, "models?context=262144&supported_parameters=reasoning&output_modalities=text&sort=intelligence-high-to-low")
}
func (c *Client) Benchmarks(ctx context.Context) (json.RawMessage, error) {
	return c.routerRead(ctx, "benchmarks?source=artificial-analysis")
}
func (c *Client) Key(ctx context.Context) (json.RawMessage, error) { return c.routerRead(ctx, "key") }
func (c *Client) routerRead(ctx context.Context, path string) (json.RawMessage, error) {
	if c == nil || c.provider != OpenRouter {
		return nil, &Error{Code: "configuration", Status: 503}
	}
	return c.request(ctx, path, nil)
}
func (c *Client) Responses(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.provider != OpenAI {
		return nil, &Error{Code: "configuration", Status: 503}
	}
	return c.post(ctx, "responses", body)
}
func (c *Client) ChatCompletions(ctx context.Context, body json.RawMessage) (json.RawMessage, error) {
	if c == nil || c.provider != OpenRouter {
		return nil, &Error{Code: "configuration", Status: 503}
	}
	return c.post(ctx, "chat/completions", body)
}
func (c *Client) post(ctx context.Context, path string, body json.RawMessage) (json.RawMessage, error) {
	if len(body) == 0 || len(body) > maxRequestBytes || !json.Valid(body) {
		return nil, &Error{Code: "input", Status: 400}
	}
	return c.request(ctx, path, body)
}
func (c *Client) fail(code string, status int) error {
	return &Error{Code: code, Status: status, provider: c.provider}
}
func (c *Client) request(ctx context.Context, path string, body json.RawMessage) (data json.RawMessage, err error) {
	if c == nil || c.http == nil {
		return nil, &Error{Code: "configuration", Status: 503}
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	method := "GET"
	timeout := c.readTimeout
	if body != nil {
		method = "POST"
		timeout = c.writeTimeout
	}
	call, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	base := "https://api.openai.com/v1/"
	if c.provider == OpenRouter {
		base = "https://openrouter.ai/api/v1/"
	}
	if c.provider == OpenRouter && path == "@alpha/decisions" {
		base = "https://openrouter.ai/api/alpha/"
		path = "decisions"
	}
	req, e := http.NewRequestWithContext(call, method, base+path, bytes.NewReader(body))
	if e != nil {
		return nil, c.fail("configuration", 503)
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	// Любой отказ после передачи POST транспорту считаем неподтверждённым результатом.
	// Это консервативный сигнал, а не автоматический retry или возврат резерва.
	if body != nil {
		defer func() {
			if err != nil {
				err = &uncertain{err}
			}
		}()
	}
	res, e := c.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, c.fail("network", 502)
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e != nil {
		return nil, c.fail("network", 502)
	}
	invalid := "invalid_response"
	if c.provider == OpenRouter {
		invalid = "network"
	}
	if len(raw) > maxResponseBytes {
		return nil, c.fail(invalid, 502)
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil || payload == nil {
		return nil, c.fail(invalid, 502)
	}
	bad := false
	if x, ok := payload["error"]; ok {
		s := strings.TrimSpace(string(x))
		bad = s != "null" && s != "false" && s != "0" && s != `""`
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || bad {
		status := res.StatusCode
		if c.provider == OpenRouter {
			if res.StatusCode >= 200 && res.StatusCode < 300 {
				var v struct {
					Code json.RawMessage `json:"code"`
				}
				_ = json.Unmarshal(payload["error"], &v)
				t := strings.Trim(string(v.Code), `"`)
				n, e := strconv.Atoi(t)
				status = 502
				if e == nil && n != 0 {
					status = n
				}
			}
			if status == 401 || (status == 403 && body == nil) {
				return nil, c.fail("invalid_key", 400)
			}
			code := "provider_" + strconv.Itoa(status)
			var v struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(payload["error"], &v)
			lower := strings.ToLower(v.Message)
			if status == 400 && (strings.Contains(lower, "reasoning") || strings.Contains(lower, "effort")) {
				code = "reasoning_parameter"
			}
			return nil, &Error{Code: code, Status: 502, ProviderStatus: status, provider: c.provider}
		}
		mapped := 502
		if status == 401 {
			mapped = 409
		}
		if status == 429 {
			mapped = 429
		}
		return nil, &Error{Code: "provider_" + strconv.Itoa(status), Status: mapped, provider: c.provider}
	}
	return json.RawMessage(raw), nil
}
