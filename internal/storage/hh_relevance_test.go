//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"itmap/internal/aiproviders"
	"itmap/internal/parser"
	"itmap/internal/profiles"
)

type relevanceRunner struct {
	calls   int
	after   func()
	failure error
}

func (r *relevanceRunner) Automatic(ctx context.Context, req aiproviders.GenerationRequest, cfg profiles.ModelInput, bill aiproviders.Billing) (aiproviders.AIResult, error) {
	if req.Task != "relevance" || cfg.Provider != "openrouter" || cfg.OpenRouterModel != "jev" || req.MaxTokens != 4096 {
		return aiproviders.AIResult{}, errors.New("CI wrong relevance request")
	}
	if req.CheckActive != nil {
		if err := req.CheckActive(ctx); err != nil {
			return aiproviders.AIResult{}, err
		}
	}
	meta := json.RawMessage(`{"provider":"openrouter","model":"typesafe/jev-1.13","contextLength":100,"pricing":{"prompt":"0.00001","completion":"0"},"payload":{"questions":{"q":{}}}}`)
	_, err := bill.Run(ctx, meta, func(context.Context) (json.RawMessage, error) {
		r.calls++
		if r.after != nil {
			r.after()
		}
		if r.failure != nil {
			return nil, r.failure
		}
		return json.RawMessage(`{"model":"typesafe/jev-1.13","usage":{"cost":0.001}}`), nil
	})
	if err != nil {
		return aiproviders.AIResult{}, err
	}
	if req.CheckActive != nil {
		if err = req.CheckActive(ctx); err != nil {
			return aiproviders.AIResult{}, err
		}
	}
	content := `{"relevance":67}`
	if req.ValidateContent != nil {
		if err = req.ValidateContent(content); err != nil {
			return aiproviders.AIResult{}, err
		}
	}
	return aiproviders.AIResult{Model: aiproviders.JEVModelID, Message: aiproviders.Message{Content: content}, Usage: json.RawMessage(`{"cost":0.001}`)}, nil
}

func hhRelevanceFixture(t *testing.T) (*schedulerFixture, *parser.JobLease, int64, int64) {
	t.Helper()
	s, importLease, candidate, importer := hhImportFixture(t)
	for _, q := range []string{"SELECT COUNT(*) FROM user_profiles WHERE user_name='CI_OWNER'", "SELECT COUNT(*) FROM system_prompts WHERE kind='relevance'", "SELECT COUNT(*) FROM prompt_model_settings WHERE kind='relevance'", "SELECT COUNT(*) FROM openrouter_settings"} {
		if s.count(q) != 0 {
			t.Fatal("relevance namespace not empty")
		}
	}
	s.exec("INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('CI_OWNER','CI synthetic resume',1)")
	s.exec("INSERT INTO system_prompts(kind,markdown,revision) VALUES('relevance','CI relevance prompt',1)")
	s.exec("INSERT INTO prompt_model_settings(kind,provider,openai_model,openrouter_model,reasoning_effort,revision) VALUES('relevance','openrouter','','jev','medium',1)")
	s.exec("INSERT INTO openrouter_settings(id,revision) VALUES(1,1)")
	t.Cleanup(func() {
		for _, q := range []string{"DELETE FROM user_profiles WHERE user_name='CI_OWNER'", "DELETE FROM system_prompts WHERE kind='relevance'", "DELETE FROM prompt_model_settings WHERE kind='relevance'", "DELETE FROM openrouter_settings WHERE id=1"} {
			if _, err := s.db.ExecContext(context.Background(), q); err != nil {
				t.Error("relevance cleanup", err)
			}
		}
	})
	imported, err := importer.Execute(s.ctx, importLease)
	if err != nil {
		t.Fatal("import before relevance", err)
	}
	if err = importLease.Finish(s.ctx, parser.FinishOptions{Status: "completed"}); err != nil {
		t.Fatal("finish import", err)
	}
	var id, run string
	if err = s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_jobs WHERE candidate_id=? AND type='score_relevance'", candidate).Scan(&id); err != nil {
		t.Fatal("missing relevance job", err)
	}
	if err = s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
		t.Fatal(err)
	}
	lease, err := s.repo.ClaimJob(s.ctx, run, parser.ClaimOptions{JobID: &id})
	if err != nil || lease == nil {
		t.Fatal("claim relevance", err)
	}
	return s, lease, candidate, imported.VacancyID
}

