//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/parser"
	"os/exec"
	"strings"
	"testing"
)

type handoffFixturePlanner struct {
	plan       *parser.CandidateStagePlan
	calls      int
	poison     bool
	wrongOwner bool
}

func (p *handoffFixturePlanner) Next(ctx context.Context, tx *sql.Tx, job parser.Job) (*parser.CandidateStagePlan, error) {
	p.calls++
	if p.poison {
		if _, e := tx.ExecContext(ctx, "UPDATE scheduler_jobs SET locked_by='CI_POISON' WHERE id=?", job.ID); e != nil {
			return nil, e
		}
	}
	if p.plan == nil {
		return nil, nil
	}
	copy := *p.plan
	if p.wrongOwner {
		v := int64(999999)
		copy.CompanyID = &v
	}
	return &copy, nil
}
func handoffFixtureLease(t *testing.T, f *schedulerFixture, existing *parser.CandidateStagePlan) *parser.JobLease {
	billingReset(f)
	run := f.begin("CI_HANDOFF")
	if existing != nil {
		f.exec("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,priority,run_after,created_at,updated_at) VALUES('CI_EXISTING',?,?,?,?,?,?,?,?,90,0,0,0)", f.repo.Generation, testStageHash(existing.Key), existing.Type, existing.CompanyID, existing.AgencyID, existing.CandidateID, existing.SourceID, `{}`)
	}
	schedulerSeedJob(f, "CI_HANDOFF_JOB", 990001)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy',company_id=990002,source_id='companySite' WHERE id='CI_HANDOFF_JOB'")
	return schedulerRequireLease(t, f, run, "CI_HANDOFF_JOB")
}
func TestMariaDBCandidateHandoffDisposable(t *testing.T) {
	f := billingFixture(t)
	raw, e := exec.Command("node", "../../scripts/check-candidate-stage-plan.mjs").Output()
	if e != nil {
		t.Fatal("source plan oracle", e)
	}
	var frames []struct {
		Name   string
		Result *parser.CandidateStagePlan
	}
	if json.Unmarshal(raw, &frames) != nil {
		t.Fatal("source fixture")
	}
	var source *parser.CandidateStagePlan
	for _, frame := range frames {
		if frame.Name == "leaf" {
			source = frame.Result
		}
	}
	if source == nil || source.Type != "assign_roles" {
		t.Fatal("missing source role plan")
	}
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001") != 0 {
		t.Fatal("synthetic candidate collision")
	}
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990001,'CI_STAGE_CANDIDATE','CI_TITLE',?,'manual','CI','CI','CI','CI')", strings.Repeat("a", 64))
	t.Cleanup(func() { f.exec("DELETE FROM vacancy_candidates WHERE id=990001") })
	t.Run("success", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := handoffFixtureLease(t, f, nil)
		store := &aiStageFixtureStore{}
		runner := &aiStageFixtureRunner{}
		planner := &handoffFixturePlanner{plan: source}
		executor := &parser.AIStageExecutor{Store: store, Generator: runner, Handoff: &parser.CandidateHandoff{Planner: planner}}
		if _, e := executor.Execute(f.ctx, lease); e != nil {
			t.Fatal(e)
		}
		if runner.calls != 1 || planner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 1 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_HANDOFF_JOB' AND status='completed'") != 1 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=? AND type='assign_roles' AND status='pending' AND priority=90", f.repo.Generation, testStageHash(source.Key)) != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='completed' AND cost_usd=0.001") != 1 {
			t.Fatal("stage, handoff and ledger not joined")
		}
	})
	t.Run("existing_dedupe", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := handoffFixtureLease(t, f, source)
		planner := &handoffFixturePlanner{plan: source}
		executor := &parser.AIStageExecutor{Store: &aiStageFixtureStore{}, Generator: &aiStageFixtureRunner{}, Handoff: &parser.CandidateHandoff{Planner: planner}}
		if _, e := executor.Execute(f.ctx, lease); e != nil {
			t.Fatal(e)
		}
		if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=?", f.repo.Generation, testStageHash(source.Key)) != 1 {
			t.Fatal("duplicate next stage created")
		}
	})
	t.Run("cached", func(t *testing.T) {
		lease := handoffFixtureLease(t, f, nil)
		store := &aiStageFixtureStore{cached: true}
		planner := &handoffFixturePlanner{plan: source}
		out, e := (&parser.AIStageExecutor{Store: store, Handoff: &parser.CandidateHandoff{Planner: planner}}).Execute(f.ctx, lease)
		if e != nil || !out.Reused || store.saves != 0 || planner.calls != 1 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=?", f.repo.Generation, testStageHash(source.Key)) != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='completed' AND reserved_usd=0 AND cost_usd=0") != 1 {
			t.Fatal("cached handoff failed", e)
		}
	})
	t.Run("late_rollback", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := handoffFixtureLease(t, f, nil)
		runner := &aiStageFixtureRunner{}
		planner := &handoffFixturePlanner{plan: source, poison: true}
		executor := &parser.AIStageExecutor{Store: &aiStageFixtureStore{}, Generator: runner, Handoff: &parser.CandidateHandoff{Planner: planner}}
		_, e := executor.Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrJobLeaseLost) {
			t.Fatal("late error missing", e)
		}
		_, e = executor.Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrAttemptReplay) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 0 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=?", f.repo.Generation, testStageHash(source.Key)) != 0 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_HANDOFF_JOB' AND status='running' AND locked_by<>'CI_POISON'") != 1 {
			t.Fatal("late fault committed or POST replayed", e)
		}
	})
	t.Run("wrong_owner", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := handoffFixtureLease(t, f, nil)
		runner := &aiStageFixtureRunner{}
		executor := &parser.AIStageExecutor{Store: &aiStageFixtureStore{}, Generator: runner, Handoff: &parser.CandidateHandoff{Planner: &handoffFixturePlanner{plan: source, wrongOwner: true}}}
		_, e := executor.Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrJobInput) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001") != 0 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=?", f.repo.Generation, testStageHash(source.Key)) != 0 {
			t.Fatal("cross-owner handoff committed", e)
		}
	})
	t.Run("unknown_outcome", func(t *testing.T) {
		lease := handoffFixtureLease(t, f, nil)
		runner := &aiStageFixtureRunner{unknown: true}
		planner := &handoffFixturePlanner{plan: source}
		_, e := (&parser.AIStageExecutor{Store: &aiStageFixtureStore{}, Generator: runner, Handoff: &parser.CandidateHandoff{Planner: planner}}).Execute(f.ctx, lease)
		if e == nil || runner.calls != 1 || planner.calls != 0 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE generation=? AND dedupe_key=?", f.repo.Generation, testStageHash(source.Key)) != 0 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='failed' AND cost_usd IS NULL AND reserved_usd>0") != 1 {
			t.Fatal("uncertain provider outcome handed off", e)
		}
	})
}
