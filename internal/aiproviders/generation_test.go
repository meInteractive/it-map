package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type completionMock struct {
	response json.RawMessage
	err      error
	payloads *[]json.RawMessage
}

func (m completionMock) Responses(_ context.Context, b json.RawMessage) (json.RawMessage, error) {
	*m.payloads = append(*m.payloads, append([]byte(nil), b...))
	return m.response, m.err
}
func (m completionMock) ChatCompletions(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
	return m.Responses(ctx, b)
}
func checkGenerationSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Response, Payload, Result json.RawMessage
		OK                        bool
		Code                      string
		Status                    int
		Message                   string
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 8 {
		t.Fatal("source frames")
	}
	for i, f := range frames {
		calls := []json.RawMessage{}
		store := &serviceStore{key: "CI_KEY", configured: true}
		service := NewOpenAIService(store)
		service.factory = func(string) (ModelTransport, error) {
			return modelClient(func(context.Context) (json.RawMessage, error) { return []byte(`{"data":[{"id":"gpt-6-luna"}]}`), nil }), nil
		}
		g := NewGenerator(service, nil)
		g.factory = func(aihttp.Provider, string) (CompletionTransport, error) {
			return completionMock{response: f.Response, payloads: &calls}, nil
		}
		r := GenerationRequest{Task: "connection_test", Messages: []Message{{Role: "system", Content: "CI_SYSTEM"}, {Role: "user", Content: "CI_USER"}}, MaxTokens: 1024}
		result, e := g.run(context.Background(), r, profiles.ModelInput{Provider: "openai", OpenAIModel: "gpt-6-luna", ReasoningEffort: "medium"}, nil)
		if len(calls) != 1 {
			t.Fatalf("frame%d POST retried", i)
		}
		var got, want any
		_ = json.Unmarshal(calls[0], &got)
		_ = json.Unmarshal(f.Payload, &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("frame%d source request differs", i)
		}
		if !f.OK {
			var typed *ServiceError
			if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Error() != f.Message {
				t.Fatalf("frame%d source error differs: %v", i, e)
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(result)
		_ = json.Unmarshal(b, &got)
		_ = json.Unmarshal(f.Result, &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("frame%d source response differs: %s %s", i, b, f.Result)
		}
	}
}
func TestGenerationSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/generation-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkGenerationSource(t, b)
}
func TestAutomaticRequiresBillingAndNoBlindRetry(t *testing.T) {
	g := NewGenerator(nil, nil)
	if _, e := g.Automatic(context.Background(), GenerationRequest{}, profiles.ModelInput{}, nil); e == nil {
		t.Fatal("automatic unbilled work accepted")
	}
	store := &serviceStore{key: "CI_KEY", configured: true}
	service := NewOpenAIService(store)
	service.factory = func(string) (ModelTransport, error) {
		return modelClient(func(context.Context) (json.RawMessage, error) { return []byte(`{"data":[{"id":"gpt-6-luna"}]}`), nil }), nil
	}
	g = NewGenerator(service, nil)
	calls := []json.RawMessage{}
	g.factory = func(aihttp.Provider, string) (CompletionTransport, error) {
		return completionMock{payloads: &calls, err: errors.New("CI_NETWORK_FAILURE")}, nil
	}
	_, e := g.ConnectionTest(context.Background(), profiles.ModelInput{Provider: "openai", OpenAIModel: "gpt-6-luna", ReasoningEffort: "medium"})
	if e == nil || len(calls) != 1 {
		t.Fatal("lost OpenAI outcome retried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = g.ConnectionTest(ctx, profiles.ModelInput{Provider: "openai"})
	if !errors.Is(e, context.Canceled) || len(calls) != 1 {
		t.Fatal("pre-cancel sent work")
	}
	if strings.Contains(generationError("error", "safe", 502).Error(), "CI_KEY") {
		t.Fatal("error leaked key")
	}
}

type sequenceCompletion struct {
	responses []json.RawMessage
	calls     *[]json.RawMessage
	index     int
}

func (m *sequenceCompletion) Responses(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
	return m.ChatCompletions(ctx, b)
}
func (m *sequenceCompletion) ChatCompletions(_ context.Context, b json.RawMessage) (json.RawMessage, error) {
	*m.calls = append(*m.calls, append([]byte(nil), b...))
	if m.index >= len(m.responses) {
		return nil, errors.New("unexpected extra request")
	}
	raw := m.responses[m.index]
	m.index++
	var v struct{ Status int }
	_ = json.Unmarshal(raw, &v)
	if v.Status == 400 {
		return nil, &aihttp.Error{Code: "reasoning_parameter", Status: 502, ProviderStatus: 400}
	}
	return raw, nil
}
func checkRouterGenerationSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Models              []map[string]any
		Responses, Payloads []json.RawMessage
		OK                  bool
		Result              json.RawMessage
		Code, Message       string
		Status              int
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 6 {
		t.Fatal("router source corpus")
	}
	for i, f := range frames {
		stamp := "2026-10-30T00:00:00.000Z"
		store := &routerMemory{key: "CI_KEY", generation: "CI_GENERATION", row: RouterSnapshot{Configured: true, Revision: 1, SyncedAt: &stamp, Models: f.Models, Status: "connected"}}
		router := NewRouterService(store)
		router.now = func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }
		calls := []json.RawMessage{}
		client := &sequenceCompletion{responses: f.Responses, calls: &calls}
		g := NewGenerator(nil, router)
		g.factory = func(aihttp.Provider, string) (CompletionTransport, error) { return client, nil }
		result, e := g.run(context.Background(), GenerationRequest{Task: "connection_test", Messages: []Message{{Role: "user", Content: "CI_USER"}}, MaxTokens: 1024}, profiles.ModelInput{Provider: "openrouter"}, nil)
		a, _ := json.Marshal(calls)
		b, _ := json.Marshal(f.Payloads)
		var got, want any
		_ = json.Unmarshal(a, &got)
		_ = json.Unmarshal(b, &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("router frame%d source requests differ: %s %s", i, a, b)
		}
		if !f.OK {
			var typed *ServiceError
			if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Error() != f.Message {
				t.Fatalf("router frame%d source error differs: %v", i, e)
			}
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		a, _ = json.Marshal(result)
		_ = json.Unmarshal(a, &got)
		_ = json.Unmarshal(f.Result, &want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("router frame%d result differs %s %s", i, a, f.Result)
		}
	}
}
func TestRouterGenerationSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/router-generation-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkRouterGenerationSource(t, b)
}

