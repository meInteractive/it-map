//go:build mariadb && schedulerwrites

package storage

import (
	"testing"
	"time"

	"itmap/internal/parser"
)

func TestMariaDBWorkerFairnessDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	now := schedulerClock(t, f.db)
	schedulerSeedJob(f, "CI_fair_old", nil)
	schedulerSeedJob(f, "CI_fair_fresh", nil)
	schedulerSeedJob(f, "CI_fair_future", nil)
	f.exec("UPDATE scheduler_jobs SET priority=0,created_at=?,run_after=? WHERE id='CI_fair_old'", now-int64(time.Hour/time.Millisecond), now-int64(31*time.Minute/time.Millisecond))
	f.exec("UPDATE scheduler_jobs SET priority=100,created_at=?,run_after=? WHERE id='CI_fair_fresh'", now, now-int64(time.Minute/time.Millisecond))
	f.exec("UPDATE scheduler_jobs SET priority=1000,created_at=?,run_after=? WHERE id='CI_fair_future'", now, now+int64(time.Hour/time.Millisecond))
	run := f.begin("CI worker fairness")
	for _, expected := range []string{"CI_fair_old", "CI_fair_fresh"} {
		lease, err := f.repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{Types: []string{"diagnostic"}, WorkerPipelineFairness: true})
		if err != nil || lease == nil || lease.Job().ID != expected {
			t.Fatal("unexpected fair selection", expected, err)
		}
		if err := lease.Finish(f.ctx, parser.FinishOptions{Status: "completed"}); err != nil {
			t.Fatal("finish failed", err)
		}
	}
	lease, err := f.repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{Types: []string{"diagnostic"}, WorkerPipelineFairness: true})
	if err != nil || lease != nil {
		t.Fatal("future job was claimed", err)
	}
	f.finish(run)
}
