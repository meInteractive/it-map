//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/parser"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type storedTitleRunner struct {
	requested []int64
	answers   map[string]struct{ Type, Choice string }
	calls     int
	after     func()
	unknown   bool
}

func (r *storedTitleRunner) ClassifyTitles(ctx context.Context, title string, roles []aiproviders.TitleRole, b aiproviders.Billing, check func(context.Context) error) (aiproviders.TitleResult, error) {
	r.requested = nil
	for _, role := range roles {
		r.requested = append(r.requested, role.RoleID)
	}
	if title != "CI_TITLE" {
		return aiproviders.TitleResult{}, errors.New("title not normalized")
	}
	if e := check(ctx); e != nil {
		return aiproviders.TitleResult{}, e
	}
	raw, e := b.Run(ctx, json.RawMessage(`{"provider":"openrouter","model":"typesafe/jev-1.13","contextLength":100,"pricing":{"prompt":"0.00001","completion":"0"},"payload":{"questions":{"q":{}}}}`), func(context.Context) (json.RawMessage, error) {
		r.calls++
		if r.after != nil {
			r.after()
		}
		if r.unknown {
			return nil, &aihttp.Error{Code: "network", Status: 502}
		}
		return json.RawMessage(`{"usage":{"cost":0.001}}`), nil
	})
	if e != nil {
		return aiproviders.TitleResult{}, e
	}
	_ = raw
	cost := 0.001
	out := aiproviders.TitleResult{Model: aiproviders.JEVModelID, CostUSD: &cost, Results: []aiproviders.TitleDecision{}}
	for _, role := range roles {
		a := r.answers["role_"+fmt.Sprint(role.RoleID)]
		d := aiproviders.TitleDecision{RoleID: role.RoleID, Name: role.Name, PromptRevision: role.Revision}
		if a.Type == "choice" && (a.Choice == "match" || a.Choice == "reject") {
			choice := a.Choice
			d.Decision = &choice
		} else {
			d.Error = &aiproviders.ServiceError{Code: "invalid_output", Message: "JEV не вернула корректное бинарное решение для этой роли.", Status: 502}
		}
		out.Results = append(out.Results, d)
	}
	return out, nil
}
func titleStageLease(t *testing.T, f *schedulerFixture) *parser.JobLease {
	billingReset(f)
	f.exec(`UPDATE firecrawl_settings SET favorites_only=0,scheduler_json='{"enabled":true,"sourceIds":["companySite"],"ownerName":"CI_USER","dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	run := f.begin("CI_TITLE_STAGE")
	schedulerSeedJob(f, "CI_TITLE_JOB", 990001)
	f.exec("UPDATE scheduler_jobs SET type='classify_title',company_id=990002,source_id='companySite' WHERE id='CI_TITLE_JOB'")
	return schedulerRequireLease(t, f, run, "CI_TITLE_JOB")
}
func titleStageRows(t *testing.T, f *schedulerFixture, q string) []map[string]any {
	t.Helper()
	rows, e := f.db.QueryContext(f.ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	columns, e := rows.Columns()
	if e != nil {
		t.Fatal(e)
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		p := make([]any, len(values))
		for i := range values {
			p[i] = &values[i]
		}
		if e = rows.Scan(p...); e != nil {
			t.Fatal(e)
		}
		m := map[string]any{}
		for i, key := range columns {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			if key == "errors_json" || key == "requested_roles_json" {
				var parsed any
				if json.Unmarshal([]byte(v.(string)), &parsed) != nil {
					t.Fatal("invalid stored JSON")
				}
				v = parsed
			}
			m[key] = v
		}
		out = append(out, m)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return out
}
func TestMariaDBStoredTitleStageDisposable(t *testing.T) {
	f := billingFixture(t)
	cmd := exec.Command("node", "../../scripts/check-stored-title-stage.mjs")
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source oracle", e)
	}
	var frames []struct {
		Name, Title, TitleHash string
		Answers                map[string]struct{ Type, Choice string }
		Attempts, Decisions    []map[string]any
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 4 {
		t.Fatal("source corpus")
	}
	for _, table := range []string{"vacancy_classification_slots", "vacancy_candidate_classification_leases"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("requires empty classification leases")
		}
		table := table
		t.Cleanup(func() { f.exec("DELETE FROM " + table) })
	}
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001") != 0 || f.count("SELECT COUNT(*) FROM company_identity WHERE id=990002") != 0 || f.count("SELECT COUNT(*) FROM vacancy_roles WHERE id IN (990101,990102)") != 0 {
		t.Fatal("synthetic namespace collision")
	}
	f.exec("INSERT INTO company_identity(id,company_name)VALUES(990002,'CI_STAGE_COMPANY')")
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_candidates WHERE id=990001")
		f.exec("DELETE FROM company_identity WHERE id=990002")
		f.exec("DELETE FROM vacancy_roles WHERE id IN (990101,990102)")
	})
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN',990002,?,?,'companySite','manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')", frames[0].Title, frames[0].TitleHash)
	for _, id := range []int{990101, 990102} {
		f.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(?,?,?,?)", id, fmt.Sprint("CI_ROOT_", id), fmt.Sprint("CI_ROLE_", id), id)
		f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(?,?,1,1)", id, fmt.Sprint("CI_RULES_", id))
	}
	t.Run("candidate_title_snapshot", func(t *testing.T) {
		f.exec(`UPDATE firecrawl_settings SET favorites_only=0,scheduler_json='{"enabled":true,"sourceIds":["companySite"],"ownerName":"CI_USER"}' WHERE id=1`)
		company, candidate, source := int64(990002), int64(990001), "companySite"
		job := parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source}
		tx, err := f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		input, err := parser.ReadCandidateTitleInput(f.ctx, tx, job, "CI_USER")
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if len(input.Roles) != 2 || input.Roles[0].ID != 990101 || input.Roles[1].ID != 990102 || input.Roles[0].Decision != nil || input.Roles[1].Decision != nil {
			tx.Rollback()
			t.Fatal("unexpected title planning snapshot")
		}
		plan, err := parser.PlanCandidateStage(parser.CandidateStageInput(input))
		if err != nil || plan == nil || plan.Type != "classify_title" {
			tx.Rollback()
			t.Fatal("missing classification was not scheduled", err)
		}
		actual, err := (parser.SQLCandidateHandoffPlanner{Owner: "CI_USER"}).Next(f.ctx, tx, job)
		if err != nil || actual == nil || actual.Type != "classify_title" {
			tx.Rollback()
			t.Fatal("SQL planner did not select classification", err)
		}
		tx.Rollback()
		f.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
		tx, err = f.db.BeginTx(f.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = parser.ReadCandidateTitleInput(f.ctx, tx, job, "CI_USER")
		tx.Rollback()
		if !errors.Is(err, parser.ErrCandidateChanged) {
			t.Fatal("favorite scope was bypassed", err)
		}
	})
	for _, frame := range frames {
		t.Run(frame.Name, func(t *testing.T) {
			f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
			lease := titleStageLease(t, f)
			runner := &storedTitleRunner{answers: frame.Answers}
			executor := &parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}
			out, err := executor.Execute(f.ctx, lease)
			if out.Classification == nil {
				t.Fatal("no stored result", err)
			}
			expectedOutcome := frame.Attempts[0]["outcome"].(string)
			if (err == nil) != (expectedOutcome == "complete") || runner.calls != 1 {
				t.Fatal("partial status or call count", err)
			}
			attempts := titleStageRows(t, f, "SELECT title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,errors_json,outcome,cost_usd FROM vacancy_classification_attempts WHERE candidate_id=990001")
			decisions := titleStageRows(t, f, "SELECT root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision FROM vacancy_classifications WHERE candidate_id=990001 ORDER BY root_role_id")
			normalize := func(v any) any { b, _ := json.Marshal(v); var a any; _ = json.Unmarshal(b, &a); return a }
			if !reflect.DeepEqual(normalize(attempts), normalize(frame.Attempts)) || !reflect.DeepEqual(normalize(decisions), normalize(frame.Decisions)) {
				t.Fatal("stored source projection differs")
			}
			company, candidate, source := int64(990002), int64(990001), "companySite"
			tx, err := f.db.BeginTx(f.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := parser.ReadCandidateTitleInput(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source}, "CI_USER")
			tx.Rollback()
			if err != nil {
				t.Fatal("title snapshot", err)
			}
			for _, role := range snapshot.Roles {
				var expected *string
				for _, decision := range frame.Decisions {
					if int64(decision["root_role_id"].(float64)) == role.ID {
						v := decision["decision"].(string)
						expected = &v
					}
				}
				if !reflect.DeepEqual(role.Decision, expected) {
					t.Fatal("snapshot differs from source decisions")
				}
			}
			if frame.Name == "rejected" {
				tx, err := f.db.BeginTx(f.ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := (parser.SQLCandidateHandoffPlanner{Owner: "CI_USER"}).Next(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source})
				tx.Rollback()
				if err != nil || plan != nil {
					t.Fatalf("rejected candidate received next stage: plan=%+v err=%v", plan, err)
				}
			}
			if frame.Name == "complete" {
				f.exec("UPDATE vacancy_candidates SET vacancy_url='https://example.com/ci',title_updated_at='2026-01-02T00:00:00Z' WHERE id=990001")
				readFull := func() (parser.CandidateFullTextInput, error) {
					tx, e := f.db.BeginTx(f.ctx, nil)
					if e != nil {
						t.Fatal(e)
					}
					defer tx.Rollback()
					return parser.ReadCandidateFullTextInput(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source}, "CI_USER")
				}
				full, e := readFull()
				plan, pe := parser.PlanCandidateStage(parser.CandidateStageInput(full))
				if e != nil || pe != nil || plan == nil || plan.Type != "fetch_full_text" || plan.Payload["refresh"] != false {
					t.Fatal("missing full text", e, pe)
				}
				tx, e := f.db.BeginTx(f.ctx, nil)
				if e != nil {
					t.Fatal(e)
				}
				actual, e := (parser.SQLCandidateHandoffPlanner{Owner: "CI_USER"}).Next(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source})
				tx.Rollback()
				if e != nil || actual == nil || actual.Type != "fetch_full_text" || actual.Payload["refresh"] != false {
					t.Fatal("SQL planner did not select full text", e)
				}
				f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('CI_PLANNER_RAW',990001,'https://example.com/ci','https://example.com/ci','CI_BODY',?,'2026-01-01T00:00:00Z','free','valid','{}','2026-01-01T00:00:00Z')", testStageHash("CI_BODY"))
				full, e = readFull()
				plan, pe = parser.PlanCandidateStage(parser.CandidateStageInput(full))
				if e != nil || pe != nil || plan == nil || plan.Type != "fetch_full_text" || plan.Payload["refresh"] != true {
					t.Fatal("stale full text", e, pe)
				}
				f.exec("INSERT INTO vacancy_content_fetches(id,candidate_id,source_url,firecrawl_mode,started_at,duration_ms,status,message) VALUES('CI_PLANNER_FETCH',990001,'https://example.com/ci','free',1767398400000,0,'success','')")
				full, e = readFull()
				if e != nil || full.Snapshot == nil || full.LastSuccessfulFetch == nil || *full.LastSuccessfulFetch != 1767398400000 {
					t.Fatal("latest successful fetch missing", e)
				}
				_, pe = parser.PlanCandidateStage(parser.CandidateStageInput(full))
				if !errors.Is(pe, parser.ErrJobInput) {
					t.Fatal("incomplete extraction snapshot was accepted", pe)
				}
				f.exec("UPDATE vacancy_raw_snapshots SET content_hash=? WHERE id='CI_PLANNER_RAW'", strings.Repeat("a", 64))
				_, e = readFull()
				if !errors.Is(e, parser.ErrSnapshotInvalid) {
					t.Fatal("corrupt full text accepted", e)
				}
				f.exec("DELETE FROM vacancy_content_fetches WHERE id='CI_PLANNER_FETCH'")
				f.exec("DELETE FROM vacancy_raw_snapshots WHERE id='CI_PLANNER_RAW'")
				f.exec("UPDATE vacancy_candidates SET vacancy_url=NULL,title_updated_at=NULL WHERE id=990001")
			}
			if f.count("SELECT COUNT(*) FROM vacancy_candidate_classification_leases") != 0 || f.count("SELECT COUNT(*) FROM vacancy_classification_slots") != 0 {
				t.Fatal("finished lease not released")
			}
			if expectedOutcome == "complete" {
				cachedLease := titleStageLease(t, f)
				if cached, err := executor.Execute(f.ctx, cachedLease); err != nil || !cached.Reused || runner.calls != 1 {
					t.Fatal("cached decisions sent paid POST", err)
				}
			}
		})
	}
	t.Run("real_dispatcher_commits_classification_and_handoff", func(t *testing.T) {
		f := f.sub(t)
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		f.exec("UPDATE vacancy_candidates SET vacancy_url='https://example.com/ci' WHERE id=990001")
		defer f.exec("UPDATE vacancy_candidates SET vacancy_url=NULL WHERE id=990001")
		lease := titleStageLease(t, f)
		runner := &storedTitleRunner{answers: frames[0].Answers}
		out, err := (parser.AIStageDispatcher{Owner: "CI_USER", Classifier: runner}).Execute(f.ctx, lease)
		if err != nil || out.Classification == nil || runner.calls != 1 {
			t.Fatal("classification and SQL handoff failed", err)
		}
		if f.count("SELECT COUNT(*) FROM vacancy_classifications WHERE candidate_id=990001") != 2 ||
			f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_TITLE_JOB' AND status='completed'") != 1 ||
			f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=990001 AND type='fetch_full_text' AND status='pending'") != 1 {
			t.Fatal("classification and next job did not commit together")
		}
	})
	t.Run("root_order", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		f.exec("UPDATE vacancy_roles SET sort_order=CASE WHEN id=990101 THEN 2 ELSE 1 END WHERE id IN (990101,990102)")
		lease := titleStageLease(t, f)
		runner := &storedTitleRunner{answers: frames[0].Answers}
		_, err := (&parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}).Execute(f.ctx, lease)
		if err != nil || !reflect.DeepEqual(runner.requested, []int64{990102, 990101}) {
			t.Fatal("source root order differs", err)
		}
		f.exec("UPDATE vacancy_roles SET sort_order=id WHERE id IN (990101,990102)")
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
	})
	t.Run("favorite_scope", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := titleStageLease(t, f)
		f.exec("UPDATE firecrawl_settings SET favorites_only=1 WHERE id=1")
		runner := &storedTitleRunner{}
		executor := &parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}
		_, err := executor.Execute(f.ctx, lease)
		if !errors.Is(err, parser.ErrCandidateChanged) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM scheduler_attempts") != 0 {
			t.Fatal("foreign favorite scope sent paid request", err)
		}
	})
	t.Run("partial_retry", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := titleStageLease(t, f)
		runner := &storedTitleRunner{answers: frames[1].Answers}
		executor := &parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}
		if _, err := executor.Execute(f.ctx, lease); err == nil {
			t.Fatal("partial response succeeded")
		}
		runner.answers = frames[0].Answers
		lease = titleStageLease(t, f)
		if _, err := executor.Execute(f.ctx, lease); err != nil {
			t.Fatal(err)
		}
		if runner.calls != 2 || !reflect.DeepEqual(runner.requested, []int64{990102}) || f.count("SELECT COUNT(*) FROM vacancy_classifications WHERE candidate_id=990001") != 2 {
			t.Fatal("retry billed already classified role")
		}
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
	})
	t.Run("unknown_failure_history", func(t *testing.T) {
		lease := titleStageLease(t, f)
		runner := &storedTitleRunner{unknown: true}
		executor := &parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}
		if _, err := executor.Execute(f.ctx, lease); err == nil {
			t.Fatal("unknown outcome succeeded")
		}
		if runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_attempts WHERE candidate_id=990001 AND outcome='error' AND model_id IS NULL AND cost_usd IS NULL") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='failed' AND cost_usd IS NULL AND reserved_usd>0") != 1 || f.count("SELECT COUNT(*) FROM vacancy_classification_slots") != 1 || f.count("SELECT COUNT(*) FROM vacancy_candidate_classification_leases") != 0 {
			t.Fatal("unknown failure lost history, reservation or slot")
		}
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		f.exec("DELETE FROM vacancy_classification_slots")
	})

	t.Run("two_slots", func(t *testing.T) {
		f.exec("DELETE FROM vacancy_classification_attempts WHERE candidate_id=990001")
		lease := titleStageLease(t, f)
		f.exec("INSERT INTO vacancy_classification_slots(slot,generation,owner_token,expires_at)VALUES(1,?,'CI_BUSY',?),(2,?,'CI_BUSY',?)", f.repo.Generation, schedulerClock(t, f.db)+300000, f.repo.Generation, schedulerClock(t, f.db)+300000)
		runner := &storedTitleRunner{}
		executor := &parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}
		_, err := executor.Execute(f.ctx, lease)
		if !errors.Is(err, parser.ErrClassificationBusy) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_candidate_classification_leases") != 0 {
			t.Fatal("third slot admitted or partial prepare committed", err)
		}
		f.exec("DELETE FROM vacancy_classification_slots")
	})
	t.Run("prompt_changed", func(t *testing.T) {
		lease := titleStageLease(t, f)
		runner := &storedTitleRunner{answers: frames[0].Answers, after: func() { f.exec("UPDATE vacancy_role_classification SET revision=revision+1 WHERE role_id=990101") }}
		executor := &parser.AIStageDispatcher{Owner: "CI_USER", Classifier: runner}
		_, err := executor.Execute(f.ctx, lease)
		if !errors.Is(err, parser.ErrCandidateChanged) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classifications WHERE candidate_id=990001") != 0 || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=990001 AND type='fetch_full_text'") != 0 {
			t.Fatal("stale prompt result saved", err)
		}
		f.exec("UPDATE vacancy_role_classification SET revision=1 WHERE role_id=990101")
		f.exec("DELETE FROM vacancy_candidate_classification_leases")
		f.exec("DELETE FROM vacancy_classification_slots")
	})
}
