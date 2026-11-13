//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/parser"
	"itmap/internal/parser/stagingmock"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

func continuousWorkerFixture(t *testing.T, loseAck bool) (*schedulerFixture, *stagingmock.Providers, parser.Worker) {
	_, s, lease, _ := hhScanFixture(t, &hhScanProvider{})
	for _, table := range []string{"vacancy_classification_slots", "vacancy_candidate_classification_leases"} {
		if s.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("classification lease fixture must be empty")
		}
		table := table
		t.Cleanup(func() {
			if _, err := s.db.ExecContext(context.Background(), "DELETE FROM "+table); err != nil {
				t.Error("classification lease cleanup failed")
			}
		})
	}
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
	s.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision) VALUES(1,'STAGING extraction prompt',1,'openai','gpt-6-luna','medium',1)")
	s.exec("INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision) VALUES(1,'STAGING roles prompt',1,'openai','gpt-6-luna','best_free','medium',1)")
	s.exec("INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('CI_OWNER','CI synthetic resume',1)")
	s.exec("INSERT INTO system_prompts(kind,markdown,revision) VALUES('relevance','CI relevance prompt',1)")
	s.exec("INSERT INTO prompt_model_settings(kind,provider,openai_model,openrouter_model,reasoning_effort,revision) VALUES('relevance','openrouter','','jev','medium',1)")
	if s.count("SELECT COUNT(*) FROM openrouter_ai_logs") != 0 {
		t.Fatal("provider log namespace not empty")
	}
	s.exec("INSERT INTO openrouter_settings(id,revision) VALUES(1,1)")
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM vacancy_detail_leases", "DELETE FROM vacancy_import_write_context", "DELETE FROM company_vacancies WHERE company_name='CI Apify Runtime'", "DELETE FROM company_vacancy_state WHERE company_name='CI Apify Runtime'", "DELETE FROM company_crm WHERE company_name='CI Apify Runtime'", "DELETE FROM user_profiles WHERE user_name='CI_OWNER'", "DELETE FROM system_prompts WHERE kind='relevance'", "DELETE FROM prompt_model_settings WHERE kind='relevance'", "DELETE FROM openrouter_ai_logs", "DELETE FROM openrouter_settings WHERE id=1", "DELETE FROM vacancy_detail_settings WHERE id=1", "DELETE FROM vacancy_role_settings WHERE id=1", "DELETE FROM vacancy_roles WHERE id=991304", "DELETE FROM apify_account_usage WHERE account_id='StagingMock'"} {
			if _, err := s.db.ExecContext(context.Background(), q); err != nil {
				t.Error("staging mock cleanup", err)
			}
		}
	})
	keys := aiproviders.NewRepository(s.db, "CI_SECRET", "sk-"+strings.Repeat("s", 16))
	if err := keys.SaveKey(s.ctx, aihttp.OpenRouter, s.repo.Generation, "sk-or-"+strings.Repeat("r", 16)); err != nil {
		t.Fatal("synthetic key setup", err)
	}
	p := &stagingmock.Providers{LoseStartAck: loseAck}
	w, err := stagingmock.Worker(s.repo, "CI_SECRET", p)
	if err != nil {
		t.Fatal(err)
	}
	return s, p, w
}

