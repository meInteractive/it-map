//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/parser"
	"itmap/internal/vacancies"
)

type e2eTitleClassifier struct{ calls int }

func (r *e2eTitleClassifier) ClassifyTitles(ctx context.Context, title string, roles []aiproviders.TitleRole, b aiproviders.Billing, check func(context.Context) error) (aiproviders.TitleResult, error) {
	if title != "CI_TITLE" && title != "CI_REJECTED" {
		return aiproviders.TitleResult{}, errors.New("unexpected synthetic title")
	}
	choices := map[string]struct{ Type, Choice string }{"role_991303": {"choice", "match"}}
	if title == "CI_REJECTED" {
		choices["role_991303"] = struct{ Type, Choice string }{"choice", "reject"}
	}
	runner := &storedTitleRunner{answers: choices}
	result, err := runner.ClassifyTitles(ctx, "CI_TITLE", roles, b, check)
	r.calls += runner.calls
	return result, err
}

func TestMariaDBParserE2EDisposable(t *testing.T) {
	for _, kind := range []string{"manual", "tick"} {
		t.Run(kind, func(t *testing.T) {
			provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE"), hhScanItem(124, "CI_REJECTED")}}}
			_, s, lease, listing := hhScanFixture(t, provider)
			var previousRun string
			if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&previousRun); err != nil {
				t.Fatal(err)
			}
			if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "cancelled"}); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.Finish(s.ctx, previousRun, "completed"); err != nil {
				t.Fatal(err)
			}
			s.exec("DELETE FROM scheduler_jobs WHERE id='CI_HH_SCAN'")
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
						t.Error("e2e cleanup", err)
					}
				}
			})
			data := map[string]any{"title": "CI_TITLE", "description_md": hhDetailBody(), "level": "Senior"}
			for _, k := range []string{"employer_name", "location", "work_format", "employment_type", "salary_from", "salary_to", "salary_currency", "salary_period", "salary_tax_type", "responsibilities", "requirements", "conditions", "published_at", "language"} {
				data[k] = nil
			}
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			extract := &extractionRunner{content: raw}
			roles := &leafStageRunner{content: json.RawMessage(`{"role_ids":[991304]}`), model: "gpt-6-luna", provider: "openai"}
			relevance := &relevanceRunner{}
			classifier := &e2eTitleClassifier{}
			factory := func(r parser.RunRepository) (parser.Worker, error) {
				return parser.Worker{Runs: r, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime, Public: &hhPublicFixture{job: &hhpublic.Job{ID: "123", URL: "https://hh.ru/vacancy/123", Title: "CI_TITLE", Description: hhDetailBody()}}, Generator: workerGenerator{extract, roles, relevance}, Classifier: classifier}, nil
			}
			control := parser.NewCycleControlWithFactory(s.db, factory)
			manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw", "demo": "CI_pw"})
			if err != nil {
				t.Fatal(err)
			}
			user, ok := manager.Authenticate("eugene", "CI_pw")
			if !ok {
				t.Fatal("synthetic login")
			}
			token, err := manager.Issue(user, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, SchedulerCycle: control, SchedulerTick: control, SchedulerToken: "CI_TICK_TOKEN", VacancyDetails: vacancies.Repository{DB: s.db}})
			server := httptest.NewServer(handler)
			defer func() { server.Close() }()
			type response struct {
				Code int
				Body []byte
			}
			call := func(id string) response {
				path := "/api/scheduler/run"
				if kind == "tick" {
					path = "/api/internal/scheduler/tick"
				}
				body, _ := json.Marshal(map[string]string{"requestId": id})
				request, err := http.NewRequestWithContext(s.ctx, "POST", server.URL+path, strings.NewReader(string(body)))
				if err != nil {
					t.Fatal(err)
				}
				if kind == "tick" {
					request.Header.Set("Authorization", "Bearer CI_TICK_TOKEN")
				} else {
					request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
				}
				res, err := server.Client().Do(request)
				if err != nil {
					t.Fatal("HTTP cycle transport", err)
				}
				defer res.Body.Close()
				raw, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal("HTTP cycle body", err)
				}
				return response{res.StatusCode, raw}
			}
			requestID := "12345678-1234-4234-8234-123456789abc"
			if kind == "tick" {
				requestID = "CI_E2E_TICK"
			}
			first := call(requestID)
			var result struct {
				Status    string `json:"status"`
				Processed int    `json:"processed"`
				Errors    int    `json:"errors"`
			}
			if first.Code != http.StatusOK || json.Unmarshal(first.Body, &result) != nil || result.Status != "completed" || result.Processed != 8 || result.Errors != 0 {
				t.Fatal("e2e cycle", first.Code, result)
			}
			if provider.starts != 1 || classifier.calls != 2 || extract.calls != 1 || roles.calls != 1 || relevance.calls != 1 {
				t.Fatal("provider counts", provider.starts, classifier.calls, extract.calls, roles.calls, relevance.calls)
			}
			if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE title_raw='CI_REJECTED' AND main_vacancy_id IS NULL") != 1 || s.count("SELECT COUNT(*) FROM vacancy_classifications WHERE decision='reject'") != 1 {
				t.Fatal("accepted or rejected vacancy mismatch")
			}
			var vacancyID int64
			if err = s.db.QueryRowContext(s.ctx, "SELECT id FROM company_vacancies WHERE company_name='CI Apify Runtime'").Scan(&vacancyID); err != nil {
				t.Fatal(err)
			}
			if s.count("SELECT COUNT(*) FROM vacancy_role_assignments WHERE vacancy_id=? AND role_id=991304", vacancyID) != 1 || s.count("SELECT COUNT(*) FROM vacancy_publication_sources WHERE vacancy_id=?", vacancyID) != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance=67", vacancyID) != 1 {
				t.Fatal("missing role, source or personal score")
			}
			if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE reserved_usd>0 AND cost_usd IS NULL") != 0 || s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE cost_usd>0") != 5 || s.count("SELECT COUNT(*) FROM apify_runs WHERE ABS(charged_usd-0.02)<0.0000001") != 1 || s.count("SELECT COUNT(*) FROM (SELECT SUM(cost_usd) AS total FROM scheduler_attempts) AS amounts WHERE ABS(total-0.003004)<0.0000001") != 1 {
				t.Fatal("billing receipts")
			}
			detailRequest, err := http.NewRequestWithContext(s.ctx, "GET", fmt.Sprintf("%s/api/vacancies/%d", server.URL, vacancyID), nil)
			if err != nil {
				t.Fatal(err)
			}
			detailRequest.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			detail, err := server.Client().Do(detailRequest)
			if err != nil {
				t.Fatal("HTTP detail transport", err)
			}
			var item vacancies.DetailResponse
			decodeErr := json.NewDecoder(detail.Body).Decode(&item)
			detail.Body.Close()
			if detail.StatusCode != http.StatusOK || decodeErr != nil || item.Vacancy.ID != vacancyID || item.Vacancy.Title != "CI_TITLE" || item.Vacancy.DescriptionMD == "" || len(item.Vacancy.PublicationSources) != 1 {
				t.Fatal("API vacancy readback", detail.StatusCode)
			}
			replay := call(requestID)
			if replay.Code != http.StatusOK || !strings.Contains(string(replay.Body), `"reused":true`) || provider.starts != 1 {
				t.Fatal("same request replay", replay.Code)
			}
			// Новый handler и control имитируют перезапуск процесса с той же БД.
			server.Close()
			control = parser.NewCycleControlWithFactory(s.db, factory)
			handler = httpapi.NewHandlerWithOptions(nil, httpapi.Options{Auth: manager, SchedulerCycle: control, SchedulerTick: control, SchedulerToken: "CI_TICK_TOKEN", VacancyDetails: vacancies.Repository{DB: s.db}})
			server = httptest.NewServer(handler)
			restartID := "12345678-1234-4234-8234-123456789abd"
			if kind == "tick" {
				restartID = "CI_E2E_RESTART"
			}
			restart := call(restartID)
			if restart.Code != http.StatusOK || provider.starts != 1 || classifier.calls != 2 || extract.calls != 1 || roles.calls != 1 || relevance.calls != 1 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 {
				t.Fatal("restart repeated work", restart.Code)
			}
		})
	}
}
