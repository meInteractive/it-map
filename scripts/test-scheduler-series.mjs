import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture();
try{
 const {api,db,env,sqlite}=f,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 for(let i=0;i<9;i++)await api.enqueueScheduledJob(db,{type:'diagnostic',key:'series-job:'+i});
 let calls=0;const options={limit:1,handlers:{diagnostic:async()=>{calls++;return{}}}};
 const prepared=await api.runScheduledSeriesTick(env,undefined,options),lostPrepare=await api.runScheduledSeriesTick(env,undefined,options);assert.equal(prepared.status,'prepared');assert.equal(prepared.series.index,0);assert.notEqual(prepared.continuationRequestId,lostPrepare.continuationRequestId);assert.equal(calls,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_runs').get().n,0,'lost preparation starts no work');
 const first=await api.runScheduledSeriesTick(env,prepared.continuationRequestId,options);assert.equal(first.series.index,1);assert.equal((await api.runScheduledSeriesTick(env,prepared.continuationRequestId,options)).reused,true);assert.equal(calls,1,'lost work ACK reuses a known ID');assert.ok(first.continuationRequestId);
 const key=first.continuationRequestId,skip=key.replace(/:2$/,':3');await assert.rejects(()=>api.runScheduledSeriesTick(env,skip,options),e=>e.code==='series_order');
 let cursor=key,last;for(let i=2;i<=5;i++){last=await api.runScheduledSeriesTick(env,cursor,options);assert.equal(last.series.index,i);const replay=await api.runScheduledSeriesTick(env,cursor,options);assert.equal(replay.reused,true);cursor=last.continuationRequestId;}
 assert.equal(cursor,null);assert.equal(calls,5);assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE status='pending'").get().n,4);
 assert.equal(last.continuationStopReason,'tick_limit');
 const again=await api.runScheduledSeriesTick(env,undefined,options);assert.equal(again.status,'prepared');assert.notEqual(again.continuationRequestId,prepared.continuationRequestId);assert.equal(calls,5,'preparation never executes jobs');
 const setting=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:false,dayBudgetUsd:0,monthBudgetUsd:0,revision:setting.revision},'Eugene');assert.equal((await api.runScheduledSeriesTick(env,undefined,options)).continuationRequestId,null);
 sqlite.prepare('UPDATE app_runtime_generation SET generation=? WHERE id=1').run(crypto.randomUUID());await assert.rejects(()=>api.runScheduledSeriesTick(env,key,options),e=>e.code==='series_order','restore invalidates previous continuations');
 console.log('Hourly series: server-issued continuation, <=5 ticks, predecessor/restore guard, replay/no extra execution, pause and remaining backlog preserved');
}finally{f.close();}
const crashed=await schedulerFixture();
try{
 const {api,db,env,sqlite}=crashed,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'crashed-work'});await api.enqueueScheduledJob(db,{type:'diagnostic',key:'other-work'});
 const key=`series:${Math.floor(Date.now()/3600000)}:1`,old=await api.beginSchedulerRun(db,{trigger:'automatic',requestId:key}),claim=await api.claimScheduledJob(old.db,old.run.id);
 sqlite.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd) VALUES('lost',?,?,?,?,'running',0.02)").run(claim.job.id,old.run.id,claim.job.type,Date.now());
 const active=await api.runScheduledSeriesTick(env,key);assert.equal(active.reused,true);assert.equal(active.status,'running');assert.equal(active.continuationRequestId,null,'live owner is not replaced');
 sqlite.prepare('UPDATE scheduler_runs SET lease_until=0 WHERE id=?').run(old.run.id);sqlite.prepare('UPDATE scheduler_jobs SET locked_until=0 WHERE id=?').run(claim.job.id);
 let calls=0;const options={limit:1,handlers:{diagnostic:async()=>{calls++;return{};}}};
 const replay=await api.runScheduledSeriesTick(env,key,options);assert.equal(replay.reused,true);assert.equal(replay.status,'interrupted');assert.ok(replay.finished_at);assert.equal(replay.continuationRequestId,key.replace(/:1$/,':2'));assert.equal(calls,0);
 assert.equal(sqlite.prepare("SELECT reserved_usd FROM scheduler_attempts WHERE id='lost'").get().reserved_usd,0.02);assert.equal(sqlite.prepare("SELECT status FROM scheduler_attempts WHERE id='lost'").get().status,'interrupted');
 await assert.rejects(()=>old.db.prepare('SELECT 1').first(),e=>e.code==='scheduler_changed');
 const next=await api.runScheduledSeriesTick(env,replay.continuationRequestId,options);assert.equal(calls,1);assert.equal(next.status,'completed');await api.runScheduledSeriesTick(env,replay.continuationRequestId,options);assert.equal(calls,1,'receipt replay cannot execute a second time');
 console.log('Lost MCP receipt: live owner protected, expired same-ID recovery returns terminal receipt/continuation, old owner fenced, reservations preserved and no duplicate work passed');
}finally{crashed.close();}
const empty=await schedulerFixture();try{const {api,db,env}=empty,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');const prepared=await api.runScheduledSeriesTick(env),result=await api.runScheduledSeriesTick(env,prepared.continuationRequestId,{handlers:{}});assert.equal(result.continuationRequestId,null);assert.equal(result.continuationStopReason,'no_ready_work');}finally{empty.close();}

const expired=await schedulerFixture();try{const {api,db,env,sqlite}=expired,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');for(let i=0;i<2;i++)await api.enqueueScheduledJob(db,{type:'diagnostic',key:'expiry:'+i});const opts={limit:1,handlers:{diagnostic:async()=>({})}},prepared=await api.runScheduledSeriesTick(env),first=await api.runScheduledSeriesTick(env,prepared.continuationRequestId,opts);sqlite.prepare('UPDATE scheduler_runs SET started_at=? WHERE id=?').run(Date.now()-720001,first.id);await assert.rejects(()=>api.runScheduledSeriesTick(env,first.continuationRequestId,opts),e=>e.code==='series_expired');assert.equal((await api.runScheduledSeriesTick(env,prepared.continuationRequestId,opts)).reused,true);assert.equal((await api.runScheduledSeriesTick(env,prepared.continuationRequestId,opts)).continuationStopReason,'series_expired');}finally{expired.close();}

// A second real occurrence before the hour boundary must not replay the old hour.
const early=await schedulerFixture(),realNow=Date.now;let clock=realNow();
try{
 Date.now=()=>clock;const {api,db,env,sqlite}=early,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 for(let i=0;i<3;i++)await api.enqueueScheduledJob(db,{type:'diagnostic',key:'early:'+i});
 const options={limit:1,handlers:{diagnostic:async()=>({})}},hour=Math.floor(clock/3600000);
 const legacy=await api.runScheduledSeriesTick(env,`series:${hour}:1`,options);clock=hour*3600000+58*60000;
 const prepared=await api.runScheduledSeriesTick(env),root=await api.runScheduledSeriesTick(env,prepared.continuationRequestId,options);assert.notEqual(root.id,legacy.id);assert.equal(root.processed,1);assert.equal(root.reused,undefined);assert.equal(root.trigger,'automatic');
 const manual=await api.runScheduledSeriesTick(env,crypto.randomUUID(),options);assert.equal(manual.trigger,'manual');assert.equal(manual.continuationStopReason,'single_tick');
 const before=(await api.schedulerDashboard(db)).lastSignalAt;assert.equal(before,root.started_at,'manual invocation cannot replace the cloud signal');
 sqlite.prepare('UPDATE app_runtime_generation SET generation=? WHERE id=1').run(crypto.randomUUID());assert.equal((await api.schedulerDashboard(db)).lastSignalAt,null,'old generation is not a current signal');
 await assert.rejects(()=>api.runScheduledSeriesTick(env,'cloud:not-a-uuid:1',options),e=>e.code==='series_order');
 console.log('Early cloud occurrence:58min creates a new series, legacy remains compatible, manual signal isolated, restored generation and malformed IDs guarded');
}finally{Date.now=realNow;early.close();}