// Проверяется потеря процесса внутри synthetic POST без выполнения cleanup.
func TestMariaDBContinuousWorkerCrashDisposable(t *testing.T) {
	if os.Getenv("ITMAP_CONTINUOUS_CRASH_CHILD") == "1" {
		ctx, db := schedulerDB(t)
		p := &stagingmock.Providers{AfterStart: func(ctx context.Context) {
			fmt.Println("CI_CONTINUOUS_POST_ACCEPTED")
			<-ctx.Done()
		}}
		worker, err := stagingmock.Worker(parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}, "CI_SECRET", p)
		if err != nil {
			t.Fatal(err)
		}
		if err := (workerengine.Engine{Worker: worker}).Run(ctx); err != nil {
			t.Fatal("child stopped before kill", err)
		}
		return
	}
	s, provider, worker := continuousWorkerFixture(t, false)
	cmd := exec.Command(os.Args[0], "-test.run=^TestMariaDBContinuousWorkerCrashDisposable$")
	cmd.Env = append(os.Environ(), "ITMAP_CONTINUOUS_CRASH_CHILD=1")
	pipe, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		t.Fatal("synthetic crash worker did not start")
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if scanner.Text() == "CI_CONTINUOUS_POST_ACCEPTED" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not reach synthetic POST")
		}
	case <-time.After(12 * time.Second):
		t.Fatal("child synthetic POST timed out")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal("cannot kill synthetic child", err)
	}
	if cmd.Wait() == nil {
		t.Fatal("child unexpectedly exited normally")
	}
	finished = true
	if s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 || s.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='running'") != 1 {
		t.Fatal("crash did not retain unknown operation and active lease")
	}
	// Ускоряем истечение leases только в принадлежащей тесту пустой БД.
	s.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
	s.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	launch := make(chan struct{}, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if profile == workerloop.Launch {
				select {
				case launch <- struct{}{}:
				default:
				}
			}
		}}).Run(ctx)
	}()
	select {
	case <-launch:
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("worker recovery timed out")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("restarted worker did not stop", err)
	}
	if provider.Starts() != 0 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 || s.count("SELECT COUNT(*) FROM apify_runs") != 1 {
		t.Fatal("recovery changed unknown operation")
	}
	if s.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='interrupted'") != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending' AND reason_code='interrupted'") != 1 {
		t.Fatal("recovery did not preserve interrupted work")
	}
}

func TestMariaDBContinuousWorkerDisposable(t *testing.T) {
	s, p, w := continuousWorkerFixture(t, false)
	workerCtx, stop := context.WithCancel(s.ctx)
	finished := make(chan error, 1)
	failures := make(chan struct{}, 1)
	go func() {
		finished <- (workerengine.Engine{Worker: w, OnResult: func(_ workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
			if outcome == workerloop.ProfileFailed || outcome == workerloop.ProfileTimeout {
				select {
				case failures <- struct{}{}:
				default:
				}
			}
		}}).Run(workerCtx)
	}()
	deadline := time.NewTimer(15 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	completed := false
wait:
	for {
		select {
		case <-ticker.C:
			if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") == 1 {
				completed = true
				break wait
			}
		case <-failures:
			break wait
		case <-deadline.C:
			break wait
		}
	}
	ticker.Stop()
	deadline.Stop()
	idleCheckFailed := false
	if completed {
		// Первый успешный launch повторно проверяет источники через секунду.
		// После этого пустой pipeline должен пройти хотя бы один polling без run.
		select {
		case <-time.After(3 * time.Second):
		case <-s.ctx.Done():
			idleCheckFailed = true
		}
		before := s.count("SELECT COUNT(*) FROM scheduler_runs")
		select {
		case <-time.After(7 * time.Second):
		case <-s.ctx.Done():
			idleCheckFailed = true
		}
		if after := s.count("SELECT COUNT(*) FROM scheduler_runs"); after != before {
			idleCheckFailed = true
		}
	}
	stop()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("continuous worker did not stop", err)
	}
	if idleCheckFailed {
		t.Fatal("idle profile check failed or created scheduler runs")
	}
	if !completed || p.Starts() != 1 || s.count("SELECT COUNT(*) FROM scheduler_runs WHERE `trigger`='automatic'") < 2 {
		t.Fatal("continuous worker did not finish pipeline in separate cycles", completed, p.Starts())
	}

	if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE title_raw='STAGING_SYNTHETIC_REJECTED' AND main_vacancy_id IS NULL") != 1 {
		t.Fatal("mock accepted/rejected vacancies mismatch")
	}
	if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") != 1 {
		t.Fatal("mock relevance missing")
	}
}

