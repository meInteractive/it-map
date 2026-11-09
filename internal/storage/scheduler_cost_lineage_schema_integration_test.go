//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"itmap/internal/dbschema"
)

// GO-433 DDL is deliberately a PROPOSAL, not a runtime migration.
// The ONLY executor is this nonce-guarded disposable CI integration test.
// It checks the trusted baseline before DDL and restores it afterwards.
// Never run this against staging, production, private backups or live jobs.
func TestMariaDBSchedulerCostLineageSchemaDisposable(t *testing.T) {
	ctx, db := openDisposableMariaDB(t)
	nowCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	pristine, err := dbschema.Inspect(nowCtx, db)
	if err != nil || dbschema.CheckFull(pristine) != nil {
		t.Fatal("refusing cost lineage DDL on an incomplete or unknown baseline")
	}
	names := []string{
		"cost_scan_cohorts",
		"cost_provider_operations",
		"cost_operation_settlements",
		"cost_operation_cohort_links",
		"cost_discovery_events",
	}
	var preExisting int
	if err := db.QueryRowContext(nowCtx, `SELECT COUNT(*)
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN
		('cost_scan_cohorts','cost_provider_operations','cost_operation_settlements','cost_operation_cohort_links','cost_discovery_events')`).Scan(&preExisting); err != nil || preExisting != 0 {
		t.Fatal("refusing schema draft: cost lineage tables already exist")
	}

	created := []string{}
	t.Cleanup(func() {
		// CI DDL auto-commits. Do not pretend rollback can remove tables.
		// This is safe only because openDisposableMariaDB verified loopback,
		// CI owner username, exact database and run-specific generation.
		cleanup, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		for i := len(created)-1; i>=0; i-- {
			if _, err := db.ExecContext(cleanup, "DROP TABLE `"+created[i]+"`"); err != nil {
				t.Error("disposable GO-433 schema cleanup did not complete")
			}
		}
		clean, err := dbschema.Inspect(cleanup, db)
		if err!=nil || dbschema.CheckFull(clean)!=nil {
			t.Error("trusted schema baseline not restored after GO-433 DDL test")
		}
	})
	raw, err := os.ReadFile("../../db/migrations/proposals/go433_cost_lineage_v1.sql")
	if err != nil { t.Fatal(err) }
	parts:=strings.Split(string(raw),";")
	nameSyntax:=regexp.MustCompile("^CREATE TABLE `([a-z0-9_]+)` \\(")
	statements:=[]string{}
	for _, part:=range parts {
		lines:=[]string{}
		for _,line:=range strings.Split(part,"\n") {
			if strings.HasPrefix(strings.TrimSpace(line),"--"){continue}
			lines=append(lines,line)
		}
		q:=strings.TrimSpace(strings.Join(lines,"\n"))
		if q==""{continue}
		statements=append(statements,q)
	}
	if len(statements)!=len(names) {t.Fatal("unreviewed number of SQL draft statements")}
	for i,stmt:=range statements {
		match:=nameSyntax.FindStringSubmatch(stmt)
		if len(match)!=2 || match[1]!=names[i] ||
			!strings.HasSuffix(stmt,"ROW_FORMAT=DYNAMIC") {
			t.Fatal("GO-433 schema draft statement order or scope changed")
		}
		// No IF NOT EXISTS: a conflicting table MUST fail instead of
		// being silently treated as the trusted extension.
		if _,err=db.ExecContext(nowCtx,stmt);err!=nil {
			t.Fatalf("GO-433 disposable CREATE TABLE %d failed: %v",i+1,err)
		}
		created=append(created,names[i])
	}
	extended,err:=dbschema.Inspect(nowCtx,db)
	if err!=nil{t.Fatal("cannot inspect disposable draft")}
	if dbschema.CheckFull(extended)==nil {
		t.Fatal("unreviewed extension unexpectedly accepted by trusted baseline; runtime activation requires separate policy change")
	}
	var count, foreign int
	if err=db.QueryRowContext(nowCtx,`SELECT COUNT(*) FROM information_schema.TABLES
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME LIKE 'cost\\_%' ESCAPE '\\'`).Scan(&count);err!=nil || count!=5{
		t.Fatal("unexpected draft table count",count,err)
	}
	if err=db.QueryRowContext(nowCtx,`SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
		WHERE CONSTRAINT_SCHEMA=DATABASE() AND TABLE_NAME LIKE 'cost\\_%' ESCAPE '\\'
		AND CONSTRAINT_TYPE='FOREIGN KEY'`).Scan(&foreign);err!=nil || foreign!=4{
		t.Fatal("draft foreign-key graph missing",foreign,err)
	}
	var generation string
	if err=db.QueryRowContext(nowCtx,"SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation);err!=nil{t.Fatal(err)}
	const at int64=1794193200000
	must:=func(q string,args ...any){
		t.Helper()
		if _,err:=db.ExecContext(nowCtx,q,args...);err!=nil{t.Fatalf("disposable lineage write failed: %v",err)}
	}
	reject:=func(q string,args ...any){
		t.Helper()
		if _,err:=db.ExecContext(nowCtx,q,args...);err==nil{t.Fatal("database accepted duplicate, forged or incompatible lineage")}
	}
	must(`INSERT INTO cost_scan_cohorts
		(cohort_id,generation,scan_job_id,source_id,source_revision,origin,started_at_ms)
		VALUES('ci-cohort-a',?,'ci-job-1','hh',7,'new_scan',?),
		      ('ci-cohort-b',?,'ci-job-2','hh',8,'reuse_existing_actor',?)`,generation,at,generation,at)
	reject(`INSERT INTO cost_scan_cohorts
		(cohort_id,generation,scan_job_id,source_id,source_revision,origin,started_at_ms)
		VALUES('ci-forged-same-job',?,'ci-job-1','hh',7,'new_scan',?)`,generation,at)
	must(`INSERT INTO cost_provider_operations
		(operation_id,provider_kind,source_ledger,source_receipt_id,billing_scope,provider_operation_id,created_at_ms)
		VALUES('ci-actor','apify_actor','apify_runs','ci-local-1','ci-account','ci-actor-run',?),
		      ('ci-unknown','apify_actor','apify_runs','ci-local-unknown','ci-account',NULL,?),
		      ('ci-account-two','apify_actor','apify_runs','ci-local-2','ci-account-2','ci-actor-run',?),
		      ('ci-ai-local','ai_attempt','scheduler_attempts','ci-attempt',NULL,NULL,?)`,at,at,at,at)
	reject(`INSERT INTO cost_provider_operations
		(operation_id,provider_kind,source_ledger,source_receipt_id,billing_scope,provider_operation_id,created_at_ms)
		VALUES('ci-duplicate-provider','apify_actor','apify_runs','ci-local-3','ci-account','ci-actor-run',?)`,at)
	reject(`INSERT INTO cost_provider_operations
		(operation_id,provider_kind,source_ledger,source_receipt_id,billing_scope,provider_operation_id,created_at_ms)
		VALUES('ci-duplicate-local','apify_actor','apify_runs','ci-local-1','ci-account','ci-other-run',?)`,at)
	reject(`INSERT INTO cost_provider_operations
		(operation_id,provider_kind,source_ledger,source_receipt_id,billing_scope,provider_operation_id,created_at_ms)
		VALUES('ci-no-account','apify_actor','apify_runs','ci-other-local',NULL,'ci-provider-id',?)`,at)
	must(`INSERT INTO cost_operation_settlements
		(settlement_id,operation_id,settlement_revision,status,source,cost_usd,reserved_usd,provider_verified,observed_at_ms)
		VALUES('ci-reserve','ci-unknown',1,'unknown','local',NULL,0.26000000,0,?),
		      ('ci-local-cost','ci-actor',1,'settled','local',0.40000000,0.40000000,0,?),
		      ('ci-provider-cost','ci-actor',2,'settled','provider',0.35000000,NULL,1,?),
		      ('ci-zero','ci-ai-local',1,'settled','local',0.00000000,0.00000000,0,?)`,at,at,at,at)
	reject(`INSERT INTO cost_operation_settlements
		(settlement_id,operation_id,settlement_revision,status,source,cost_usd,provider_verified,observed_at_ms)
		VALUES('ci-repeat-revision','ci-actor',2,'settled','provider',0.35000000,1,?)`,at)
	reject(`INSERT INTO cost_operation_settlements
		(settlement_id,operation_id,settlement_revision,status,source,cost_usd,provider_verified,observed_at_ms)
		VALUES('ci-fake-provider','ci-unknown',2,'settled','local',0.35000000,1,?)`,at)
	reject(`INSERT INTO cost_operation_settlements
		(settlement_id,operation_id,settlement_revision,status,source,cost_usd,provider_verified,observed_at_ms)
		VALUES('ci-negative','ci-unknown',2,'settled','local',-0.10000000,0,?)`,at)
	must(`INSERT INTO cost_operation_cohort_links
		(operation_id,cohort_id,relation,captured,captured_at_ms)
		VALUES('ci-actor','ci-cohort-a','initiated_by',1,?),
		      ('ci-actor','ci-cohort-a','observed_from',1,?),
		      ('ci-actor','ci-cohort-b','reused_in',1,?)`,at,at,at)
	reject(`INSERT INTO cost_operation_cohort_links
		(operation_id,cohort_id,relation,captured,captured_at_ms)
		VALUES('ci-actor','ci-cohort-a','observed_from',1,?)`,at)
	reject(`INSERT INTO cost_operation_cohort_links
		(operation_id,cohort_id,relation,captured,captured_at_ms)
		VALUES('ci-actor','ci-missing','observed_from',1,?)`,at)
	must(`INSERT INTO cost_discovery_events
		(event_id,cohort_id,operation_id,relation,dataset_item_key,candidate_id,result,captured_at_ms)
		VALUES('ci-item-1','ci-cohort-a','ci-actor','observed_from','ci-dataset-item-hash-1',43300,'created',?),
		      ('ci-item-rejected','ci-cohort-a','ci-actor','observed_from','ci-dataset-item-hash-2',NULL,'rejected',?)`,at,at)
	reject(`INSERT INTO cost_discovery_events
		(event_id,cohort_id,operation_id,relation,dataset_item_key,candidate_id,result,captured_at_ms)
		VALUES('ci-item-repeated','ci-cohort-a','ci-actor','observed_from','ci-dataset-item-hash-1',43301,'existing',?)`,at)
	reject(`INSERT INTO cost_discovery_events
		(event_id,cohort_id,operation_id,relation,dataset_item_key,candidate_id,result,captured_at_ms)
		VALUES('ci-no-link','ci-cohort-b','ci-actor','observed_from','ci-dataset-item-hash-3',43301,'created',?)`,at)
	// A failed candidate/page checkpoint rolls back its discovery event. No
	// event is treated as paid merely because it was observed.
	tx,err:=db.BeginTx(nowCtx,nil)
	if err!=nil{t.Fatal(err)}
	if _,err=tx.ExecContext(nowCtx,`INSERT INTO cost_discovery_events
		(event_id,cohort_id,operation_id,relation,dataset_item_key,candidate_id,result,captured_at_ms)
		VALUES('ci-canceled-page','ci-cohort-a','ci-actor','observed_from','ci-item-rollback',43302,'created',?)`,at);err!=nil{tx.Rollback();t.Fatal(err)}
	if err=tx.Rollback();err!=nil{t.Fatal(err)}
	var eventCount int
	if err=db.QueryRowContext(nowCtx,"SELECT COUNT(*) FROM cost_discovery_events WHERE event_id='ci-canceled-page'").Scan(&eventCount);err!=nil||eventCount!=0{
		t.Fatal("rolled-back page left a cost discovery event")
	}
	// A provider-confirmed 0 USD is distinct from an unknown charge; the
	// settlement history retains both reserve and later confirmation.
	var nullCount int
	if err=db.QueryRowContext(nowCtx,"SELECT COUNT(*) FROM cost_operation_settlements WHERE cost_usd IS NULL").Scan(&nullCount);err!=nil||nullCount!=1{
		t.Fatal("NULL reserve was rewritten as a confirmed zero")
	}
	var confirmedUSD string
	if err=db.QueryRowContext(nowCtx,`SELECT CAST(cost_usd AS CHAR) FROM cost_operation_settlements
		WHERE settlement_id='ci-provider-cost'`).Scan(&confirmedUSD);err!=nil||confirmedUSD!="0.35000000"{
		t.Fatal("confirmed decimal amount lost precision",err)
	}
	t.Log("GO-433: five DDL tables, provider/local unique identities, unknown ACK, provider confirmation, FKs, shared Actor, discovery replay, transaction rollback and cleanup checks passed")
}
