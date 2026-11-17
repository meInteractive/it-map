//go:build mariadb && schedulerwrites

package storage

import (
	"reflect"
	"strings"
	"testing"

	"itmap/internal/workerengine"
)

// Два create для одной карточки — это два успешных import receipt, но
// не две новые уникальные вакансии. Все записи только в disposable CI.
func TestMariaDBWorkerActivityImportActionsDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	const company = "CI ACTIVITY IMPORT EMPLOYER"
	for _, q := range []string{
		"SELECT COUNT(*) FROM company_vacancies WHERE id IN (990211,990212)",
		"SELECT COUNT(*) FROM vacancy_candidates WHERE id IN (990211,990212)",
		"SELECT COUNT(*) FROM company_crm WHERE company_name='CI ACTIVITY IMPORT EMPLOYER'",
	} {
		if f.count(q) != 0 {
			t.Fatal("synthetic worker import namespace not empty")
		}
	}
	f.exec("INSERT INTO company_crm(company_name) VALUES(?)", company)
	f.exec("INSERT INTO company_vacancies(id,company_name,url,title,created_at) VALUES(990211,?,'https://example.invalid/ci-activity-create','CI_NEW','2026-11-17T00:00:00Z'),(990212,?,'https://example.invalid/ci-activity-known','CI_EXISTING','2026-10-01T00:00:00Z')", company, company)
	f.exec("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at,main_vacancy_id) VALUES(990211,'CI_ACT_IMP_ONE','CI_ONE',?,'manual','CI','CI','CI','CI',990211),(990212,'CI_ACT_IMP_TWO','CI_TWO',?,'manual','CI','CI','CI','CI',990212)", strings.Repeat("a", 64), strings.Repeat("b", 64))
	t.Cleanup(func() {
		f.exec("DELETE FROM vacancy_import_operations WHERE id LIKE 'CI_activity_import_%'")
		f.exec("DELETE FROM vacancy_candidates WHERE id IN (990211,990212)")
		f.exec("DELETE FROM company_vacancies WHERE id IN (990211,990212)")
		f.exec("DELETE FROM company_crm WHERE company_name=?", company)
	})

	now := schedulerClock(t, f.db)
	other := "11111111-2222-4333-8444-555555555555"
	if other == f.repo.Generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	cases := []struct {
		id, action, jobStatus, receiptStatus, generation string
		ageMs                                            int64
		candidateID, vacancyID                           any
		hasReceipt                                       bool
	}{
		{"CI_activity_import_create_a", "create", "completed", "success", f.repo.Generation, 1000, int64(990211), int64(990211), true},
		// Второй CREATE с другим кандидатом и тем же vacancy_id не является
		// второй уникальной карточкой, хотя счётчик операций вырастет до 2.
		{"CI_activity_import_create_b", "create", "completed", "success", f.repo.Generation, 2000, int64(990212), int64(990211), true},
		{"CI_activity_import_update", "update", "completed", "success", f.repo.Generation, 3000, int64(990212), int64(990212), true},
		{"CI_activity_import_unchanged", "unchanged", "completed", "success", f.repo.Generation, 4000, int64(990212), int64(990212), true},
		{"CI_activity_import_failed_receipt", "create", "completed", "failed", f.repo.Generation, 5000, int64(990211), int64(990211), true},
		{"CI_activity_import_failed_job", "create", "failed", "success", f.repo.Generation, 6000, int64(990211), int64(990211), true},
		{"CI_activity_import_old", "create", "completed", "success", f.repo.Generation, 90000000, int64(990211), int64(990211), true},
		{"CI_activity_import_other_generation", "create", "completed", "success", other, 7000, int64(990211), int64(990211), true},
		{"CI_activity_import_no_receipt", "create", "completed", "", f.repo.Generation, 8000, int64(990211), int64(990211), false},
		{"CI_activity_import_null_vacancy", "create", "completed", "success", f.repo.Generation, 9000, int64(990211), nil, true},
		{"CI_activity_import_null_candidate", "create", "completed", "success", f.repo.Generation, 10000, nil, int64(990211), true},
	}
	for _, tc := range cases {
		schedulerSeedJob(f, tc.id, nil)
		f.exec("UPDATE scheduler_jobs SET type='import_vacancy',status=?,generation=?,candidate_id=?,created_at=?,completed_at=? WHERE id=?",
			tc.jobStatus, tc.generation, tc.candidateID, now-tc.ageMs, now-tc.ageMs, tc.id)
		if tc.hasReceipt {
			f.exec("INSERT INTO vacancy_import_operations(id,candidate_id,vacancy_id,action,status,processed_at,changes_json) VALUES(?,?,?,?,?,'CI_SYNTHETIC','{}')",
				tc.id, tc.candidateID, tc.vacancyID, tc.action, tc.receiptStatus)
		}
	}
	// Непустая очередь и другие pipeline stages не превращаются в импорты.
	schedulerSeedJob(f, "CI_activity_other_stage", nil)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy',status='completed',created_at=?,completed_at=? WHERE id='CI_activity_other_stage'", now-1000, now-1000)
	before := schedulerRows(t, f, "")
	receipts := f.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE id LIKE 'CI_activity_import_%'")
	got, err := workerengine.NewActivityReader(f.db, f.repo.Generation).ReadActivity(f.ctx)
	if err != nil {
		t.Fatal("read-only activity import counters unavailable", err)
	}
	if got.ImportCreateOperations != 2 || got.ImportUpdateOperations != 1 || got.ImportUnchangedOperations != 1 {
		t.Fatal("import operation counters confused retries, generations, incomplete jobs or unique vacancies", got)
	}
	if got.PipelineCompleted != 10 || got.PipelineFailed != 1 {
		t.Fatal("pipeline job count does not reflect completed/failed jobs", got)
	}
	if !reflect.DeepEqual(before, schedulerRows(t, f, "")) || receipts != f.count("SELECT COUNT(*) FROM vacancy_import_operations WHERE id LIKE 'CI_activity_import_%'") {
		t.Fatal("activity diagnostics mutated stored facts")
	}
	if _, err := workerengine.NewActivityReader(f.db, other).ReadActivity(f.ctx); err == nil {
		t.Fatal("stale worker generation accepted")
	}
}
