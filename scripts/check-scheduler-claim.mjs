// Actual unchanged scheduler functions, synthetic SQLite :memory: only.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {claimScheduledJob,finishScheduledJob} from '../server/repositories/scheduler.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
globalThis.fetch=()=>{throw new Error('Scheduler oracle forbids network');};
const generation='11111111-2222-4333-8444-555555555555',now=1800000000000;
const names={ID:'id',Generation:'generation',DedupeKey:'dedupe_key',Type:'type',Status:'status',CompanyID:'company_id',AgencyID:'agency_id',CandidateID:'candidate_id',SourceID:'source_id',Payload:'payload',Priority:'priority',RunAfter:'run_after',Attempts:'attempts',MaxAttempts:'max_attempts',LockedUntil:'locked_until',CreatedAt:'created_at',UpdatedAt:'updated_at',LastError:'last_error',ReasonCode:'reason_code',CompletedAt:'completed_at'};
const job=(id,extra={})=>({ID:id,Generation:generation,DedupeKey:id,Type:'diagnostic',Status:'pending',CompanyID:null,AgencyID:null,CandidateID:null,SourceID:null,Payload:{},Priority:0,RunAfter:now-600000,Attempts:0,MaxAttempts:4,LockedUntil:0,CreatedAt:now-7200000,UpdatedAt:now-7200000,LastError:'',ReasonCode:'',CompletedAt:null,...extra});
async function reference(v){
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
 sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 sqlite.function('julianday',()=>v.now/86400000+2440587.5);
 const previousNow=Date.now;Date.now=()=>v.now;
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)};}};},async batch(statements){sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 try{
  sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(v.generation);
  sqlite.prepare('INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,?)').run(v.settingsRaw??JSON.stringify(v.settings));
  sqlite.prepare("INSERT INTO scheduler_runs(id,generation,trigger,started_at,status,lease_until) VALUES('CI_run',?,'manual',?,'running',?)").run(v.generation,v.now-1,v.now+3600000);
  for(const j of v.jobs){const row={...j,Payload:j.PayloadRaw??JSON.stringify(j.Payload)};const keys=Object.keys(names);sqlite.prepare(`INSERT INTO scheduler_jobs(${keys.map(k=>names[k]).join(',')},locked_by) VALUES(${keys.map(()=>'?').join(',')},?)`).run(...keys.map(k=>row[k]??null),j.Status==='running'?'CI_previous':'');}
  for(const [i,key] of (v.operations??[]).entries())sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES(?,?,?,'free','CI_account','CI_hash','2026-01-01','2027-01-01',0.01,'failed',0,0)").run('CI_op_'+i,v.generation,key);
  for(const [id,page] of Object.entries(v.pages??{}))sqlite.prepare("INSERT INTO vacancy_discovery_runs(id,generation,user_name,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,?,'CI_user','hh','https://example.invalid',0,0,?,0,?)").run(id,v.generation,JSON.stringify({paused:{apify:{page}}}),v.now+3600000);
  const bound=generationBoundDb(db,v.generation,{sql:"EXISTS(SELECT 1 FROM scheduler_runs WHERE id='CI_run' AND status='running' AND lease_until>?)",bindings:[v.now],failure:()=>new Error('CI run expired')});
  let claim,error='';try{claim=await claimScheduledJob(bound,'CI_run',{now:v.now,...v.options});}catch(e){if(e.code!=='input')throw e;error='input';}
  let payload=null;
  if(claim&&v.finish){Date.now=()=>v.finishNow??v.now;await finishScheduledJob(claim.db,claim,{...v.finish});payload=sqlite.prepare('SELECT payload FROM scheduler_jobs WHERE id=?').get(claim.job.id).payload;}
  const rows=sqlite.prepare('SELECT * FROM scheduler_jobs ORDER BY id').all().map(r=>({...r,locked_by:claim&&r.id===claim.job.id&&r.locked_by?'<claimed>':r.locked_by}));
  return {...v,selected:claim?.job.id??'',error,rows,finishedPayload:payload};
 }finally{Date.now=previousNow;sqlite.close();}
}
if(process.argv.includes('--reference')){const v=JSON.parse(readFileSync(0,'utf8'));process.stdout.write(JSON.stringify(Array.isArray(v)?await (async()=>{const out=[];for(const x of v)out.push(await reference(x));return out;})():await reference(v)));}
else{
 const cases=[],add=(name,jobs,options={},extra={})=>cases.push({name,generation,now,settings:{enabled:true},jobs,options,...extra});
 add('priority',[job('a'),job('b',{Priority:1})]);
 add('aging',[job('a',{Priority:2}),job('b',{CreatedAt:now-5*3600000})]);
 add('due',[job('a',{Priority:100,RunAfter:now+1}),job('b')]);
 add('other generation',[job('a',{Priority:100,Generation:'22222222-2222-4333-8444-555555555555'}),job('b')]);
 add('run after tie',[job('a'),job('b',{RunAfter:now-600001})]);
 add('created tie',[job('a'),job('b',{CreatedAt:now-7200001})]);
 add('binary id tie',[job('я'),job('a'),job('А'),job('A')]);
 add('candidate busy',[job('a',{CandidateID:17,Priority:100}),job('b'),job('running',{CandidateID:17,Status:'running'})]);
 add('old generation not busy',[job('a',{CandidateID:17,Priority:100}),job('running',{CandidateID:17,Status:'running',Generation:'22222222-2222-4333-8444-555555555555'})]);
 add('agency disabled',[job('a',{AgencyID:17,Priority:100}),job('b')]);
 add('agency enabled',[job('a',{AgencyID:17,Priority:100}),job('b')],{},{settings:{enabled:true,agenciesEnabled:true}});
 add('empty types',[job('a')],{types:[]});add('unknown types',[job('a')],{types:['unknown']});
 add('disabled before invalid types',[job('a')],{types:['unknown']},{settings:{enabled:false}});
 add('diagnostic disabled',[job('a'),job('b',{Type:'classify_title',Priority:100})],{diagnostic:true},{settings:{enabled:false}});
 add('job id',[job('a',{Priority:100}),job('b')],{jobId:'b'});
 add('hh penalty',[job('a',{SourceID:'hh',Type:'scan_source',Priority:49}),job('b')]);
 add('hh fetch forbidden',[job('a',{SourceID:'hh',Type:'fetch_full_text',Priority:100}),job('b')],{allowApify:false});
 const hh=()=>job('hh',{SourceID:'hh',Type:'scan_source',Priority:50});
 add('exact page tie',[job('a'),hh()],{},{operations:['listing:hh:0']});
 add('no new starts',[hh()],{allowApifyStarts:false});
 add('old page not permission',[hh()],{allowApifyStarts:false},{pages:{hh:1},operations:['listing:hh:0']});
 add('exact page permission',[hh()],{allowApifyStarts:false},{pages:{hh:1},operations:['listing:hh:1']});
 for(const runId of [null,false,0,1.23,'И😀',{x:1},[1,2]]){const key=runId===null?'hh':typeof runId==='object'?JSON.stringify(runId):typeof runId==='boolean'?'0':String(runId);add('JSON run '+JSON.stringify(runId),[{...hh(),Payload:{runId}}],{allowApifyStarts:false},{operations:['listing:'+key+':0']});}
 add('deferred eligible',[{...hh(),ReasonCode:'apify_busy'}],{deferredApifyOnly:true});
 add('deferred too recent',[{...hh(),ReasonCode:'apify_budget',RunAfter:now-299999}],{deferredApifyOnly:true});
 add('deferred wrong reason',[hh()],{deferredApifyOnly:true});
 for(const finish of [{},{status:'pending',delayMs:60000,deferred:true,code:'apify_busy',message:'CI синтетика'},{status:'failed'},{status:'cancelled'},{payload:{2:'two',1:'one',a:'😀',n:1e100}},{payload:null}])add('finish '+JSON.stringify(finish),[job('a',{Attempts:2,Payload:{z:1}})],{},{finish});
 for(const [raw,expected] of [['{"enabled":true,"agenciesEnabled":true,"agenciesEnabled":false}',true],['{"enabled":true,"agenciesEnabled":false,"agenciesEnabled":true}',false]])add('agency duplicate '+expected,[job('a',{AgencyID:17,Priority:100}),job('b')],{},{settingsRaw:raw});
 add('run duplicate first',[{...hh(),PayloadRaw:'{"runId":"first","runId":"last"}'}],{allowApifyStarts:false},{operations:['listing:first:0']});
 add('finish duplicate last',[job('a',{PayloadRaw:'{"runId":"first","runId":"last","n":1e400}'})],{},{finish:{}});
 add('finish lone UTF16 target rejection',[job('a',{PayloadRaw:'{"s":"\\ud800"}'})],{},{finish:{},mariadbReject:'lone_UTF16'});
 let seed=312;const random=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed;};
 for(let n=0;n<100;n++){const jobs=Array.from({length:10},(_,i)=>job('CI_'+i,{Priority:random()%101,RunAfter:now-(random()%1000000),CreatedAt:now-(random()%30000000),Type:random()%2?'scan_source':'diagnostic',SourceID:random()%2?'hh':null}));add('random '+n,jobs,{allowApify:!!(random()%2)});}
 for(const v of cases)process.stdout.write(JSON.stringify(await reference(v))+'\n');
}
