import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {hhTitleQuery,hhTitleFilterEvidence} from '../server/services/hh-title-filter.js';
import {hhInput} from '../server/integrations/apify.js';
import {hhUrlEvidence} from '../server/services/apify-evidence.js';
const it=hhTitleQuery(['root-it-manager']),both=hhTitleQuery(['root-journalist','root-it-manager']);
assert.ok(it.startsWith('NAME:('));assert.match(it,/owner/);assert.match(it,/лид\*/);assert.match(it,/владел\*/);assert.match(it,/продукт\*/);assert.doesNotMatch(it,/плат[её]ж|fintech|SberPay/i);
assert.doesNotMatch(it,/журналист/);assert.match(both,/журналист/);assert.equal(hhTitleQuery(['unknown']),null);assert.equal(hhTitleQuery([]),null);
assert.equal(hhTitleFilterEvidence(both),'role_titles_v1');
const filtered=hhInput('https://hh.ru/search/vacancy?employer_id=42&area=1&text=existing&order_by=relevance',{page:3,orderBy:'publication_time',titleQuery:it}),url=new URL(filtered.startUrl);
assert.equal(url.searchParams.get('employer_id'),'42');assert.equal(url.searchParams.get('area'),'1');assert.equal(url.searchParams.get('page'),'3');assert.equal(url.searchParams.get('text'),'(existing) AND ('+it+')');assert.equal(url.searchParams.get('order_by'),'relevance');
assert.equal(hhInput('https://hh.ru/vacancy/123',{detail:true,titleQuery:it}).startUrl,'https://hh.ru/vacancy/123','detail never filtered');
assert.equal(hhUrlEvidence(hhInput('https://hh.ru/employer/42',{titleQuery:both}).startUrl).titleFilter,'role_titles_v1');
const f=await schedulerFixture(),{api,db,env,sqlite}=f,original=fetch;let starts=0,bodies=[],pageSize=100;
globalThis.fetch=async(input,options={})=>{
 const u=new URL(input);assert.equal(u.hostname,'api.apify.com');
 if(u.pathname.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{id:'FREE',monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5}}});
 if(u.pathname.endsWith('/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:'2026-10-01T00:00:00Z',endAt:'2026-11-01T00:00:00Z'},limits:{maxMonthlyUsageUsd:5},current:{monthlyUsageUsd:0}}});
 if(options.method==='POST'){starts++;bodies.push(JSON.parse(options.body));return Response.json({data:{id:'run'+starts,defaultDatasetId:'data'+starts,status:'SUCCEEDED',usageTotalUsd:.01,finishedAt:'2000-01-01T00:00:00Z'}});}
 if(u.pathname.includes('/actor-runs/')){const n=u.pathname.split('/').at(-1).slice(3);return Response.json({data:{id:'run'+n,defaultDatasetId:'data'+n,status:'SUCCEEDED',usageTotalUsd:.01,finishedAt:'2000-01-01T00:00:00Z'}});}
 if(u.pathname.includes('/datasets/')){
  const n=Number(u.pathname.split('/')[3].slice(4));if(!u.pathname.endsWith('/items'))return Response.json({data:{id:'data'+n,itemCount:pageSize,modifiedAt:'2000-01-01T00:00:00Z'}});
  const offset=Number(u.searchParams.get('offset'));return Response.json(Array.from({length:Math.max(0,Math.min(10,pageSize-offset))},(_,i)=>({vacancy_id:String(n*1000+offset+i+1),url:'https://hh.ru/vacancy/'+(n*1000+offset+i+1),title:'Product Owner',employer_id:'42'})));
 }
 throw Error('Unexpected external request');
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];catalog.companies[0].vacancySources={hh:'https://hh.ru/employer/42'};sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(catalog.companies[0].name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(catalog.companies[0].name);
 sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_SYNTHETIC_FILTER_1234'));
 const source=(await api.readVacancyParsingSources(db)).find(s=>s.sourceId==='hh');await api.saveVacancyParsingSource(db,source.companyId,'hh',{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});
 let r=await api.fetchVacancyListing(env,source.companyId,'hh',{userName:'Eugene'});assert.equal(r.titleFilter.status,'applied');assert.equal(new URL(bodies[0].startUrl).searchParams.get('text'),both);r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');
 sqlite.prepare("UPDATE vacancy_role_classification SET parsing_enabled=0 WHERE role_id=(SELECT id FROM vacancy_roles WHERE stable_key='root-journalist')").run();
 let n=0;while(new URL(bodies.at(-1).startUrl).searchParams.get('page')==='0'&&n++<20){r=await api.resumeVacancyDiscoveryRun(env,r.runId,'Eugene');r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');}
 assert.ok(n<20);assert.equal(starts,2);assert.equal(new URL(bodies[1].startUrl).searchParams.get('text'),both,'frozen query survives role setting changes and pagination');
 // A paid legacy cursor without a filter keeps its identity and query on page2.
 const row=sqlite.prepare('SELECT result_json FROM vacancy_discovery_runs WHERE id=?').get(r.runId),legacy=JSON.parse(row.result_json);delete legacy.paused.apify.titleFilter;delete legacy.titleFilter;sqlite.prepare('UPDATE vacancy_discovery_runs SET result_json=? WHERE id=?').run(JSON.stringify(legacy),r.runId);
 n=0;while(starts<3&&n++<20){r=await api.resumeVacancyDiscoveryRun(env,r.runId,'Eugene');r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');}
 assert.equal(new URL(bodies[2].startUrl).searchParams.has('text'),false,'legacy continuation is never silently narrowed');
 pageSize=1;const fresh=await api.fetchVacancyListing(env,source.companyId,'hh',{userName:'Eugene'});assert.equal(fresh.titleFilter.query,it,'next new scan reflects active roots');
 let settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['hh'],dayBudgetUsd:0,monthBudgetUsd:0,revision:settings.revision},'Eugene');
 const key='hh-filter-check:'+source.companyId+':'+crypto.randomUUID(),before=starts;
 const run=await api.runScheduledSeriesTick(env,key);assert.equal(run.errors,0);assert.equal(starts,before+1,'explicit live check executes exactly one normal Actor stage');
 assert.equal((await api.runScheduledSeriesTick(env,key)).reused,true);assert.equal(starts,before+1,'same request receipt cannot repeat start');
 await api.runScheduledSeriesTick(env,'hh-filter-check:'+source.companyId+':'+crypto.randomUUID());assert.equal(starts,before+1,'new UUID cannot restart completed filtered scan');
 settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:false,sourceIds:['hh'],dayBudgetUsd:0,monthBudgetUsd:0,revision:settings.revision},'Eugene');await assert.rejects(()=>api.runScheduledSeriesTick(env,'hh-filter-check:'+source.companyId+':'+crypto.randomUUID()),e=>e.code==='source_scope');assert.equal(starts,before+1);
 await assert.rejects(()=>api.runScheduledSeriesTick(env,'hh-filter-check:999999:'+crypto.randomUUID()));
 console.log('HH prefilter: title-only broad OR, roles/no sector, employer/area/custom query preservation, paid INPUT, frozen pages, legacy cursor, next-scan role changes, detail unchanged and foreign scope passed');
}finally{globalThis.fetch=original;f.close();}