// Перезапуск не даёт второй POST, если подтверждение первого потеряно.
func TestMariaDBContinuousWorkerLostACKDisposable(t *testing.T) {
	s, p, worker := continuousWorkerFixture(t, true)
	run := func() {
		ctx, cancel := context.WithCancel(s.ctx)
		defer cancel()
		done := make(chan error, 1)
		launch := make(chan struct{}, 1)
		go func() {
			done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
				if profile == workerloop.Launch {
					select {
					case launch <- struct{}{}:
					default:
					}
				}
			}}).Run(ctx)
		}()
		select {
		case <-launch:
		case <-time.After(12 * time.Second):
			cancel()
			<-done
			t.Fatal("worker launch did not finish")
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop", err)
		}
	}
	run()
	if p.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 {
		t.Fatal("unknown paid outcome was not retained", p.Starts())
	}
	// В disposable БД принудительно возвращаем тот же job в очередь, чтобы
	// проверить guard даже при повторно доступной работе после перезапуска.
	s.exec("UPDATE scheduler_jobs SET status='pending',run_after=0 WHERE type='scan_source' AND status='failed'")
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='scan_source' AND status='pending'") != 1 {
		t.Fatal("synthetic retry fixture missing")
	}
	run()
	if p.Starts() != 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd>0") != 1 {
		t.Fatal("restart repeated POST or lost reserve", p.Starts())
	}
}

// Незавершённый Actor продолжается отдельным reconcile без нового POST.
func TestMariaDBContinuousWorkerReconcileDisposable(t *testing.T) {
	s, p, worker := continuousWorkerFixture(t, false)
	p.Async = true
	ctx, cancel := context.WithCancel(s.ctx)
	firstDone := make(chan error, 1)
	launched := make(chan struct{}, 1)
	go func() {
		firstDone <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if profile == workerloop.Launch {
				select {
				case launched <- struct{}{}:
				default:
				}
				cancel()
			}
		}}).Run(ctx)
	}()
	select {
	case <-launched:
	case <-time.After(12 * time.Second):
		cancel()
		<-firstDone
		t.Fatal("initial launch timed out")
	}
	cancel()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatal("first worker did not stop", err)
	}
	if p.Starts() != 1 || p.Polls() != 0 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='running' AND run_id='MockRun0'") != 1 {
		t.Fatal("synthetic actor did not remain running", p.Starts(), p.Polls())
	}
	// Ускоряем наступление срока только в disposable БД.
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='scan_source' AND status='pending'")
	ctx, cancel = context.WithCancel(s.ctx)
	done := make(chan error, 1)
	go func() { done <- (workerengine.Engine{Worker: worker}).Run(ctx) }()
	deadline := time.NewTimer(12 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	completed := false
wait:
	for {
		select {
		case <-ticker.C:
			if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") == 1 {
				completed = true
				break wait
			}
		case <-deadline.C:
			break wait
		}
	}
	ticker.Stop()
	deadline.Stop()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal("second worker did not stop", err)
	}
	if !completed || p.Starts() != 1 || p.Polls() < 1 || s.count("SELECT COUNT(*) FROM apify_runs WHERE state='succeeded' AND charged_usd IS NOT NULL") != 1 {
		t.Fatal("reconcile did not finish without another POST", completed, p.Starts(), p.Polls())
	}
}

func TestMariaDBContinuousWorkerApifyBudgetDisposable(t *testing.T) {
	s, p, worker := continuousWorkerFixture(t, false)
	p.ExhaustedAccount = true
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if profile == workerloop.Pipeline {
				cancel()
			}
		}}).Run(ctx)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop", err)
		}
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("budget test timed out")
	}
	if p.Starts() != 0 || s.count("SELECT COUNT(*) FROM apify_runs") != 0 {
		t.Fatal("exhausted Apify budget admitted actor", p.Starts())
	}
	if s.count("SELECT COUNT(*) FROM scheduler_runs WHERE `trigger`='automatic'") == 0 {
		t.Fatal("worker did not attempt scheduling")
	}
}

