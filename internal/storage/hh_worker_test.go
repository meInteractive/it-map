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
	"itmap/internal/apifyruns"
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
	if s.count("SELECT COUNT(*) FROM scheduler_jobs j JOIN scheduler_attempts a ON a.job_id=j.id JOIN scheduler_runs r ON r.id=a.run_id WHERE j.id=? AND j.status='running' AND a.status='running' AND j.locked_until<r.lease_until", job) != 1 {
		t.Fatal("job lease outlives worker run")
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

func TestMariaDBHHWorkerGenerationDisposable(t *testing.T) {
	s, lease, _, vacancy := hhRelevanceFixture(t)
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
	defer s.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", s.repo.Generation)
	runner := &relevanceRunner{after: func() {
		s.exec("UPDATE app_runtime_generation SET generation='22222222-2222-4333-8444-555555555555' WHERE id=1")
	}}
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Generator: runner}
	_, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_GENERATION"}, MaxJobs: 20})
	if err == nil || runner.calls != 1 {
		t.Fatal("generation rotation accepted", err, runner.calls)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd>0 AND cost_usd IS NULL", job) != 1 {
		t.Fatal("generation rotation lost reserve")
	}
	if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance IS NOT NULL", vacancy) != 0 {
		t.Fatal("stale generation wrote score")
	}
}

type workerCancelPublic struct{ started chan struct{} }

func (p workerCancelPublic) Fetch(ctx context.Context, _ string) (*hhpublic.Job, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestMariaDBHHWorkerCancelDisposable(t *testing.T) {
	_, s, lease, _ := hhDetailFixture(t)
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
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	started := make(chan struct{})
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Public: workerCancelPublic{started}}
	finished := make(chan error, 1)
	go func() {
		_, err := w.Cycle(ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_CANCEL"}, MaxJobs: 20})
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation result", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not cancel")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='pending' AND locked_by=''", job) != 1 {
		t.Fatal("cancelled job not released")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='interrupted' AND id<>?", run) != 1 {
		t.Fatal("cancelled run not interrupted")
	}
}

