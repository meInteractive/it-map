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
	"os/exec"
	"reflect"
	"testing"
)

type leafStageRunner struct {
	content         json.RawMessage
	model, provider string
	calls           int
	failure         error
	unknown         bool
	after           func()
}

func (r *leafStageRunner) Automatic(ctx context.Context, req aiproviders.GenerationRequest, _ profiles.ModelInput, b aiproviders.Billing) (aiproviders.AIResult, error) {
	if req.Task != "vacancy_role" || req.MaxTokens != 4096 {
		return aiproviders.AIResult{}, errors.New("CI wrong role request")
	}
	if req.CheckActive != nil {
		if e := req.CheckActive(ctx); e != nil {
			return aiproviders.AIResult{}, e
		}
	}
	meta := json.RawMessage(`{"provider":"openai","model":"gpt-6-luna","payload":{"input":[{"role":"user","content":"CI"}],"max_output_tokens":32768}}`)
	usage := json.RawMessage(`{"input_tokens":10,"output_tokens":2}`)
	if r.provider == "openrouter" {
		meta = json.RawMessage(`{"provider":"openrouter","model":"typesafe/jev-1.13","contextLength":100,"pricing":{"prompt":"0.00001","completion":"0"},"payload":{"questions":{"q":{}}}}`)
		usage = json.RawMessage(`{"cost":0.001}`)
	}
	response, _ := json.Marshal(map[string]any{"model": r.model, "usage": usage})
	_, e := b.Run(ctx, meta, func(context.Context) (json.RawMessage, error) {
		r.calls++
		if r.after != nil {
			r.after()
		}
		if r.unknown {
			return nil, r.failure
		}
		return response, nil
	})
	if e != nil {
		return aiproviders.AIResult{}, e
	}
	if req.CheckActive != nil {
		if e = req.CheckActive(ctx); e != nil {
			return aiproviders.AIResult{}, e
		}
	}
	if r.failure != nil {
		return aiproviders.AIResult{}, r.failure
	}
	if req.ValidateContent != nil {
		if e = req.ValidateContent(string(r.content)); e != nil {
			return aiproviders.AIResult{}, e
		}
	}
	return aiproviders.AIResult{Model: r.model, Message: aiproviders.Message{Content: string(r.content)}, Usage: usage}, nil
}
func leafStageLease(t *testing.T, f *schedulerFixture) *parser.JobLease {
	billingReset(f)
	f.exec(`UPDATE firecrawl_settings SET favorites_only=0,scheduler_json='{"enabled":true,"sourceIds":["companySite"],"ownerName":"CI_USER","dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	run := f.begin("CI_LEAF_STAGE")
	schedulerSeedJob(f, "CI_LEAF_JOB", 990001)
	f.exec("UPDATE scheduler_jobs SET type='assign_roles',company_id=990002,source_id='companySite' WHERE id='CI_LEAF_JOB'")
	return schedulerRequireLease(t, f, run, "CI_LEAF_JOB")
}
func TestMariaDBLeafStageSourceDisposable(t *testing.T) {
	f := billingFixture(t)
	raw, e := exec.Command("node", "../../scripts/check-leaf-role-request.mjs").Output()
	if e != nil {
		t.Fatal("source oracle", e)
	}
	var frames []struct {
		Name, Provider, Title, Markdown, Prompt, Message string
		Value, Stored                                    json.RawMessage
		OK                                               bool
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 4 {
		t.Fatal("source corpus")
	}
	extractionRaw, e := exec.Command("node", "../../scripts/check-extraction-request.mjs").Output()
	if e != nil {
		t.Fatal(e)
	}
	var extra []struct{ Value, Stored json.RawMessage }
	if json.Unmarshal(extractionRaw, &extra) != nil || len(extra) == 0 {
		t.Fatal("extraction source")
	}
	var extractionStored struct {
		ConfigHash string `json:"config_hash"`
	}
	_ = json.Unmarshal(extra[0].Stored, &extractionStored)
	if extractionStored.ConfigHash == "" {
		t.Fatal("source config hash")
	}
	for _, q := range []string{"SELECT COUNT(*) FROM vacancy_candidates WHERE id=990001", "SELECT COUNT(*) FROM company_identity WHERE id=990002", "SELECT COUNT(*) FROM vacancy_roles WHERE id IN (990101,990102,990103)", "SELECT COUNT(*) FROM vacancy_detail_settings", "SELECT COUNT(*) FROM vacancy_role_settings", "SELECT COUNT(*) FROM vacancy_detail_leases"} {
		if f.count(q) != 0 {
			t.Fatal("synthetic namespace not empty")
		}
	}
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(990002,'CI_STAGE_COMPANY')")
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN',990002,'CI_TITLE',?,'https://example.com/ci-job','companySite','manual','CI','CI','CI','CI')", testStageHash("CI_TITLE"))
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(990101,'CI_ROOT','CI_ROLE',1)")
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order)VALUES(990102,'CI_LEAF_1','CI_ONE',990101,1),(990103,'CI_LEAF_2','CI_TWO',990101,2)")
	f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(990101,'CI_ROLE_RULES',1,1)")
	f.exec("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at)VALUES('CI_ATTEMPT',990001,'CI_TITLE',?,1,?,'[]','complete','CI')", testStageHash("CI_TITLE"), aiproviders.JEVModelID)
	f.exec("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at)VALUES(990001,'CI_ATTEMPT',990101,'CI_ROLE','match',?,?,?,1,?,1,'CI')", aiproviders.JEVModelID, aiproviders.JEVModelID, testStageHash("CI_ROLE_RULES"), testStageHash("CI_TITLE"))
	f.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision)VALUES(1,'CI_EXTRACT',1,'openai','gpt-6-luna','medium',1)")
	f.exec("INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision)VALUES(1,'CI_ROLE_PROMPT',1,'openai','gpt-6-luna','best_free','medium',1)")
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_detail_leases")
		f.exec("DELETE FROM vacancy_role_settings WHERE id=1")
		f.exec("DELETE FROM vacancy_detail_settings WHERE id=1")
		f.exec("DELETE FROM vacancy_candidates WHERE id=990001")
		f.exec("DELETE FROM vacancy_roles WHERE id IN (990102,990103)")
		f.exec("DELETE FROM vacancy_roles WHERE id=990101")
		f.exec("DELETE FROM company_identity WHERE id=990002")
	})
	seed := func(i int) {
		f.exec("DELETE FROM vacancy_raw_snapshots WHERE candidate_id=990001")
		f.exec("UPDATE vacancy_role_settings SET markdown='CI_ROLE_PROMPT',prompt_revision=1,provider=?,openrouter_model=?,model_revision=1 WHERE id=1", map[bool]string{true: "openrouter", false: "openai"}[frames[i].Provider == "jev"], map[bool]string{true: "jev", false: "best_free"}[frames[i].Provider == "jev"])
		f.exec("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at)VALUES('CI_SNAPSHOT',990001,'https://example.com/ci-job','https://example.com/ci-job','CI_MARKDOWN',?,'CI','free','valid','{}','2026-01-01')", testStageHash("CI_MARKDOWN"))
		f.exec("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,extracted_data,processed_at,duration_ms,status,diagnostics)VALUES('CI_EXTRACTION',990001,'CI_SNAPSHOT','openai','gpt-6-luna',?,?,2,?,'2026-01-01',0,'success','{}')", extractionStored.ConfigHash, testStageHash("CI_EXTRACT"), string(extra[0].Value))
	}
	for i, frame := range frames {
		t.Run(frame.Name, func(t *testing.T) {
			seed(i)
			lease := leafStageLease(t, f)
			company, candidate, source := int64(990002), int64(990001), "companySite"
			readPlanner := func() (parser.CandidateLeafInput, error) {
				tx, err := f.db.BeginTx(f.ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				return parser.ReadCandidateLeafInput(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source}, "CI_USER")
			}
			before, err := readPlanner()
			if err != nil {
				t.Fatal("planner before roles", err)
			}
			next, err := parser.PlanCandidateStage(parser.CandidateStageInput(before))
			if err != nil || next == nil || next.Type != "assign_roles" {
				t.Fatal("missing role result not planned", err)
			}
			tx, err := f.db.BeginTx(f.ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := (parser.SQLCandidateHandoffPlanner{Owner: "CI_USER"}).Next(f.ctx, tx, parser.Job{CompanyID: &company, CandidateID: &candidate, SourceID: &source})
			tx.Rollback()
			if err != nil || !reflect.DeepEqual(planned, next) {
				t.Fatal("SQL planner did not select source role stage", err)
			}
			model, provider := "gpt-6-luna", "openai"
			content := frame.Value
			var failure error
			if frame.Provider == "jev" {
				model, provider = aiproviders.JEVModelID, "openrouter"
				content = json.RawMessage(`{"role_ids":[990102]}`)
				if !frame.OK {
					failure = &aiproviders.ServiceError{Code: "invalid_output", Status: 502, Message: frame.Message}
				}
			}
			runner := &leafStageRunner{content: content, model: model, provider: provider, failure: failure}
			dispatcher := parser.AIStageDispatcher{Owner: "CI_USER", Generator: runner}
			out, e := dispatcher.Execute(f.ctx, lease)
			if frame.OK && e != nil || !frame.OK && e == nil || runner.calls != 1 {
				t.Fatal("source role result failed", e)
			}
			var inputHash, treeHash, promptHash, configHash, providerDB, status, message string
			var modelDB, roleIDs, code sql.NullString
			var input, output sql.NullInt64
			var cost sql.NullFloat64
			e = f.db.QueryRowContext(f.ctx, "SELECT input_hash,tree_hash,prompt_version,config_hash,model_id,provider,role_ids,status,error_code,message,input_tokens,output_tokens,cost_usd FROM vacancy_role_results WHERE candidate_id=990001").Scan(&inputHash, &treeHash, &promptHash, &configHash, &modelDB, &providerDB, &roleIDs, &status, &code, &message, &input, &output, &cost)
			if e != nil {
				t.Fatal(e)
			}
			after, err := readPlanner()
			if err != nil || after.LeafInputHash != inputHash || (after.Leaf != nil) != frame.OK {
				t.Fatal("planner role result differs", err)
			}
			if frame.OK {
				wanted := 0
				if len(after.Leaf.RoleIDs) > 0 {
					wanted = 1
				}
				if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE candidate_id=990001 AND type='import_vacancy' AND status='pending'") != wanted || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_LEAF_JOB' AND status='completed'") != 1 {
					t.Fatal("role result and next stage were not committed together")
				}
			}
			nullString := func(v sql.NullString) any {
				if v.Valid {
					return v.String
				}
				return nil
			}
			nullInt := func(v sql.NullInt64) any {
				if v.Valid {
					return v.Int64
				}
				return nil
			}
			nullFloat := func(v sql.NullFloat64) any {
				if v.Valid {
					return v.Float64
				}
				return nil
			}
			var parsedIDs any
			if roleIDs.Valid {
				_ = json.Unmarshal([]byte(roleIDs.String), &parsedIDs)
			}
			got := map[string]any{"input_hash": inputHash, "tree_hash": treeHash, "prompt_version": promptHash, "config_hash": configHash, "model_id": nullString(modelDB), "provider": providerDB, "role_ids": parsedIDs, "status": status, "error_code": nullString(code), "message": message, "input_tokens": nullInt(input), "output_tokens": nullInt(output), "cost_usd": nullFloat(cost)}
			encoded, _ := json.Marshal(got)
			var normalized, expected any
			_ = json.Unmarshal(encoded, &normalized)
			_ = json.Unmarshal(frame.Stored, &expected)
			if !reflect.DeepEqual(normalized, expected) {
				t.Fatalf("source role result differs: %s expected %s", encoded, frame.Stored)
			}
			if f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
				t.Fatal("finished role leases remain")
			}
			if frame.OK {
				cached := leafStageLease(t, f)
				out, e = dispatcher.Execute(f.ctx, cached)
				if e != nil || !out.Reused || runner.calls != 1 {
					t.Fatal("role cache sent extra POST", e)
				}
			}
		})
	}
	t.Run("missing_extraction", func(t *testing.T) {
		seed(0)
		f.exec("DELETE FROM vacancy_extractions WHERE candidate_id=990001")
		lease := leafStageLease(t, f)
		runner := &leafStageRunner{}
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrExtractionMissing) || runner.calls != 0 {
			t.Fatal("missing extraction sent POST", e)
		}
	})
	t.Run("missing_generator_releases_leases", func(t *testing.T) {
		seed(0)
		lease := leafStageLease(t, f)
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrAIStageUnavailable) || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
			t.Fatal("unused role leases remain", e)
		}
	})
	t.Run("attempt_replay_releases_leases", func(t *testing.T) {
		seed(0)
		lease := leafStageLease(t, f)
		if _, e := lease.StartAttempt(f.ctx); e != nil {
			t.Fatal(e)
		}
		runner := &leafStageRunner{content: frames[0].Value, model: "gpt-6-luna", provider: "openai"}
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrAttemptReplay) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
			t.Fatal("replay held unused role leases", e)
		}
	})
	t.Run("budget_denial", func(t *testing.T) {
		seed(0)
		lease := leafStageLease(t, f)
		f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"sourceIds":["companySite"],"dayBudgetUsd":0,"monthBudgetUsd":0}' WHERE id=1`)
		runner := &leafStageRunner{content: frames[0].Value, model: "gpt-6-luna", provider: "openai"}
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrBudgetLimit) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 0 {
			t.Fatal("budget denial failed", e)
		}
	})
	t.Run("unknown_outcome", func(t *testing.T) {
		seed(0)
		lease := leafStageLease(t, f)
		runner := &leafStageRunner{model: "gpt-6-luna", provider: "openai", unknown: true, failure: &aiproviders.ServiceError{Code: "network", Status: 502, Message: "CI_NETWORK"}}
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if e == nil || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases") != 2 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE status='failed' AND reserved_usd>0 AND cost_usd IS NULL") != 1 || f.count("SELECT COUNT(*) FROM vacancy_role_results WHERE status='error' AND cost_usd IS NULL") != 1 {
			t.Fatal("unknown role operation lost reservation or leases", e)
		}
		f.exec("DELETE FROM vacancy_detail_leases")
	})
	t.Run("tree_changed", func(t *testing.T) {
		seed(0)
		lease := leafStageLease(t, f)
		runner := &leafStageRunner{content: frames[0].Value, model: "gpt-6-luna", provider: "openai", after: func() { f.exec("UPDATE vacancy_roles SET name='CI_CHANGED' WHERE id=990102") }}
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrLeafChanged) || runner.calls != 1 || f.count("SELECT COUNT(*) FROM vacancy_role_results WHERE status='success'") != 0 {
			t.Fatal("stale tree result saved", e)
		}
		f.exec("UPDATE vacancy_roles SET name='CI_ONE' WHERE id=990102")
		f.exec("DELETE FROM vacancy_detail_leases")
	})
	t.Run("two_slots", func(t *testing.T) {
		seed(0)
		lease := leafStageLease(t, f)
		now := schedulerClock(t, f.db)
		f.exec("INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at)VALUES('llm:1','00000000-0000-4000-8000-000000000001','CI_BUSY',?),('llm:2',?,'CI_BUSY',?)", now+300000, f.repo.Generation, now+300000)
		runner := &leafStageRunner{}
		_, e := (&parser.AIStageExecutor{Store: parser.LeafStageStore{Owner: "CI_USER"}, Generator: runner}).Execute(f.ctx, lease)
		if !errors.Is(e, parser.ErrDetailBusy) || runner.calls != 0 || f.count("SELECT COUNT(*) FROM vacancy_detail_leases WHERE lease_key='candidate:990001'") != 0 {
			t.Fatal("third slot admitted", e)
		}
		f.exec("DELETE FROM vacancy_detail_leases")
	})
}
