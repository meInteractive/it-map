import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,oldFetch=globalThis.fetch;let outbound=0;globalThis.fetch=async()=>{outbound++;throw new Error('No provider expected')};
try{
 let s=await api.schedulerSettings(db);s=await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:.01,monthBudgetUsd:.02,revision:s.revision},'Eugene');
 const empty=await api.runDueTasks(env,{requestId:'empty'});assert.equal(empty.processed,0);assert.equal(outbound,0);
 for(let i=0;i<55;i++)await api.enqueueScheduledJob(db,{type:'diagnostic',key:'diag'+i});
 const future=await api.enqueueScheduledJob(db,{type:'diagnostic',key:'future',runAfter:Date.now()+10000000});
 const handlers={diagnostic:async()=>({diagnostic:true})};
 const batch=await api.runDueTasks(env,{requestId:'batch',handlers});assert.equal(batch.processed,50);assert.equal(batch.remaining,6);assert.equal(batch.reason,'limit');assert.equal((await api.runDueTasks(env,{requestId:'batch',handlers})).reused,true);
 const next=await api.runDueTasks(env,{requestId:'next',handlers});assert.equal(next.processed,5);assert.equal(next.remaining,1);assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(future.id).status,'pending');
 let released,entered;const hold=new Promise(r=>entered=r),gate=new Promise(r=>released=r);await api.enqueueScheduledJob(db,{type:'diagnostic',key:'hold'});
 const active=api.runDueTasks(env,{requestId:'concurrent-a',handlers:{diagnostic:async()=>{entered();await gate;return {}}}});await hold;await assert.rejects(()=>api.runDueTasks(env,{requestId:'concurrent-b',handlers}),e=>e.code==='scheduler_busy');released();await active;
 const broken=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'temporary'});await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'independent'});const failure=await api.runDueTasks(env,{requestId:'fail-independent',handlers:{fetch_full_text:async()=>{throw api.schedulerError('network','Временный сбой.',503)},extract_vacancy:async()=>({reused:true})}});assert.equal(failure.errors,1);assert.equal(failure.succeeded,1);const retry=sqlite.prepare('SELECT status,run_after FROM scheduler_jobs WHERE id=?').get(broken.id);assert.equal(retry.status,'pending');assert.ok(retry.run_after>Date.now()+300000,'network cooldown survives cycle');
 const ins=id=>sqlite.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status) VALUES(?,'synthetic','synthetic','classify_title',?,'running')").run(id,Date.now());ins('spend1');ins('spend2');
 const req={provider:'openrouter',model:'synthetic/jev',pricing:{prompt:'0.000001',completion:'0.000001',request:'0'},contextLength:1000,payload:{questions:{role_1:{}}}};
 let paid=0,finish,started;const wait=new Promise(r=>started=r),pending=new Promise(r=>finish=r),b1=api.schedulerBilling(db,'spend1',async()=>{}),b2=api.schedulerBilling(db,'spend2',async()=>{});
 const one=b1.run(req,async()=>{paid++;started();await pending;return {usage:{cost:.0001}}});await wait;
 await assert.rejects(()=>b2.run(req,async()=>{paid++;return {}}),e=>e.code==='scheduler_budget');assert.equal(paid,1);finish();await one;
 await b2.run(req,async()=>{paid++;return {usage:{}}});assert.equal(b2.unknown,true);assert.ok(sqlite.prepare("SELECT reserved_usd FROM scheduler_attempts WHERE id='spend2'").get().reserved_usd>0);
 s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 ins('free');await api.schedulerBilling(db,'free',async()=>{}).run({provider:'openrouter',model:'synthetic/free',free:true},async()=>({usage:{}}));
 assert.throws(()=>api.schedulerUpperCost({provider:'openai',model:'unknown',payload:{}}),e=>e.code==='budget_unknown');
 const spend=await api.schedulerSpending(db);assert.ok(spend.monthUsd>=.0001);assert.ok(spend.byType.some(x=>x.unknown>0));assert.equal(outbound,0);
 console.log('Dispatcher: empty/no outbound, due/future, <=50/remaining, repeated signal/concurrent cycles, atomic reservations and concurrent budget denial, actual release/unknown holds, free models at zero budget, unknown paid tariff fail-closed passed');
}finally{globalThis.fetch=oldFetch;f.close();}

