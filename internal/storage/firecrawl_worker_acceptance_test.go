//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/firecrawl"
	"itmap/internal/parser"
	"itmap/internal/profiles"
	"itmap/internal/vacancies"
)

type firecrawlAcceptanceProvider struct {
	calls            []string
	listing, vacancy string
}

func (p *firecrawlAcceptanceProvider) Execute(_ context.Context, lease *parser.JobLease, step parser.FirecrawlScrapeStep) (firecrawl.ScrapeResult, error) {
	kind := lease.Job().Type
	p.calls = append(p.calls, kind)
	switch {
	case kind == "scan_source" && step.URL == p.listing:
		return firecrawl.ScrapeResult{FinalURL: step.URL, TargetStatus: 200, Markdown: "[CI_TITLE](" + p.vacancy + ")"}, nil
	case kind == "fetch_full_text" && step.URL == p.vacancy:
		return firecrawl.ScrapeResult{FinalURL: step.URL, TargetStatus: 200, Markdown: hhDetailBody()}, nil
	default:
		return firecrawl.ScrapeResult{}, fmt.Errorf("unexpected synthetic scrape: %s %s", kind, step.URL)
	}
}

type firecrawlAcceptanceAI struct {
	extraction *extractionRunner
	roles      *leafStageRunner
}

func (p firecrawlAcceptanceAI) Automatic(ctx context.Context, req aiproviders.GenerationRequest, model profiles.ModelInput, billing aiproviders.Billing) (aiproviders.AIResult, error) {
	switch req.Task {
	case "vacancy_extraction":
		return p.extraction.Automatic(ctx, req, model, billing)
	case "vacancy_role":
		return p.roles.Automatic(ctx, req, model, billing)
	default:
		return aiproviders.AIResult{}, fmt.Errorf("unexpected synthetic AI task: %s", req.Task)
	}
}

func TestMariaDBFirecrawlWorkerDisposable(t *testing.T) {
	t.Run("companySite", func(t *testing.T) {
		testFirecrawlWorker(t, "companySite", "https://careers.company.com/jobs", "https://careers.company.com/jobs/42", `{}`)
	})
	t.Run("getmatch", func(t *testing.T) {
		testFirecrawlWorker(t, "getmatch", "https://getmatch.ru/companies/ci", "https://getmatch.ru/vacancies/42", `{"adapter":"getmatch","html":false,"titleClass":"","detailPathPrefix":"","allowedHosts":[],"pagination":null,"useEnhancedProxy":false}`)
	})
}

