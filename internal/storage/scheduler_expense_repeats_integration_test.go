//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"encoding/json"
	"testing"
	"time"

	"itmap/internal/parser"
)

func TestMariaDBSchedulerExpenseRepeatsDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=openSettingsSQL(t)
	defer func(){
		f.exec("DELETE FROM scheduler_attempts WHERE id LIKE 'ci-go434-%'")
		f.exec("DELETE FROM scheduler_jobs WHERE id LIKE 'ci-go434-%'")
		f.exec("DELETE FROM apify_runs WHERE id LIKE 'ci-go434-%'")
	}()
	at:=time.Date(2026,11,9,10,0,0,0,time.UTC).UnixMilli()
	// Two local records for the same external Actor are ONE provider run;
	// a paid empty Actor and an unknown start remain separate evidence.
	for _,row:=range []struct{id,op,actor string;cost any;reserve float64}{
		{"ci-go434-actor-a","ci-go434-key-a","ci-actor-a",0.4,0.3},
		{"ci-go434-actor-a-copy","ci-go434-key-a-copy","ci-actor-a",0.4,0.3},
		{"ci-go434-empty","ci-go434-key-b","ci-actor-b",0.1,0.3},
		{"ci-go434-uncertain","ci-go434-key-unknown","",nil,0.26},
	}{
		f.exec(`INSERT INTO apify_runs
		 (id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,
		  reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at)
		 VALUES(?,?,?,'free','ci-go434-account','ci-hash','2026-11-01','2026-12-01',
		  ?,?,'succeeded',?,'ci-dataset',?,?)`,
		  row.id,f.generation,row.op,row.reserve,row.cost,row.actor,at,at)
	}
	for _,job:=range []struct{id,kind string}{
		{"ci-go434-job-classify","classify_title"},
		{"ci-go434-job-extract","extract_vacancy"},
	}{
		f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,source_id,payload,status,priority,run_after,created_at,updated_at)
		 VALUES(?,?,?,?,'hh','{}','completed',1,?,?,?)`,job.id,f.generation,job.id,job.kind,at,at,at)
	}
	for _,row:=range []struct{id,job,kind string;cost any;reserve float64}{
		{"ci-go434-ai-1","ci-go434-job-classify","classify_title",0.02,0.05},
		{"ci-go434-ai-2","ci-go434-job-classify","classify_title",nil,0.08},
		{"ci-go434-ai-3","ci-go434-job-extract","extract_vacancy",0.06,0.08},
	}{
		f.exec(`INSERT INTO scheduler_attempts
		  (id,job_id,run_id,type,source_id,started_at,status,provider,model,reserved_usd,cost_usd)
		  VALUES(?,?,'ci-go434-scheduler-cycle',?,'hh',?,'completed','openrouter','ci-model',?,?)`,
		  row.id,row.job,row.kind,at,row.reserve,row.cost)
	}
	before:=dashboardFingerprint(f)
	repo:=parser.ExpenseRepeatsRepository{DB:f.reader,Now:func()time.Time{
		return time.Date(2026,11,10,10,0,0,0,time.UTC)
	}}
	read:=func(days int)map[string]any{
		t.Helper()
		raw,err:=repo.ReadExpenseRepeats(f.ctx,days)
		if err!=nil{t.Fatal(err)}
		var value map[string]any
		if err=json.Unmarshal(raw,&value);err!=nil{t.Fatal(err)}
		return value
	}
	report:=read(7)
	a:=report["apify"].(map[string]any)
	ai:=report["ai"].(map[string]any)
	if a["actorOperations"]!=float64(2)||a["repeatedReceiptGroups"]!=float64(1)||
		a["extraLocalReceipts"]!=float64(1)||a["suspectedLaunchGroups"]!=float64(0)||
		a["missingActorIdentity"]!=float64(1)||
		a["unknownStartReserveUsd"]!=float64(.26){
		t.Fatalf("wrong bounded Actor evidence: %+v",a)
	}
	if ai["observedAttempts"]!=float64(3)||ai["repeatGroups"]!=float64(1)||
		ai["extraAttempts"]!=float64(1)||report["providerRepeatVerified"]!=false{
		t.Fatalf("wrong AI confidence evidence: %+v",report)
	}
	stages:=ai["stages"].([]any)
	if len(stages)!=1||stages[0].(map[string]any)["type"]!="classify_title"{
		t.Fatalf("wrong classified attempt groups: %+v",stages)
	}
	monthly:=read(30)
	if monthly["periodDays"]!=float64(30)||monthly["from"]!="2026-10-12"{
		t.Fatalf("wrong Moscow period: %+v",monthly)
	}
	if got:=dashboardFingerprint(f);got!=before{t.Fatal("GET diagnostics modified disposable MariaDB")}
}