const backlog=await schedulerFixture();
try{
 const {api,db,env}=backlog,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 await api.enqueueScheduledJobs(db,Array.from({length:65},(_,i)=>({type:'diagnostic',key:'backlog:'+i})));
 const prepare=db.prepare.bind(db);db.prepare=sql=>{assert.ok(!sql.includes('FROM app_catalog'),'ready backlog must execute before catalog reconciliation');return prepare(sql);};
 const run=await api.runDueTasks(env,{requestId:'ready-backlog'});assert.equal(run.processed,50);assert.equal(run.remaining,15);assert.equal(run.succeeded,50);
 console.log('Automatic dispatcher: 65 queued stages run immediately without expensive reconciliation or manual confirmation passed');
}finally{backlog.close();}

// Real DOMException has numeric code=23; it must enter the bounded retry policy.
const timeoutFixture=await schedulerFixture();
try{
 const {api,db,env,sqlite}=timeoutFixture,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 const timed=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'numeric-timeout',maxAttempts:2});
 const handlers={extract_vacancy:async()=>{throw new DOMException('Synthetic timeout','TimeoutError')}};
 let run=await api.runDueTasks(env,{requestId:'timeout-one',handlers});assert.equal(run.errors,1);
 let job=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(timed.id);assert.equal(job.reason_code,'timeout');assert.equal(job.status,'pending');assert.equal(job.attempts,1);assert.ok(job.run_after>Date.now()+300000);assert.match(job.last_error,/не успела ответить/);
 const attempt=sqlite.prepare('SELECT * FROM scheduler_attempts WHERE job_id=?').get(timed.id);assert.equal(attempt.reason_code,'timeout');assert.equal(attempt.status,'pending');
 sqlite.prepare('UPDATE scheduler_jobs SET run_after=1 WHERE id=?').run(timed.id);await api.runDueTasks(env,{requestId:'timeout-two',handlers});job=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(timed.id);assert.equal(job.status,'failed');assert.equal(job.attempts,2,'timeouts cannot retry forever');
 console.log('Numeric DOMException TimeoutError: canonical timeout, automatic cooldown/retry, readable diagnostic and bounded exhaustion passed');
}finally{timeoutFixture.close();}

// A long LLM stage waits for a fresh cycle rather than starting with 70s left.
const windowFixture=await schedulerFixture(),realNow=Date.now;let clock=realNow();
try{
 Date.now=()=>clock;const {api,db,env,sqlite}=windowFixture,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'consumes-time',priority:100});const waiting=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'needs-fresh-window'});let modelCalls=0;
 const handlers={diagnostic:async()=>{clock+=40000;return {}},extract_vacancy:async()=>{modelCalls++;return {}}};
 const run=await api.runDueTasks(env,{requestId:'short-window',handlers});assert.equal(run.reason,'time_slice');assert.equal(modelCalls,0);let job=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(waiting.id);assert.equal(job.status,'pending');assert.equal(job.attempts,0);assert.equal(job.reason_code,'','unfit job is never claimed or delayed');
 clock+=2000;await api.runDueTasks(env,{requestId:'fresh-window',handlers});assert.equal(modelCalls,1);assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(waiting.id).status,'completed');
 console.log('LLM time window: stage defers before external call with no consumed attempt, fresh cycle completes it');
}finally{Date.now=realNow;windowFixture.close();}

const schemaRetry=await schedulerFixture();
try{
 const {api,db,env,sqlite}=schemaRetry,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 const j=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'bad-model-schema',maxAttempts:2});
 const opts={handlers:{extract_vacancy:async()=>{throw Object.assign(Error('Bad model field'),{code:'invalid_output',status:422});}}};
 await api.runDueTasks(env,{requestId:'schema-retry-one',...opts});assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(j.id).status,'pending');
 sqlite.prepare('UPDATE scheduler_jobs SET run_after=0 WHERE id=?').run(j.id);await api.runDueTasks(env,{requestId:'schema-retry-two',...opts});assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(j.id).status,'failed');
 console.log('Structured schema retry: a different model may repair the response, but max attempts still terminates repeated invalid output');
}finally{schemaRetry.close();}


