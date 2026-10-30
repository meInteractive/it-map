//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/parser"
	"itmap/internal/profiles"
	"strings"
	"testing"
)

type aiStageFixtureStore struct {
	cached, failSave, changed bool
	task                      string
	saves                     int
}

func (s *aiStageFixtureStore) Prepare(_ context.Context, _ *sql.Tx, j parser.Job) (parser.AIStagePlan, error) {
	task := map[string]string{"classify_title": "title_classification", "extract_vacancy": "vacancy_extraction", "assign_roles": "vacancy_role", "score_relevance": "relevance"}[j.Type]
	s.task = task
	p := parser.AIStagePlan{Request: aiproviders.GenerationRequest{Task: task, Instructions: "CI_PRIVATE_PROMPT"}, Config: profiles.ModelInput{Provider: "openrouter", OpenRouterModel: "jev"}, Title: "CI_TITLE", TitleRoles: []aiproviders.TitleRole{{RoleID: 1, Name: "CI_ROLE", Markdown: "CI_RULES", Revision: 1}}}
	if s.cached {
		p.Cached = &parser.AIStageOutput{Generation: aiproviders.AIResult{Model: "CI_CACHED"}}
	}
	return p, nil
}
func (s *aiStageFixtureStore) Revalidate(context.Context, *sql.Tx, parser.Job, parser.AIStagePlan) error {
	if s.changed {
		return errors.New("CI_PROVENANCE_CHANGED")
	}
	return nil
}
func (s *aiStageFixtureStore) Save(ctx context.Context, tx *sql.Tx, j parser.Job, _ parser.AIStagePlan, o parser.AIStageOutput) error {
	s.saves++
	_, e := tx.ExecContext(ctx, "INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at) VALUES(?,990001,'CI_TITLE',?,1,'CI_MODEL','[]','complete','CI_TIME')", "CI_STAGE_"+j.ID, strings.Repeat("a", 64))
	if e != nil {
		return e
	}
	if s.failSave {
		return errors.New("CI_LATE_SAVE_FAILURE")
	}
	return nil
}

type aiStageFixtureRunner struct {
	calls   int
	unknown bool
	store   *aiStageFixtureStore
	after   func()
}

