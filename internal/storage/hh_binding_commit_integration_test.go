//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/consumercontrol"
	"itmap/internal/integrations/apify"
	"itmap/internal/parser"
)

func testBoundHHBindingUnknownCommit(t *testing.T, f *schedulerFixture) {
	t.Helper()
	billingReset(f)
	if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN ('go_consumer_control','go_consumer_schema_version','go_hh_attempt_bindings')") != 0 || f.count("SELECT COUNT(*) FROM apify_runs WHERE id='00000000000000000000000000000002'") != 0 {
		t.Fatal("HH binding fixture is not empty")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, q := range []string{"DELETE FROM apify_runs WHERE id='00000000000000000000000000000002'", "DROP TABLE IF EXISTS go_hh_attempt_bindings", "DROP TABLE IF EXISTS go_consumer_schema_version", "DROP TABLE IF EXISTS go_consumer_control"} {
			if _, err := f.db.ExecContext(ctx, q); err != nil {
				t.Error("HH binding fixture cleanup failed")
			}
		}
	})
	f.exec(`CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	f.exec(`CREATE TABLE go_consumer_schema_version (id BIGINT NOT NULL PRIMARY KEY, version BIGINT NOT NULL, status VARCHAR(16) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	f.exec(`CREATE TABLE go_hh_attempt_bindings (operation_id VARCHAR(255) NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, operation_key VARCHAR(80) NOT NULL, job_id VARCHAR(255) NOT NULL, attempt_id VARCHAR(255) NOT NULL, UNIQUE KEY hh_binding_identity (generation,operation_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	f.exec("INSERT INTO go_consumer_schema_version VALUES(1,1,'complete')")
	token, err := consumercontrol.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := token.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.exec("INSERT INTO go_consumer_control VALUES(1,?,1,'worker',?,?)", f.repo.Generation, digest[:], schedulerClock(t, f.db)+60000)
	repo, fired := billingCommitRepository(t, f.repo.Generation, "INSERT INTO go_hh_attempt_bindings")
	req := consumercontrol.Request{Generation: f.repo.Generation, Revision: 1, Origin: consumercontrol.WorkerRun, Token: token}
	start, bound, err := repo.BeginBoundRun(f.ctx, parser.RunOptions{RequestID: "CI HH binding commit", Diagnostic: true, Duration: 20 * time.Second}, req)
	if err != nil || bound == nil {
		t.Fatal("bound run failed", err)
	}
	jobID := "CI_hh_bind_commit_job"
	schedulerSeedJob(f, jobID, nil)
	lease, err := bound.ClaimJob(f.ctx, parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := bound.StartBoundAttempt(f.ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	if err = attempt.Reserve(f.ctx, syntheticPaid); err != nil {
		t.Fatal(err)
	}
	v := apifysettings.OperationVersion{Generation: f.repo.Generation, CompanyID: 991302, SourceRevision: 1, SourceURL: "https://hh.ru/employer/456", Profile: "free"}
	step := "CI page"
	key, err := apifyruns.HHJobKey(f.repo.Generation, jobID, step)
	if err != nil {
		t.Fatal(err)
	}
	opKey, err := apifyruns.HHOperationIdentity(v, key, apify.HHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	now := schedulerClock(t, f.db)
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES('00000000000000000000000000000002',?,?,'free','CIAccount','CIhash','CIstart','CIend',0.25,'starting',?,?)`, f.repo.Generation, opKey, now, now)
	if err = attempt.BindHHReservation(f.ctx, v, step, apify.HHOptions{}); !errors.Is(err, parser.ErrRunStorage) || !fired.Load() {
		t.Fatal("unknown binding commit not reproduced", err)
	}
	if f.count("SELECT COUNT(*) FROM go_hh_attempt_bindings WHERE operation_id='00000000000000000000000000000002'") != 1 {
		t.Fatal("committed binding disappeared")
	}
	if err = attempt.BindHHReservation(f.ctx, v, step, apify.HHOptions{}); err != nil {
		t.Fatal("idempotent recovery failed", err)
	}
	if f.count("SELECT COUNT(*) FROM go_hh_attempt_bindings") != 1 || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0.26 AND cost_usd IS NULL", jobID) != 1 || f.count("SELECT COUNT(*) FROM apify_runs WHERE id='00000000000000000000000000000002' AND state='starting' AND reserved_usd=0.25 AND charged_usd IS NULL") != 1 {
		t.Fatal("recovery duplicated link or changed reserve")
	}
	if err = repo.Finish(f.ctx, start.Run.ID, "completed"); err != nil {
		t.Fatal(err)
	}
}
