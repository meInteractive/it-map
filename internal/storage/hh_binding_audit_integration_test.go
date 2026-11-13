//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/paidrecovery"
)

func testHHBindingAudit(t *testing.T, f *schedulerFixture) {
	t.Helper()
	if f.count("SELECT COUNT(*) FROM apify_runs WHERE account_id='CIAudit' OR id IN ('00000000000000000000000000000001','00000000000000000000000000000002','00000000000000000000000000000003','00000000000000000000000000000004','00000000000000000000000000000005','00000000000000000000000000000006')") != 0 {
		t.Fatal("audit fixture already exists")
	}
	f.exec(`CREATE TABLE go_hh_attempt_bindings (operation_id VARCHAR(255) NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, operation_key VARCHAR(80) NOT NULL, job_id VARCHAR(255) NOT NULL, attempt_id VARCHAR(255) NOT NULL, UNIQUE KEY hh_binding_identity (generation,operation_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, q := range []string{"DELETE FROM apify_runs WHERE account_id='CIAudit'", "DELETE FROM scheduler_attempts WHERE id IN ('CI_audit_attempt_ok','CI_audit_attempt_no_job','CI_audit_attempt_no_run')", "DELETE FROM scheduler_jobs WHERE id IN ('CI_audit_job_ok','CI_audit_job_no_run')", "DELETE FROM scheduler_runs WHERE id='CI_audit_run'", "DROP TABLE go_hh_attempt_bindings"} {
			if _, err := f.db.ExecContext(ctx, q); err != nil {
				t.Error("audit fixture cleanup failed")
			}
		}
	})
	old := "11111111-2222-4333-8444-555555555555"
	if old == f.repo.Generation {
		old = "11111111-2222-4333-8444-555555555556"
	}
	f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status) VALUES('CI_audit_run',?,'automatic',1,'interrupted')", old)
	for _, job := range []string{"CI_audit_job_ok", "CI_audit_job_no_run"} {
		f.exec("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,priority,run_after,created_at,updated_at) VALUES(?,?,?,'diagnostic',0,0,0,0)", job, old, job)
	}
	for _, a := range [][3]string{{"CI_audit_attempt_ok", "CI_audit_job_ok", "CI_audit_run"}, {"CI_audit_attempt_no_job", "CI_audit_missing_job", "CI_audit_run"}, {"CI_audit_attempt_no_run", "CI_audit_job_no_run", "CI_audit_missing_run"}} {
		f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd) VALUES(?,?,?,'diagnostic',1,'interrupted',0.25)", a[0], a[1], a[2])
	}
	id := func(n int) string { return fmt.Sprintf("%032x", n) }
	key := func(n int) string { return fmt.Sprintf("go398:%064x", n) }
	bind := func(n int, job, attempt string) {
		f.exec("INSERT INTO go_hh_attempt_bindings VALUES(?,?,?,?,?)", id(n), old, key(n), job, attempt)
	}
	op := func(n int, actualKey string) {
		f.exec(`INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES(?,?,?,'free','CIAudit','CIhash','CIstart','CIend',0.25,'uncertain',1,1)`, id(n), old, actualKey)
	}
	bind(1, "CI_audit_job_ok", "CI_audit_attempt_ok")
	op(1, key(1))
	bind(2, "CI_audit_job_ok", "CI_audit_attempt_ok")
	bind(3, "CI_audit_job_ok", "CI_audit_missing_attempt")
	op(3, key(3))
	bind(4, "CI_audit_missing_job", "CI_audit_attempt_no_job")
	op(4, key(4))
	bind(5, "CI_audit_job_no_run", "CI_audit_attempt_no_run")
	op(5, key(5))
	bind(6, "CI_audit_job_ok", "CI_audit_attempt_ok")
	op(6, key(66))
	got, err := paidrecovery.InspectBindings(f.ctx, f.db, f.repo.Generation)
	if err != nil || got.Total != 6 || got.OldGeneration != 6 || got.Consistent != 1 || got.MissingOperation != 1 || got.MissingAttempt != 1 || got.MissingJob != 1 || got.MissingRun != 1 || got.IdentityMismatch != 1 {
		t.Fatal("binding audit categories mismatch", got, err)
	}
	f.exec("UPDATE go_consumer_schema_version SET status='incomplete'")
	if got, err = paidrecovery.InspectBindings(f.ctx, f.db, f.repo.Generation); !errors.Is(err, consumercontrol.ErrState) || got != (paidrecovery.BindingAudit{}) {
		t.Fatal("incomplete marker accepted", err)
	}
	f.exec("UPDATE go_consumer_schema_version SET status='complete'")
	f.exec("UPDATE go_hh_attempt_bindings SET operation_key='bad' WHERE operation_id=?", id(1))
	if got, err = paidrecovery.InspectBindings(f.ctx, f.db, f.repo.Generation); !errors.Is(err, paidrecovery.ErrBindingAuditIncompatible) || got != (paidrecovery.BindingAudit{}) {
		t.Fatal("malformed binding accepted", err)
	}
	f.exec("UPDATE go_hh_attempt_bindings SET operation_key=? WHERE operation_id=?", key(1), id(1))
	holder, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var generation string
	if err = holder.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		holder.Rollback()
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	_, err = paidrecovery.InspectBindings(blocked, f.db, f.repo.Generation)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		holder.Rollback()
		t.Fatal("audit bypassed generation lock", err)
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	var q strings.Builder
	q.WriteString("INSERT INTO go_hh_attempt_bindings(operation_id,generation,operation_key,job_id,attempt_id) VALUES ")
	args := make([]any, 0, 257*3)
	for i := 0; i < 257; i++ {
		if i > 0 {
			q.WriteByte(',')
		}
		q.WriteString("(?,?,?,'CIjob','CIattempt')")
		args = append(args, id(i+100), old, key(i+100))
	}
	f.exec(q.String(), args...)
	if got, err = paidrecovery.InspectBindings(f.ctx, f.db, f.repo.Generation); !errors.Is(err, paidrecovery.ErrBindingAuditLimit) || got != (paidrecovery.BindingAudit{}) {
		t.Fatal("binding limit did not fail closed", err)
	}
}
