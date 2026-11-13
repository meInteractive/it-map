//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
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
	stop()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("continuous worker did not stop", err)
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
