//go:build mariadb && schedulerwrites

package storage

import (
	"reflect"
	"testing"

	"itmap/internal/paidrecovery"
	"itmap/internal/parser"
	"itmap/internal/workerengine"
)

// Диагностика нулевого денежного резерва не разрешает повторный POST.
// Потеря строки scheduler_jobs делает привязку к кандидату невозможной,
// но отсутствие связи не должно скрывать zero-reserve evidence.
func TestMariaDBWorkerRecoveryZeroReserveProviderUnknownDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	const oldRun = "CI_ZERO_AI_OLD_RUN"
	const currentRun = "CI_ZERO_AI_CURRENT_RUN"
	old := "11111111-2222-4333-8444-555555555555"
	if old == f.repo.Generation {
		old = "11111111-2222-4333-8444-555555555556"
	}
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES(?,?,'automatic',1,'interrupted'),(?,?,'automatic',1,'interrupted')", oldRun, old, currentRun, f.repo.Generation)

	for _, tc := range []struct {
		id, run, kind, provider, status string
		reserve                         float64
		cost                            any
	}{
		{"CI_ZERO_AI_ORPHAN", "CI_ZERO_AI_NO_RUN", "extract_vacancy", "openrouter", "interrupted", 0, nil},
		{"CI_ZERO_AI_OLD", oldRun, "classify_title", "openai", "interrupted", 0, nil},
		{"CI_ZERO_AI_CURRENT", currentRun, "assign_roles", "openrouter", "interrupted", 0, nil},
		{"CI_ZERO_AI_CURRENT_POSITIVE", currentRun, "extract_vacancy", "openai", "running", 0.25, nil},
		{"CI_ZERO_AI_OLD_POSITIVE", oldRun, "extract_vacancy", "openrouter", "interrupted", 0.25, nil},
		{"CI_ZERO_AI_ORPHAN_KNOWN", "CI_ZERO_AI_NO_RUN", "extract_vacancy", "openrouter", "completed", 0, 0.0},
		{"CI_ZERO_AI_OLD_NO_PROVIDER", oldRun, "extract_vacancy", "", "interrupted", 0, nil},
		{"CI_ZERO_AI_OLD_NON_AI", oldRun, "scan_source", "apify", "interrupted", 0, nil},
		{"CI_ZERO_AI_CURRENT_NO_PROVIDER", currentRun, "score_relevance", "", "interrupted", 0, nil},
	} {
		f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,provider,started_at,status,reserved_usd,cost_usd) VALUES(?,'CI_ZERO_AI_NO_JOB',?,?,?,1,?,?,?)",
			tc.id, tc.run, tc.kind, tc.provider, tc.status, tc.reserve, tc.cost)
	}
	before := f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id LIKE 'CI_ZERO_AI_%'")
	unknownBefore := f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id LIKE 'CI_ZERO_AI_%' AND cost_usd IS NULL")

	snapshot, err := workerengine.NewRecoveryReader(f.db, f.repo.Generation).ReadRecovery(f.ctx)
	if err != nil {
		t.Fatal("cannot inspect synthetic zero-reserve evidence", err)
	}
	if snapshot.SchedulerOrphanZeroReserveProviderUnknown != 1 ||
		snapshot.SchedulerHistoricalZeroReserveProviderUnknown != 1 ||
		snapshot.CurrentAIZeroReserveProviderUnknown != 1 ||
		snapshot.SchedulerOrphanUnknown != 1 ||
		snapshot.SchedulerHistoricalUnknown != 2 ||
		snapshot.SchedulerOrphanCostRecorded != 1 ||
		snapshot.CurrentAIReservedRunning != 1 ||
		snapshot.CurrentAIReservedOther != 0 {
		t.Fatal("zero-reserve unknown or paid category was misclassified", snapshot)
	}
	exact, err := f.repo.InspectHistoricalPaidAttempts(f.ctx)
	if err != nil || exact.OrphanZeroReserveProviderUnknown != 1 ||
		exact.HistoricalZeroReserveProviderUnknown != 1 ||
		exact.HistoricalUnknown != 2 ||
		exact.OrphanUnknown != 1 {
		t.Fatal("historical read lost zero-reserve provider evidence", exact, err)
	}
	again, err := workerengine.NewRecoveryReader(f.db, f.repo.Generation).ReadRecovery(f.ctx)
	if err != nil || !reflect.DeepEqual(again, snapshot) {
		t.Fatal("read-only recovery snapshot is not stable", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id LIKE 'CI_ZERO_AI_%'") != before ||
		f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id LIKE 'CI_ZERO_AI_%' AND cost_usd IS NULL") != unknownBefore ||
		f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id='CI_ZERO_AI_ORPHAN' AND provider='openrouter' AND reserved_usd=0 AND cost_usd IS NULL AND status='interrupted'") != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id='CI_ZERO_AI_ORPHAN_KNOWN' AND reserved_usd=0 AND cost_usd=0 AND status='completed'") != 1 {
		t.Fatal("diagnostics changed monetary evidence")
	}
	other := parser.RunRepository{DB: f.db, Generation: old}
	if _, err := other.InspectHistoricalPaidAttempts(f.ctx); err == nil {
		t.Fatal("stale generation accepted")
	}
	if _, err := workerengine.NewRecoveryReader(f.db, old).ReadRecovery(f.ctx); err == nil {
		t.Fatal("stale recovery reader accepted")
	}
}

