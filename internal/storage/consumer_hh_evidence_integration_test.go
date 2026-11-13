//go:build mariadb && schedulerwrites

package storage

import (
	"errors"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/consumercontrol"
	"itmap/internal/integrations/apify"
	"itmap/internal/parser"
)

func testBoundHHEvidence(t *testing.T, f *schedulerFixture, req consumercontrol.Request) {
	t.Helper()
	start, bound, err := f.repo.BeginBoundRun(f.ctx, parser.RunOptions{RequestID: "CI hh evidence run", Diagnostic: true, Duration: 20 * time.Second}, req)
	if err != nil || bound == nil {
		t.Fatal("bound run unavailable", err)
	}
	defer f.exec("DELETE FROM scheduler_runs WHERE id=?", start.Run.ID)
	jobID := "CI_hh_evidence_job"
	schedulerSeedJob(f, jobID, nil)
	defer f.exec("DELETE FROM scheduler_jobs WHERE id=?", jobID)
	defer f.exec("DELETE FROM scheduler_attempts WHERE job_id=?", jobID)
	defer f.exec("DELETE FROM apify_runs WHERE id='CI_bound_hh_evidence'")
	lease, err := bound.ClaimJob(f.ctx, parser.ClaimOptions{JobID: &jobID, Diagnostic: true, Lease: 10 * time.Second})
	if err != nil {
		t.Fatal("claim unavailable", err)
	}
	attempt, err := bound.StartBoundAttempt(f.ctx, lease)
	if err != nil {
		t.Fatal("attempt unavailable", err)
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
	defer f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true}' WHERE id=1`)
	if err = attempt.Reserve(f.ctx, []byte(`{"provider":"apify","model":"CI_actor","capUsd":0.26}`)); err != nil {
		t.Fatal("reserve failed", err)
	}
	v := apifysettings.OperationVersion{Generation: f.repo.Generation, CompanyID: 991302, SourceRevision: 1, SourceURL: "https://hh.ru/employer/456", Profile: "free"}
	step := "CI page 1"
	options := apify.HHOptions{}
	if _, err = attempt.InspectHHRecovery(f.ctx, v, step, options); !errors.Is(err, parser.ErrHHEvidenceAbsent) {
		t.Fatal("missing evidence accepted", err)
	}
	jobKey, err := apifyruns.HHJobKey(f.repo.Generation, jobID, step)
	if err != nil {
		t.Fatal(err)
	}
	opKey, err := apifyruns.HHOperationIdentity(v, jobKey, options)
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES('CI_bound_hh_evidence',?,?,'free','CIAccount','CIhash','CIstart','CIend',0.26,'uncertain',1,1)`, f.repo.Generation, opKey)
	if _, err = attempt.InspectHHRecovery(f.ctx, v, "CI other step", options); !errors.Is(err, parser.ErrHHEvidenceAbsent) {
		t.Fatal("other step matched", err)
	}
	state, err := attempt.InspectHHRecovery(f.ctx, v, step, options)
	if err != nil || state != apifyruns.RecoveryUnknown {
		t.Fatal("unknown evidence mismatch", state, err)
	}
	f.exec("UPDATE apify_runs SET state='running',run_id='CIRun',dataset_id='CIData' WHERE id='CI_bound_hh_evidence'")
	state, err = attempt.InspectHHRecovery(f.ctx, v, step, options)
	if err != nil || state != apifyruns.RecoveryReconcile {
		t.Fatal("reconcile evidence mismatch", state, err)
	}
	f.exec("UPDATE apify_runs SET state='succeeded',charged_usd=0.08 WHERE id='CI_bound_hh_evidence'")
	state, err = attempt.InspectHHRecovery(f.ctx, v, step, options)
	if err != nil || state != apifyruns.RecoveryTerminalCostRecorded {
		t.Fatal("terminal evidence mismatch", state, err)
	}
	f.exec("UPDATE apify_runs SET state='running' WHERE id='CI_bound_hh_evidence'")
	if _, err = attempt.InspectHHRecovery(f.ctx, v, step, options); !errors.Is(err, parser.ErrMoneyUncertain) {
		t.Fatal("invalid paid state accepted", err)
	}
	var until int64
	if err = f.db.QueryRowContext(f.ctx, "SELECT lease_until FROM go_consumer_control WHERE id=1").Scan(&until); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE go_consumer_control SET lease_until=1 WHERE id=1")
	if _, err = attempt.InspectHHRecovery(f.ctx, v, step, options); !errors.Is(err, consumercontrol.ErrDenied) {
		t.Fatal("expired owner read paid evidence", err)
	}
	f.exec("UPDATE go_consumer_control SET lease_until=? WHERE id=1", until)
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE job_id=? AND reserved_usd=0.26 AND cost_usd IS NULL", jobID) != 1 {
		t.Fatal("observation changed reservation")
	}
	if err = f.repo.Finish(f.ctx, start.Run.ID, "completed"); err != nil {
		t.Fatal(err)
	}
}
