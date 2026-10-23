import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,originalFetch=fetch,realNow=Date.now;let clock=realNow();Date.now=()=>clock;
let posts=0,running=false;const operations=new Map(),offsets=[];
globalThis.fetch=async(url,o={})=>{
 const u=new URL(url);
 if(url.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{id:'FREE',monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5}}});
 if(url.endsWith('/users/me/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:'2026-10-01T00:00:00Z',endAt:'2026-11-01T00:00:00Z'},current:{monthlyUsageUsd:0},limits:{maxMonthlyUsageUsd:5}}});
 if(url.includes('/actors/')&&o.method==='POST'){posts++;const input=JSON.parse(o.body),employer=new URL(input.startUrl).searchParams.get('employer_id'),id='run'+posts,record={id,defaultDatasetId:'data'+posts,employer,status:employer==='42'?'TIMED-OUT':running?'RUNNING':'SUCCEEDED',usageTotalUsd:.01};operations.set(id,record);return Response.json({data:record});}
 if(url.includes('/actor-runs/'))return Response.json({data:operations.get(u.pathname.split('/').at(-1))});
 if(url.includes('/datasets/')){
  const id=u.pathname.split('/')[3],record=operations.get('run'+id.slice(4)),total=record.employer==='42'?96:1;
  if(!u.pathname.endsWith('/items'))return Response.json({data:{id,itemCount:total,modifiedAt:'2000-01-01T00:00:00Z'}});
  clock+=1100;const offset=Number(u.searchParams.get('offset'));offsets.push([record.employer,offset]);
  return Response.json(Array.from({length:Math.max(0,Math.min(10,total-offset))},(_,i)=>({vacancy_id:String(Number(record.employer)*1000+offset+i),title:'Developer '+offset+' '+i,url:'https://hh.ru/vacancy/'+(Number(record.employer)*1000+offset+i),employer_id:record.employer})));
 }
 if(url.includes('models?output_modalities'))return Response.json({data:[{id:api.JEV_MODEL_ID,context_length:32000,pricing:{prompt:'0.000000042',completion:'0',request:'0'}}]});
 if(url.endsWith('/decisions')){clock+=150;const p=JSON.parse(o.body);return Response.json({model:api.JEV_MODEL_ID,answers:Object.fromEntries(Object.keys(p.questions).map(k=>[k,{type:'choice',choice:'reject'}])),usage:{cost:.00001}});}
 throw Error('Unexpected outbound: '+url);
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,3);catalog.agencies=[];
 for(const [i,c] of catalog.companies.entries()){c.vacancySources={hh:'https://hh.ru/employer/'+(42+i)};sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(c.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(c.name);}
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_SYNTHETIC_ADMISSION_1234'));
 await api.candidateClassificationContext(db);await api.orSettings(env);sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').run(await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC'));
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['hh'],dayBudgetUsd:1,monthBudgetUsd:5,revision:s.revision},'Eugene');await api.scheduleDueSources(db,'Eugene');
 const sources=(await api.readVacancyParsingSources(db)).filter(s=>s.sourceId==='hh'),source=sources.find(s=>s.url.includes('/42')),oldJob=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE company_id=? AND type='scan_source'").get(source.companyId);
 // The paid operation already exists. Its failed terminal dataset is reusable.
 let preview=await api.fetchVacancyListing(env,source.companyId,'hh',{userName:'Eugene',discoveryId:oldJob.id});assert.equal(posts,1);preview=await api.saveVacancyDiscoveryRun(db,preview.runId,'Eugene');assert.equal(preview.found,10);
 let multiRead=false;
 for(let i=0;i<6;i++){
  clock+=2000;const beforePosts=posts,beforeReads=offsets.length;await api.runDueTasks(env,{requestId:'admission-'+i});
  const count=offsets.length-beforeReads;multiRead||=count>1;assert.ok(count<=5,'HH reads remain bounded');assert.ok(posts-beforePosts<=1,'at most one new Actor POST in a whole tick');
  if(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='scan_source' AND status='pending'").get().n===0)break;
 }
 assert.ok(multiRead,'several ready chunks can progress in one tick');assert.equal(posts,3,'original dataset never restarts; two other sources each start once');
 const stored=JSON.parse(sqlite.prepare('SELECT result_json FROM vacancy_discovery_runs WHERE id=?').get(oldJob.id).result_json);assert.equal(stored.rawRows,96);assert.equal(stored.coverage.datasetComplete,true);assert.equal(stored.coverage.hhComplete,false);assert.equal(stored.errors[0].code,'apify_partial_run');
 assert.deepEqual(offsets.filter(([e])=>e==='42').map(([,o])=>o),[0,10,20,30,40,50,60,70,80,90]);
 // Deny before both preflight and durable insertion: no new/uncertain identity.
 const ledgerCount=sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,postCount=posts;
 for(const late of [false,true]){
  const deny=()=>{throw api.schedulerError('apify_cycle_limit','Synthetic tick quota');};
  const blocked=await api.fetchVacancyListing({...env,SCHEDULER_APIFY_ADMISSION:{assertCanStart:late?()=>{}:deny,takeStart:deny}},sources.find(s=>s.url.includes('/43')).companyId,'hh',{userName:'Eugene'});
  assert.equal(blocked.paused.code,'apify_cycle_limit');assert.equal(posts,postCount);assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,ledgerCount);
 }
 // An existing page0 cannot authorize a new paid page1 with starts disabled.
 sqlite.prepare("UPDATE scheduler_jobs SET status='pending',run_after=1 WHERE id=?").run(oldJob.id);sqlite.prepare("UPDATE vacancy_discovery_runs SET result_json=json_set(result_json,'$.paused',json(?)) WHERE id=?").run(JSON.stringify({apify:{page:1}}),oldJob.id);
 const lane=await api.beginSchedulerRun(db,{requestId:'exact-page'});assert.equal(await api.claimScheduledJob(lane.db,lane.run.id,{types:['scan_source'],allowApifyStarts:false}),null);
 sqlite.prepare("UPDATE scheduler_runs SET status='completed',finished_at=?,lease_until=0 WHERE id=?").run(clock,lane.run.id);sqlite.prepare("UPDATE scheduler_jobs SET status='completed' WHERE id=?").run(oldJob.id);
 // A running Actor is polled once, then the job waits a full minute.
 running=true;const fresh=sources.find(s=>s.url.includes('/44')),job=await api.enqueueScheduledJob(db,{type:'scan_source',key:'backoff',companyId:fresh.companyId,sourceId:'hh',payload:{sourceRevision:fresh.revision,sourceUrl:fresh.url,manual:true}}),before=posts;
 await api.runDueTasks(env,{requestId:'poll-backoff'});assert.equal(posts-before,1);const pending=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(job.id);assert.equal(pending.status,'pending');assert.equal(pending.reason_code,'apify_running');assert.ok(pending.run_after>=clock+50000);assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_attempts WHERE job_id=?').get(job.id).n,1);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM apify_runs WHERE state IN ('starting','uncertain')").get().n,0,'quota denial never leaves empty durable Actor identities');
 assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Apify admission: multiple bounded dataset chunks,96 raw rows salvaged without POST replay,<=1 new Actor/tick across sources, exact cursor page gating, partial coverage,60s poll backoff and no quota orphan passed.');
}finally{globalThis.fetch=originalFetch;Date.now=realNow;f.close();}
