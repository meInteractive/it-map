//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"strings"
	"testing"

	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/parser"
	"itmap/internal/parser/stagingmock"
)

func TestMariaDBParserStagingMockDisposable(t *testing.T) {
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
	p := &stagingmock.Providers{}
	w, err := stagingmock.Worker(s.repo, "CI_SECRET", p)
	if err != nil {
		t.Fatal(err)
	}
	w.ScheduleSources = true
	w.ContinueOnStageFailure = true
	result, err := w.Cycle(s.ctx, parser.WorkerOptions{Run: parser.RunOptions{RequestID: "12345678-1234-4234-8234-123456789abc"}, MaxJobs: 50})
	if err != nil || result.Run.Status != "completed" || result.Processed != 8 || p.Starts() != 1 {
		rows, queryErr := s.db.QueryContext(s.ctx, "SELECT type,status,reason_code FROM scheduler_jobs ORDER BY type")
		if queryErr == nil {
			for rows.Next() {
				var kind, status, reason string
				if rows.Scan(&kind, &status, &reason) == nil {
					t.Log("synthetic job", kind, status, reason)
				}
			}
			rows.Close()
		}
		rows, queryErr = s.db.QueryContext(s.ctx, "SELECT decision,COUNT(*) FROM vacancy_classifications GROUP BY decision")
		if queryErr == nil {
			for rows.Next() {
				var decision string
				var count int
				if rows.Scan(&decision, &count) == nil {
					t.Log("synthetic classification", decision, count)
				}
			}
			rows.Close()
		}
		t.Fatalf("mock cycle status=%s processed=%d starts=%d err=%v", result.Run.Status, result.Processed, p.Starts(), err)
	}
	if s.count("SELECT COUNT(*) FROM company_vacancies WHERE company_name='CI Apify Runtime'") != 1 || s.count("SELECT COUNT(*) FROM vacancy_candidates WHERE title_raw='STAGING_SYNTHETIC_REJECTED' AND main_vacancy_id IS NULL") != 1 {
		t.Fatal("mock accepted/rejected vacancies mismatch")
	}
	if s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND relevance=67") != 1 {
		t.Fatal("mock relevance missing")
	}
}
