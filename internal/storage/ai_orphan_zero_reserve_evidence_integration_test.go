//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"itmap/internal/parser"
)

// Старый orphan с нулевым резервом не является доказательством бесплатного
// POST. Но без job/candidate связи нельзя приписать эту попытку другой
// вакансии и блокировать ВСЕ новые AI-задания в текущем поколении.
func TestMariaDBOrphanZeroReserveAIEvidenceNoGlobalBlockDisposable(t *testing.T) {
	f := billingFixture(t)
	billingReset(f)
	const candidateID = int64(990511)
	if f.count("SELECT COUNT(*) FROM vacancy_candidates WHERE id=?", candidateID) != 0 {
		t.Fatal("synthetic candidate namespace not empty")
	}
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990511,'CI_ORPHAN_ZERO_CANDIDATE','CI_TITLE',?,'manual','CI','CI','CI','CI')", strings.Repeat("c", 64))
	t.Cleanup(func() {
		billingReset(f)
		f.exec("DELETE FROM vacancy_candidates WHERE id=?", candidateID)
	})
	f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,provider,started_at,status,reserved_usd,cost_usd) VALUES('CI_AI_ORPHAN_ZERO','CI_MISSING_JOB','CI_MISSING_RUN','extract_vacancy','openrouter',1,'interrupted',0,NULL)")
	history, err := f.repo.InspectHistoricalPaidAttempts(f.ctx)
	if err != nil || history.OrphanZeroReserveProviderUnknown != 1 || history.OrphanUnknown != 1 {
		t.Fatal("orphaned provider-selected unknown not visible", history, err)
	}
	run := f.begin("CI_ORPHAN_UNRELATED_RUN")
	schedulerSeedJob(f, "CI_ORPHAN_UNRELATED_JOB", candidateID)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id='CI_ORPHAN_UNRELATED_JOB'")
	lease := schedulerRequireLease(t, f, run, "CI_ORPHAN_UNRELATED_JOB")
	attempt, err := lease.StartAttempt(f.ctx)
	if err != nil {
		t.Fatal("cannot start independent synthetic attempt", err)
	}
	posts := 0
	_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
		posts++
		return syntheticUsage, nil
	})
	if err != nil || posts != 1 || attempt.Requests() != 1 || attempt.Unknown() {
		t.Fatal("unattributable old orphan globally blocked unrelated work", err, posts)
	}
	if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "completed"}}); err != nil {
		t.Fatal("independent result not settled", err)
	}
	f.finish(run)
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id='CI_AI_ORPHAN_ZERO' AND provider='openrouter' AND reserved_usd=0 AND cost_usd IS NULL AND status='interrupted'") != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id='CI_ORPHAN_UNRELATED_JOB' AND status='completed' AND cost_usd=0.01") != 1 {
		t.Fatal("old uncertain and new confirmed evidence was mixed or changed")
	}
	after, err := f.repo.InspectHistoricalPaidAttempts(f.ctx)
	if err != nil || after.OrphanZeroReserveProviderUnknown != 1 {
		t.Fatal("paid result erased old zero-reserve uncertainty", after, err)
	}
}
