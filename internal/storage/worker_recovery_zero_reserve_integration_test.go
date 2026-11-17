//go:build mariadb && schedulerwrites

package storage

import (
	"reflect"
	"testing"

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
