// Package apify is an isolated HTTP adapter. No settings, keys or work are read
// or started at initialization; callers must supply a token and durable admission.
package apify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const apiBase = "https://api.apify.com/v2"

var allowedPath = regexp.MustCompile(`^/(?:users/me(?:/limits|/usage/monthly)?|actors/[A-Za-z0-9_~.-]+/runs|actor-runs/[A-Za-z0-9]+|datasets/[A-Za-z0-9]+(?:/items)?|key-value-stores/[A-Za-z0-9]+/records/INPUT|logs/[A-Za-z0-9]+)(?:\?|$)`)
var readOnlyPath = regexp.MustCompile(`^/(?:users|datasets|key-value-stores|logs)/`)
var logPath = regexp.MustCompile(`^/logs/[A-Za-z0-9]+(?:\?|$)`)
var messages = map[string]string{"apify_auth": "Apify: проверьте API-токен и его права.", "apify_network": "Apify временно недоступен.", "apify_timeout": "Apify не ответил вовремя.", "apify_data": "Apify вернул неподдерживаемые данные.", "apify_budget": "Недостаточно доступного бюджета Apify для нового запуска с учётом резервов. Сбор сохранён для продолжения.", "apify_disabled": "Apify выключен или API-токен не настроен.", "apify_scope": "Для Apify допустим только источник hh.ru с конкретным работодателем или вакансией.", "apify_busy": "Другой запуск Apify ещё выполняется."}

type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string {
	if m, ok := messages[e.Code]; ok {
		return m
	}
	return "Не удалось выполнить запрос Apify."
}
func apiError(code string, status int) *Error { return &Error{Code: code, Status: status} }

type startError struct{ cause error }

func (e *startError) Error() string { return e.cause.Error() }
func (e *startError) Unwrap() error { return e.cause }

// IsStartUncertain means POST may have reached the provider. Never blindly
// retry it; the future durable ledger must preserve its reservation/identity.
func IsStartUncertain(err error) bool { var uncertain *startError; return errors.As(err, &uncertain) }

type Client struct {
	token   string
	http    *http.Client
	timeout time.Duration
}

func (c *Client) String() string   { return "apify.Client{credentials:redacted}" }
func (c *Client) GoString() string { return c.String() }
func New(token string) (*Client, error) {
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return nil, apiError("apify_disabled", 400)
	}
	return &Client{token: token, timeout: 15 * time.Second, http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type requestOptions struct {
	method   string
	body     json.RawMessage
	maxBytes int64
	text     bool
}

func (c *Client) request(ctx context.Context, path string, options requestOptions) ([]byte, bool, error) {
	if c == nil || c.http == nil || c.token == "" {
		return nil, false, apiError("apify_disabled", 400)
	}
	method := options.method
	if method == "" {
		method = "GET"
	}
	if !allowedPath.MatchString(path) || (readOnlyPath.MatchString(path) && method != "GET") || (options.text && (!logPath.MatchString(path) || method != "GET")) {
		return nil, false, apiError("apify_scope", 400)
	}
	if len(options.body) > 0 && !json.Valid(options.body) {
		return nil, false, apiError("apify_data", 400)
	}
	max := options.maxBytes
	if max == 0 {
		max = 4000000
	}
	if max < 0 || max > 4000000 {
		return nil, false, apiError("apify_data", 400)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, apiBase+path, bytes.NewReader(options.body))
	if err != nil {
		return nil, false, apiError("apify_scope", 400)
	}
	req.GetBody = nil // No replayable POST body; no application-level retries.
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, true, ctx.Err()
		}
		if requestCtx.Err() != nil {
			return nil, true, apiError("apify_timeout", 503)
		}
		return nil, true, apiError("apify_network", 503)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code, status := "apify_data", 503
		switch response.StatusCode {
		case 401, 403:
			code, status = "apify_auth", 400
		case 402:
			code = "apify_budget"
		case 429:
			code = "apify_busy"
		default:
			if response.StatusCode >= 500 {
				code = "apify_network"
			}
		}
		return nil, true, apiError(code, status)
	}
	if response.StatusCode == 204 || response.StatusCode == 205 {
		return nil, true, apiError("apify_data", 502)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, true, ctx.Err()
		}
		return nil, true, apiError("apify_network", 503)
	}
	if int64(len(data)) > max {
		return nil, true, apiError("apify_data", 502)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) || (!options.text && !json.Valid(data)) {
		return nil, true, apiError("apify_data", 502)
	}
	return data, true, nil
}

type ActorRun struct {
	ID, DefaultDatasetID, Status string
	Raw                          json.RawMessage
}

