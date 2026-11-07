//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"itmap/internal/parser"
)

// Вся фикстура синтетическая. Не выполняется без одноразовой itmap_ci с nonce.
func TestMariaDBSchedulerFunnelDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=dashboardFixture(t)
	for _,table:=range []string{"vacancy_candidates","parser_candidate_intakes","vacancy_import_operations",
		"vacancy_classifications","vacancy_raw_snapshots","vacancy_role_results","company_vacancies"}{
		var n int
		if err:=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*) FROM "+table).Scan(&n);err!=nil || n!=0{
			t.Fatalf("funnel fixture requires empty %s",table)
		}
	}
	t.Cleanup(func(){
		ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
		defer cancel()
		for _,table:=range []string{"vacancy_publication_sources","vacancy_role_results","vacancy_extractions",
			"vacancy_raw_snapshots","vacancy_import_operations","vacancy_classifications",
			"vacancy_classification_attempts","parser_candidate_intakes",
			"vacancy_candidate_identities","vacancy_candidates","company_vacancies",
			"company_crm","company_identity","vacancy_role_classification","vacancy_roles"}{
			if _,e:=f.db.ExecContext(ctx,"DELETE FROM "+table);e!=nil{t.Errorf("cleanup %s failed",table)}
		}
	})
	f.exec("INSERT INTO company_identity(id,company_name) VALUES(991,'CI Funnel Employer')")
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI Funnel Employer')")
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name) VALUES(901,'ci-funnel-root','CI Funnel Root')")
	const prompt="CI FUNNEL PROMPT"
	f.exec("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled) VALUES(901,?,1,1)",prompt)
	hash:=sha256.Sum256([]byte(prompt))
	promptHash:=hex.EncodeToString(hash[:])
	f.exec("INSERT INTO company_vacancies(id,company_name,url,title) VALUES(991,'CI Funnel Employer','https://example.invalid/funnel/991','CI Published 991'),(992,'CI Funnel Employer','https://example.invalid/funnel/992','CI Published 992')")
	const titleHash="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	type specimen struct {id int64; parser,source,received,method string; mainID any}
	specimens:=[]specimen{
		{1,"apify","hh","2026-11-05T10:00:00Z","site",nil},
		{2,"firecrawl","hh","2026-11-05T11:00:00Z","firecrawl",nil},
		{3,"http","companySite","2026-11-06T10:00:00Z","site",nil},
		{4,"unknown","hh","2026-11-06T11:00:00Z","site",nil},
		{5,"apify","hh","2026-10-01T10:00:00Z","site",nil},
		{6,"http","companySite","2026-11-07T10:00:00Z","site",nil},
		{7,"manual","companySite","2026-11-07T11:00:00Z","manual",nil},
		{8,"apify","hh","2026-11-04T10:00:00Z","site",991},
	}
	for _,v:=range specimens{
		token:=fmt.Sprintf("ci-funnel-token-%d",v.id)
		f.exec(`INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,
		  acquisition_method,first_seen_at,last_seen_at,created_at,updated_at,source_id,main_vacancy_id)
		  VALUES(?,?,991,?,?, ?,?,?,?,?,?,?)`,
			v.id,token,fmt.Sprintf("CI Funnel %d",v.id),titleHash,v.method,v.received,v.received,
			v.received,v.received,v.source,v.mainID)
		if v.parser!="unknown"{
			f.exec("INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) VALUES(?,?,?,?)",
				token,v.parser,v.source,v.received)
		}
	}
	classify:=func(id int64,decision,at string,ordinal int){
		attempt:=fmt.Sprintf("ci-funnel-attempt-%d-%d",id,ordinal)
		f.exec(`INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,
		  model_key,requested_roles_json,outcome,classified_at) VALUES(?,?,?, ?,1,'typesafe/jev-1.13','[901]','complete',?)`,
			attempt,id,fmt.Sprintf("CI Funnel %d",id),titleHash,at)
		f.exec(`INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,
		  decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at)
		  VALUES(?,?,901,'CI Funnel Root',?,'typesafe/jev-1.13','CI',?,1,?,1,?)`,
			id,attempt,decision,promptHash,titleHash,at)
	}
	classify(1,"match","2026-11-06T10:00:00Z",1)
	classify(1,"match","2026-11-07T10:00:00Z",2) // повтор не увеличивает accepted
	classify(2,"match","2026-11-06T10:00:00Z",1)
	classify(3,"reject","2026-11-06T10:00:00Z",1)
	classify(5,"match","2026-10-02T10:00:00Z",1)
	snapshot:=func(id int64,at string,ordinal int)string{
		key:=fmt.Sprintf("ci-funnel-snap-%d-%d",id,ordinal)
		f.exec(`INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,
		  content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at)
		  VALUES(?,?,? ,?, ?,?,'free','valid','{}',?)`,
			key,id,"https://example.invalid/ci/full","https://example.invalid/ci/full",
			titleHash,at,at)
		return key
	}
	s1:=snapshot(1,"2026-11-05T10:00:00Z",1)
	_ = snapshot(1,"2026-11-07T10:00:00Z",2) // повторы полного текста не увеличивают стадию
	f.exec(`INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,config_hash,
	  prompt_version,schema_version,processed_at,duration_ms,status)
	  VALUES('ci-funnel-extract',1,?,'ci','hash','version',1,'2026-11-06T10:00:00Z',1,'success')`,s1)
	f.exec(`INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,
	  prompt_version,config_hash,provider,role_ids,status,processed_at,duration_ms)
	  VALUES('ci-funnel-role',1,?,'in','tree','v','c','ci','[901]','success','2026-11-06T10:00:00Z',1)`,s1)
	imported:=func(id int64,action,at,op string,vacancy int64){
		f.exec(`INSERT INTO vacancy_import_operations(id,candidate_id,vacancy_id,action,status,processed_at,changes_json)
		  VALUES(?,?,?,?, 'success',?,'{}')`,op,id,vacancy,action,at)
	}
	imported(1,"create","2026-11-07T10:00:00Z","ci-funnel-import-1",991)
	imported(1,"unchanged","2026-11-07T11:00:00Z","ci-funnel-import-1-retry",991)
	imported(2,"update","2026-11-07T10:00:00Z","ci-funnel-import-2",991)
	imported(5,"create","2026-11-07T10:00:00Z","ci-funnel-import-5",992)
	f.exec(`INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,candidate_id,source_id,
	  status,reason_code,priority,run_after,created_at,updated_at)
	  VALUES('ci-funnel-error',?,'ci-funnel-error','fetch_full_text',991,6,'companySite',
	  'pending','stage_failed',10,0,1,1)`,f.generation)
	repo:=parser.FunnelRepository{DB:f.reader,Now:func()time.Time{return time.Date(2026,11,7,20,30,0,0,time.UTC)}}
	read:=func(source,method string)map[string]any{
		t.Helper()
		data,e:=repo.ReadFunnel(f.ctx,source,method)
		if e!=nil {t.Fatalf("funnel SQL read failed for %s/%s: %v",source,method,e)}
		var result map[string]any
		if json.Unmarshal(data,&result)!=nil {t.Fatal("invalid funnel JSON")}
		return result
	}
	before:=dashboardFingerprint(f)
	all:=read("all","all")
	if all["from"]!="2026-10-09" || all["to"]!="2026-11-07" || all["timezone"]!="Europe/Moscow"{
		t.Fatalf("30 Moscow days missing: %v–%v",all["from"],all["to"])
	}
	if len(all["daily"].([]any))!=30 {t.Fatal("expected 30 rows including zero days")}
	must:=func(obj map[string]any,fields map[string]float64){
		t.Helper()
		for key,want:=range fields{
			if obj[key]!=want {t.Fatalf("%s got %v, want %v",key,obj[key],want)}
		}
	}
	must(all["cohort"].(map[string]any),map[string]float64{
		"received":6,"accepted":2,"fulltext":1,"structured":1,"roles":1,
		"published":3,"rejected":1,"error":1,"waiting":1,
		"created":1,"linked":1,"undatedPublished":1,
	})
	must(all["totals"].(map[string]any),map[string]float64{
		"received":6,"accepted":2,"fulltext":1,"structured":1,"roles":1,"published":3,
	})
	if all["averages"].(map[string]any)["published"]!=float64(.1) {t.Fatal("average should be 3/30")}
	if all["daily"].([]any)[29].(map[string]any)["published"]!=float64(3) {t.Fatal("late publication event lost")}
	must(read("hh","apify")["cohort"].(map[string]any),map[string]float64{"received":2,"published":2})
	must(read("hh","firecrawl")["cohort"].(map[string]any),map[string]float64{"received":1,"published":1})
	must(read("companySite","http")["cohort"].(map[string]any),map[string]float64{"received":2,"rejected":1,"error":1})
	must(read("hh","unknown")["cohort"].(map[string]any),map[string]float64{"received":1,"waiting":1})
	if before!=dashboardFingerprint(f) {t.Fatal("read-only funnel mutated MariaDB")}
	if _,e:=repo.ReadFunnel(f.ctx,"hh","bad");!errors.Is(e,parser.ErrFunnelInput){
		t.Fatal("unsupported parser should be HTTP 400")
	}
	// Смена корневого промпта делает старый reject неактуальным.
	f.exec("UPDATE vacancy_role_classification SET markdown='CI CHANGED PROMPT' WHERE role_id=901")
	must(read("companySite","http")["cohort"].(map[string]any),map[string]float64{"rejected":0,"waiting":1,"error":1})
	t.Log("PASS: 30 MSK dates, exact stage events, cohort, retries, source/publication duplicates, late imports, undated links, current root prompt, source/parser filters, SELECT-only")
}
