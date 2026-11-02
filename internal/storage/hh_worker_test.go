//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/parser"
	"itmap/internal/profiles"
)

type workerGenerator struct {
	extraction *extractionRunner
	roles      *leafStageRunner
	relevance  *relevanceRunner
}

func (g workerGenerator) Automatic(ctx context.Context, req aiproviders.GenerationRequest, cfg profiles.ModelInput, bill aiproviders.Billing) (aiproviders.AIResult, error) {
	switch req.Task {
	case "vacancy_extraction":
		return g.extraction.Automatic(ctx, req, cfg, bill)
	case "vacancy_role":
		return g.roles.Automatic(ctx, req, cfg, bill)
	case "relevance":
		return g.relevance.Automatic(ctx, req, cfg, bill)
	}
	return aiproviders.AIResult{}, errors.New("CI unexpected task")
}

func TestMariaDBHHWorkerDisposable(t *testing.T) {
	provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
	_, s, scan, listing := hhScanFixture(t, provider)
	s.exec("INSERT INTO vacancy_roles(id,name,stable_key,parent_id,sort_order) VALUES(991304,'CI HH leaf','ci-hh-leaf',991303,1)")
	s.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision) VALUES(1,'CI extraction prompt',1,'openai','gpt-6-luna','medium',1)")
	s.exec("INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision) VALUES(1,'CI roles prompt',1,'openai','gpt-6-luna','best_free','medium',1)")
	s.exec("INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('CI_OWNER','CI synthetic resume',1)")
	s.exec("INSERT INTO system_prompts(kind,markdown,revision) VALUES('relevance','CI relevance prompt',1)")
	s.exec("INSERT INTO prompt_model_settings(kind,provider,openai_model,openrouter_model,reasoning_effort,revision) VALUES('relevance','openrouter','','jev','medium',1)")
	s.exec("INSERT INTO openrouter_settings(id,revision) VALUES(1,1)")
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM vacancy_detail_leases", "DELETE FROM vacancy_import_write_context", "DELETE FROM company_vacancies WHERE company_name='CI Apify Runtime'", "DELETE FROM company_vacancy_state WHERE company_name='CI Apify Runtime'", "DELETE FROM company_crm WHERE company_name='CI Apify Runtime'", "DELETE FROM user_profiles WHERE user_name='CI_OWNER'", "DELETE FROM system_prompts WHERE kind='relevance'", "DELETE FROM prompt_model_settings WHERE kind='relevance'", "DELETE FROM openrouter_settings WHERE id=1", "DELETE FROM vacancy_detail_settings WHERE id=1", "DELETE FROM vacancy_role_settings WHERE id=1", "DELETE FROM vacancy_roles WHERE id=991304"} {
			if _, err := s.db.ExecContext(context.Background(), q); err != nil {
				t.Error("worker cleanup", err)
			}
		}
	})
	var run string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := scan.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, run, "completed"); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"title": "CI_TITLE", "description_md": hhDetailBody()}
	for _, k := range []string{"employer_name", "location", "work_format", "employment_type", "level", "salary_from", "salary_to", "salary_currency", "salary_period", "salary_tax_type", "responsibilities", "requirements", "conditions", "published_at", "language"} {
		data[k] = nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	extraction := &extractionRunner{content: raw}
	roles := &leafStageRunner{content: json.RawMessage(`{"role_ids":[991304]}`), model: "gpt-6-luna", provider: "openai"}
	relevance := &relevanceRunner{}
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime, Public: &hhPublicFixture{job: &hhpublic.Job{ID: "123", URL: "https://hh.ru/vacancy/123", Title: "CI_TITLE", Description: hhDetailBody()}}, Generator: workerGenerator{extraction, roles, relevance}, Classifier: &storedTitleRunner{answers: map[string]struct{ Type, Choice string }{"role_991303": {"choice", "match"}}}}
	result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER"}, MaxJobs: 20})
	if err != nil {
		t.Fatal("cycle", result, err)
	}
	if result.Processed != 7 || provider.starts != 1 || extraction.calls != 1 || roles.calls != 1 || relevance.calls != 1 {
		t.Fatalf("cycle result %+v provider %d extraction %d roles %d relevance %d", result, provider.starts, extraction.calls, roles.calls, relevance.calls)
	}
	if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") != 1 {
		t.Fatal("vacancy or relevance missing")
	}
	result, err = w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_REPLAY"}, MaxJobs: 20})
	if err != nil || result.Processed != 0 || provider.starts != 1 {
		t.Fatal("replay", result, err)
	}
}
