//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/parser"
	"itmap/internal/profiles"
	"os/exec"
	"reflect"
	"testing"
)

func testStageHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type extractionRunner struct {
	content json.RawMessage
	calls   int
	after   func()
	fail    error
	denied  bool
}

func (r *extractionRunner) Automatic(ctx context.Context, req aiproviders.GenerationRequest, _ profiles.ModelInput, b aiproviders.Billing) (aiproviders.AIResult, error) {
	if req.Task != "vacancy_extraction" || req.MaxTokens != 32768 || len(req.Messages) != 2 {
		return aiproviders.AIResult{}, errors.New("CI wrong request")
	}
	if req.CheckActive != nil {
		if e := req.CheckActive(ctx); e != nil {
			return aiproviders.AIResult{}, e
		}
	}
	meta := json.RawMessage(`{"provider":"openai","model":"gpt-6-luna","payload":{"input":[{"role":"user","content":"CI"}],"max_output_tokens":32768}}`)
	_, e := b.Run(ctx, meta, func(context.Context) (json.RawMessage, error) {
		r.calls++
		if r.after != nil {
			r.after()
		}
		if r.fail != nil {
			return nil, r.fail
		}
		return json.RawMessage(`{"model":"gpt-6-luna","usage":{"input_tokens":10,"output_tokens":2}}`), nil
	})
	if e != nil {
		return aiproviders.AIResult{}, e
	}
	if req.CheckActive != nil {
		if e = req.CheckActive(ctx); e != nil {
			return aiproviders.AIResult{}, e
		}
	}
	if req.ValidateContent != nil {
		if e = req.ValidateContent(string(r.content)); e != nil {
			return aiproviders.AIResult{}, e
		}
	}
	return aiproviders.AIResult{Model: "gpt-6-luna", Message: aiproviders.Message{Content: string(r.content)}, Usage: json.RawMessage(`{"input_tokens":10,"output_tokens":2}`)}, nil
}
func extractionStageLease(t *testing.T, f *schedulerFixture) *parser.JobLease {
	billingReset(f)
	f.exec(`UPDATE firecrawl_settings SET favorites_only=0,scheduler_json='{"enabled":true,"sourceIds":["companySite"],"ownerName":"CI_USER","dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	run := f.begin("CI_EXTRACTION_STAGE")
	schedulerSeedJob(f, "CI_EXTRACTION_JOB", 990001)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy',company_id=990002,source_id='companySite' WHERE id='CI_EXTRACTION_JOB'")
	return schedulerRequireLease(t, f, run, "CI_EXTRACTION_JOB")
}
func TestMariaDBExtractionStageSourceDisposable(t *testing.T) {
	f := billingFixture(t)
	raw, e := exec.Command("node", "../../scripts/check-extraction-request.mjs").Output()
	if e != nil {
		t.Fatal("source oracle", e)
	}
	var frames []struct {
		Name, Raw, Prompt, Title, Company, URL string
		Value                                  json.RawMessage
		OK                                     bool
		Data, Diagnostics, Stored              json.RawMessage
		Code, Message                          string
		Status, Calls                          int
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 7 {
		t.Fatal("source corpus")
	}
	for _, q := range []string{"SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001", "SELECT COUNT(*) FROM company_identity WHERE id=990002", "SELECT COUNT(*) FROM vacancy_roles WHERE id=990101", "SELECT COUNT(*) FROM vacancy_detail_settings", "SELECT COUNT(*) FROM vacancy_detail_leases"} {
		if f.count(q) != 0 {
			t.Fatal("synthetic namespace not empty")
		}
	}
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(990002,'CI_STAGE_COMPANY')")
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN',990002,'CI_TITLE',?,'https://example.com/ci-job','companySite','manual','CI','CI','CI','CI')", testStageHash("CI_TITLE"))
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(990101,'CI_ROOT','CI_ROLE',1)")
	f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(990101,'CI_ROLE_RULES',1,1)")
	f.exec("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at)VALUES('CI_ATTEMPT',990001,'CI_TITLE',?,1,?,'[]','complete','CI')", testStageHash("CI_TITLE"), aiproviders.JEVModelID)
	f.exec("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at)VALUES(990001,'CI_ATTEMPT',990101,'CI_ROLE','match',?,?,?,1,?,1,'CI')", aiproviders.JEVModelID, aiproviders.JEVModelID, testStageHash("CI_ROLE_RULES"), testStageHash("CI_TITLE"))
	f.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision)VALUES(1,'CI_EXTRACTION_RULES',1,'openai','gpt-6-luna','medium',1)")
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_detail_leases")
		f.exec("DELETE FROM vacancy_detail_settings WHERE id=1")
		f.exec("DELETE FROM vacancy_candidates WHERE id=990001")
		f.exec("DELETE FROM vacancy_roles WHERE id=990101")
		f.exec("DELETE FROM company_identity WHERE id=990002")
	})
	seed := func(frame int) {
		f.exec("DELETE FROM vacancy_raw_snapshots WHERE candidate_id=990001")
		f.exec("UPDATE vacancy_detail_settings SET markdown=?,prompt_revision=1,model_revision=1 WHERE id=1", frames[frame].Prompt)
		f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at)VALUES('CI_SNAPSHOT',990001,?,?,?,?,'CI','free','valid','{}','2026-01-01')", frames[frame].URL, frames[frame].URL, frames[frame].Raw, testStageHash(frames[frame].Raw))
	}
	for i, frame := range frames {
		t.Run(frame.Name, func(t *testing.T) {
			seed(i)
			lease := extractionStageLease(t, f)
			company, candidate, source := int64(990002), int64(990001), "companySite"
			readPlanner := func() (parser.CandidateExtractionInput, error) {
				tx, err := f.db.BeginTx(f.ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				return parser.ReadCandidateExtractionInput(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source}, "CI_USER")
			}
			before, err := readPlanner()
			if err != nil {
				t.Fatal("planner before extraction", err)
			}
			next, err := parser.PlanCandidateStage(parser.CandidateStageInput(before))
			if err != nil || next == nil || next.Type != "extract_vacancy" {
				t.Fatal("missing extraction not planned", err)
			}
			tx, err := f.db.BeginTx(f.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := (parser.SQLCandidateHandoffPlanner{Owner: "CI_USER"}).Next(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source})
			tx.Rollback()
			if err != nil || planned == nil || planned.Type != "extract_vacancy" {
				t.Fatal("SQL planner did not select extraction", err)
			}
			runner := &extractionRunner{content: frame.Value}
			executor := &parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}, Generator: runner}
			out, e := executor.Execute(f.ctx, lease)
			var actualConfig, actualPrompt string
			var schemaVersion int
			var stored map[string]any
			if e2 := f.db.QueryRowContext(f.ctx, "SELECT config_hash,prompt_version,schema_version FROM vacancy_extractions WHERE candidate_id=990001").Scan(&actualConfig, &actualPrompt, &schemaVersion); e2 != nil {
				t.Fatal(e2)
			}
			_ = json.Unmarshal(frame.Stored, &stored)
			if actualConfig != stored["config_hash"] || actualPrompt != stored["prompt_version"] || float64(schemaVersion) != stored["schema_version"] {
				t.Fatal("source provenance hash differs")
			}
			after, err := readPlanner()
			if err != nil || after.ExtractionConfigHash != actualConfig || after.ExtractionPromptVersion != actualPrompt || (after.ExtractionID != "") != frame.OK {
				t.Fatal("planner extraction differs from source", err)
			}
			if !frame.OK {
				var diag, status string
				var code sql.NullString
				if e2 := f.db.QueryRowContext(f.ctx, "SELECT diagnostics,status,error_code FROM vacancy_extractions WHERE candidate_id=990001").Scan(&diag, &status, &code); e2 != nil {
					t.Fatal(e2)
				}
				var diagnostic any
				_ = json.Unmarshal([]byte(diag), &diagnostic)
				if status != stored["status"] || !code.Valid || code.String != stored["error_code"] || !reflect.DeepEqual(diagnostic, stored["diagnostics"]) {
					t.Fatal("source error receipt differs")
				}
			}
			if frame.Calls == 0 {
				var typed *aiproviders.ServiceError
				if !errors.As(e, &typed) || typed.Code != frame.Code || typed.Status != frame.Status || typed.Message != frame.Message || runner.calls != 0 {
					t.Fatal("preflight mismatch", e)
				}
				if f.count("SELECT COUNT(*) FROM vacancy_extractions WHERE status='error' AND error_code=?", frame.Code) != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='failed' AND reserved_usd=0 AND cost_usd=0") != 1 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
					t.Fatal("preflight failure receipt missing")
				}
				return
			}
			if runner.calls != 1 {
				t.Fatal("incorrect calls", runner.calls)
			}
			if frame.OK {
				if e != nil || out.Generation.Model != "gpt-6-luna" {
					t.Fatal("source success failed", e)
				}
				f.exec("UPDATE vacancy_detail_settings SET markdown='CI_CHANGED_PROMPT' WHERE id=1")
				changed, err := readPlanner()
				if err != nil || changed.ExtractionID != "" || changed.ExtractionPromptVersion == actualPrompt {
					t.Fatal("stale prompt result accepted", err)
				}
				f.exec("UPDATE vacancy_detail_settings SET markdown=? WHERE id=1", frame.Prompt)
				var data, diag string
				var input, output sql.NullInt64
				var cost sql.NullFloat64
				e = f.db.QueryRowContext(f.ctx, "SELECT extracted_data,diagnostics,input_tokens,output_tokens,cost_usd FROM vacancy_extractions WHERE candidate_id=990001 AND status='success'").Scan(&data, &diag, &input, &output, &cost)
				if e != nil {
					t.Fatal(e)
				}
				var actualData, expectedData, actualDiag, expectedDiag any
				_ = json.Unmarshal([]byte(data), &actualData)
				_ = json.Unmarshal(frame.Data, &expectedData)
				_ = json.Unmarshal([]byte(diag), &actualDiag)
				_ = json.Unmarshal(frame.Diagnostics, &expectedDiag)
				if !reflect.DeepEqual(actualData, expectedData) || !reflect.DeepEqual(actualDiag, expectedDiag) || !input.Valid || input.Int64 != 10 || !output.Valid || output.Int64 != 2 || !cost.Valid || cost.Float64 != 0.000002 {
					t.Fatal("stored extraction differs from source")
				}
				if f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
					t.Fatal("leases not released")
				}
				cached := extractionStageLease(t, f)
				out, e = executor.Execute(f.ctx, cached)
				if e != nil || !out.Reused || runner.calls != 1 {
					t.Fatal("cache triggered extra POST", e)
				}
			} else {
				var typed *aiproviders.ServiceError
				if !errors.As(e, &typed) || typed.Code != frame.Code || typed.Status != frame.Status || typed.Message != frame.Message {
					t.Fatal("source failure mismatch", e)
				}
				if f.count("SELECT COUNT(*) FROM vacancy_extractions WHERE status='error' AND error_code='invalid_output'") != 1 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
					t.Fatal("failure receipt or lease release missing")
				}
			}
		})
	}
	t.Run("missing_generator_releases_leases", func(t *testing.T) {
		seed(0)
		lease := extractionStageLease(t, f)
		_, e := (&parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrAIStageUnavailable) || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
			t.Fatal("unused detail leases remain", e)
		}
	})
	t.Run("attempt_replay_releases_leases", func(t *testing.T) {
		seed(0)
		lease := extractionStageLease(t, f)
		if _, e := lease.StartAttempt(f.ctx); e != nil {
			t.Fatal(e)
		}
		runner := &extractionRunner{content: frames[0].Value}
		_, e := (&parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrAttemptReplay) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
			t.Fatal("attempt replay held unused leases", e)
		}
	})
	t.Run("budget_denial", func(t *testing.T) {
		seed(0)
		lease := extractionStageLease(t, f)
		f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"sourceIds":["companySite"],"dayBudgetUsd":0,"monthBudgetUsd":0}' WHERE id=1`)
		runner := &extractionRunner{content: frames[0].Value}
		_, e := (&parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrBudgetLimit) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
			t.Fatal("budget deny failed", e)
		}
	})
	t.Run("unknown_outcome", func(t *testing.T) {
		seed(0)
		lease := extractionStageLease(t, f)
		runner := &extractionRunner{content: frames[0].Value, fail: &aiproviders.ServiceError{Code: "network", Status: 502, Message: "CI_NETWORK"}}
		executor := &parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}, Generator: runner}
		if _, e := executor.Execute(f.ctx, lease); e == nil {
			t.Fatal("unknown outcome accepted")
		}
		if runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 2 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='failed' AND reserved_usd>0 AND cost_usd IS NULL") != 1 || f.count("SELECT COUNT(*) FROM vacancy_extractions WHERE status='error' AND cost_usd IS NULL") != 1 {
			t.Fatal("unknown reservation, slots or history lost")
		}
		f.exec("DELETE FROM vacancy_detail_leases")
	})
	t.Run("prompt_changed", func(t *testing.T) {
		seed(0)
		lease := extractionStageLease(t, f)
		runner := &extractionRunner{content: frames[0].Value, after: func() {
			f.exec("UPDATE vacancy_detail_settings SET prompt_revision=prompt_revision+1,markdown='CI_CHANGED' WHERE id=1")
		}}
		_, e := (&parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrExtractionChanged) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_extractions WHERE status='success'") != 0 {
			t.Fatal("changed prompt was accepted", e)
		}
		f.exec("DELETE FROM vacancy_detail_leases")
	})
	t.Run("two_slots", func(t *testing.T) {
		seed(0)
		lease := extractionStageLease(t, f)
		now := schedulerClock(t, f.db)
		f.exec("INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at)VALUES('llm:1',?,'CI_BUSY',?),('llm:2',?,'CI_BUSY',?)", f.repo.Generation, now+300000, f.repo.Generation, now+300000)
		runner := &extractionRunner{content: frames[0].Value}
		_, e := (&parser.AIStageExecutor{Store: parser.ExtractionStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrDetailBusy) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases WHERE lease_key='candidate:990001'") != 0 {
			t.Fatal("third slot admitted", e)
		}
		f.exec("DELETE FROM vacancy_detail_leases")
	})
}
