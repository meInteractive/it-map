//go:build mariadb && schedulerwrites

package storage

import (
	"itmap/internal/workerengine"
	"reflect"
	"testing"
)

func TestMariaDBWorkerActivityDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	now := schedulerClock(t, f.db)
	other := "11111111-2222-4333-8444-555555555555"
	if other == f.repo.Generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	for _, v := range []struct {
		id, kind, status, generation string
		created, completed           int64
	}{
		{"CI_activity_scan", "scan_source", "completed", f.repo.Generation, now - 1000, now - 1000},
		{"CI_activity_pipe", "extract_vacancy", "completed", f.repo.Generation, now - 1000, now - 1000},
		{"CI_activity_failed", "fetch_full_text", "failed", f.repo.Generation, now - 1000, now - 1000},
		{"CI_activity_old", "assign_roles", "completed", f.repo.Generation, now - 90000000, now - 90000000},
		{"CI_activity_other", "extract_vacancy", "completed", other, now - 1000, now - 1000},
	} {
		schedulerSeedJob(f, v.id, nil)
		f.exec("UPDATE scheduler_jobs SET type=?,status=?,generation=?,created_at=?,completed_at=? WHERE id=?", v.kind, v.status, v.generation, v.created, v.completed, v.id)
	}
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES
 ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1',?,'CI_op1','free','CIAccount','CIhash','CIstart','CIend',0.25,0.10,'succeeded','CIRun1','CIData1',?,?),
 ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2',?,'CI_op2','free','CIAccount','CIhash','CIstart','CIend',0.25,NULL,'running','CIRun2','CIData2',?,?),
 ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa3',?,'CI_op3','free','CIAccount','CIhash','CIstart','CIend',0.25,0.10,'succeeded','CIRun3','CIData3',?,?)`, f.repo.Generation, now-1000, now-1000, f.repo.Generation, now-1000, now-1000, other, now-1000, now-1000)
	t.Cleanup(func() {
		f.exec("DELETE FROM apify_runs WHERE id IN ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa3')")
	})
	before := schedulerRows(t, f, "")
	got, err := workerengine.NewActivityReader(f.db, f.repo.Generation).ReadActivity(f.ctx)
	if err != nil || got.ActorStarted != 2 || got.ActorSucceededCostRecorded != 1 || got.ActorActiveOrCostMissing != 1 || got.ScanCompleted != 1 || got.PipelineCreated != 2 || got.PipelineCompleted != 1 || got.PipelineFailed != 1 {
		t.Fatal("unexpected activity aggregate", got, err)
	}
	if !reflect.DeepEqual(before, schedulerRows(t, f, "")) {
		t.Fatal("diagnostic mutated queue")
	}
	if _, err = workerengine.NewActivityReader(f.db, other).ReadActivity(f.ctx); err == nil {
		t.Fatal("stale generation accepted")
	}
}