func TestMariaDBHHWorkerActorLimitDisposable(t *testing.T) {
	t.Run("targeted company request is isolated and idempotent", func(t *testing.T) {
		provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
		_, s, lease, listing := hhScanFixture(t, provider)
		var run string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
			t.Fatal(err)
		}
		if err := lease.Finish(s.ctx, parser.FinishOptions{Status: "cancelled"}); err != nil {
			t.Fatal(err)
		}
		if err := s.repo.Finish(s.ctx, run, "completed"); err != nil {
			t.Fatal(err)
		}
		s.exec("INSERT INTO company_identity(id,company_name) VALUES(991305,'CI other HH company')")
		t.Cleanup(func() { _, _ = s.db.ExecContext(context.Background(), "DELETE FROM company_identity WHERE id=991305") })
		s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_OTHER',?,'CI_OTHER','scan_source',991305,'hh','{}',100,0,0,0)`, s.repo.Generation)
		control := parser.NewCycleControlWithFactory(s.db, func(r parser.RunRepository) (parser.Worker, error) {
			return parser.Worker{Runs: r, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime}, nil
		})
		command, err := control.PrepareTargetedHH(s.ctx)
		if err != nil {
			t.Fatal(err)
		}
		id := "12345678-1234-4234-8234-123456789abc"
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func() { _, e := command(s.ctx, id, 991302); results <- e }()
		}
		success := 0
		for i := 0; i < 2; i++ {
			e := <-results
			if e == nil {
				success++
			} else if !errors.Is(e, parser.ErrRunBusy) {
				t.Fatal("concurrent request", e)
			}
		}
		if success == 0 {
			t.Fatal("both requests rejected")
		}
		raw, err := command(s.ctx, id, 991302)
		if err != nil {
			t.Fatal("replay", err)
		}
		var receipt struct {
			JobID string `json:"jobId"`
		}
		if json.Unmarshal(raw, &receipt) != nil || receipt.JobID == "" {
			t.Fatal("target receipt missing scan job")
		}
		var candidate int64
		if err = s.db.QueryRowContext(s.ctx, "SELECT id FROM vacancy_candidates WHERE company_id=991302 LIMIT 1").Scan(&candidate); err != nil {
			t.Fatal(err)
		}
		s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,candidate_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_OTHER_CAND',?,'CI_OTHER_CAND','classify_title',991305,?,'hh','{}',100,0,0,0)`, s.repo.Generation, candidate)
		scope := &parser.ScanCandidateScope{RequestID: id, ScanJobID: receipt.JobID, CompanyID: 991302}
		classifier := &storedTitleRunner{answers: map[string]struct{ Type, Choice string }{"role_991303": {"choice", "reject"}}}
		worker := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), Classifier: classifier, CandidateScope: scope}
		stage, err := worker.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_SCOPED_STAGE"}, MaxJobs: 1})
		if err != nil || stage.Processed != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_OTHER_CAND' AND status='pending' AND attempts=0") != 1 {
			t.Fatal("candidate scope escaped", stage, err)
		}
		s.exec("UPDATE scheduler_jobs SET status='pending' WHERE id=?", receipt.JobID)
		partial, err := worker.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_PARTIAL_SCOPE"}, MaxJobs: 1})
		if err != nil || partial.Processed != 0 {
			t.Fatal("saved partial scope rejected", partial, err)
		}
		s.exec("UPDATE scheduler_jobs SET status='completed' WHERE id=?", receipt.JobID)
		s.exec("UPDATE vacancy_discovery_runs SET result_json=JSON_REMOVE(JSON_SET(result_json,'$.LineageVersion',0),'$.CandidateIDs') WHERE id=?", "go399:"+receipt.JobID)
		if _, err = worker.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_OLD_SCOPE"}, MaxJobs: 1}); !errors.Is(err, parser.ErrHHListingChanged) {
			t.Fatal("legacy scope accepted", err)
		}
		bad := *scope
		bad.CompanyID = 991305
		worker.CandidateScope = &bad
		if _, err = worker.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_BAD_SCOPE"}, MaxJobs: 1}); !errors.Is(err, parser.ErrHHListingChanged) {
			t.Fatal("foreign scope accepted", err)
		}
		if _, err = command(s.ctx, id, 991305); !errors.Is(err, parser.ErrRunBusy) && !errors.Is(err, parser.ErrHHListingChanged) {
			t.Fatal("request reused for other company", err)
		}
		if provider.starts != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_OTHER' AND status='pending' AND attempts=0") != 1 {
			t.Fatal("unexpected provider or queue activity", provider.starts)
		}
	})
	t.Run("targeted cycle leaves other jobs untouched", func(t *testing.T) {
		provider := &hhScanProvider{rows: [][]json.RawMessage{{hhScanItem(123, "CI_TITLE")}}}
		_, s, lease, listing := hhScanFixture(t, provider)
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
		s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE id='CI_HH_SCAN'")
		s.exec("INSERT INTO company_identity(id,company_name) VALUES(991305,'CI other HH company')")
		t.Cleanup(func() { _, _ = s.db.ExecContext(context.Background(), "DELETE FROM company_identity WHERE id=991305") })
		s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_OTHER',?,'CI_OTHER','scan_source',991305,'hh','{}',100,0,0,0)`, s.repo.Generation)
		w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime, TargetJobID: "CI_HH_SCAN"}
		result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_TARGETED"}, MaxJobs: 1})
		if err != nil || result.Processed != 1 || provider.starts != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_OTHER' AND status='pending' AND attempts=0") != 1 {
			t.Fatal("targeted cycle changed another job", result, provider.starts, err)
		}
	})
	t.Run("resume mode cannot start a new Actor", func(t *testing.T) {
		provider := &hhScanProvider{rows: [][]json.RawMessage{{}}}
		_, s, lease, listing := hhScanFixture(t, provider)
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
		w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: listing.Runtime, ResumePreviousDataset: true}
		result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_RESUME_LIMIT"}, MaxJobs: 1})
		if !errors.Is(err, parser.ErrHHListingChanged) || result.Processed != 1 || provider.starts != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 || s.count("SELECT COUNT(*) FROM vacancy_discovery_runs WHERE company_id=991302") != 0 {
			t.Fatal("resume started an Actor", result, provider.starts, err)
		}
	})
	provider := &hhScanProvider{rows: [][]json.RawMessage{{}}}
	_, s, lease, listing := hhScanFixture(t, provider)
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
	s.exec("INSERT INTO company_identity(id,company_name) VALUES(991305,'CI second HH company')")
	s.exec(`UPDATE app_catalog SET data_json='{"companies":[{"name":"CI Apify Runtime","vacancySources":{"hh":"https://hh.ru/employer/456"}},{"name":"CI second HH company","vacancySources":{"hh":"https://hh.ru/employer/456"}}],"agencies":[]}' WHERE id=1`)
	settings := apifysettings.NewRepository(s.db, "CI_SECRET")
	if _, err := settings.SaveHHSource(s.ctx, s.repo.Generation, 991305, apifysettings.HHSourceInput{SourceURL: "https://hh.ru/employer/456", Enabled: true, Revision: 0}); err != nil {
		t.Fatal("second HH source", err)
	}

	t.Cleanup(func() {
		if _, err := s.db.ExecContext(context.Background(), "DELETE FROM company_identity WHERE id=991305"); err != nil {
			t.Error("worker second company cleanup", err)
		}
	})
	s.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_HH_SCAN_2',?,'CI_HH_SCAN_2','scan_source',991305,'hh','{}',90,0,0,0)`, s.repo.Generation)
	runtime := listing.Runtime.(apifyruns.Service)
	w := parser.Worker{Runs: s.repo, Settings: apifysettings.NewRepository(s.db, "CI_SECRET"), ListingRuntime: &runtime}
	result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "CI_WORKER_ACTOR_LIMIT"}, MaxJobs: 20})
	if err != nil || result.Processed != 2 || provider.starts != 1 {
		t.Fatal("actor limit", result, provider.starts, err)
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending'") != 1 {
		t.Fatal("second scan was not deferred")
	}
}