// Старые сохранённые записи учитываются по составу, а не только по количеству.
func TestMariaDBWorkerReleaseLedgerDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_ledger_run',?,'automatic',1,'interrupted')", f.repo.Generation)
	f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,source_id,status,priority,run_after,created_at,updated_at)
		VALUES('CI_ledger_job',?,'CI_ledger_job','classify_title','hh','failed',10,1,1,1)`, f.repo.Generation)
	f.exec(`INSERT INTO scheduler_attempts(id,job_id,run_id,type,provider,started_at,finished_at,status,reserved_usd)
		VALUES('CI_ledger_attempt','CI_ledger_job','CI_ledger_run','classify_title','openai',1,2,'interrupted',0)`)
	base, err := paidrecovery.ReadReleaseLedger(f.ctx, f.db, f.repo.Generation)
	if err != nil || base.Count != 1 || len(base.Digest) != 64 {
		t.Fatal("old ledger evidence unavailable", base, err)
	}
	f.exec(`INSERT INTO firecrawl_usage_events(id,key_hash,profile,period_start,period_end,operation,reserved_credits,outcome,started_at,active_until,generation)
		VALUES('CI_FC_RELEASE','CI_HASH','free','CI_START','CI_END','scrape',4,'uncertain',1,?,?)`,
		schedulerClock(t, f.db)+60000, f.repo.Generation)
	t.Cleanup(func() { f.exec("DELETE FROM firecrawl_usage_events WHERE id='CI_FC_RELEASE'") })
	if _, err := paidrecovery.ReadReleaseLedgerWithFirecrawl(f.ctx, f.db, f.repo.Generation); err == nil {
		t.Fatal("active Firecrawl reservation did not defer release")
	}
	f.exec("UPDATE firecrawl_usage_events SET active_until=0 WHERE id='CI_FC_RELEASE'")
	if ledger, err := paidrecovery.ReadReleaseLedgerWithFirecrawl(f.ctx, f.db, f.repo.Generation); err != nil || ledger != base {
		t.Fatal("expired Firecrawl reservation still blocks release", ledger, err)
	}
	f.exec("UPDATE scheduler_attempts SET reserved_usd=0.01 WHERE id='CI_ledger_attempt'")
	changed, err := paidrecovery.ReadReleaseLedger(f.ctx, f.db, f.repo.Generation)
	if err != nil || changed.Count != base.Count || changed.Digest == base.Digest {
		t.Fatal("same-count mutation was not detected", changed, err)
	}
	f.exec("UPDATE scheduler_jobs SET status='running' WHERE id='CI_ledger_job'")
	if _, err := paidrecovery.ReadReleaseLedger(f.ctx, f.db, f.repo.Generation); err == nil {
		t.Fatal("active job was accepted as old evidence")
	}
	f.exec("UPDATE scheduler_jobs SET status='failed' WHERE id='CI_ledger_job'")
	f.exec("UPDATE scheduler_attempts SET finished_at=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) WHERE id='CI_ledger_attempt'")
	if _, err := paidrecovery.ReadReleaseLedger(f.ctx, f.db, f.repo.Generation); err == nil {
		t.Fatal("recent attempt was accepted as old evidence")
	}
}