type billingProbe struct {
	calls   int
	deny    bool
	request json.RawMessage
}

func (b *billingProbe) Run(ctx context.Context, request json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	b.calls++
	b.request = append([]byte(nil), request...)
	if b.deny {
		return nil, errors.New("CI_BUDGET_DENIED")
	}
	return work(ctx)
}
func TestAutomaticReservationPrecedesProviderWork(t *testing.T) {
	store := &serviceStore{key: "CI_KEY", configured: true}
	service := NewOpenAIService(store)
	service.factory = func(string) (ModelTransport, error) {
		return modelClient(func(context.Context) (json.RawMessage, error) { return []byte(`{"data":[{"id":"gpt-6-luna"}]}`), nil }), nil
	}
	g := NewGenerator(service, nil)
	calls := []json.RawMessage{}
	g.factory = func(aihttp.Provider, string) (CompletionTransport, error) {
		return completionMock{payloads: &calls, response: []byte(`{"status":"completed","model":"gpt-6-luna","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"valid"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)}, nil
	}
	bill := &billingProbe{deny: true}
	cfg := profiles.ModelInput{Provider: "openai", OpenAIModel: "gpt-6-luna", ReasoningEffort: "medium"}
	req := GenerationRequest{Task: "vacancy_extraction", Messages: []Message{{Role: "user", Content: "CI_INPUT"}}, MaxTokens: 100}
	if _, e := g.Automatic(context.Background(), req, cfg, bill); e == nil || bill.calls != 1 || len(calls) != 0 {
		t.Fatal("budget denial still sent provider POST")
	}
	bill.deny = false
	if _, e := g.Automatic(context.Background(), req, cfg, bill); e != nil || bill.calls != 2 || len(calls) != 1 {
		t.Fatal("admitted request did not call once", e)
	}
	var meta map[string]any
	if json.Unmarshal(bill.request, &meta) != nil || meta["provider"] != "openai" || meta["model"] != "gpt-6-luna" || meta["payload"] == nil {
		t.Fatal("billing metadata incomplete")
	}
}
