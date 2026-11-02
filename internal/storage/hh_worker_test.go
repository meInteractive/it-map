//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/integrations/hhpublic"
	"itmap/internal/parser"
	"itmap/internal/profiles"
	"itmap/internal/vacancies"
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
	data["level"] = "Senior"
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
	var vacancyID int64
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM company_vacancies WHERE company_name='CI Apify Runtime'").Scan(&vacancyID); err != nil {
		t.Fatal(err)
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
	request := httptest.NewRequest("GET", fmt.Sprintf("/api/vacancies/%d", vacancyID), nil)
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var api vacancies.DetailResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &api) != nil || api.Vacancy.ID != vacancyID || api.Vacancy.Title != "CI_TITLE" || api.Vacancy.DescriptionMD == "" || len(api.Vacancy.PublicationSources) != 1 {
		t.Fatal("API readback", response.Code)
	}
	result, err = w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_REPLAY"}, MaxJobs: 20})
	if err != nil || result.Processed != 0 || provider.starts != 1 {
		t.Fatal("replay", result, err)
	}
}

func TestMariaDBHHWorkerUncertainDisposable(t *testing.T) {
	s, lease, _, vacancy := hhRelevanceFixture(t)
	var run string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
		t.Fatal(err)
	}
	job := lease.Job().ID
	if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, run, "completed"); err != nil {
		t.Fatal(err)
	}
	runner := &relevanceRunner{after: func() {
		s.exec("UPDATE user_profiles SET resume_md='CI changed resume',revision=revision+1 WHERE user_name='CI_OWNER'")
	}}
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Generator: runner}
	_, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_STALE"}, MaxJobs: 20})
	if err == nil || runner.calls != 1 {
		t.Fatal("stale paid result accepted", err, runner.calls)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND status='interrupted' AND reserved_usd>0 AND cost_usd IS NULL", job) != 1 {
		t.Fatal("unknown cost not preserved")
	}
	if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance IS NOT NULL", vacancy) != 0 {
		t.Fatal("stale score saved")
	}
	var failedRun string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&failedRun); err != nil {
		t.Fatal(err)
	}
	s.exec("UPDATE scheduler_runs SET lease_until=1 WHERE id=?", failedRun)
	s.exec("UPDATE scheduler_jobs SET status='pending',run_after=0,attempts=0,completed_at=NULL WHERE id=?", job)
	s.exec("UPDATE user_profiles SET resume_md='CI synthetic resume',revision=1 WHERE user_name='CI_OWNER'")
	_, err = w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_RECOVER"}, MaxJobs: 20})
	if !errors.Is(err, parser.ErrMoneyUncertain) || runner.calls != 1 {
		t.Fatal("uncertain cost allowed another request", err, runner.calls)
	}
}

func TestMariaDBHHWorkerCrashDisposable(t *testing.T) {
	if os.Getenv("ITMAP_HH_WORKER_CRASH_CHILD") == "1" {
		ctx, db := schedulerDB(t)
		var generation string
		if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil {
			t.Fatal(err)
		}
		runner := &relevanceRunner{after: func() { fmt.Println("CI_HH_WORKER_PAID_READY"); select {} }}
		w := parser.Worker{Runs: parser.RunRepository{DB: db, Generation: generation}, Settings: apifysettings.NewRepository(db, "CI_SECRET"), Generator: runner}
		_, err := w.Cycle(ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_CRASH"}, MaxJobs: 20})
		t.Fatal("child ended before kill", err)
	}
	s, lease, _, _ := hhRelevanceFixture(t)
	job := lease.Job().ID
	var run string
	if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "pending", Deferred: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.Finish(s.ctx, run, "completed"); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestMariaDBHHWorkerCrashDisposable$", "-test.count=1")
	child.Env = append(os.Environ(), "ITMAP_HH_WORKER_CRASH_CHILD=1")
	pipe, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if scanner.Text() == "CI_HH_WORKER_PAID_READY" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("worker child not ready")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker child timeout")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if child.Wait() == nil {
		t.Fatal("worker child exited without SIGKILL")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND status='running' AND reserved_usd>0 AND cost_usd IS NULL", job) != 1 {
		t.Fatal("crash lost reservation")
	}
	s.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
	s.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id=?", job)
	runner := &relevanceRunner{}
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Generator: runner}
	_, err = w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_AFTER_CRASH"}, MaxJobs: 20})
	if !errors.Is(err, parser.ErrMoneyUncertain) || runner.calls != 0 {
		t.Fatal("crash recovery allowed paid replay", err, runner.calls)
	}
}