// An unfit high-priority model stage cannot block useful shorter work.
const fit=await schedulerFixture(),originalNow=Date.now;let now=originalNow();
try{
 Date.now=()=>now;const {api,db,env,sqlite}=fit,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'first',priority:100});const long=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'long'});const short=await api.enqueueScheduledJob(db,{type:'scan_source',key:'short'});let scans=0,models=0;
 const run=await api.runDueTasks(env,{requestId:'fit-work',handlers:{diagnostic:async()=>{now+=40000;return{}},extract_vacancy:async()=>{models++;return{}},scan_source:async()=>{scans++;return{}}}});
 assert.equal(scans,1);assert.equal(models,0);assert.equal(run.processed,2);assert.equal(run.reason,'time_slice');assert.equal(sqlite.prepare('SELECT attempts FROM scheduler_jobs WHERE id=?').get(long.id).attempts,0);assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(short.id).status,'completed');
 console.log('Fit-aware queue: short work runs with70s left while a long LLM stage stays untouched for the next cycle');
}finally{Date.now=originalNow;fit.close();}

const parallel=await schedulerFixture();
try{
 const {api,db,env,sqlite}=parallel,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 await api.enqueueScheduledJob(db,{type:'assign_roles',candidateId:101,key:'pair-role'});await api.enqueueScheduledJob(db,{type:'extract_vacancy',candidateId:102,key:'pair-extract'});await api.enqueueScheduledJob(db,{type:'extract_vacancy',candidateId:101,key:'same-candidate'});
 let release,started,active=0,maxActive=0;const ids=new Set(),gate=new Promise(r=>release=r),entered=new Promise(r=>started=r),progress=[];
 const handler=async(_,claim)=>{assert.ok(!ids.has(claim.job.candidate_id));ids.add(claim.job.candidate_id);active++;maxActive=Math.max(active,maxActive);if(active===2)started();await gate;active--;ids.delete(claim.job.candidate_id);return {}};
 const pending=api.runDueTasks(env,{requestId:'parallel-models',handlers:{extract_vacancy:handler,assign_roles:handler},onProgress:p=>progress.push({...p})});await entered;assert.equal(active,2);release();const run=await pending;assert.equal(run.processed,3);assert.equal(run.succeeded,3);assert.equal(maxActive,2);assert.equal(progress.at(-1).processed,3);assert.equal(sqlite.prepare('SELECT processed FROM scheduler_runs WHERE id=?').get(run.id).processed,3);
 console.log('Two LLM lanes: distinct candidates run concurrently, same-candidate stage waits, progress persists and global cycle stays bounded');
}finally{parallel.close();}

// JEV can use a shorter remainder while extraction retains its full window.
const jevWindow=await schedulerFixture(),beforeJevNow=Date.now;let jevClock=beforeJevNow();
try{
 Date.now=()=>jevClock;const {api,db,env,sqlite}=jevWindow,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 sqlite.prepare("INSERT INTO vacancy_role_settings(id,markdown,provider,openrouter_model) VALUES(1,'synthetic','openrouter','jev')").run();
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'jev-prelude',priority:100});
 const roles=await Promise.all([201,202].map(candidateId=>api.enqueueScheduledJob(db,{type:'assign_roles',candidateId,key:'jev-window:'+candidateId})));
 const extraction=await api.enqueueScheduledJob(db,{type:'extract_vacancy',candidateId:203,key:'luna-waits'});
 let release,entered,active=0,peak=0;const gate=new Promise(r=>release=r),pair=new Promise(r=>entered=r);
 const role=async()=>{active++;peak=Math.max(peak,active);if(active===2)entered();await gate;active--;return{}};
 const work=api.runDueTasks(env,{requestId:'jev-short-pair',handlers:{diagnostic:async()=>{jevClock+=40000;return{}},assign_roles:role,extract_vacancy:async()=>{throw Error('Luna must wait')}}});
 await pair;release();const run=await work;assert.equal(peak,2);assert.equal(run.processed,3);assert.equal(run.reason,'time_slice');
 for(const job of roles)assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(job.id).status,'completed');
 assert.equal(sqlite.prepare('SELECT attempts FROM scheduler_jobs WHERE id=?').get(extraction.id).attempts,0);
 console.log('JEV45s window: two distinct role jobs run with70s left; Luna extraction stays unclaimed');
}finally{Date.now=beforeJevNow;jevWindow.close();}

