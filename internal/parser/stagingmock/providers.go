// Package stagingmock содержит синтетические ответы для изолированной проверки.
package stagingmock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/integrations/apify"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/parser"
)

const SourceURL = "https://hh.ru/employer/456"
const acceptedTitle = "STAGING_SYNTHETIC_TITLE"
const rejectedTitle = "STAGING_SYNTHETIC_REJECTED"

var description = "Задачи: развивать продукт и управлять командой. Требования: опыт управления, навыки планирования. " + strings.Repeat("Синтетическое описание вакансии для проверки парсера. ", 15)

var errUnexpected = errors.New("неожиданный запрос синтетическому провайдеру")

// Providers не содержит сетевого клиента и принимает только известные фикстуры.
type Providers struct {
	starts  atomic.Int64
	polls   atomic.Int64
	aiCalls atomic.Int64
	// LoseStartAck имитирует исполненный запрос без полученного ответа.
	LoseStartAck bool
	// AfterStart позволяет синтетическому тесту остановить процесс после POST.
	AfterStart func(context.Context)
	// BeforeAccount позволяет проверить остановку до резервирования операции.
	BeforeAccount func(context.Context)
	// Async возвращает работающий synthetic Actor до первого reconcile.
	Async bool
	// ExhaustedAccount возвращает исчерпанный synthetic лимит Apify.
	ExhaustedAccount bool
}

func (p *Providers) Starts() int64  { return p.starts.Load() }
func (p *Providers) Polls() int64   { return p.polls.Load() }
func (p *Providers) AICalls() int64 { return p.aiCalls.Load() }