func TestMariaDBHHRelevanceDisposable(t *testing.T) {
	t.Run("score is personal and import schedules it", func(t *testing.T) {
		s, lease, candidate, vacancy := hhRelevanceFixture(t)
		s.exec("INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance) VALUES('CI_OTHER',?,14)", vacancy)
		runner := &relevanceRunner{}
		out, err := (parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}).Execute(s.ctx, lease)
		if err != nil || runner.calls != 1 || out.Reused || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance=67", vacancy) != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OTHER' AND vacancy_id=? AND relevance=14", vacancy) != 1 {
			t.Fatal("score isolation", err)
		}
		if s.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=? AND type='score_relevance' AND payload LIKE '%CI synthetic resume%'", candidate) != 0 {
			t.Fatal("resume leaked to job")
		}
	})
	t.Run("saved zero is reused without provider", func(t *testing.T) {
		s, lease, _, vacancy := hhRelevanceFixture(t)
		s.exec("INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance) VALUES('CI_OWNER',?,0)", vacancy)
		runner := &relevanceRunner{}
		out, err := (parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}).Execute(s.ctx, lease)
		if err != nil || !out.Reused || runner.calls != 0 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance=0", vacancy) != 1 {
			t.Fatal("cached zero", err)
		}
	})
	t.Run("stale model blocks before paid work", func(t *testing.T) {
		s, lease, _, vacancy := hhRelevanceFixture(t)
		s.exec("UPDATE prompt_model_settings SET revision=revision+1 WHERE kind='relevance'")
		runner := &relevanceRunner{}
		_, err := (parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}).Execute(s.ctx, lease)
		if err == nil || runner.calls != 0 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance IS NOT NULL", vacancy) != 0 {
			t.Fatal("stale model admitted", err)
		}
	})
	for name, mutation := range map[string]string{
		"role":   "UPDATE vacancy_roles SET name='CI changed leaf' WHERE id=991304",
		"prompt": "UPDATE system_prompts SET markdown='CI changed prompt',revision=revision+1 WHERE kind='relevance'",
		"model":  "UPDATE prompt_model_settings SET revision=revision+1 WHERE kind='relevance'",
		"resume": "UPDATE user_profiles SET resume_md='CI changed resume',revision=revision+1 WHERE user_name='CI_OWNER'",
		"key":    "UPDATE openrouter_settings SET revision=revision+1 WHERE id=1",
		"owner":  "UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.ownerName','CI_OTHER') WHERE id=1",
		"level":  "UPDATE company_vacancies SET level='Head' WHERE company_name='CI Apify Runtime'",
	} {
		t.Run("changed "+name+" during provider work blocks save", func(t *testing.T) {
			s, lease, _, vacancy := hhRelevanceFixture(t)
			runner := &relevanceRunner{after: func() { s.exec(mutation) }}
			_, err := (parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}).Execute(s.ctx, lease)
			if err == nil || runner.calls != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance IS NOT NULL", vacancy) != 0 || s.count("SELECT COUNT(*) FROM company_vacancies WHERE id=?", vacancy) != 1 {
				t.Fatal("stale relevance saved or vacancy lost", err)
			}
		})
	}
	t.Run("budget refusal spends nothing", func(t *testing.T) {
		s, lease, _, vacancy := hhRelevanceFixture(t)
		s.exec("UPDATE firecrawl_settings SET scheduler_json=JSON_SET(scheduler_json,'$.dayBudgetUsd',0,'$.monthBudgetUsd',0) WHERE id=1")
		runner := &relevanceRunner{}
		_, err := (parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}).Execute(s.ctx, lease)
		if !errors.Is(err, parser.ErrBudgetLimit) || runner.calls != 0 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance IS NOT NULL", vacancy) != 0 {
			t.Fatal("budget refusal", err)
		}
	})
	t.Run("unknown outcome forbids second paid call", func(t *testing.T) {
		s, lease, _, vacancy := hhRelevanceFixture(t)
		runner := &relevanceRunner{failure: errors.New("CI uncertain outcome")}
		dispatcher := parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}
		if _, err := dispatcher.Execute(s.ctx, lease); err == nil || runner.calls != 1 {
			t.Fatal("unknown outcome not recorded", err)
		}
		id := lease.Job().ID
		s.exec("UPDATE scheduler_jobs SET status='pending',locked_by='',locked_until=0,run_after=0 WHERE id=?", id)
		var run string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
			t.Fatal(err)
		}
		retry, err := s.repo.ClaimJob(s.ctx, run, parser.ClaimOptions{JobID: &id})
		if err != nil || retry == nil {
			t.Fatal("retry claim", err)
		}
		if _, err = dispatcher.Execute(s.ctx, retry); !errors.Is(err, parser.ErrMoneyUncertain) || runner.calls != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance IS NOT NULL", vacancy) != 0 {
			t.Fatal("unknown paid retry", err)
		}
	})
	t.Run("uncertain other job blocks until reconciled", func(t *testing.T) {
		s, lease, candidate, vacancy := hhRelevanceFixture(t)
		s.exec("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,candidate_id,source_id,status,priority,run_after,created_at,updated_at) VALUES('CI_PREVIOUS_SCORE',?,'CI_PREVIOUS_DEDUPE','score_relevance',991302,?,'hh','failed',0,0,0,0)", s.repo.Generation, candidate)
		s.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_PREVIOUS_ATTEMPT','CI_PREVIOUS_SCORE','CI_OLD_RUN','score_relevance',0,'failed',0.001,NULL)")
		runner := &relevanceRunner{}
		dispatcher := parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}
		if _, err := dispatcher.Execute(s.ctx, lease); !errors.Is(err, parser.ErrMoneyUncertain) || runner.calls != 0 {
			t.Fatal("different uncertain job admitted", err)
		}
		s.exec("UPDATE scheduler_attempts SET cost_usd=0.001 WHERE id='CI_PREVIOUS_ATTEMPT'")
		id := lease.Job().ID
		s.exec("UPDATE scheduler_jobs SET status='pending',locked_by='',locked_until=0,run_after=0 WHERE id=?", id)
		var run string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
			t.Fatal(err)
		}
		retry, err := s.repo.ClaimJob(s.ctx, run, parser.ClaimOptions{JobID: &id})
		if err != nil || retry == nil {
			t.Fatal("reconciled retry claim", err)
		}
		if _, err = dispatcher.Execute(s.ctx, retry); err != nil || runner.calls != 1 || s.count("SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name='CI_OWNER' AND vacancy_id=? AND relevance=67", vacancy) != 1 {
			t.Fatal("reconciled retry", err)
		}
	})
	t.Run("uncertain receipt survives job replacement", func(t *testing.T) {
		s, lease, _, vacancy := hhRelevanceFixture(t)
		runner := &relevanceRunner{failure: errors.New("CI uncertain outcome")}
		dispatcher := parser.AIStageDispatcher{Owner: "CI_OWNER", Generator: runner}
		if _, err := dispatcher.Execute(s.ctx, lease); err == nil || runner.calls != 1 {
			t.Fatal("unknown outcome", err)
		}
		oldID := lease.Job().ID
		if s.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND cost_usd IS NULL AND reserved_usd>0 AND JSON_EXTRACT(result_json,'$.relevanceVacancyId')=? AND result_json NOT LIKE '%CI synthetic resume%'", oldID, vacancy) != 1 {
			t.Fatal("durable relevance scope missing")
		}
		s.exec("UPDATE scheduler_jobs SET id='CI_REPLACED_SCORE',status='pending',locked_by='',locked_until=0,run_after=0 WHERE id=?", oldID)
		var run string
		if err := s.db.QueryRowContext(s.ctx, "SELECT id FROM scheduler_runs WHERE status='running'").Scan(&run); err != nil {
			t.Fatal(err)
		}
		id := "CI_REPLACED_SCORE"
		retry, err := s.repo.ClaimJob(s.ctx, run, parser.ClaimOptions{JobID: &id})
		if err != nil || retry == nil {
			t.Fatal("replacement claim", err)
		}
		if _, err = dispatcher.Execute(s.ctx, retry); !errors.Is(err, parser.ErrMoneyUncertain) || runner.calls != 1 {
			t.Fatal("replacement bypassed unknown outcome", err)
		}
	})
}
