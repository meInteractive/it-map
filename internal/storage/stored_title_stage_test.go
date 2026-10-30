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
		executor := &parser.AIStageExecutor{Store: parser.TitleStageStore{Owner: "CI_USER"}, Classifier: runner}
		_, err := executor.Execute(f.ctx, lease)
		if !errors.Is(err, parser.ErrCandidateChanged) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_classifications WHERE candidate_id=990001") != 0 {
			t.Fatal("stale prompt result saved", err)
		}
		f.exec("UPDATE vacancy_role_classification SET revision=1 WHERE role_id=990101")
		f.exec("DELETE FROM vacancy_candidate_classification_leases")
		f.exec("DELETE FROM vacancy_classification_slots")
	})
}