func (r *aiStageFixtureRunner) invoke(ctx context.Context, b aiproviders.Billing, check func(context.Context) error) error {
	if check != nil {
		if e := check(ctx); e != nil {
			return e
		}
	}
	_, e := b.Run(ctx, json.RawMessage(`{"provider":"openrouter","model":"typesafe/jev-1.13","contextLength":100,"pricing":{"prompt":"0.00001","completion":"0"},"payload":{"questions":{"q":{}}}}`), func(context.Context) (json.RawMessage, error) {
		r.calls++
		if r.after != nil {
			r.after()
		}
		if r.unknown {
			return nil, errors.New("CI_UNKNOWN_PAID_OUTCOME")
		}
		return json.RawMessage(`{"usage":{"cost":0.001}}`), nil
	})
	return e
}
func (r *aiStageFixtureRunner) Automatic(ctx context.Context, p aiproviders.GenerationRequest, _ profiles.ModelInput, b aiproviders.Billing) (aiproviders.AIResult, error) {
	e := r.invoke(ctx, b, p.CheckActive)
	return aiproviders.AIResult{Model: aiproviders.JEVModelID, Message: aiproviders.Message{Content: "CI_PRIVATE_ANSWER"}}, e
}
func (r *aiStageFixtureRunner) ClassifyTitles(ctx context.Context, _ string, _ []aiproviders.TitleRole, b aiproviders.Billing, check func(context.Context) error) (aiproviders.TitleResult, error) {
	e := r.invoke(ctx, b, check)
	choice := "match"
	return aiproviders.TitleResult{Model: aiproviders.JEVModelID, Results: []aiproviders.TitleDecision{{RoleID: 1, Decision: &choice}}}, e
}
func aiExecutionLease(t *testing.T, f *schedulerFixture, kind string) *parser.JobLease {
	billingReset(f)
	run := f.begin("CI_AI_EXECUTION")
	schedulerSeedJob(f, "CI_AI_JOB", nil)
	f.exec("UPDATE scheduler_jobs SET type=?,payload=? WHERE id='CI_AI_JOB'", kind, `{"instructions":"CI_UNTRUSTED_JOB_PAYLOAD"}`)
	return schedulerRequireLease(t, f, run, "CI_AI_JOB")
}
func TestMariaDBParserAIExecutionDisposable(t *testing.T) {
	f := billingFixture(t)
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001") != 0 {
		t.Fatal("fixture candidate collision")
	}
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990001,'CI_STAGE_CANDIDATE','CI_TITLE',?,'manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')", strings.Repeat("a", 64))
	t.Cleanup(func() { f.exec("DELETE FROM vacancy_candidates WHERE id=990001") })
	for _, kind := range []string{"classify_title", "extract_vacancy", "assign_roles", "score_relevance"} {
		t.Run(kind, func(t *testing.T) {
			f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
			lease := aiExecutionLease(t, f, kind)
			store := &aiStageFixtureStore{}
			runner := &aiStageFixtureRunner{store: store}
			e := &parser.AIStageExecutor{Store: store, Generator: runner, Classifier: runner}
			if _, err := e.Execute(f.ctx, lease); err != nil {
				t.Fatal(err)
			}
			if runner.calls != 1 || store.saves != 1 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_AI_JOB' AND status='completed'") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='completed' AND cost_usd=0.001") != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 1 {
				t.Fatal("provider/result/job/ledger not joined")
			}
			if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE result_json LIKE '%CI_PRIVATE%'") != 0 {
				t.Fatal("private data in scheduler receipt")
			}
		})
	}
	t.Run("cache", func(t *testing.T) {
		lease := aiExecutionLease(t, f, "score_relevance")
		store := &aiStageFixtureStore{cached: true}
		e := &parser.AIStageExecutor{Store: store}
		out, err := e.Execute(f.ctx, lease)
		if err != nil || !out.Reused || store.saves != 0 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='completed' AND reserved_usd=0 AND cost_usd=0") != 1 {
			t.Fatal("cache required provider or rewrote result", err)
		}
	})
	t.Run("budget", func(t *testing.T) {
		lease := aiExecutionLease(t, f, "score_relevance")
		f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0,"monthBudgetUsd":0}' WHERE id=1`)
		store := &aiStageFixtureStore{}
		runner := &aiStageFixtureRunner{}
		e := &parser.AIStageExecutor{Store: store, Generator: runner}
		_, err := e.Execute(f.ctx, lease)
		if !errors.Is(err, parser.ErrBudgetLimit) || runner.calls != 0 || store.saves != 0 {
			t.Fatal("exhausted budget sent paid POST", err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		lease := aiExecutionLease(t, f, "score_relevance")
		store := &aiStageFixtureStore{}
		runner := &aiStageFixtureRunner{unknown: true}
		e := &parser.AIStageExecutor{Store: store, Generator: runner}
		if _, err := e.Execute(f.ctx, lease); err == nil {
			t.Fatal("unknown outcome succeeded")
		}
		_, _ = e.Execute(f.ctx, lease)
		if runner.calls != 1 || store.saves != 0 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='failed' AND reserved_usd>0 AND cost_usd IS NULL") != 1 {
			t.Fatal("uncertain money was released or POST replayed")
		}
	})
	t.Run("late_save_rollback", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := aiExecutionLease(t, f, "score_relevance")
		store := &aiStageFixtureStore{failSave: true}
		runner := &aiStageFixtureRunner{}
		e := &parser.AIStageExecutor{Store: store, Generator: runner}
		if _, err := e.Execute(f.ctx, lease); err == nil {
			t.Fatal("late fault succeeded")
		}
		_, err := e.Execute(f.ctx, lease)
		if !errors.Is(err, parser.ErrAttemptReplay) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 0 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE status='running'") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='running'") != 1 {
			t.Fatal("late Save not atomic or POST replayed", err)
		}
	})
	t.Run("provenance_changed", func(t *testing.T) {
		lease := aiExecutionLease(t, f, "assign_roles")
		store := &aiStageFixtureStore{}
		runner := &aiStageFixtureRunner{after: func() { store.changed = true }}
		e := &parser.AIStageExecutor{Store: store, Generator: runner}
		if _, err := e.Execute(f.ctx, lease); err == nil || runner.calls != 1 || store.saves != 0 {
			t.Fatal("changed provenance saved")
		}
	})
}
