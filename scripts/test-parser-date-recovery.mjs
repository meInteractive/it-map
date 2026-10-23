import assert from 'node:assert/strict';import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,sqlite}=f;
try{
 await api.schedulerSettings(db);sqlite.exec("INSERT INTO vacancy_detail_settings(id,markdown,provider,openai_model) VALUES(1,'preserve','openai','gpt-6-luna')");
 const ids={};let n=0;
 for(const [name,status,provider,model,generation,error,valid] of [
 ['old-date','failed','openrouter','old/model:free',null,'Дата публикации должна быть действительной датой YYYY-MM-DD.',true],
 ['luna-date','failed','openai','gpt-6-luna',null,'Дата публикации должна быть действительной датой YYYY-MM-DD.',true],
 ['other-error','failed','openrouter','old/model:free',null,'Другое поле.',true],
 ['cancelled','cancelled','openrouter','old/model:free',null,'Дата публикации YYYY-MM-DD',true],
 ['old-epoch','failed','openrouter','old/model:free','old-epoch','Дата публикации YYYY-MM-DD',true],
 ['no-text','failed','openrouter','old/model:free',null,'Дата публикации YYYY-MM-DD',false]]){
  const id=++n,url='https://career.habr.com/vacancies/'+n;sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(?,?,'Title',?,?,'habr','site','now','now','now','now')").run(id,name,'a'.repeat(64),url);
  if(valid)sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES(?,?,?,?,?,?,?,'free','valid','{}','now')").run(name,id,url,url,'text','b'.repeat(64),'now');
  const j=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:name,candidateId:id,sourceId:'habr'});ids[name]=j.id;
  sqlite.prepare("UPDATE scheduler_jobs SET status=?,reason_code='invalid_output',last_error=?,attempts=4,generation=coalesce(?,generation) WHERE id=?").run(status,error,generation,j.id);
  sqlite.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,provider,model) VALUES(?,?,'fixture','extract_vacancy',1,'failed',?,?)").run(name,j.id,provider,model);
 }
 const sql=fs.readFileSync('drizzle/0071_parser_date_recovery.sql','utf8');sqlite.exec(sql);sqlite.exec(sql);
 for(const [name,id] of Object.entries(ids)){const row=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(id);assert.equal(row.status,name==='old-date'?'pending':name==='cancelled'?'cancelled':'failed',name);assert.equal(row.attempts,name==='old-date'?0:4);}
 assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_attempts').get().n,6);
 console.log('Date recovery: only old free/date/valid-text/Luna/current epoch requeued; current Luna, other validation, cancelled, old epoch, missing text and attempt history protected.');
}finally{f.close();}
