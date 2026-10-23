import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=fetch,runs=await moduleFor('server/services/apify-runs.js');
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const company=catalog.companies[0];company.vacancySources={hh:'https://hh.ru/employer/42'};
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(company.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(company.name);
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_SYNTHETIC_FAILED_1234'));
let starts=0,total=100,terminal='TIMED-OUT',poll=false,bad=false;const records=new Map();
globalThis.fetch=async(url,o={})=>{
 const u=new URL(url);assert.equal(u.hostname,'api.apify.com','no Firecrawl, HH or model calls');
 if(u.pathname.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{id:'FREE',monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5}}});
 if(u.pathname.endsWith('/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:'2026-10-01T00:00:00Z',endAt:'2026-11-01T00:00:00Z'},current:{monthlyUsageUsd:0},limits:{maxMonthlyUsageUsd:5}}});
 if(o.method==='POST'){starts++;const item={total,terminal,bad};records.set(String(starts),item);return Response.json({data:{id:'run'+starts,defaultDatasetId:'data'+starts,status:poll?'RUNNING':terminal,usageTotalUsd:.05,finishedAt:poll?null:'2000-01-01T00:00:00Z'}});}
 if(u.pathname.includes('/actor-runs/')){const n=u.pathname.split('/').at(-1).slice(3),item=records.get(n);return Response.json({data:{id:'run'+n,defaultDatasetId:'data'+n,status:item.terminal,usageTotalUsd:.05,finishedAt:'2000-01-01T00:00:00Z'}});}
 if(u.pathname.includes('/datasets/')){const n=u.pathname.split('/')[3].slice(4),item=records.get(n);if(!u.pathname.endsWith('/items'))return Response.json({data:{id:'data'+n,itemCount:item.total,modifiedAt:'2000-01-01T00:00:00Z'}});
  const offset=Number(u.searchParams.get('offset'));return Response.json(Array.from({length:Math.max(0,Math.min(10,item.total-offset))},(_,i)=>{const k=offset+i,id=Number(n)*1000+k+1;return item.bad&&k===1?{}:{title:'Manager '+id,vacancy_id:String(id),url:'https://hh.ru/vacancy/'+id,employer_id:item.bad&&k===7?'999':'42'};}));}
 throw Error('Unexpected outbound request');
};
try{
 const s=(await api.readVacancyParsingSources(db)).find(x=>x.sourceId==='hh');await api.saveVacancyParsingSource(db,s.companyId,'hh',{enabled:true,sourceUrl:s.url,revision:s.revision,config:s.config});
 async function collect(){const before=starts;let r=await api.fetchVacancyListing(env,s.companyId,'hh',{userName:'Eugene'});r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');let n=0;
  while(r.paused&&n++<20){r=await api.resumeVacancyDiscoveryRun(env,r.runId,'Eugene');r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');}
  assert.ok(n<20);assert.equal(starts,before+1,'terminal dataset never starts a replacement or next page');assert.equal(r.status,'partial');assert.equal(r.errors[0].code,'apify_partial_run');assert.equal(r.coverage.datasetComplete,true);assert.equal(r.coverage.hhComplete,false);assert.equal(r.rawRows,total);
  assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing').get().last_success_at,null);return r;
 }
 let r=await collect();assert.equal(r.found,100);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,100);
 const before=starts;await assert.rejects(runs.executeApifyRun(env,{operationKey:'listing:'+r.runId+':0',input:{}}),e=>e.code==='apify_failed');assert.equal(starts,before,'strict callers still fail without relaunch');
 total=27;terminal='FAILED';bad=true;r=await collect();assert.equal(r.found,25);assert.equal(r.quarantinedCount,2,'failed provider data still requires valid title and employer identity');
 total=0;terminal='ABORTED';bad=false;r=await collect();assert.equal(r.found,0,'empty terminal dataset is explicitly partial');
 total=1;terminal='TIMED-OUT';poll=true;r=await collect();assert.equal(r.found,1,'normal RUNNING→TIMED-OUT continuation salvages the same dataset');
 assert.ok(sqlite.prepare('SELECT state FROM apify_runs').all().every(x=>x.state==='failed'));assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,126);
 console.log('Failed Apify listing: reads all100 paid rows without next Actor, async timeout resume, failed/aborted/empty datasets, per-row quarantine, strict callers and no false source success passed.');
}finally{globalThis.fetch=old;f.close();}
