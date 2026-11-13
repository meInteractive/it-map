//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/consumercontrol"
	"itmap/internal/integrations/apify"
	"itmap/internal/parser"
)

func testBoundHHBinding(t *testing.T, f *schedulerFixture, req consumercontrol.Request) {
	t.Helper()
	f.exec(`CREATE TABLE go_consumer_schema_version (id BIGINT NOT NULL PRIMARY KEY, version BIGINT NOT NULL, status VARCHAR(16) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_consumer_schema_version"); err != nil {
			t.Error("marker cleanup failed")
		}
	})
	f.exec("INSERT INTO go_consumer_schema_version VALUES(1,1,'complete')")
	f.exec(`CREATE TABLE go_hh_attempt_bindings (operation_id VARCHAR(255) NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, operation_key VARCHAR(80) NOT NULL, job_id VARCHAR(255) NOT NULL, attempt_id VARCHAR(255) NOT NULL, UNIQUE KEY hh_binding_identity (generation,operation_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := f.db.ExecContext(ctx, "DROP TABLE go_hh_attempt_bindings"); err != nil {
			t.Error("binding cleanup failed")
		}
	})
	start, bound, err := f.repo.BeginBoundRun(f.ctx, parser.RunOptions{RequestID: "CI HH binding run", Diagnostic: true, Duration: 20 * time.Second}, req)
	if err != nil || bound == nil {
		t.Fatal("bound run failed", err)
	}
	defer f.exec("DELETE FROM scheduler_runs WHERE id=?", start.Run.ID)
	jobID := "CI_hh_bind_job"
	schedulerSeedJob(f, jobID, nil)
	defer f.exec("DELETE FROM scheduler_jobs WHERE id=?", jobID)
	defer f.exec("DELETE FROM scheduler_attempts WHERE job_id=?", jobID)
	if f.count("SELECT COUNT(*) FROM apify_runs WHERE id='00000000000000000000000000000001'") != 0 {
		t.Fatal("synthetic operation ID is already in use")
	}
	defer f.exec("DELETE FROM apify_runs WHERE id='00000000000000000000000000000001'")
	lease, err := bound.ClaimJob(f.ctx, parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := bound.StartBoundAttempt(f.ctx, lease)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	defer f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true}' WHERE id=1`)
	v := apifysettings.OperationVersion{Generation: f.repo.Generation, CompanyID: 991302, SourceRevision: 1, SourceURL: "https://hh.ru/employer/456", Profile: "free"}
	step := "CI page"
	options := apify.HHOptions{}
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, parser.ErrMoneyUncertain) {
		t.Fatal("unreserved attempt bound", err)
	}
	if err = attempt.Reserve(f.ctx, []byte(`{"provider":"apify","model":"CI_actor","capUsd":0.26}`)); err != nil {
		t.Fatal(err)
	}
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHEvidenceAbsent) {
		t.Fatal("missing operation bound", err)
	}
	key, err := apifyruns.HHJobKey(f.repo.Generation, jobID, step)
	if err != nil {
		t.Fatal(err)
	}
	opKey, err := apifyruns.HHOperationIdentity(v, key, options)
	if err != nil {
		t.Fatal(err)
	}
	now := schedulerClock(t, f.db)
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES('00000000000000000000000000000001',?,?,'free','CIAccount','CIhash','CIstart','CIend',0.25,'starting',?,?)`, f.repo.Generation, opKey, now, now)
	if _, err = attempt.InspectBoundHHRecovery(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHEvidenceAbsent) {
		t.Fatal("unbound operation accepted as durable evidence", err)
	}
	f.exec("UPDATE apify_runs SET reserved_usd=0.24 WHERE id='00000000000000000000000000000001'")
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHBindingConflict) {
		t.Fatal("unexpected provider reservation bound", err)
	}
	f.exec("UPDATE apify_runs SET reserved_usd=0.25 WHERE id='00000000000000000000000000000001'")
	f.exec("UPDATE apify_runs SET created_at=? WHERE id='00000000000000000000000000000001'", now-300000)
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHBindingConflict) {
		t.Fatal("expired starting reservation bound", err)
	}
	f.exec("UPDATE apify_runs SET created_at=? WHERE id='00000000000000000000000000000001'", now)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- attempt.BindHHReservation(f.ctx, v, step, options) }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("idempotent concurrent bind failed", err)
		}
	}
	if f.count("SELECT COUNT(*) FROM go_hh_attempt_bindings WHERE operation_id='00000000000000000000000000000001' AND job_id=?", jobID) != 1 {
		t.Fatal("binding missing or duplicated")
	}
	if state, err := attempt.InspectBoundHHRecovery(f.ctx, v, step, options); err != nil || state != apifyruns.RecoveryUnknown {
		t.Fatal("bound unknown evidence rejected", state, err)
	}
	f.exec("UPDATE go_hh_attempt_bindings SET attempt_id='CI_wrong' WHERE operation_id='00000000000000000000000000000001'")
	if _, err = attempt.InspectBoundHHRecovery(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHBindingConflict) {
		t.Fatal("wrong attempt accepted as durable evidence", err)
	}
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHBindingConflict) {
		t.Fatal("wrong attempt rebound", err)
	}
	f.exec("UPDATE go_hh_attempt_bindings SET attempt_id=(SELECT id FROM scheduler_attempts WHERE job_id=? LIMIT 1) WHERE operation_id='00000000000000000000000000000001'", jobID)
	f.exec("UPDATE apify_runs SET state='uncertain' WHERE id='00000000000000000000000000000001'")
	if state, err := attempt.InspectBoundHHRecovery(f.ctx, v, step, options); err != nil || state != apifyruns.RecoveryUnknown {
		t.Fatal("uncertain bound evidence rejected", state, err)
	}
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHBindingConflict) {
		t.Fatal("uncertain operation accepted as unstarted", err)
	}
	f.exec("UPDATE apify_runs SET state='running',run_id='CIRun',dataset_id='CIData' WHERE id='00000000000000000000000000000001'")
	if state, err := attempt.InspectBoundHHRecovery(f.ctx, v, step, options); err != nil || state != apifyruns.RecoveryReconcile {
		t.Fatal("known bound run not available for reconciliation", state, err)
	}
	f.exec("UPDATE apify_runs SET state='succeeded',charged_usd=0.08 WHERE id='00000000000000000000000000000001'")
	if state, err := attempt.InspectBoundHHRecovery(f.ctx, v, step, options); err != nil || state != apifyruns.RecoveryTerminalCostRecorded {
		t.Fatal("terminal bound evidence rejected", state, err)
	}
	f.exec("UPDATE apify_runs SET state='starting',run_id='',dataset_id='',charged_usd=NULL WHERE id='00000000000000000000000000000001'")
	f.exec("UPDATE go_consumer_schema_version SET status='incomplete' WHERE id=1")
	if _, err = attempt.InspectBoundHHRecovery(f.ctx, v, step, options); !errors.Is(err, consumercontrol.ErrState) {
		t.Fatal("incomplete marker accepted for evidence", err)
	}
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, consumercontrol.ErrState) {
		t.Fatal("incomplete marker accepted", err)
	}
	f.exec("UPDATE go_consumer_schema_version SET status='complete' WHERE id=1")
	f.exec("UPDATE go_consumer_control SET mode='draining',revision=4 WHERE id=1")
	if state, err := attempt.InspectBoundHHRecovery(f.ctx, v, step, options); err != nil || state != apifyruns.RecoveryUnknown {
		t.Fatal("original owner could not inspect in draining", state, err)
	}
	if err = attempt.BindHHReservation(f.ctx, v, step, options); !errors.Is(err, consumercontrol.ErrStale) {
		t.Fatal("draining allowed binding", err)
	}
	f.exec("UPDATE go_consumer_control SET mode='worker',revision=3 WHERE id=1")
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0.26 AND cost_usd IS NULL", jobID) != 1 || f.count("SELECT COUNT(*) FROM apify_runs WHERE id='00000000000000000000000000000001' AND reserved_usd=0.25 AND charged_usd IS NULL") != 1 {
		t.Fatal("binding changed paid reserves")
	}
	if err = f.repo.Finish(f.ctx, start.Run.ID, "completed"); err != nil {
		t.Fatal(err)
	}
}