// Re-read the selected provider after claim, before recording a paid attempt.
const switchWindow=await schedulerFixture(),beforeSwitchNow=Date.now;let switchClock=beforeSwitchNow();
try{
 Date.now=()=>switchClock;const {api,db,env,sqlite}=switchWindow,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 sqlite.prepare("INSERT INTO vacancy_role_settings(id,markdown,provider,openrouter_model) VALUES(1,'synthetic','openrouter','jev')").run();
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'switch-prelude',priority:100});const job=await api.enqueueScheduledJob(db,{type:'assign_roles',candidateId:204,key:'switch-role'});
 let calls=0;const run=await api.runDueTasks(env,{requestId:'provider-switch',handlers:{diagnostic:async()=>{switchClock+=40000;sqlite.prepare("UPDATE vacancy_role_settings SET provider='openai',openai_model='gpt-6-luna' WHERE id=1").run();return{}},assign_roles:async()=>{calls++;return{}}}});
 assert.equal(calls,0);assert.equal(run.reason,'time_slice');const waiting=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(job.id);assert.equal(waiting.status,'pending');assert.equal(waiting.attempts,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_attempts WHERE job_id=?').get(job.id).n,0);
 switchClock+=2000;await api.runDueTasks(env,{requestId:'provider-switch-next',handlers:{assign_roles:async()=>{calls++;return{}}}});assert.equal(calls,1);
 console.log('Role provider switch: fresh Luna settings reject a short window without paid attempt; next cycle completes');
}finally{Date.now=beforeSwitchNow;switchWindow.close();}

// A successful HTTP response without a Rabota body gets one retry, never a loop.
const rabotaRetry=await schedulerFixture();
try{
 const {api,db,env,sqlite}=rabotaRetry,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 const job=await api.enqueueScheduledJob(db,{type:'fetch_full_text',sourceId:'rabota',key:'missing-rabota-body'});
 const handlers={fetch_full_text:async()=>{throw Object.assign(Error('Missing body'),{code:'rabota_incomplete_body',status:422});}};
 await api.runDueTasks(env,{requestId:'body-retry-first',handlers});let row=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(job.id);assert.equal(row.status,'pending');assert.equal(row.attempts,1);assert.ok(row.run_after>Date.now()+50000);
 sqlite.prepare('UPDATE scheduler_jobs SET run_after=0 WHERE id=?').run(job.id);await api.runDueTasks(env,{requestId:'body-retry-second',handlers});row=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(job.id);assert.equal(row.status,'failed');assert.equal(row.attempts,2);
 const archived=await api.enqueueScheduledJob(db,{type:'fetch_full_text',sourceId:'rabota',key:'closed-rabota-body'});await api.runDueTasks(env,{requestId:'body-invalid-permanent',handlers:{fetch_full_text:async()=>{throw Object.assign(Error('Closed'),{code:'invalid_content',status:422});}}});assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(archived.id).status,'failed');
 console.log('Rabota missing body: one cooldown/retry, exhausted after two attempts; permanent invalid content is not retried.');
}finally{rabotaRetry.close();}