func parseRun(data []byte) (ActorRun, error) {
	var wrapper struct{ Data json.RawMessage }
	if json.Unmarshal(data, &wrapper) != nil {
		return ActorRun{}, apiError("apify_data", 502)
	}
	var result struct {
		ID      string `json:"id"`
		Dataset string `json:"defaultDatasetId"`
		Status  string `json:"status"`
	}
	if json.Unmarshal(wrapper.Data, &result) != nil || !providerID.MatchString(result.ID) || !providerID.MatchString(result.Dataset) {
		return ActorRun{}, apiError("apify_data", 502)
	}
	run := ActorRun{ID: result.ID, DefaultDatasetID: result.Dataset, Status: result.Status, Raw: append(json.RawMessage(nil), wrapper.Data...)}
	if _, err := run.State(); err != nil {
		return ActorRun{}, err
	}
	return run, nil
}
func (r ActorRun) State() (string, error) {
	switch r.Status {
	case "SUCCEEDED":
		return "succeeded", nil
	case "FAILED", "TIMED-OUT", "ABORTED":
		return "failed", nil
	case "READY", "RUNNING", "ABORTING", "TIMING-OUT":
		return "running", nil
	}
	return "", apiError("apify_data", 502)
}
func (c *Client) Start(ctx context.Context, source string, options HHOptions) (ActorRun, error) {
	input, err := HHInput(source, options)
	if err != nil {
		return ActorRun{}, err
	}
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(input) != nil {
		return ActorRun{}, apiError("apify_data", 400)
	}
	data, attempted, err := c.request(ctx, "/actors/"+Actor+"/runs?timeout=180&maxTotalChargeUsd=0.25&restartOnError=false&waitForFinish=0", requestOptions{method: "POST", body: bytes.TrimSpace(body.Bytes())})
	if err == nil {
		var run ActorRun
		run, err = parseRun(data)
		if err == nil {
			return run, nil
		}
	}
	if attempted {
		return ActorRun{}, &startError{cause: err}
	}
	return ActorRun{}, err
}
func (c *Client) Run(ctx context.Context, id string) (ActorRun, error) {
	if !providerID.MatchString(id) {
		return ActorRun{}, apiError("apify_data", 400)
	}
	data, _, err := c.request(ctx, "/actor-runs/"+id, requestOptions{})
	if err != nil {
		return ActorRun{}, err
	}
	run, err := parseRun(data)
	if err != nil || run.ID != id {
		return ActorRun{}, apiError("apify_data", 502)
	}
	return run, nil
}
func (c *Client) DatasetItems(ctx context.Context, id string, offset int64, clean bool) ([]json.RawMessage, error) {
	if !providerID.MatchString(id) || offset < 0 || offset > maxSafeInteger {
		return nil, apiError("apify_data", 400)
	}
	flag := strconv.FormatBool(clean)
	data, _, err := c.request(ctx, "/datasets/"+id+"/items?format=json&clean="+flag+"&skipHidden=true&skipEmpty="+flag+"&offset="+strconv.FormatInt(offset, 10)+"&limit=10", requestOptions{maxBytes: 1800000})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '[' {
		return nil, apiError("apify_data", 502)
	}
	var items []json.RawMessage
	if json.Unmarshal(data, &items) != nil || len(items) > 10 {
		return nil, apiError("apify_data", 502)
	}
	return items, nil
}

type DatasetMetadata struct {
	ItemCount      int64  `json:"itemCount"`
	CleanItemCount *int64 `json:"cleanItemCount"`
	ModifiedAt     string `json:"modifiedAt,omitempty"`
}

func safeCount(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil || n < 0 || n > maxSafeInteger || n != float64(int64(n)) {
		return 0, false
	}
	return int64(n), true
}
func (c *Client) DatasetInfo(ctx context.Context, id string) (DatasetMetadata, error) {
	if !providerID.MatchString(id) {
		return DatasetMetadata{}, apiError("apify_data", 400)
	}
	data, _, err := c.request(ctx, "/datasets/"+id, requestOptions{maxBytes: 64000})
	if err != nil {
		return DatasetMetadata{}, err
	}
	var wrapper struct{ Data map[string]json.RawMessage }
	if json.Unmarshal(data, &wrapper) != nil {
		return DatasetMetadata{}, apiError("apify_data", 502)
	}
	var actualID string
	if json.Unmarshal(wrapper.Data["id"], &actualID) != nil || actualID != id {
		return DatasetMetadata{}, apiError("apify_data", 502)
	}
	count, valid := safeCount(wrapper.Data["itemCount"])
	if !valid {
		return DatasetMetadata{}, apiError("apify_data", 502)
	}
	result := DatasetMetadata{ItemCount: count}
	if clean, valid := safeCount(wrapper.Data["cleanItemCount"]); valid {
		result.CleanItemCount = &clean
	}
	var modified string
	if json.Unmarshal(wrapper.Data["modifiedAt"], &modified) == nil {
		_, err := time.Parse(time.RFC3339Nano, modified)
		valid := err == nil && !strings.Contains(modified, ",")
		if len(modified) >= 6 {
			z := modified[len(modified)-6:]
			if (z[0] == '+' || z[0] == '-') && (z[1:3] > "23" || z[4:] > "59") {
				valid = false
			}
		}
		if valid {
			result.ModifiedAt = modified
		}
	}
	return result, nil
}