func (p *Providers) Account(ctx context.Context) (apify.BillingAccount, error) {
	if err := ctx.Err(); err != nil {
		return apify.BillingAccount{}, err
	}
	if p.BeforeAccount != nil {
		p.BeforeAccount(ctx)
	}
	if err := ctx.Err(); err != nil {
		return apify.BillingAccount{}, err
	}
	now := time.Now().UTC()
	used := float64(0)
	if p.ExhaustedAccount {
		used = 5
	}
	return apify.BillingAccount{AccountID: "StagingMock", PlanCreditsUSD: 5, ProviderLimitUSD: 5, ProviderUsedUSD: used, PeriodStart: now.Truncate(time.Hour).Add(-time.Hour).Format(time.RFC3339), PeriodEnd: now.Truncate(time.Hour).Add(2 * time.Hour).Format(time.RFC3339), CheckedAt: now.UnixMilli()}, nil
}
func (p *Providers) Start(ctx context.Context, source string, opts apify.HHOptions) (apify.ActorRun, error) {
	if err := ctx.Err(); err != nil {
		return apify.ActorRun{}, err
	}
	if source != SourceURL || opts.Page != 0 || opts.Detail {
		return apify.ActorRun{}, errUnexpected
	}
	p.starts.Add(1)
	if p.AfterStart != nil {
		p.AfterStart(ctx)
	}
	if err := ctx.Err(); err != nil {
		return apify.ActorRun{}, err
	}
	if p.LoseStartAck {
		return apify.ActorRun{}, errors.New("синтетическая потеря подтверждения")
	}
	if p.Async {
		return apify.ActorRun{ID: "MockRun0", DefaultDatasetID: "MockData0", Status: "RUNNING", Raw: json.RawMessage(`{"id":"MockRun0","defaultDatasetId":"MockData0","status":"RUNNING","userId":"StagingMock"}`)}, nil
	}
	return apify.ActorRun{ID: "MockRun0", DefaultDatasetID: "MockData0", Status: "SUCCEEDED", Raw: json.RawMessage(`{"id":"MockRun0","defaultDatasetId":"MockData0","status":"SUCCEEDED","userId":"StagingMock","usageTotalUsd":0.02}`)}, nil
}
func (p *Providers) Run(ctx context.Context, id string) (apify.ActorRun, error) {
	if err := ctx.Err(); err != nil {
		return apify.ActorRun{}, err
	}
	if !p.Async || id != "MockRun0" {
		return apify.ActorRun{}, errUnexpected
	}
	p.polls.Add(1)
	return apify.ActorRun{ID: "MockRun0", DefaultDatasetID: "MockData0", Status: "SUCCEEDED", Raw: json.RawMessage(`{"id":"MockRun0","defaultDatasetId":"MockData0","status":"SUCCEEDED","userId":"StagingMock","usageTotalUsd":0.02}`)}, nil
}
func (*Providers) DatasetInfo(ctx context.Context, id string) (apify.DatasetMetadata, error) {
	if err := ctx.Err(); err != nil {
		return apify.DatasetMetadata{}, err
	}
	if id != "MockData0" {
		return apify.DatasetMetadata{}, errUnexpected
	}
	return apify.DatasetMetadata{ItemCount: 2, ModifiedAt: "2020-01-01T00:00:00Z"}, nil
}
func (*Providers) DatasetItems(ctx context.Context, id string, offset int64, clean bool) ([]json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id != "MockData0" || clean || offset < 0 || offset > 2 {
		return nil, errUnexpected
	}
	posted := time.Now().In(time.FixedZone("MSK", 3*60*60)).AddDate(0, 0, -1).Format("2006-01-02") + "T12:00:00+03:00"
	items := []json.RawMessage{
		json.RawMessage(fmt.Sprintf(`{"title":"STAGING_SYNTHETIC_TITLE","url":"https://hh.ru/vacancy/123","vacancy_id":123,"employer_id":456,"location":"Москва","date_posted":%q}`, posted)),
		json.RawMessage(fmt.Sprintf(`{"title":"STAGING_SYNTHETIC_REJECTED","url":"https://hh.ru/vacancy/124","vacancy_id":124,"employer_id":456,"location":"Москва","date_posted":%q}`, posted)),
	}
	return items[offset:], nil
}
func (*Providers) Fetch(ctx context.Context, url string) (*hhpublic.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if url != "https://hh.ru/vacancy/123" {
		return nil, errUnexpected
	}
	return &hhpublic.Job{ID: "123", URL: url, Title: acceptedTitle, Description: description}, nil
}
func (*Providers) Models(context.Context) (json.RawMessage, error) {
	return json.RawMessage(`{"data":[{"id":"gpt-6-luna"}]}`), nil
}
func (*Providers) DecisionModels(context.Context) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"data": []any{map[string]any{"id": aiproviders.JEVModelID, "context_length": 262144, "pricing": map[string]string{"prompt": "0.000001", "completion": "0"}}}})
}
func (p *Providers) Decisions(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.aiCalls.Add(1)
	var request struct {
		State     json.RawMessage            `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if json.Unmarshal(raw, &request) != nil || len(request.Questions) == 0 {
		return nil, errUnexpected
	}
	answers := make(map[string]any, len(request.Questions))
	for key := range request.Questions {
		if key == "relevance" {
			answers[key] = map[string]any{"type": "score", "score": 6}
			continue
		}
		if !strings.HasPrefix(key, "role_") {
			return nil, errUnexpected
		}
		choice := "match"
		if bytes.Contains(request.State, []byte(rejectedTitle)) {
			choice = "reject"
		}
		answers[key] = map[string]any{"type": "choice", "choice": choice}
	}
	return json.Marshal(map[string]any{"model": aiproviders.JEVModelID, "answers": answers, "usage": map[string]any{"cost": 0.001}})
}
func (p *Providers) Responses(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.aiCalls.Add(1)
	var content string
	switch {
	case bytes.Contains(raw, []byte("STAGING extraction prompt")):
		data := map[string]any{"title": acceptedTitle, "description_md": description, "level": "Senior"}
		for _, key := range []string{"employer_name", "location", "work_format", "employment_type", "salary_from", "salary_to", "salary_currency", "salary_period", "salary_tax_type", "responsibilities", "requirements", "conditions", "published_at", "language"} {
			data[key] = nil
		}
		b, _ := json.Marshal(data)
		content = string(b)
	case bytes.Contains(raw, []byte("STAGING roles prompt")):
		content = `{"role_ids":[991304]}`
	default:
		return nil, errUnexpected
	}
	return json.Marshal(map[string]any{"status": "completed", "model": "gpt-6-luna", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": content}}}}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}})
}
func (*Providers) ChatCompletions(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, errUnexpected
}
func (*Providers) RankedModels(context.Context) (json.RawMessage, error) { return nil, errUnexpected }
func (*Providers) Benchmarks(context.Context) (json.RawMessage, error)   { return nil, errUnexpected }
func (*Providers) Key(context.Context) (json.RawMessage, error)          { return nil, errUnexpected }

// Worker подключает только синтетические адаптеры; nil-зависимости запрещены.
func Worker(runs parser.RunRepository, secret string, p *Providers) (parser.Worker, error) {
	if runs.DB == nil || secret == "" || p == nil {
		return parser.Worker{}, fmt.Errorf("staging mock: отсутствуют зависимости")
	}
	settings := apifysettings.NewRepository(runs.DB, secret)
	keys := aiproviders.NewRepository(runs.DB, secret, "sk-"+strings.Repeat("s", 16))
	openAI := aiproviders.NewOpenAIServiceWithTransport(keys, func(string) (aiproviders.ModelTransport, error) { return p, nil })
	router := aiproviders.NewRouterServiceWithTransport(keys, func(string) (aiproviders.RouterTransport, error) { return p, nil })
	gen := aiproviders.NewGeneratorWithTransport(openAI, router, func(aihttp.Provider, string) (aiproviders.CompletionTransport, error) { return p, nil })
	runtime := apifyruns.Service{Repository: apifyruns.Repository{DB: runs.DB, Settings: settings}, NewClient: func(string) (apifyruns.Provider, error) { return p, nil }}
	return parser.Worker{Runs: runs, Settings: settings, ListingRuntime: runtime, DetailRuntime: runtime, Public: p, Generator: gen, Classifier: router}, nil
}
