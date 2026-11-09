//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"itmap/internal/parser"
)

// GO-433 is evaluated exclusively against the nonce-guarded disposable CI DB.
// No provider network, no paid API, no parser scheduler and no DDL.
func TestMariaDBSchedulerUnitCostDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=dashboardFixture(t)
	t.Cleanup(func(){
		ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
		defer cancel()
		for _,query:=range []string{
			"DELETE FROM vacancy_raw_snapshots WHERE id LIKE 'ci-go433-%'",
			"DELETE FROM vacancy_import_operations WHERE id LIKE 'ci-go433-%'",
			"DELETE FROM parser_candidate_intakes WHERE candidate_token LIKE 'ci-go433-%'",
			"DELETE FROM vacancy_candidate_identities WHERE candidate_id BETWEEN 43300 AND 43303",
			"DELETE FROM vacancy_candidates WHERE creation_token LIKE 'ci-go433-%'",
			"DELETE FROM company_vacancies WHERE id IN (43300,43301,43302)",
			"DELETE FROM company_crm WHERE company_name='CI GO433 Employer'",
			"DELETE FROM company_identity WHERE id=43300",
			"DELETE FROM apify_runs WHERE id LIKE 'ci-go433-%'",
		}{
			if _,err:=f.db.ExecContext(ctx,query);err!=nil{t.Error("GO-433 disposable fixture cleanup failed",err)}
		}
	})
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(43300,'CI GO433 Employer')")
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI GO433 Employer')")
	f.exec(`INSERT INTO company_vacancies(id,company_name,url,title,created_at) VALUES
	  (43300,'CI GO433 Employer','https://example.invalid/ci-go433/1','CI New HH','2026-11-08T21:00:00Z'),
	  (43301,'CI GO433 Employer','https://example.invalid/ci-go433/2','CI New Site','2026-11-09T21:00:00Z'),
	  (43302,'CI GO433 Employer','https://example.invalid/ci-go433/3','CI Old Site','2026-10-19T12:00:00Z')`)
	for idx:=0;idx<4;idx++{
		id:=int64(43300+idx)
		source,method:="hh","apify"
		if idx==1||idx==2{source,method="companySite","http"}
		token:=fmt.Sprintf("ci-go433-%d",idx)
		f.exec(`INSERT INTO vacancy_candidates
		 (id,creation_token,company_id,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at,source_id,main_vacancy_id)
		 VALUES(?,?,43300,?,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
		  'site','2026-11-08T09:00:00Z','2026-11-09T09:00:00Z',
		  '2026-11-08T09:00:00Z','2026-11-09T09:00:00Z',?,?)`,
		  id,token,fmt.Sprintf("CI GO433 Vacancy %d",idx),source,
		  []int64{43300,43301,43300,43302}[idx])
		f.exec("INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) VALUES(?,?,?,'2026-11-08T09:00:00Z')",token,method,source)
	}
	// Two source candidates created the same main vacancy; only its FIRST
	// successful create counts. Retries, updates and failed creates do not.
	for _,row:=range []struct{
		id string; candidate, vacancy int64; action,status,at string
	}{
		{"ci-go433-create-hh",43300,43300,"create","success","2026-11-08T21:00:00Z"},
		{"ci-go433-create-site-retry",43302,43300,"create","success","2026-11-09T13:00:00Z"},
		{"ci-go433-create-site",43301,43301,"create","success","2026-11-09T21:00:00Z"},
		{"ci-go433-duplicate-update",43303,43301,"update","success","2026-11-09T13:00:00Z"},
		{"ci-go433-failed",43303,43302,"create","failed","2026-11-09T13:00:00Z"},
	}{
		f.exec("INSERT INTO vacancy_import_operations(id,candidate_id,vacancy_id,action,status,processed_at,changes_json) VALUES(?,?,?,?,?,?,'{}')",
			row.id,row.candidate,row.vacancy,row.action,row.status,row.at)
	}
	f.exec(`INSERT INTO vacancy_raw_snapshots
	 (id,candidate_id,source_url,final_url,content_hash,fetched_at,firecrawl_mode,firecrawl_credits,content_status,diagnostics,created_at)
	 VALUES('ci-go433-snap-1',43300,'https://example.invalid/ci-go433/1','https://example.invalid/ci-go433/1','hash',
	  '2026-11-09T10:00:00Z','free',2.5,'valid','{}','2026-11-09T10:00:00Z'),
	  ('ci-go433-snap-2',43301,'https://example.invalid/ci-go433/2','https://example.invalid/ci-go433/2','hash2',
	  '2026-11-09T10:00:00Z','free',NULL,'valid','{}','2026-11-09T10:00:00Z')`)
	// Same Actor accounted twice by local receipts, plus paid empty scan
	// and unresolved start. Duplicates must not multiply by candidate count.
	at:=time.Date(2026,11,9,10,0,0,0,time.UTC).UnixMilli()
	for _,row:=range []struct{id,actor string; charge any; reserve float64}{
		{"ci-go433-actor-1","ci-actor-1",0.4,0.26},
		{"ci-go433-actor-1-copy","ci-actor-1",0.4,0.26},
		{"ci-go433-empty","ci-empty",0.1,0.26},
		{"ci-go433-unknown","",nil,0.26},
	}{
		f.exec(`INSERT INTO apify_runs
		 (id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at)
		 VALUES(?,?,?,'free','ci-account','ci-hash','2026-11-01','2026-12-01',?,?, 'succeeded',?,'ci-dataset',?,?)`,
		 row.id,f.generation,row.id,row.reserve,row.charge,row.actor,at,at)
	}
	for i,row:=range []struct{source,provider string; cost any; reserve float64}{
		{"hh","openrouter",0.2,0.3},
		{"hh","openai",nil,0.5},
		{"companySite","openrouter",0.1,0.2},
		{"companySite","openai",0.0,0},
	}{
		job:=fmt.Sprintf("ci-go433-job-%d",i)
		f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,source_id,payload,status,priority,run_after,created_at,updated_at)
		 VALUES(?,?,?,'classify_title',?,'{}','completed',1,?,?,?)`,job,f.generation,job,row.source,at,at,at)
		f.exec(`INSERT INTO scheduler_attempts(id,job_id,run_id,type,source_id,started_at,status,provider,model,reserved_usd,cost_usd)
		 VALUES(?,?,'ci-go433-scheduler-cycle','classify_title',? ,?,'completed',?,'ci-model',?,?)`,
			fmt.Sprintf("ci-go433-attempt-%d",i),job,row.source,at,row.provider,row.reserve,row.cost)
	}
	// Attribution coverage: two AI attempts have an actual first CREATE,
	// one points to an existing main vacancy without matching create and one
	// is a source scan without candidate. None of these links is a provider
	// invoice or proof that the model created the main vacancy.
	f.exec("UPDATE scheduler_jobs SET candidate_id=43300 WHERE id='ci-go433-job-0'")
	f.exec("UPDATE scheduler_jobs SET candidate_id=43301 WHERE id='ci-go433-job-2'")
	f.exec("UPDATE scheduler_jobs SET candidate_id=43303,source_id='hh' WHERE id='ci-go433-job-3'")
	// Missing job and dangling candidate must be counted without JOIN
	// multiplication, and do not create new paid charges (known zero).
	f.exec(`INSERT INTO scheduler_attempts
	 (id,job_id,run_id,type,source_id,started_at,status,provider,model,reserved_usd,cost_usd)
	 VALUES('ci-go433-orphan-attempt','ci-go433-orphan-job','ci-cycle',
	 'classify_title','hh',?,'failed','openrouter','ci-model',0,0)`,at)
	f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,candidate_id,source_id,payload,status,priority,run_after,created_at,updated_at)
	 VALUES('ci-go433-dangling-job',?,'ci-go433-dangling-job','classify_title',999999,
	 'hh','{}','completed',1,?,?,?)`,f.generation,at,at,at)
	f.exec(`INSERT INTO scheduler_attempts
	 (id,job_id,run_id,type,source_id,started_at,status,provider,model,reserved_usd,cost_usd)
	 VALUES('ci-go433-dangling-attempt','ci-go433-dangling-job','ci-cycle',
	 'classify_title','hh',?,'failed','openrouter','ci-model',0,0)`,at)
	now:=time.Date(2026,11,10,10,0,0,0,time.UTC)
	repo:=parser.UnitCostRepository{DB:f.reader,Now:func()time.Time{return now}}
	read:=func(days int,source string)map[string]any{
		t.Helper()
		raw,err:=repo.ReadUnitCost(f.ctx,days,source)
		if err!=nil{t.Fatal("read-only GO-433 query failed",err)}
		var body map[string]any
		if err=json.Unmarshal(raw,&body);err!=nil{t.Fatal(err)}
		return body
	}
	must:=func(v map[string]any,key string,want float64){
		t.Helper()
		got,ok:=v[key].(float64)
		if !ok||math.Abs(got-want)>1e-9{t.Fatalf("wrong %s: %v, want %v",key,v[key],want)}
	}
	before:=dashboardFingerprint(f)
	all:=read(7,"all")
	must(all,"newUniqueVacancies",2)
	must(all,"totalConfirmedUsd",0.8)
	must(all,"partialPeriodRatioUsd",0.4)
	must(all["apify"].(map[string]any),"confirmedUsd",0.5)
	must(all["apify"].(map[string]any),"confirmedOperations",2)
	must(all["apify"].(map[string]any),"unknownReservedUsd",0.26)
	must(all["ai"].(map[string]any),"confirmedUsd",0.3)
	must(all["ai"].(map[string]any),"unknownReservedUsd",0.5)
	must(all,"firecrawlCredits",2.5)
	must(all,"unknownCreditRows",1)
	lineage:=all["aiLineage"].(map[string]any)
	for k,v:=range map[string]float64{
		"attempts":6,"withJob":5,"withCandidate":3,"withMainVacancy":3,
		"withMatchingCreate":2,"missingJob":1,"missingCandidate":1,
		"missingMainVacancy":0,"sourceMismatch":1,
		"knownUsdWithMainLink":0.3,"knownUsdWithoutMainLink":0,
	}{must(lineage,k,v)}
	if lineage["verifiedProviderAttribution"]!=false{t.Fatal("false provider attribution claim")}
	if len(all["daily"].([]any))!=7{t.Fatal("missing seven Moscow daily points")}
	if all["calendarProxy"]!=true||all["incompleteCosts"]!=true{t.Fatal("false complete-cohort claim")}
	if all["daily"].([]any)[5].(map[string]any)["newVacancies"]!=float64(1) ||
		all["daily"].([]any)[6].(map[string]any)["newVacancies"]!=float64(1) {
		t.Fatal("Moscow midnight created an incorrect daily denominator")
	}
	hh:=read(7,"hh")
	must(hh,"newUniqueVacancies",1)
	must(hh,"totalConfirmedUsd",0.2)
	must(hh,"partialPeriodRatioUsd",0.2)
	must(hh["aiLineage"].(map[string]any),"withMainVacancy",1)
	must(hh["aiLineage"].(map[string]any),"missingJob",1)
	must(hh["aiLineage"].(map[string]any),"missingCandidate",1)
	must(hh["aiLineage"].(map[string]any),"knownUsdWithMainLink",0.2)
	if hh["apify"].(map[string]any)["unavailableForSource"]!=true{t.Fatal("Apify incorrectly attributed to HH")}
	site:=read(7,"companySite")
	must(site,"newUniqueVacancies",1)
	must(site,"totalConfirmedUsd",0.1)
	must(site,"partialPeriodRatioUsd",0.1)
	must(site["aiLineage"].(map[string]any),"withMainVacancy",2)
	must(site["aiLineage"].(map[string]any),"withMatchingCreate",1)
	must(site["aiLineage"].(map[string]any),"sourceMismatch",1)
	zero:=parser.UnitCostRepository{DB:f.reader,Now:func()time.Time{return time.Date(2026,12,15,12,0,0,0,time.UTC)}}
	raw,err:=zero.ReadUnitCost(f.ctx,7,"all")
	if err!=nil{t.Fatal(err)}
	var no map[string]any
	if json.Unmarshal(raw,&no)!=nil || no["partialPeriodRatioUsd"]!=nil || no["newUniqueVacancies"]!=float64(0){
		t.Fatalf("zero-publication period returned false zero-cost KPI: %v",no)
	}
	if before!=dashboardFingerprint(f){t.Fatal("GO-433 SELECT mutated disposable MariaDB")}
	t.Log("PASS: first create, multi-source dedupe, paid empty Actor, unknown reserve, credit-only Firecrawl, Moscow boundary, source isolation, zero denominator, SELECT-only")
}