// Returned pause and thrown conflicts use the same bounded provider cadence.
const waits=await schedulerFixture();
try{
 const {api,db,env,sqlite}=waits,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 for(const code of ['apify_busy','apify_budget'])for(const thrown of [false,true]){
  const job=await api.enqueueScheduledJob(db,{type:'scan_source',sourceId:'hh',key:code+':'+thrown});let calls=0;
  const before=Date.now();await api.runDueTasks(env,{requestId:'wait:'+code+':'+thrown,handlers:{scan_source:async()=>{calls++;if(thrown)throw api.schedulerError(code,'synthetic');return {defer:true,code};}}});
  const row=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(job.id),delay=code==='apify_busy'?60000:300000;
  assert.equal(row.status,'pending');assert.equal(row.reason_code,code);assert.equal(row.attempts,0);assert.equal(calls,1);
  assert.ok(row.run_after>=before+delay&&row.run_after<=Date.now()+delay);
  await api.runDueTasks(env,{requestId:'no-poll:'+code+':'+thrown,handlers:{scan_source:async()=>{throw Error('Cooldown must prevent another call');}}});
  assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_attempts WHERE job_id=?').get(job.id).n,1);
  sqlite.prepare("UPDATE scheduler_jobs SET status='completed' WHERE id=?").run(job.id);
 }
 console.log('Apify waits: returned/thrown busy60s and budget300s, no immediate repoll or consumed retry passed.');
}finally{waits.close();}

// Parked source admission gets one fair recheck, without displacing pipeline work.
const fair=await schedulerFixture();
try{
 const {api,db,env,sqlite}=fair,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 for(const code of ['apify_busy','apify_budget']){
  const j=await api.enqueueScheduledJob(db,{type:'scan_source',sourceId:'hh',key:'parked:'+code,runAfter:Date.now()-600000});
  sqlite.prepare("UPDATE scheduler_jobs SET reason_code=?,created_at=? WHERE id=?").run(code,Date.now()-3600000,j.id);
 }
 await api.enqueueScheduledJobs(db,Array.from({length:65},(_,i)=>({type:'classify_title',key:'fair-title:'+i})));
 await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'higher-import'});
 const order=[];const handlers={import_vacancy:async()=>{order.push('import');return{}},classify_title:async()=>{order.push('title');return{}},scan_source:async()=>{order.push('parked');return{defer:true,code:'apify_busy'}}};
 await api.runDueTasks(env,{requestId:'fair-parked-backlog',limit:6,handlers});
 assert.equal(order[0],'import','accepted pipeline work retains priority');assert.equal(order[1],'parked','one parked source progresses ahead of an unending title backlog');assert.equal(order.filter(x=>x==='parked').length,1);
 const parked=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE type='scan_source' AND reason_code='apify_busy' ORDER BY updated_at DESC LIMIT 1").get();assert.equal(parked.attempts,0);assert.ok(parked.run_after>Date.now()+50000);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='classify_title' AND status='pending'").get().n,61);
 console.log('Deferred Apify fairness: one overdue slot/budget recheck despite65 titles, higher import priority, busy cooldown and no retry consumption passed.');
}finally{fair.close();}

const legacyWaits=await schedulerFixture(),beforeLegacyFetch=fetch;let legacyOutbound=0;
try{
 globalThis.fetch=async()=>{legacyOutbound++;throw Error('No provider expected')};
 const {api,db,env,sqlite}=legacyWaits,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 const checked=Date.now()-600000,rows=[];
 for(const [code,status] of [['apify_busy','pending'],['apify_budget','pending'],['apify_busy','failed'],['apify_busy','cancelled']]){
  const j=await api.enqueueScheduledJob(db,{type:'scan_source',sourceId:'hh',key:'legacy:'+code+':'+status,runAfter:checked+3600000});
  sqlite.prepare('UPDATE scheduler_jobs SET reason_code=?,status=?,updated_at=? WHERE id=?').run(code,status,checked,j.id);rows.push({...j,code,status});
 }
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'legacy-first',priority:100});await api.runDueTasks(env,{requestId:'legacy-deadlines',limit:1});
 for(const j of rows){const row=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(j.id);assert.equal(row.status,j.status);assert.equal(row.updated_at,checked);assert.equal(row.attempts,0);assert.equal(row.run_after,checked+(j.status==='pending'?(j.code==='apify_busy'?60000:300000):3600000));}
 assert.equal(legacyOutbound,0);console.log('Legacy waits: pending deadlines use new cadence, checkedAt preserved, terminal jobs untouched and no provider calls passed.');
}finally{globalThis.fetch=beforeLegacyFetch;legacyWaits.close();}
