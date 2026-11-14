//go:build mariadb && v39restore

package storage

import (
	"context"
	"testing"
	"time"

	"itmap/internal/paidrecovery"
)

// Настоящий restore синтетического backup сохраняет оба денежных журнала.
func TestMariaDBRestorePaidWorkerDisposable(t *testing.T) {
	f := openRestoreFixture(t)
	old := f.generation()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []string{
			"DELETE FROM scheduler_attempts WHERE id='CI_restore_unknown_ai'",
			"DELETE FROM scheduler_runs WHERE id='CI_restore_unknown_run'",
			"DELETE FROM apify_runs WHERE id='eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee'",
		} {
			if _, err := f.db.ExecContext(ctx, q); err != nil {
				t.Error("synthetic paid restore cleanup failed")
			}
		}
	})
	if f.count("SELECT COUNT(*) FROM apify_runs") != 0 || f.count("SELECT COUNT(*) FROM scheduler_attempts") != 0 || f.count("SELECT COUNT(*) FROM scheduler_runs") != 0 {
		t.Fatal("paid restore fixture requires empty operational ledgers")
	}
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_restore_unknown_run',?,'automatic',1,'interrupted')", old)
	f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('CI_restore_unknown_ai','CI_restore_old_job','CI_restore_unknown_run','classify_title',1,'interrupted',0.25,NULL)")
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',?,'CI_restore_old_op','free','CIRestorePaid','CIhash','CIstart','CIend',0.25,NULL,'uncertain','','',1,1)`, old)
	result := f.restore(f.body)
	if result.Generation == old || f.generation() != result.Generation {
		t.Fatal("restore did not rotate generation")
	}
	if f.count("SELECT COUNT(*) FROM apify_runs WHERE id='eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee' AND state='uncertain' AND run_id='' AND charged_usd IS NULL AND reserved_usd=0.25") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id='CI_restore_unknown_ai' AND cost_usd IS NULL AND reserved_usd=0.25") != 1 {
		t.Fatal("restore lost unknown paid evidence")
	}
	if f.count("SELECT COUNT(*) FROM firecrawl_settings WHERE JSON_EXTRACT(scheduler_json,'$.enabled')=true") != 0 || f.count("SELECT COUNT(*) FROM apify_settings WHERE enabled<>0 OR paid_allowed<>0") != 0 {
		t.Fatal("restore left automatic paid work enabled")
	}
	snapshot, err := paidrecovery.Inspect(f.ctx, f.db, result.Generation)
	if err != nil || snapshot.Apify.Unknown != 1 || snapshot.Scheduler.HistoricalUnknown != 1 {
		t.Fatal("restored paid evidence not visible to recovery", err)
	}
}
