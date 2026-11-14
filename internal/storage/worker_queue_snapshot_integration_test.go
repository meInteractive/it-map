//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"reflect"
	"testing"

	"itmap/internal/workerengine"
)

func TestMariaDBWorkerQueueSnapshotDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	now := schedulerClock(t, f.db)
	for _, v := range []struct {
		id, kind, status string
		after            int64
	}{
		{"CI_scan_due", "scan_source", "pending", now - 20000},
		{"CI_scan_delayed", "scan_source", "pending", now + 60000},
		{"CI_scan_running", "scan_source", "running", now},
		{"CI_scan_failed", "scan_source", "failed", now},
		{"CI_pipeline_due", "classify_title", "pending", now - 10000},
		{"CI_pipeline_delayed", "fetch_full_text", "pending", now + 60000},
		{"CI_pipeline_running", "extract_vacancy", "running", now},
		{"CI_pipeline_failed", "score_relevance", "failed", now},
		{"CI_completed", "assign_roles", "completed", now},
		{"CI_diagnostic", "diagnostic", "pending", now},
	} {
		schedulerSeedJob(f, v.id, nil)
		f.exec("UPDATE scheduler_jobs SET type=?,status=?,run_after=? WHERE id=?", v.kind, v.status, v.after, v.id)
	}
	other := "11111111-2222-4333-8444-555555555555"
	if other == f.repo.Generation {
		other = "11111111-2222-4333-8444-555555555556"
	}
	schedulerSeedJob(f, "CI_other_generation", nil)
	f.exec("UPDATE scheduler_jobs SET generation=?,type='classify_title',status='pending' WHERE id='CI_other_generation'", other)
	before := schedulerRows(t, f, "")
	got, err := workerengine.NewQueueReader(f.db, f.repo.Generation).ReadQueue(f.ctx)
	if err != nil {
		t.Fatal("queue snapshot failed", err)
	}
	if got.Scan.Due != 1 || got.Scan.Delayed != 1 || got.Scan.Running != 1 || got.Scan.Failed != 1 || got.Scan.OldestDueSeconds < 20 || got.Pipeline.Due != 1 || got.Pipeline.Delayed != 1 || got.Pipeline.Running != 1 || got.Pipeline.Failed != 1 || got.Pipeline.OldestDueSeconds < 10 {
		t.Fatal("unexpected aggregate", got)
	}
	if !reflect.DeepEqual(before, schedulerRows(t, f, "")) {
		t.Fatal("snapshot changed scheduler rows")
	}
	if _, err = workerengine.NewQueueReader(f.db, other).ReadQueue(f.ctx); err == nil {
		t.Fatal("stale generation accepted")
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err = workerengine.NewQueueReader(f.db, f.repo.Generation).ReadQueue(cancelled); err == nil {
		t.Fatal("cancelled snapshot accepted")
	}
}