func testFirecrawlWorker(t *testing.T, source, listing, vacancyURL, config string) {
	f := openSchedulerFixture(t)
	f.exec(`UPDATE firecrawl_settings SET enabled=1,key_encrypted='CI_SYNTHETIC',favorites_only=0,
		scheduler_json='{"enabled":true,"ownerName":"CI_OWNER","sourceIds":[],"intervalDays":1,"dayBudgetUsd":5,"monthBudgetUsd":5}' WHERE id=1`)
	f.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.sourceIds',JSON_ARRAY(?)) WHERE id=1`, source)
	t.Cleanup(func() {
		for _, query := range []string{
			"DELETE FROM vacancy_import_operations WHERE candidate_id IN (SELECT id FROM vacancy_candidates WHERE company_id=94901)",
			"DELETE FROM company_vacancies WHERE company_name='CI Firecrawl Pipeline'",
			"DELETE FROM parser_candidate_intakes WHERE candidate_token IN (SELECT creation_token FROM vacancy_candidates WHERE company_id=94901)",
			"DELETE FROM vacancy_candidates WHERE company_id=94901",
			"DELETE FROM vacancy_source_parsing WHERE company_id=94901",
			"DELETE FROM company_crm WHERE company_name='CI Firecrawl Pipeline'",
			"DELETE FROM company_identity WHERE id=94901",
			"DELETE FROM app_catalog WHERE id=1",
			"DELETE FROM vacancy_roles WHERE id=94902",
			"DELETE FROM vacancy_roles WHERE id=94901",
			"DELETE FROM vacancy_detail_settings WHERE id=1",
			"DELETE FROM vacancy_role_settings WHERE id=1",
		} {
			if _, err := f.db.ExecContext(context.Background(), query); err != nil {
				t.Errorf("firecrawl fixture cleanup %q: %v", query, err)
			}
		}
	})
	f.exec("INSERT INTO app_catalog(id,data_json) VALUES(1,?)", `{"companies":[{"name":"CI Firecrawl Pipeline"}],"agencies":[]}`)
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(94901,'CI Firecrawl Pipeline')")
	f.exec("INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,enabled,auto_enabled,config_json) VALUES(94901,?,?,1,1,?)", source, listing, config)
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order) VALUES(94901,'ci-firecrawl-root','CI Engineering',0)")
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order) VALUES(94902,'ci-firecrawl-leaf','CI Engineer',94901,0)")
	f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled) VALUES(94901,'CI title prompt',1,1)")
	f.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision) VALUES(1,'CI extraction prompt',1,'openai','gpt-6-luna','medium',1)")
	f.exec("INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision) VALUES(1,'CI roles prompt',1,'openai','gpt-6-luna','best_free','medium',1)")

	data := map[string]any{"title": "CI_TITLE", "description_md": hhDetailBody()}
	for _, field := range []string{"employer_name", "location", "work_format", "employment_type", "level", "salary_from", "salary_to", "salary_currency", "salary_period", "salary_tax_type", "responsibilities", "requirements", "conditions", "published_at", "language"} {
		data[field] = nil
	}
	data["level"] = "Senior"
	content, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	provider := &firecrawlAcceptanceProvider{listing: listing, vacancy: vacancyURL}
	title := &storedTitleRunner{answers: map[string]struct{ Type, Choice string }{"role_94901": {"choice", "match"}}}
	extraction := &extractionRunner{content: content}
	roles := &leafStageRunner{content: json.RawMessage(`{"role_ids":[94902]}`), model: "gpt-6-luna", provider: "openai"}
	worker := parser.Worker{Runs: f.repo, Settings: apifysettings.Repository{DB: f.db}, Firecrawl: provider,
		Classifier: title, Generator: firecrawlAcceptanceAI{extraction: extraction, roles: roles}}
	for i, stage := range []string{"scan_source", "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "idle"} {
		result, err := worker.Cycle(f.ctx, parser.WorkerOptions{
			Run:          parser.RunOptions{RequestID: fmt.Sprintf("CI_FIRECRAWL_BUNDLE_%d", i), Trigger: "automatic"},
			QueueProfile: "companySite", MaxJobs: 1,
		})
		want := 1
		if stage == "idle" {
			want = 0
		}
		if err != nil || result.Run.Status != "completed" || result.Processed != want {
			t.Fatalf("%s: result=%+v err=%v", stage, result, err)
		}
		if stage != "idle" && f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE source_id=? AND type=? AND status='completed'", source, stage) != 1 {
			t.Fatalf("stage %s lacks completion receipt", stage)
		}
	}
	if len(provider.calls) != 2 || provider.calls[0] != "scan_source" || provider.calls[1] != "fetch_full_text" ||
		title.calls != 1 || extraction.calls != 1 || roles.calls != 1 {
		t.Fatalf("provider calls=%v title=%d extraction=%d roles=%d", provider.calls, title.calls, extraction.calls, roles.calls)
	}
	var vacancy int64
	err = f.db.QueryRowContext(f.ctx, "SELECT id FROM company_vacancies WHERE company_name='CI Firecrawl Pipeline' AND url=?", vacancyURL).Scan(&vacancy)
	if err != nil {
		t.Fatal("published vacancy:", err)
	}
	detail, err := (vacancies.Repository{DB: f.db}).DetailByID(f.ctx, "CI_OWNER", vacancy)
	if err != nil || detail.Vacancy.Title != "CI_TITLE" || detail.Vacancy.DescriptionMD == "" || len(detail.Vacancy.PublicationSources) != 1 {
		t.Fatalf("published vacancy readback: %v", err)
	}
	if f.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE vacancy_id=? AND status='success' AND action='create'", vacancy) != 1 {
		t.Fatal("expected a single publication receipt")
	}
}
