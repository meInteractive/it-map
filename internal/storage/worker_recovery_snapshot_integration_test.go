//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"testing"
	"time"

	"itmap/internal/workerengine"
)

func TestMariaDBWorkerRecoverySnapshotDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	if f.count("SELECT COUNT(*) FROM apify_runs") != 0 {
		t.Fatal("recovery fixture requires empty paid ledger")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DELETE FROM apify_runs WHERE id='eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee'"); err != nil {
			t.Error("synthetic recovery cleanup failed")
		}
	})
	old := "11111111-2222-4333-8444-555555555555"
	if old == f.repo.Generation {
		old = "11111111-2222-4333-8444-555555555556"
	}
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_recovery_old_run',?,'automatic',1,'interrupted')", old)
	f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_recovery_old_ai','CI_old_job','CI_recovery_old_run','classify_title',1,'interrupted',0.25,NULL)")
	f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_recovery_orphan_ai','CI_other_job','CI_missing_run','classify_title',1,'interrupted',0.25,NULL)")
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',?,'CI_old_op','free','CIRecovery','CIhash','CIstart','CIend',0.25,NULL,'uncertain','','',1,1)`, old)
	got, err := workerengine.NewRecoveryReader(f.db, f.repo.Generation).ReadRecovery(f.ctx)
	if err != nil || got.ActorUnknown != 1 || got.ActorReconcile != 0 || got.SchedulerHistoricalUnknown != 1 || got.SchedulerOrphanUnknown != 1 {
		t.Fatal("recovery aggregate incorrect", err, got)
	}
	if f.count("SELECT COUNT(*) FROM apify_runs WHERE id='eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee' AND charged_usd IS NULL AND reserved_usd=0.25") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE cost_usd IS NULL AND reserved_usd=0.25") != 2 {
		t.Fatal("recovery snapshot changed paid evidence")
	}
	if _, err = workerengine.NewRecoveryReader(f.db, old).ReadRecovery(f.ctx); err == nil {
		t.Fatal("stale generation accepted")
	}
}
