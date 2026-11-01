//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/parser"
	"itmap/internal/vacancies"
)

func hhImportFixture(t *testing.T) (*schedulerFixture, *parser.JobLease, int64, parser.HHImportExecutor) {
	t.Helper()
	_, s, detail, candidate := hhDetailFixture(t)
	s.exec("INSERT INTO vacancy_roles(id,name,stable_key,parent_id,sort_order) VALUES(991304,'CI HH leaf','ci-hh-leaf',991303,1)")
	s.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision) VALUES(1,'CI extraction prompt',1,'openai','gpt-6-luna','medium',1)")
	s.exec("INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision) VALUES(1,'CI roles prompt',1,'openai','gpt-6-luna','best_free','medium',1)")
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM vacancy_detail_leases WHERE lease_key='import:global'", "DELETE FROM vacancy_import_write_context", "DELETE FROM company_vacancies WHERE company_name='CI Apify Runtime'", "DELETE FROM company_vacancy_state WHERE company_name='CI Apify Runtime'", "DELETE FROM company_crm WHERE company_name='CI Apify Runtime'", "DELETE FROM vacancy_detail_settings WHERE id=1", "DELETE FROM vacancy_role_settings WHERE id=1", "DELETE FROM vacancy_roles WHERE id=991304"} {
			if _, err := s.db.ExecContext(context.Background(), q); err != nil {
				t.Error("import cleanup", err)
			}
		}
	})
	pub := &hhPublicFixture{job: &hhpublic.Job{ID: "123", URL: "https://hh.ru/vacancy/123", Title: "CI_TITLE", Description: hhDetailBody()}}
	if _, err := (parser.HHFullTextExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER", Public: pub}).Execute(s.ctx, detail); err != nil {
		t.Fatal("detail", err)
	}
	if err := detail.Finish(s.ctx, parser.FinishOptions{Status: "completed"}); err != nil {
		t.Fatal("finish detail", err)
	}
	claim := func(kind string) *parser.JobLease {
		var job, run string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_jobs WHERE candidate_id=? AND type=?", candidate, kind).Scan(&job); err != nil {
			t.Fatal("job", kind, err)
		}
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
			t.Fatal(err)
		}
		lease, err := s.repo.ClaimJob(s.ctx, run, parser.ClaimOptions{JobID: &job})
		if err != nil || lease == nil {
			t.Fatal("claim", kind, err)
		}
		return lease
	}
	data := map[string]any{"title": "CI_TITLE", "description_md": hhDetailBody()}
	for _, k := range []string{"employer_name", "location", "work_format", "employment_type", "level", "salary_from", "salary_to", "salary_currency", "salary_period", "salary_tax_type", "responsibilities", "requirements", "conditions", "published_at", "language"} {
		data[k] = nil
	}
	data["level"] = "Senior"
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	extract := parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: &extractionRunner{content: raw}}
	if _, err = extract.Execute(s.ctx, claim("extract_vacancy")); err != nil {
		t.Fatal("extract", err)
	}
	leaf := parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: &leafStageRunner{content: json.RawMessage(`{"role_ids":[991304]}`), model: "gpt-6-luna", provider: "openai"}}
	if _, err = leaf.Execute(s.ctx, claim("assign_roles")); err != nil {
		t.Fatal("roles", err)
	}
	return s, claim("import_vacancy"), candidate, parser.HHImportExecutor{Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Owner: "CI_OWNER"}
}

