//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"itmap/internal/aiproviders"
)

type e2eProviders struct {
	mu                                   sync.Mutex
	titles, relevance, extraction, roles int
	extractionContent                    json.RawMessage
	server                               *httptest.Server
}

func newE2EProviders(extraction json.RawMessage) *e2eProviders {
	p := &e2eProviders{extractionContent: extraction}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/openai/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"id": "gpt-6-luna"}}})
		case "/jev/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": aiproviders.JEVModelID, "context_length": 262144, "pricing": map[string]string{"prompt": "0.00001", "completion": "0"}}}})
		case "/jev/decisions":
			var request struct {
				State     json.RawMessage            `json:"state"`
				Questions map[string]json.RawMessage `json:"questions"`
			}
			if json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&request) != nil || len(request.Questions) == 0 {
				http.Error(w, "bad synthetic request", 400)
				return
			}
			answers := map[string]any{}
			p.mu.Lock()
			for key := range request.Questions {
				if key == "relevance" {
					p.relevance++
					answers[key] = map[string]any{"type": "score", "score": 6}
					continue
				}
				if !strings.HasPrefix(key, "role_") {
					p.mu.Unlock()
					http.Error(w, "bad synthetic question", 400)
					return
				}
				p.titles++
				choice := "match"
				if bytes.Contains(request.State, []byte("CI_REJECTED")) {
					choice = "reject"
				}
				answers[key] = map[string]any{"type": "choice", "choice": choice}
			}
			p.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"model": aiproviders.JEVModelID, "answers": answers, "usage": map[string]any{"cost": 0.001}})
		case "/openai/responses":
			body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
			if err != nil {
				http.Error(w, "bad synthetic request", 400)
				return
			}
			content := string(p.extractionContent)
			p.mu.Lock()
			if bytes.Contains(body, []byte("CI extraction prompt")) {
				p.extraction++
			} else if bytes.Contains(body, []byte("CI roles prompt")) {
				p.roles++
				content = `{"role_ids":[991304]}`
			} else {
				p.mu.Unlock()
				http.Error(w, "unknown synthetic task", 400)
				return
			}
			p.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed", "model": "gpt-6-luna", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": content}}}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
		default:
			http.NotFound(w, r)
		}
	}))
	return p
}
func (p *e2eProviders) counts() (int, int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.titles, p.relevance, p.extraction, p.roles
}
func (p *e2eProviders) request(ctx context.Context, method, path string, body json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.server.URL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.server.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("synthetic provider rejected request")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	return json.RawMessage(raw), err
}
func (p *e2eProviders) Models(ctx context.Context) (json.RawMessage, error) {
	return p.request(ctx, "GET", "/openai/models", nil)
}
func (p *e2eProviders) DecisionModels(ctx context.Context) (json.RawMessage, error) {
	return p.request(ctx, "GET", "/jev/models", nil)
}
func (p *e2eProviders) Decisions(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
	return p.request(ctx, "POST", "/jev/decisions", b)
}
func (p *e2eProviders) Responses(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
	return p.request(ctx, "POST", "/openai/responses", b)
}
func (p *e2eProviders) ChatCompletions(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("unexpected synthetic chat")
}
func (p *e2eProviders) RankedModels(context.Context) (json.RawMessage, error) {
	return nil, errors.New("unexpected synthetic ranked models")
}
func (p *e2eProviders) Benchmarks(context.Context) (json.RawMessage, error) {
	return nil, errors.New("unexpected synthetic benchmarks")
}
func (p *e2eProviders) Key(context.Context) (json.RawMessage, error) {
	return nil, errors.New("unexpected synthetic key endpoint")
}
