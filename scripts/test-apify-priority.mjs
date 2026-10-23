import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,oldFetch=globalThis.fetch;
globalThis.fetch=async()=>{throw new Error('Unexpected provider request')};
try{
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['hh','companySite'],dayBudgetUsd:1,monthBudgetUsd:5,revision:s.revision},'Eugene');
 const hh=[];for(let n=0;n<8;n++)hh.push(await api.enqueueScheduledJob(db,{type:n?'fetch_full_text':'scan_source',key:'hh-low-'+n,sourceId:'hh'}));
 const fire=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'firecrawl',sourceId:'companySite'});
 const jev=await api.enqueueScheduledJob(db,{type:'classify_title',key:'stored-hh-classification',sourceId:'hh'});
 const model=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'stored-hh-model',sourceId:'hh'});
 const imported=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'stored-hh-import',sourceId:'hh'});
 const order=[],handlers=Object.fromEntries(['scan_source','fetch_full_text','classify_title','extract_vacancy','import_vacancy'].map(type=>[type,async(_,claim)=>{order.push(claim.job.id);return {}}]));
 const first=await api.runDueTasks(env,{requestId:'low-apify-first',handlers});
 assert.equal(first.processed,9);assert.deepEqual(order.slice(0,2),[imported.id,model.id]);assert.ok(order.indexOf(fire.id)<order.indexOf(jev.id));assert.ok(order.findIndex(id=>hh.slice(1).some(j=>j.id===id))<order.indexOf(jev.id),'accepted HH detail precedes fresh title classification');assert.equal(order.filter(id=>hh.some(j=>j.id===id)).length,5,'bounded HH reads are separate from Actor admission');assert.equal(first.remaining,3);assert.equal(first.reason,'time_slice');
 order.length=0;const second=await api.runDueTasks(env,{requestId:'low-apify-second',handlers});assert.equal(second.processed,3);assert.equal(second.remaining,0);
 const gated=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'gated-hh',sourceId:'hh'});
 const run=await api.beginSchedulerRun(db,{requestId:'low-apify-disabled-lane'});assert.equal(await api.claimScheduledJob(run.db,run.run.id,{allowApify:false}),null,'quota excludes collection and detail');
 const downstream=await api.enqueueScheduledJob(db,{type:'assign_roles',key:'hh-stored-roles',sourceId:'hh'});const next=await api.claimScheduledJob(run.db,run.run.id,{allowApify:false});assert.equal(next.job.id,downstream.id,'stored HH processing remains allowed');await api.finishScheduledJob(run.db,next);
 // Aging still works, rather than permanently excluding an older HH request.
 const remaining=gated;sqlite.prepare('UPDATE scheduler_jobs SET created_at=? WHERE id=?').run(Date.now()-200*3600000,remaining.id);
 const fresh=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'fresh-import',sourceId:'companySite'});const aged=await api.claimScheduledJob(run.db,run.run.id);assert.equal(aged.job.id,remaining.id);assert.notEqual(aged.job.id,fresh.id);
 console.log('Low Apify priority: accepted HH detail/model/import and Firecrawl precede fresh discovery, at most5 HH acquisition stages per cycle, next tick resumes, quota keeps downstream allowed, aging preserved.');
}finally{globalThis.fetch=oldFetch;f.close();}

const deferred=await schedulerFixture();
try{
 const {api,db,env,sqlite}=deferred,s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision},'Eugene');
 const a=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'busy-a',sourceId:'hh'});await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'busy-b',sourceId:'hh'});
 sqlite.prepare('UPDATE scheduler_jobs SET created_at=? WHERE id=?').run(Date.now()-200*3600000,a.id);
 await api.enqueueScheduledJob(db,{type:'diagnostic',key:'other-useful-work'});let calls=0,useful=0;
 await api.runDueTasks(env,{requestId:'busy-quota',handlers:{fetch_full_text:async()=>{calls++;throw Object.assign(new Error('Busy'),{code:'apify_busy',status:409})},diagnostic:async()=>{useful++;return{}}}});
 assert.equal(calls,2);assert.equal(useful,1);assert.equal(sqlite.prepare('SELECT attempts FROM scheduler_jobs WHERE id=?').get(a.id).attempts,0,'waiting keeps retry count and remains bounded by the HH read quota');
 console.log('HH read quota also bounds waiting/error stages, while other useful work continues.');
}finally{deferred.close();}

const preparation=await schedulerFixture();
try{
 const {api,db,sqlite}=preparation,catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const co=catalog.companies[0];co.vacancySources={hh:'https://hh.ru/employer/42',companySite:'https://careers.fixture.org/jobs'};sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(co.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(co.name);
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['hh','companySite'],dayBudgetUsd:1,monthBudgetUsd:5,revision:s.revision},'Eugene');
 await api.scheduleDueSources(db,'Eugene',Date.now(),{limit:1});assert.equal(sqlite.prepare('SELECT source_id FROM scheduler_jobs').get().source_id,'companySite');
 console.log('Source preparation fills the bounded batch with other providers before HH.');
}finally{preparation.close();}