func TestMariaDBContinuousWorkerAIBudgetDisposable(t *testing.T) {
	s, p, worker := continuousWorkerFixture(t, false)
	s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',0,'$.monthBudgetUsd',0) WHERE id=1`)
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if profile == workerloop.Pipeline {
				cancel()
			}
		}}).Run(ctx)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop", err)
		}
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("budget test timed out")
	}
	if p.Starts() != 1 || p.AICalls() != 0 {
		t.Fatal("AI budget did not prevent calls independently of Apify", p.Starts(), p.AICalls())
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND reason_code='budget' AND attempts=0") == 0 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 0 {
		t.Fatal("AI budget test did not reach classification")
	}
	// Повторная проверка без пополнения не должна расходовать попытки или вызывать AI.
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE status='pending' AND reason_code='budget'")
	ctx, cancel = context.WithCancel(s.ctx)
	done = make(chan error, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if profile == workerloop.Pipeline {
				cancel()
			}
		}}).Run(ctx)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop after repeated budget check", err)
		}
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("repeated budget check timed out")
	}
	if p.AICalls() != 0 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND reason_code='budget' AND attempts=0") == 0 {
		t.Fatal("repeated budget check consumed work or called provider")
	}
	// Пополнение и наступление срока моделируются только в disposable БД.
	s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',5,'$.monthBudgetUsd',5) WHERE id=1`)
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE status='pending' AND reason_code='budget'")
	ctx, cancel = context.WithCancel(s.ctx)
	done = make(chan error, 1)
	go func() {
		done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
			if profile == workerloop.Pipeline {
				cancel()
			}
		}}).Run(ctx)
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("worker did not stop after budget refill", err)
		}
	case <-time.After(12 * time.Second):
		cancel()
		<-done
		t.Fatal("budget refill test timed out")
	}
	if p.Starts() != 1 || p.AICalls() == 0 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 {
		t.Fatal("worker did not resume after refill", p.Starts(), p.AICalls())
	}
}

func TestMariaDBContinuousWorkerBudgetPriorDisposable(t *testing.T) {
	for _, prior := range []struct{ name, cost, code string }{
		{"unknown", "NULL", "money_uncertain"},
		{"settled", "0.02", "budget"},
	} {
		t.Run(prior.name, func(t *testing.T) {
			s, p, worker := continuousWorkerFixture(t, false)
			s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',0,'$.monthBudgetUsd',0) WHERE id=1`)
			runUntil := func(target workerloop.ProfileName) {
				ctx, cancel := context.WithCancel(s.ctx)
				done := make(chan error, 1)
				go func() {
					done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
						if profile == target {
							cancel()
						}
					}}).Run(ctx)
				}()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatal("worker did not stop", err)
					}
				case <-time.After(12 * time.Second):
					cancel()
					<-done
					t.Fatal("budget history test timed out")
				}
			}
			runUntil(workerloop.Launch)
			if p.Starts() != 1 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending'") == 0 {
				t.Fatal("classification fixture missing")
			}
			s.exec(`INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) SELECT CONCAT('CI_prior_',id),id,'CI_prior_run','classify_title',1,'interrupted',0.25,` + prior.cost + ` FROM scheduler_jobs WHERE type='classify_title'`)
			runUntil(workerloop.Pipeline)
			if p.AICalls() != 0 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND reason_code='budget'") != 0 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='failed' AND reason_code=?", prior.code) == 0 {
				t.Fatal("prior paid evidence was automatically deferred", prior.name, p.AICalls())
			}
		})
	}
}

