package stagingmock

import (
	"context"
	"encoding/json"
	"testing"

	"itmap/internal/aiproviders"
	"itmap/internal/integrations/apify"
	"itmap/internal/parser"
)

func TestFixturesRejectUnknownSourcesAndReturnStableData(t *testing.T) {
	p := &Providers{}
	ctx := context.Background()
	if _, err := p.Start(ctx, "https://hh.ru/employer/999", apify.HHOptions{}); err == nil || p.Starts() != 0 {
		t.Fatal("unexpected source accepted")
	}
	run, err := p.Start(ctx, SourceURL, apify.HHOptions{})
	if err != nil || run.Status != "SUCCEEDED" || p.Starts() != 1 {
		t.Fatal("synthetic run failed", err)
	}
	rows, err := p.DatasetItems(ctx, run.DefaultDatasetID, 0, false)
	if err != nil || len(rows) != 2 {
		t.Fatal("synthetic dataset failed", err)
	}
	if _, err := p.DatasetItems(ctx, run.DefaultDatasetID, 0, true); err == nil {
		t.Fatal("clean dataset unexpectedly accepted")
	}
	if _, err := p.Fetch(ctx, "https://hh.ru/vacancy/999"); err == nil {
		t.Fatal("unknown vacancy accepted")
	}
	if _, err := p.Fetch(ctx, "https://hh.ru/vacancy/123"); err != nil {
		t.Fatal(err)
	}
	if issues := parser.CheckFulltextContent(description, 200); len(issues) != 0 {
		t.Fatal("invalid synthetic vacancy description", issues)
	}
}

func TestLostAcknowledgementOccursAfterSyntheticStart(t *testing.T) {
	p := &Providers{LoseStartAck: true}
	if _, err := p.Start(context.Background(), SourceURL, apify.HHOptions{}); err == nil || p.Starts() != 1 {
		t.Fatal("потеря подтверждения не последовала за синтетическим запуском", err)
	}
}

func TestAsyncActorFinishesOnReconcile(t *testing.T) {
	p := &Providers{Async: true}
	first, err := p.Start(context.Background(), SourceURL, apify.HHOptions{})
	if err != nil || first.Status != "RUNNING" || p.Starts() != 1 {
		t.Fatal("асинхронный Actor не запущен", err)
	}
	if _, err = p.Run(context.Background(), "unexpected"); err == nil || p.Polls() != 0 {
		t.Fatal("чужой run принят")
	}
	done, err := p.Run(context.Background(), first.ID)
	if err != nil || done.Status != "SUCCEEDED" || p.Polls() != 1 || p.Starts() != 1 {
		t.Fatal("асинхронный Actor не завершён", err)
	}
}

func TestSyntheticAIRejectsUnknownRequests(t *testing.T) {
	p := &Providers{}
	ctx := context.Background()
	models, err := p.DecisionModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(models, &catalog) != nil || len(catalog.Data) != 1 || catalog.Data[0].ID != aiproviders.JEVModelID {
		t.Fatal("incorrect synthetic model")
	}
	raw, err := p.Decisions(ctx, json.RawMessage(`{"state":"STAGING_SYNTHETIC_REJECTED","questions":{"role_1":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var decision struct {
		Answers map[string]struct {
			Choice string `json:"choice"`
		} `json:"answers"`
	}
	if json.Unmarshal(raw, &decision) != nil || decision.Answers["role_1"].Choice != "reject" {
		t.Fatal("rejection missing")
	}
	if _, err := p.Responses(ctx, json.RawMessage(`{"prompt":"unexpected"}`)); err == nil {
		t.Fatal("unknown prompt accepted")
	}
	if _, err := Worker(parser.RunRepository{}, "secret", p); err == nil {
		t.Fatal("missing database accepted")
	}
}
