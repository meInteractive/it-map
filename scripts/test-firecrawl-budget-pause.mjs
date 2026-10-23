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



const originalFetch=globalThis.fetch;let left=1000,plan=1000,period={billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'},paid=0,checks=0,urls=[];
globalThis.fetch=async(url,options={})=>{
 if(url.startsWith('https://cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});
 if(options.method==='HEAD'){const u=new URL(url);if(u.searchParams.get('page')==='2'&&!u.searchParams.has('redirected')){u.searchParams.set('redirected','1');return new Response(null,{status:302,headers:{location:u.href}});}return new Response(null,{status:200});}
 if(url.endsWith('credit-usage')){checks++;return Response.json({success:true,data:{remainingCredits:left,planCredits:plan,...period}});}
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');paid++;const input=JSON.parse(options.body);urls.push(input.url);const page=Number(new URL(input.url).searchParams.get('page')||1);
 if(page===1)left=3;
 return Response.json({success:true,data:{markdown:'[Архитектор '+page+'](/vacancies/'+page+')'+(page<3?' [Следующая](?page='+(page+1)+(new URL(input.url).searchParams.has('redirected')?'&redirected=1':'')+')':''),links:['/vacancies/'+page],metadata:{statusCode:200,url:input.url,creditsUsed:1}}});
};
try{
 let state=await (await request('/api/firecrawl/settings')).json();state=await (await request('/api/firecrawl/key','PUT',{apiKey:'fc-SYNTHETIC-FREE-KEY',revision:state.revision})).json();state=await (await request('/api/firecrawl/settings','PUT',{enabled:true,favoritesOnly:false,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:state.revision})).json();
 assert.equal((await request('/api/firecrawl/usage','GET',undefined,user)).status,403);
 let view=await (await request('/api/firecrawl/usage')).json();assert.equal(view.fresh,false);assert.equal(view.budget,null);
 let response=await request('/api/firecrawl/usage/refresh','POST',{});assert.equal(response.status,200,await response.clone().text());view=await response.json();assert.equal(view.fresh,true);assert.equal(view.budget.remaining,1000);assert.equal(paid,0);
 let source=(await (await request('/api/firecrawl/sources')).json()).sources.find(s=>s.companyName===company&&s.sourceId==='companySite');source=await (await request('/api/firecrawl/sources/'+source.companyId+'/'+source.sourceId,'PUT',{enabled:true,sourceUrl:source.url,revision:source.revision,config:{...source.config,pagination:{queryParam:'page'}}})).json();
 response=await request('/api/firecrawl/test','POST',{companyId:source.companyId,sourceId:source.sourceId});assert.equal(response.status,200,await response.clone().text());let result=await response.json();assert.equal(result.found,1);assert.equal(result.paused.code,'free_exhausted');assert.equal(result.paused.remainingPages,2);assert.ok(result.paused.visited.every(u=>!u.includes('page=2')),'failed redirect target remains unprocessed');assert.equal(paid,1);assert.equal(result.creditsReported,1);assert.ok(result.paused.pageUrl.includes('page=2'));
 sqlite.prepare('UPDATE vacancy_discovery_runs SET expires_at=1 WHERE id=?').run(result.runId);
 let pending=await (await request('/api/firecrawl/pending?companyId='+source.companyId+'&sourceId='+source.sourceId)).json();assert.equal(pending.runs.length,1);assert.equal((await request('/api/firecrawl/runs/'+result.runId)).status,200,'paused survives normal TTL');
 sqlite.exec('UPDATE firecrawl_settings SET favorites_only=1,revision=revision+1 WHERE id=1');
 assert.equal((await (await request('/api/firecrawl/pending')).json()).runs.length,0,'excluded paused company is hidden');
 const beforeScope=paid;for(const suffix of ['', '/save','/resume']){const denied=await request('/api/firecrawl/runs/'+result.runId+suffix,suffix?'POST':'GET',suffix?{}:undefined);assert.equal(denied.status,403);assert.equal((await denied.json()).code,'favorites_only');}assert.equal(paid,beforeScope,'blocked resume/save never calls provider');
 sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(source.companyName);sqlite.prepare("INSERT OR REPLACE INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(source.companyName);
 assert.equal((await (await request('/api/firecrawl/pending')).json()).runs.length,1,'favorite brings paused run back');
 const count=paid;response=await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{});assert.equal(response.status,200,await response.clone().text());assert.equal((await response.json()).savedNew,1);assert.equal(paid,count,'save invokes no provider');
 // API-confirmed new billing period, not a guessed calendar reset/top-up.
 period={billingPeriodStart:new Date(Date.now()-3600000).toISOString(),billingPeriodEnd:new Date(Date.now()+29*86400000).toISOString()};left=1000;
 response=await request('/api/firecrawl/runs/'+result.runId+'/resume','POST',{});assert.equal(response.status,200,await response.clone().text());result=await response.json();assert.equal(result.runId,pending.runs[0].runId);assert.equal(result.paused,null);assert.equal(result.found,3);assert.equal(paid,3);assert.equal(urls.filter(u=>!u.includes('page=')).length,1,'resume does not refetch page1');assert.equal(result.loadedPages,3);assert.equal(result.creditsReported,3);
 pending=await (await request('/api/firecrawl/pending')).json();assert.equal(pending.runs.length,0);
 response=await request('/api/firecrawl/runs/'+result.runId+'/save','POST',{});let saved=await response.json();assert.equal(saved.savedNew,2);assert.equal(saved.savedExisting,1);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,3);
 const row=sqlite.prepare('SELECT free_usage_json FROM firecrawl_settings').get(),old=JSON.parse(row.free_usage_json);old.checkedAt=Date.now()-61000;sqlite.prepare('UPDATE firecrawl_settings SET free_usage_json=?').run(JSON.stringify(old));view=await (await request('/api/firecrawl/usage')).json();assert.equal(view.fresh,false);assert.equal(view.budget,null,'stale values are not presented as current');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_classification_attempts').get().n,0);
 console.log('Budget/pause API: admin statistics, cheap refresh, known expense, budget stop before page2, durable pending/TTL, save without provider, exact-period resume through redirects without refetch, dedupe, stale hiding and no main/JEV passed');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
