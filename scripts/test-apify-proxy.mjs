import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,oldFetch=globalThis.fetch;
const {fetchApifyDetail}=await moduleFor('server/services/apify-vacancies.js');
const token='apify_api_SYNTHETIC_PROXY_1234',cipher=await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token),hash=await api.candidateHash(token);
await api.schedulerSettings(db);const generation=sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation;
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(cipher);
sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(1,'candidate','Manager',?,'https://hh.ru/vacancy/1','hh','site','now','now','now','now')").run('a'.repeat(64));
sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('old',?,'detail:1:1:missing','free','account',?,'start','end',0.25,0.01,'succeeded','oldRun','oldData',1,1)").run(generation,hash);
const c={id:1,titleRevision:1,vacancyUrl:'https://hh.ru/vacancy/1'};let starts=0,ready=false,complete=true,htmlOnly=false;
const description='Обязанности: управление командой. Требования: опыт разработки. Условия: удалённая работа. '.repeat(10);
globalThis.fetch=async(url,options={})=>{
 if(url.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{id:'FREE',monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5}}});
 if(url.endsWith('/users/me/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:'2026-10-01T00:00:00Z',endAt:'2026-11-01T00:00:00Z'},current:{monthlyUsageUsd:0},limits:{maxMonthlyUsageUsd:5}}});
 if(options.method==='POST'){starts++;const input=JSON.parse(options.body);assert.equal(input.proxyConfiguration.useApifyProxy,true);assert.equal(input.proxyConfiguration.apifyProxyGroups,undefined);assert.equal(input.api_only,false);assert.equal(input.results_wanted,1);assert.equal(new URL(url).searchParams.get('maxTotalChargeUsd'),'0.25');return Response.json({data:{id:'proxyRun',defaultDatasetId:'proxyData',status:'RUNNING'}});}
 if(url.includes('/actor-runs/'))return Response.json({data:{id:'proxyRun',defaultDatasetId:'proxyData',status:ready?'SUCCEEDED':'RUNNING',usageTotalUsd:.02,buildId:'proxyBuild'}});
 if(url.includes('/datasets/')){assert.ok(url.includes('proxyData'),'old bad dataset is never reused');return Response.json([{vacancy_id:'1',url:c.vacancyUrl,...(complete?{title:'Manager',...(htmlOnly?{description_html:'<p>'+description+'</p>'}:{description_text:description})}:{location:'Москва'})}]);}
 throw new Error('Unexpected URL');
};
try{
 await assert.rejects(fetchApifyDetail(env,c),e=>e.code==='apify_running');assert.equal(starts,1);assert.ok(sqlite.prepare("SELECT operation_key FROM apify_runs WHERE id<>'old'").get().operation_key.endsWith(':proxy-v1'));
 ready=true;const result=await fetchApifyDetail(env,c);assert.equal(result.snapshot.content_status,'valid');assert.equal(starts,1);assert.equal(sqlite.prepare("SELECT charged_usd FROM apify_runs WHERE id='old'").get().charged_usd,.01);
 assert.equal(JSON.parse(result.snapshot.diagnostics).buildId,'proxyBuild');htmlOnly=true;const htmlResult=await fetchApifyDetail(env,c);assert.equal(htmlResult.snapshot.content_status,'valid');assert.equal(starts,1,'HTML-only detail does not relaunch Actor');htmlOnly=false;
 complete=false;await assert.rejects(fetchApifyDetail(env,c),e=>e.code==='apify_incomplete');assert.equal(starts,1,'bad provider result never restarts Actor');
 const job=await api.enqueueScheduledJob(db,{type:'fetch_full_text',sourceId:'hh',candidateId:1,key:'legacy'});sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='apify_data',attempts=4 WHERE id=?").run(job.id);
 const other=await api.enqueueScheduledJob(db,{type:'fetch_full_text',sourceId:'hh',candidateId:1,key:'other'});sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='apify_incomplete',attempts=4 WHERE id=?").run(other.id);
 const migration=fs.readFileSync('drizzle/0070_apify_proxy_recovery.sql','utf8');sqlite.exec(migration);assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(job.id).status,'pending');assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(other.id).status,'failed');sqlite.exec(migration);assert.equal(sqlite.prepare('SELECT attempts FROM scheduler_jobs WHERE id=?').get(job.id).attempts,0);
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:1,monthBudgetUsd:5,revision:s.revision},'Eugene');
 const unrelated=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'higher-priority-other',sourceId:'companySite'});let selected=[];
 const opts={jobId:job.id,limit:1,handlers:{fetch_full_text:async(_,claim)=>{selected.push(claim.job.id);return{}},import_vacancy:async()=>{throw Error('Unrelated job ran')}}};
 await api.runDueTasks(env,{...opts,requestId:'one-proxy-job'});assert.deepEqual(selected,[job.id]);assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(unrelated.id).status,'pending');await api.runDueTasks(env,{...opts,requestId:'one-proxy-job'});assert.equal(selected.length,1,'same diagnostic receipt is idempotent');
 await assert.rejects(api.runScheduledSeriesTick(env,'apify-proxy-check:wrong'),e=>e.code==='input');
 await assert.rejects(api.runScheduledSeriesTick(env,'apify-proxy-check:999999:11111111-1111-4111-8111-111111111111'),e=>e.code==='apify_check_stage');
 // Exercise the public check namespace with a real eligible candidate/stage.
 const source=(await api.readVacancyParsingSources(db)).find(r=>r.sourceId==='hh');
 await api.saveVacancyParsingSource(db,source.companyId,'hh',{enabled:true,sourceUrl:source.url,config:source.config,revision:source.revision});
 sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(source.companyName);
 sqlite.prepare("INSERT OR REPLACE INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(source.companyName);
 sqlite.prepare('UPDATE vacancy_candidates SET company_id=? WHERE id=1').run(source.companyId);
 const settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['hh'],dayBudgetUsd:1,monthBudgetUsd:5,revision:settings.revision},'Eugene');
 const roles=await api.candidateClassificationContext(db),at=new Date().toISOString();
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,outcome,classified_at) VALUES('accepted',1,'Manager',?,1,?,?,'[]','complete',?)").run('a'.repeat(64),roles[0].modelKey,roles[0].modelKey,at);
 for(const [n,r] of roles.entries())sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(1,'accepted',?,?,?,?,?,?,?,?,1,?)").run(r.roleId,r.name,n?'reject':'match',r.modelKey,r.modelKey,r.promptVersion,r.revision,'a'.repeat(64),at);
 sqlite.exec('DELETE FROM vacancy_content_fetches; DELETE FROM vacancy_raw_snapshots');complete=true;
 const checkKey='apify-proxy-check:1:11111111-1111-4111-8111-111111111111';
 const checked=await api.runScheduledSeriesTick(env,checkKey);assert.equal(checked.trigger,'manual');assert.equal(checked.processed,1);assert.equal(checked.succeeded,1);assert.equal(checked.continuationRequestId,null);assert.equal(starts,1);
 assert.equal((await api.runScheduledSeriesTick(env,checkKey)).reused,true,'replay succeeds after the candidate moved to extraction');
 await assert.rejects(api.runScheduledSeriesTick(env,'apify-proxy-check:1:22222222-2222-4222-8222-222222222222'),e=>e.code==='apify_check_stage','no extra fetch after valid text');
 console.log('Apify proxy: Free/default proxy/no residential, fresh stable key vs old bad dataset, async poll/no replay, incomplete diagnosis, ledger preserved/selective migration, one-job isolated dispatcher/replay/invalid check guards passed.');
}finally{globalThis.fetch=oldFetch;f.close();}