// Подтверждение COMMIT теряется после записи budget deferral в disposable БД.
func TestMariaDBContinuousWorkerBudgetCommitDisposable(t *testing.T) {
	s, p, worker := continuousWorkerFixture(t, false)
	s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',0,'$.monthBudgetUsd',0) WHERE id=1`)
	runUntil := func(w parser.Worker, target workerloop.ProfileName) {
		ctx, cancel := context.WithCancel(s.ctx)
		done := make(chan error, 1)
		go func() {
			done <- (workerengine.Engine{Worker: w, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
				if profile == target {
					cancel()
				}
			}}).Run(ctx)
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("worker did not stop", err)
			}
		case <-time.After(12 * time.Second):
			cancel()
			<-done
			t.Fatal("commit recovery timed out")
		}
	}
	runUntil(worker, workerloop.Launch)
	if p.Starts() != 1 || p.AICalls() != 0 {
		t.Fatal("fixture did not reach classification")
	}
	repo, fired := billingCommitRepository(t, s.repo.Generation, "UPDATE scheduler_jobs SET status=?,attempts=GREATEST")
	faultWorker, err := stagingmock.Worker(repo, "CI_SECRET", p)
	if err != nil {
		t.Fatal(err)
	}
	runUntil(faultWorker, workerloop.Pipeline)
	if !fired.Load() || p.AICalls() != 0 || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='classify_title' AND status='pending' AND reason_code='budget' AND attempts=0") != 1 {
		t.Fatal("lost acknowledgement did not retain budget deferral", fired.Load(), p.AICalls())
	}
	if s.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='running'") != 1 {
		t.Fatal("uncertain commit unexpectedly released run")
	}
	// Только в тестовой БД ускоряем истечение владельца и пополнение бюджета.
	s.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
	s.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE status='running'")
	s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',5,'$.monthBudgetUsd',5) WHERE id=1`)
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE status='pending' AND reason_code='budget'")
	runUntil(worker, workerloop.Pipeline)
	if p.Starts() != 1 || p.AICalls() == 0 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 {
		t.Fatal("worker failed to recover after unknown commit", p.Starts(), p.AICalls())
	}
}

func TestMariaDBContinuousWorkerRelevanceBudgetDisposable(t *testing.T) {
	s, p, worker := continuousWorkerFixture(t, false)
	// Строим pipeline по одному заданию, пока не появится готовая relevance.
	worker.ScheduleSources = true
	worker.ContinueOnStageFailure = true
	for i := 0; i < 12; i++ {
		if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='score_relevance' AND status='pending'") != 0 {
			break
		}
		result, err := worker.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: fmt.Sprintf("CI_RELEVANCE_BUDGET_%d", i), Trigger: "automatic"}, MaxJobs: 1, QueueProfile: "balanced"})
		if err != nil || result.Run.Status != "completed" {
			t.Fatal("pipeline setup failed", err, result.Run.Status)
		}
	}
	if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='score_relevance' AND status='pending'") != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance IS NOT NULL") != 0 {
		t.Fatal("relevance fixture missing")
	}
	s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',0,'$.monthBudgetUsd',0) WHERE id=1`)
	previousCalls := p.AICalls()
	runPipeline := func() {
		ctx, cancel := context.WithCancel(s.ctx)
		done := make(chan error, 1)
		go func() {
			done <- (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, _ workerloop.ProfileOutcome) {
				if profile == workerloop.Pipeline {
					cancel()
				}
			}}).Run(ctx)
		}()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("worker did not stop", err)
			}
		case <-time.After(12 * time.Second):
			cancel()
			<-done
			t.Fatal("relevance budget test timed out")
		}
	}
	runPipeline()
	if p.AICalls() != previousCalls || s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE type='score_relevance' AND status='pending' AND reason_code='budget' AND attempts=0") != 1 {
		t.Fatal("relevance did not defer without a paid call", p.AICalls(), previousCalls)
	}
	s.exec(`UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',5,'$.monthBudgetUsd',5) WHERE id=1`)
	s.exec("UPDATE scheduler_jobs SET run_after=0 WHERE type='score_relevance' AND status='pending' AND reason_code='budget'")
	runPipeline()
	if p.Starts() != 1 || p.AICalls() <= previousCalls || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") != 1 {
		t.Fatal("relevance did not resume", p.Starts(), p.AICalls())
	}
}
