//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"itmap/internal/parser"
)

// Два разных scheduler job ID могут указывать на одного кандидата. Если
// первый платный вызов не получил достоверного завершения, второй job того
// же AI-этапа не должен повторно отправлять запрос провайдеру.
func TestMariaDBAICandidateCrossJobUnknownPaidDisposable(t *testing.T) {
	const (
		candidateID = int64(990411)
		otherID     = int64(990412)
		priorJob    = "CI_AI_CROSS_PRIOR"
		currentJob  = "CI_AI_CROSS_CURRENT"
	)
	for _, tc := range []struct {
		name, currentType, priorType, priorStatus, provider string
		priorCandidate                                 any
		reserve                                        float64
		cost                                           any
		block                                          bool
	}{
		{"extract_lost_ack_zero_reserve", "extract_vacancy", "extract_vacancy", "interrupted", "openrouter", candidateID, 0, nil, true},
		{"extract_settled_before_stage_save", "extract_vacancy", "extract_vacancy", "interrupted", "openrouter", candidateID, 0, 0.001, true},
		{"classify_unknown", "classify_title", "classify_title", "interrupted", "openrouter", candidateID, 0.1, nil, true},
		{"assign_roles_unknown", "assign_roles", "assign_roles", "interrupted", "openrouter", candidateID, 0.1, nil, true},
		{"relevance_unknown", "score_relevance", "score_relevance", "interrupted", "openrouter", candidateID, 0.1, nil, true},
		{"failed_without_cost", "extract_vacancy", "extract_vacancy", "failed", "openrouter", candidateID, 0.1, nil, true},
		{"completed_missing_cost", "extract_vacancy", "extract_vacancy", "completed", "openrouter", candidateID, 0.1, nil, true},
		{"completed_known_cost", "extract_vacancy", "extract_vacancy", "completed", "openrouter", candidateID, 0.001, 0.001, false},
		{"different_candidate", "extract_vacancy", "extract_vacancy", "interrupted", "openrouter", otherID, 0.1, nil, false},
		{"different_stage", "extract_vacancy", "assign_roles", "interrupted", "openrouter", candidateID, 0.1, nil, false},
		{"provider_never_selected", "extract_vacancy", "extract_vacancy", "interrupted", "", candidateID, 0, nil, false},
	}{
		t.Run(tc.name, func(t *testing.T) {
			f := billingFixture(t)
			billingReset(f)
			for _, id := range []int64{candidateID, otherID} {
				if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=?", id) != 0 {
					t.Fatal("synthetic candidate namespace not empty")
				}
			}
			f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990411,'CI_CROSS_CANDIDATE','CI_TITLE',?,'manual','CI','CI','CI','CI'),(990412,'CI_CROSS_OTHER','CI_TITLE',?,'manual','CI','CI','CI','CI')", strings.Repeat("a", 64), strings.Repeat("b", 64))
			t.Cleanup(func() {
				billingReset(f)
				f.exec("DELETE FROM vacancy_candidates WHERE id IN (990411,990412)")
			})
			// При другом старом неопределённом AI в этом же generation
			// historical guard не должен маскировать проверку конкретного кандидата.
			f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_AI_CROSS_PRIOR_RUN',?,'automatic',1,'interrupted')", f.repo.Generation)
			schedulerSeedJob(f, priorJob, tc.priorCandidate)
			f.exec("UPDATE scheduler_jobs SET type=?,status='failed' WHERE id=?", tc.priorType, priorJob)
			f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,provider,started_at,status,reserved_usd,cost_usd) VALUES('CI_AI_CROSS_PRIOR_ATTEMPT',?,?,?,?,1,?,?,?)", priorJob, "CI_AI_CROSS_PRIOR_RUN", tc.priorType, tc.provider, tc.priorStatus, tc.reserve, tc.cost)
			f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":5,"monthBudgetUsd":5}' WHERE id=1`)
			run := f.begin("CI_AI_CROSS_CURRENT_RUN")
			schedulerSeedJob(f, currentJob, candidateID)
			f.exec("UPDATE scheduler_jobs SET type=? WHERE id=?", tc.currentType, currentJob)
			lease := schedulerRequireLease(t, f, run, currentJob)
			attempt, err := lease.StartAttempt(f.ctx)
			if err != nil {
				t.Fatal("new owned attempt unavailable", err)
			}
			postCalls := 0
			_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
				postCalls++
				return syntheticUsage, nil
			})
			if tc.block {
				if !errors.Is(err, parser.ErrMoneyUncertain) || postCalls != 0 || attempt.Requests() != 0 || attempt.Unknown() {
					t.Fatal("different job bypassed candidate-scoped paid guard", err, postCalls)
				}
			} else if err != nil || postCalls != 1 || attempt.Requests() != 1 || attempt.Unknown() {
				t.Fatal("independent/settled request incorrectly denied", err, postCalls)
			}
			var reserve float64
			var cost sql.NullFloat64
			var status string
			var provider sql.NullString
			if err = f.db.QueryRowContext(f.ctx, "SELECT reserved_usd,cost_usd,status,provider FROM scheduler_attempts WHERE id='CI_AI_CROSS_PRIOR_ATTEMPT'").Scan(&reserve, &cost, &status, &provider); err != nil {
				t.Fatal("prior money evidence missing", err)
			}
			if reserve != tc.reserve || status != tc.priorStatus || provider.String != tc.provider || cost.Valid != (tc.cost != nil) {
				t.Fatal("candidate guard changed prior money evidence")
			}
			finishStatus := "completed"
			if tc.block {
				finishStatus = "failed"
			}
			if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: finishStatus}}); err != nil {
				t.Fatal("new attempt could not finish without altering prior evidence", err)
			}
			f.finish(run)
			if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND status=? AND cost_usd=?", currentJob, finishStatus, func() float64 { if tc.block { return 0 }; return 0.01 }()) != 1 {
				t.Fatal("new attempt ledger or job result inconsistent")
			}
		})
	}
}
