import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?firecrawl-settings'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);


const secret='fc-SYNTHETIC-FIRECRAWL-KEY';let providerCalls=0,mode='normal',pagesSeen=[];
const originalFetch=globalThis.fetch;
globalThis.fetch=async(url,options={})=>{
 if(url.startsWith('https://cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});
 if(url==='https://api.firecrawl.dev/v2/team/credit-usage')return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 if(options.method==='HEAD')return new Response(null,{status:200});
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');providerCalls++;const input=JSON.parse(options.body);pagesSeen.push(input.url);assert.equal(input.proxy,'basic');assert.deepEqual(input.formats,['markdown','links']);
 const page=Number(new URL(input.url).searchParams.get('page')||1);
 if(mode==='target')return Response.json({success:true,data:{markdown:'missing',metadata:{statusCode:404}}});
 if(mode==='empty')return Response.json({success:true,data:{markdown:'У компании нет открытых вакансий',links:[],metadata:{statusCode:200}}});
 if(mode==='unpaired')return Response.json({success:true,data:{markdown:'# Вакансии',links:['/vacancies/42'],metadata:{statusCode:200}}});
 if(mode==='mixed'||mode==='sliced'&&page===1)return Response.json({success:true,data:{markdown:'[Архитектор платформы](/vacancies/42)'+(mode==='sliced'?' [Следующая](?page=2)':''),links:['/vacancies/42','/vacancies/999'],metadata:{statusCode:200,url:input.url,creditsUsed:1}}});
 if(mode==='partial'&&page===2)return Response.json({success:true,data:{markdown:'Access denied',metadata:{statusCode:403}}});
 return Response.json({success:true,data:{markdown:'[Архитектор платформы](/vacancies/42) [Инженер](/vacancies/43) [Подробнее](/vacancies/42) [Главная](/)'+(mode==='pages'||mode==='partial'?' [Следующая](?page='+(page+1)+')':''),links:['/vacancies/42','/vacancies/43'],metadata:{statusCode:200,url:input.url,creditsUsed:1}}});
};
try{
 let state=await (await request('/api/firecrawl/settings')).json();state=await (await request('/api/firecrawl/key','PUT',{apiKey:secret,revision:state.revision})).json();state=await (await request('/api/firecrawl/settings','PUT',{enabled:true,favoritesOnly:false,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:state.revision})).json();
 let sources=(await (await request('/api/firecrawl/sources')).json()).sources;
 let source=sources.find(s=>s.companyName===company&&s.sourceId==='companySite');assert.ok(source);assert.equal(source.enabled,false);
 const saveSource=async(enabled=true,config=source.config)=>{const r=await request('/api/firecrawl/sources/'+source.companyId+'/'+source.sourceId,'PUT',{enabled,config,sourceUrl:source.url,revision:source.revision});assert.equal(r.status,200,await r.clone().text());source=await r.json();};
 assert.equal((await request('/api/firecrawl/test','POST',{companyId:source.companyId,sourceId:source.sourceId})).status,400);
 await saveSource();await request('/api/classification/settings');const mainBefore=sqlite.prepare('SELECT * FROM company_vacancies').all(),rolesBefore=sqlite.prepare('SELECT * FROM vacancy_roles').all(),promptsBefore=sqlite.prepare('SELECT * FROM vacancy_role_classification').all();
 const test=async()=>{sqlite.prepare('UPDATE firecrawl_usage_events SET started_at=?').run(Date.now()-61000);const r=await request('/api/firecrawl/test','POST',{companyId:source.companyId,sourceId:source.sourceId});assert.equal(r.status,200,await r.clone().text());return r.json();};
 let result=await test();assert.equal(result.found,2);assert.equal(result.status,'complete');assert.equal(result.requests,1);assert.equal(result.creditsReported,1);assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').get(source.companyId,source.sourceId).last_success_at,null,'preview alone is not a fully saved scan');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,0,'preview makes no candidate writes');
 assert.equal((await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{jobs:[{title:'malicious',url:'http://127.1/'}]})).status,400);
 const callsBefore=providerCalls;
 const partialSave=await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene',{maxJobs:1});assert.equal(partialSave.savePending,true);assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').get(source.companyId,source.sourceId).last_success_at,null,'unfinished save cannot refresh source');
 let response=await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{});assert.equal(response.status,200,await response.clone().text());let saved=await response.json();assert.equal(saved.savedNew,1);assert.ok(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').get(source.companyId,source.sourceId).last_success_at);assert.equal(providerCalls,callsBefore,'save does not call Firecrawl');assert.equal(saved.jobs[0].processingStatus,'pending_classification');
 assert.equal((await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{},user)).status,403);
 response=await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{});saved=await response.json();assert.equal(saved.savedNew,0);assert.equal(saved.savedExisting,2);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,2);
 result=await test();assert.equal(result.existingCount,2);assert.equal(result.newCount,0);assert.equal(result.jobs[0].title,'Архитектор платформы');
 const success='2026-01-01T00:00:00.000Z';sqlite.prepare('UPDATE vacancy_source_parsing SET last_success_at=? WHERE company_id=? AND source_id=?').run(success,source.companyId,source.sourceId);
 const unchanged=()=>assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').get(source.companyId,source.sourceId).last_success_at,success,'partial scan preserves last full success');
 mode='pages';await saveSource(true,{...source.config,pagination:{queryParam:'page'}});sqlite.prepare('UPDATE vacancy_source_parsing SET last_success_at=? WHERE company_id=? AND source_id=?').run(success,source.companyId,source.sourceId);pagesSeen=[];result=await test();assert.equal(result.loadedPages,3);assert.equal(result.requests,3);assert.equal(new Set(pagesSeen).size,3);assert.equal(result.found,2);assert.equal(result.status,'partial');assert.ok(result.warnings.some(w=>w.includes('лимит страниц')));await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');unchanged();
 mode='mixed';result=await test();assert.equal(result.status,'partial');assert.equal(result.errors.length,0);assert.ok(result.pages[0].diagnostics.unpaired);await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');unchanged();
 mode='sliced';result=await api.fetchVacancyListing(env,source.companyId,source.sourceId,{userName:'Eugene',maxPagesPerSlice:1});assert.equal(result.paused.code,'slice');await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');result=await api.resumeVacancyDiscoveryRun(env,result.runId,'Eugene',{maxPagesPerSlice:1});assert.equal(result.paused,null);assert.equal(result.status,'partial','earlier unpaired links survive a clean final slice');await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');unchanged();
 mode='partial';result=await test();assert.equal(result.status,'partial');assert.equal(result.found,2);assert.equal(result.errors[0].code,'blocked');assert.equal(result.loadedPages,1);await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');unchanged();
 mode='target';result=await test();assert.equal(result.status,'error');assert.equal(result.errors[0].code,'target');assert.equal(result.found,0);
 mode='unpaired';result=await test();assert.equal(result.status,'error');assert.equal(result.errors[0].code,'extraction');
 mode='empty';result=await test();assert.equal(result.status,'complete');assert.equal(result.found,0);assert.equal(result.pages[0].emptyConfirmed,true);await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene');assert.notEqual(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').get(source.companyId,source.sourceId).last_success_at,success,'explicit complete empty scan can refresh after save');
 mode='normal';result=await test();await saveSource(false);assert.equal((await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{})).status,409);await saveSource();
 result=await test();sqlite.prepare('UPDATE vacancy_discovery_runs SET expires_at=1 WHERE id=?').run(result.runId);assert.equal((await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{})).status,410);
 const hh=sources.find(s=>s.sourceId==='hh');assert.equal(hh.supported,true);assert.equal(hh.method,'apify');
 assert.deepEqual(sqlite.prepare('SELECT * FROM company_vacancies').all(),mainBefore);assert.deepEqual(sqlite.prepare('SELECT * FROM vacancy_roles').all(),rolesBefore);assert.deepEqual(sqlite.prepare('SELECT * FROM vacancy_role_classification').all(),promptsBefore);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_classification_attempts').get().n,0,'discovery never invokes JEV');
 const backup=await api.createBackup(db);assert.ok(!backup.storage.tables.some(t=>t.name==='vacancy_discovery_runs'));assert.ok(backup.storage.tables.find(t=>t.name==='vacancy_source_parsing').rows.length>0);
 console.log('Discovery API: existing sources only/disabled defaults, ACL, preview without staging writes, server-owned save/no extra scrape, dedupe/status/no JEV, linked pagination cap, partial/errors/empty, stale config/TTL and main/roles/prompts preservation passed');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
