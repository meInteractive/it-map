import assert from 'node:assert/strict';import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=fetch;
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const company=catalog.companies[0];company.vacancySources={hh:'https://hh.ru/employer/42'};
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(company.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(company.name);
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_SYNTHETIC_RAW_1234'));
const rows=Array.from({length:27},(_,i)=>({vacancy_id:String(i+1),url:'https://hh.ru/vacancy/'+(i+1),title:'Manager '+(i+1),employer_id:'42'}));
rows[1]={};rows[5]={vacancy_id:'6',url:'https://hh.ru/vacancy/6',secret:'PRIVATE_BAD_BODY'};rows[18]={...rows[18],employer_id:'999'};
let starts=0,offsets=[],metadataCalls=0,cap=false;
globalThis.fetch=async(url,o={})=>{
 const u=new URL(url);
 if(u.pathname.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{id:'FREE',monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5}}});
 if(u.pathname.endsWith('/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:'2026-10-01T00:00:00Z',endAt:'2026-11-01T00:00:00Z'},current:{monthlyUsageUsd:0},limits:{maxMonthlyUsageUsd:5}}});
 if(o.method==='POST'){starts++;assert.equal(new URL(JSON.parse(o.body).startUrl).searchParams.get('order_by'),'publication_time');return Response.json({data:{id:'run'+starts,defaultDatasetId:'data'+starts,status:'SUCCEEDED',usageTotalUsd:cap?.25:.01}});}
 if(u.pathname.includes('/actor-runs/')){const id=u.pathname.split('/').at(-1);return Response.json({data:{id,defaultDatasetId:'data'+id.slice(3),status:'SUCCEEDED',usageTotalUsd:.01}});}
 if(!u.pathname.endsWith('/items')){metadataCalls++;return Response.json({data:{id:u.pathname.split('/').at(-1),itemCount:metadataCalls===1?0:27,cleanItemCount:26,modifiedAt:'2000-01-01T00:00:00Z'}});}
 assert.equal(u.searchParams.get('clean'),'false');assert.equal(u.searchParams.get('skipEmpty'),'false');assert.equal(u.searchParams.get('skipHidden'),'true');const offset=Number(u.searchParams.get('offset'));offsets.push(offset);return Response.json(rows.slice(offset,offset+10));
};
try{
 const s=(await api.readVacancyParsingSources(db)).find(s=>s.sourceId==='hh');await api.saveVacancyParsingSource(db,s.companyId,'hh',{enabled:true,sourceUrl:s.url,config:s.config,revision:s.revision});
 let result=await api.fetchVacancyListing(env,s.companyId,'hh',{userName:'Eugene'}),n=0;
 assert.equal(result.jobs.length,8,'two bad rows do not discard the eight good rows');assert.equal(result.paused.apify.offset,10);
 assert.equal(result.paused.apify.orderBy,'publication_time','ordering is persisted before a new scan starts');
 result=await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');
 while(result.paused&&n++<10){result=await api.resumeVacancyDiscoveryRun(env,result.runId,'Eugene');result=await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');}
 assert.equal(n,2);assert.deepEqual(offsets,[0,10,20]);assert.equal(starts,1);assert.equal(result.rawRows,27);assert.equal(result.quarantinedCount,3);assert.equal(result.emptyCount,1);assert.equal(result.found,24);assert.equal(result.status,'partial');assert.equal(result.coverage.datasetComplete,true);assert.equal(result.coverage.hhComplete,false);
 assert.equal(result.errors[0].code,'apify_partial_data');assert.equal(result.quarantine.length,3);assert.ok(!JSON.stringify(result).includes('PRIVATE_BAD_BODY'));assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,24);
 assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').get(s.companyId,'hh').last_success_at,null);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM vacancy_candidates WHERE external_job_id='19'").get().n,0,'employer scope remains mandatory');
 // A paused pre-upgrade clean cursor must replay raw rows with the same Actor.
 let legacy=await api.fetchVacancyListing(env,s.companyId,'hh',{userName:'Eugene'});legacy=await api.saveVacancyDiscoveryRun(db,legacy.runId,'Eugene');
 delete legacy.paused.apify.rowMode;delete legacy.paused.apify.orderBy;legacy.paused.apify.offset=8;
 sqlite.prepare('UPDATE vacancy_discovery_runs SET result_json=? WHERE id=?').run(JSON.stringify(legacy),legacy.runId);
 const startsBefore=starts;legacy=await api.resumeVacancyDiscoveryRun(env,legacy.runId,'Eugene');assert.equal(offsets.at(-1),0,'ambiguous clean offset is not reused');assert.equal(starts,startsBefore,'upgrade never changes paid operation key');
 assert.equal(legacy.paused.apify.orderBy,undefined,'legacy resume does not silently change ordering');assert.equal(new URL(legacy.paused.pageUrl).searchParams.has('order_by'),false);
 legacy=await api.saveVacancyDiscoveryRun(db,legacy.runId,'Eugene');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,24,'replay deduplicates existing identities');
 console.log('Apify raw pagination: metadata lag, raw offsets past empty/incomplete/foreign rows, per-row quarantine, 24 good saves, partial completeness and no new Actor on resume passed.');
}finally{globalThis.fetch=old;f.close();}