func TestMariaDBHHImportDisposable(t *testing.T) {
	t.Run("create concurrent replay and current source", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		var run string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
			t.Fatal(err)
		}
		jobID := lease.Job().ID
		second, err := s.repo.ClaimJob(s.ctx, run, parser.ClaimOptions{JobID: &jobID})
		if err != nil || second != nil {
			t.Fatal("second worker acquired running job", err)
		}
		var wg sync.WaitGroup
		results := make([]parser.HHImportResult, 2)
		errs := make([]error, 2)
		for i := range results {
			wg.Add(1)
			go func(i int) { defer wg.Done(); results[i], errs[i] = executor.Execute(s.ctx, lease) }(i)
		}
		wg.Wait()
		for i := range results {
			if errs[i] != nil {
				t.Fatal("import", i, errs[i])
			}
		}
		if results[0].VacancyID != results[1].VacancyID || results[0].Reused == results[1].Reused || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 || s.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE candidate_id=? AND status='success'", candidate) != 1 || s.count("SELECT COUNT(*) FROM vacancy_role_assignments WHERE vacancy_id=? AND role_id=991304", results[0].VacancyID) != 1 {
			t.Fatalf("unexpected concurrent result %+v", results)
		}
		var title string
		if err := s.db.QueryRowContext(s.ctx, "SELECT title FROM company_vacancies WHERE id=?", results[0].VacancyID).Scan(&title); err != nil || title != "CI_TITLE" {
			t.Fatal("vacancy readback", err)
		}
		detail, err := (vacancies.Repository{DB: s.db}).DetailByID(s.ctx, "CI_OWNER", results[0].VacancyID)
		if err != nil || detail.Vacancy.Title != "CI_TITLE" || detail.Vacancy.DescriptionMD == "" || len(detail.Vacancy.PublicationSources) != 1 {
			t.Fatal("Go detail readback", err)
		}
		manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "admin-secret", "demo": "demoPassword"})
		if err != nil {
			t.Fatal(err)
		}
		user, ok := manager.Authenticate("demo", "demoPassword")
		if !ok {
			t.Fatal("synthetic login")
		}
		token, err := manager.Issue(user, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, VacancyDetails: vacancies.Repository{DB: s.db}})
		request := httptest.NewRequest("GET", fmt.Sprintf("/api/vacancies/%d", results[0].VacancyID), nil)
		request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var api vacancies.DetailResponse
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &api) != nil || api.Vacancy.ID != results[0].VacancyID || api.Vacancy.Title != "CI_TITLE" {
			t.Fatal("authenticated API readback", response.Code)
		}
		s.exec("UPDATE vacancy_source_parsing SET enabled=0 WHERE company_id=991302 AND source_id='hh'")
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("disabled source replay accepted")
		}
		s.exec("UPDATE vacancy_source_parsing SET enabled=1 WHERE company_id=991302 AND source_id='hh'")
		s.exec("UPDATE vacancy_import_operations SET changes_json='{}' WHERE candidate_id=?", candidate)
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("invalid receipt replay accepted")
		}
	})
	t.Run("changed role dependencies block import", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		s.exec("UPDATE vacancy_role_settings SET markdown='CI changed roles',prompt_revision=prompt_revision+1 WHERE id=1")
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("stale role result accepted")
		}
		if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=? AND main_vacancy_id IS NOT NULL", candidate) != 0 || s.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE candidate_id=?", candidate) != 0 {
			t.Fatal("stale import persisted")
		}
	})
	t.Run("changed source blocks queued import", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		s.exec("UPDATE vacancy_source_parsing SET source_url='https://hh.ru/employer/999',revision=revision+1 WHERE company_id=991302 AND source_id='hh'")
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("changed source accepted")
		}
		if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=? AND main_vacancy_id IS NOT NULL", candidate) != 0 {
			t.Fatal("changed source imported")
		}
	})
	t.Run("uninitialized legacy jobs block import", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		s.exec("UPDATE app_catalog SET data_json=JSON_SET(data_json,'$.companies[0].jobs',JSON_ARRAY(JSON_OBJECT('title',' ','url',' '))) WHERE id=1")
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("uninitialized catalog jobs accepted")
		}
		if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=? AND main_vacancy_id IS NOT NULL", candidate) != 0 || s.count("SELECT COUNT(*) FROM company_vacancy_state WHERE company_name='CI Apify Runtime'") != 0 {
			t.Fatal("uninitialized catalog marked imported")
		}
	})
	t.Run("active global import lease blocks and expired lease recovers", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		s.exec("INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at) VALUES('import:global',?,'CI_OTHER',?)", s.repo.Generation, time.Now().Add(time.Minute).UnixMilli())
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("active import lease ignored")
		}
		if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=? AND main_vacancy_id IS NOT NULL", candidate) != 0 {
			t.Fatal("busy import persisted")
		}
		s.exec("UPDATE vacancy_detail_leases SET expires_at=1 WHERE lease_key='import:global'")
		if _, err := executor.Execute(s.ctx, lease); err != nil {
			t.Fatal("expired import lease", err)
		}
	})
	t.Run("previous generation import lease does not block", func(t *testing.T) {
		s, lease, _, executor := hhImportFixture(t)
		s.exec("INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at) VALUES('import:global','00000000-0000-4000-8000-000000000000','CI_OLD',?)", time.Now().Add(time.Minute).UnixMilli())
		if _, err := executor.Execute(s.ctx, lease); err != nil {
			t.Fatal("previous generation lease", err)
		}
	})
	t.Run("existing publication preserves manual title", func(t *testing.T) {
		s, lease, _, executor := hhImportFixture(t)
		s.exec("INSERT INTO company_crm(company_name) VALUES('CI Apify Runtime')")
		s.exec("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(991305,'CI Apify Runtime','https://moscow.hh.ru/vacancy/123?utm_source=old','CI manual title','')")
		s.exec("INSERT INTO vacancy_parser_fields(vacancy_id,field,value_json,manual) VALUES(991305,'title','\"CI old title\"',1)")
		result, err := executor.Execute(s.ctx, lease)
		if err != nil || result.VacancyID != 991305 || result.Action != "update" {
			t.Fatal("exact identity update", result, err)
		}
		if s.count("SELECT COUNT(*) FROM company_vacancies WHERE id=991305 AND title='CI manual title' AND description_md<>''") != 1 {
			t.Fatal("manual title was not preserved or description missing")
		}
	})
	t.Run("unchanged import preserves source timestamp", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		s.exec("INSERT INTO company_crm(company_name) VALUES('CI Apify Runtime')")
		s.exec("INSERT INTO company_vacancies(id,company_name,url,title,description_md,role,level) VALUES(991305,'CI Apify Runtime','https://hh.ru/vacancy/123','CI_TITLE',?,'CI HH leaf','Senior')", hhDetailBody())
		s.exec("INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(991305,991304)")
		s.exec("INSERT INTO vacancy_parser_roles(vacancy_id,role_id,candidate_id) VALUES(991305,991304,?)", candidate)
		s.exec("INSERT INTO vacancy_publication_sources(candidate_id,vacancy_id,url,canonical_url,extraction_id,updated_at) SELECT ?,991305,'https://hh.ru/vacancy/123','https://hh.ru/vacancy/123',id,'CI_OLD' FROM vacancy_extractions WHERE candidate_id=? AND status='success'", candidate, candidate)
		s.exec("INSERT INTO vacancy_publication_keys(identity_key,vacancy_id) SELECT identity_key,991305 FROM vacancy_candidate_identities WHERE candidate_id=?", candidate)
		s.exec("INSERT INTO vacancy_publication_keys(identity_key,vacancy_id) VALUES(?,991305) ON DUPLICATE KEY UPDATE vacancy_id=VALUES(vacancy_id)", "url:"+testStageHash("https://hh.ru/vacancy/123"))
		result, err := executor.Execute(s.ctx, lease)
		if err != nil || result.Action != "unchanged" || result.VacancyID != 991305 || s.count("SELECT COUNT(*) FROM vacancy_publication_sources WHERE candidate_id=? AND updated_at='CI_OLD'", candidate) != 1 || s.count("SELECT COUNT(*) FROM vacancy_parser_fields WHERE vacancy_id=991305") != 0 {
			t.Fatal("unchanged import", result, err)
		}
	})
	t.Run("cross source content links existing vacancy", func(t *testing.T) {
		s, lease, _, executor := hhImportFixture(t)
		s.exec("INSERT INTO company_crm(company_name) VALUES('CI Apify Runtime')")
		s.exec("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(991305,'CI Apify Runtime','https://career.example.org/jobs/1','CI_TITLE',?)", hhDetailBody())
		result, err := executor.Execute(s.ctx, lease)
		if err != nil || result.VacancyID != 991305 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 {
			t.Fatal("cross source match", result, err)
		}
	})
	t.Run("ambiguous cross source content fails closed", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		s.exec("INSERT INTO company_crm(company_name) VALUES('CI Apify Runtime')")
		s.exec("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(991305,'CI Apify Runtime','https://career.example.org/jobs/1','CI_TITLE',?),(991306,'CI Apify Runtime','https://other.example.org/jobs/2','CI_TITLE',?)", hhDetailBody(), hhDetailBody())
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("ambiguous publications accepted")
		}
		if s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=? AND main_vacancy_id IS NOT NULL", candidate) != 0 || s.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE candidate_id=?", candidate) != 0 {
			t.Fatal("ambiguous import persisted")
		}
	})
	t.Run("late failure rolls back new vacancy", func(t *testing.T) {
		s, lease, candidate, executor := hhImportFixture(t)
		// Следующий AUTO_INCREMENT известен только внутри одноразовой CI-БД.
		var next int64
		if err := s.db.QueryRowContext(s.ctx, "SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='company_vacancies'").Scan(&next); err != nil {
			t.Fatal(err)
		}
		s.exec("INSERT INTO vacancy_import_write_context(vacancy_id,generation,owner_token) VALUES(?,?,?)", next, s.repo.Generation, "CI_CONFLICT")
		if _, err := executor.Execute(s.ctx, lease); err == nil {
			t.Fatal("late write failure accepted")
		}
		if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 0 || s.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE candidate_id=?", candidate) != 0 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=? AND main_vacancy_id IS NOT NULL", candidate) != 0 {
			t.Fatal("partial import survived rollback", fmt.Sprint(next))
		}
	})
}
