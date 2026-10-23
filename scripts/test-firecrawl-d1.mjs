import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');await prepareBackend();const api=await import('../.tests/backend.mjs');
let calls=0,hold=false,release,pending=false;
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'isolated-firecrawl'},bindings:{SITE_PASSWORD:'synthetic',SESSION_SECRET:'fc-d1-secret'},outboundService:async request=>{
 const url=new URL(request.url);if(url.href==='https://api.firecrawl.dev/v2/team/credit-usage')return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});if(url.hostname==='cloudflare-dns.com')return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});if(request.method==='HEAD')return new Response(null,{status:200});
 assert.equal(url.href,'https://api.firecrawl.dev/v2/scrape');calls++;const input=await request.json();if(hold)await new Promise(r=>{pending=true;release=r;});
 if(input.url.includes('rabota.ru')){assert.equal(input.url,'https://www.rabota.ru/company/sberbank/vacancies/');return Response.json({success:true,data:{markdown:'[Product Owner (AI)](/vacancy/54443972/)\nВакансии с 1 по 30\nНайдено 196 свежих вакансий в Москве',metadata:{statusCode:200,url:input.url,creditsUsed:1}}});}
 return Response.json({success:true,data:{markdown:'[Архитектор платформы](/vacancies/42)',links:['/vacancies/42'],metadata:{statusCode:200,url:input.url,creditsUsed:1}}});
}};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const login=await mf.dispatchFetch('https://fc.test/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'synthetic'}),redirect:'manual'});const cookie=login.headers.get('set-cookie').split(';')[0];
 const req=(path,method='GET',body)=>mf.dispatchFetch('https://fc.test'+path,{method,headers:{cookie,origin:'https://fc.test','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 let settings=await (await req('/api/firecrawl/settings')).json();settings=await (await req('/api/firecrawl/key','PUT',{apiKey:'fc-SYNTHETIC-D1-KEY',revision:settings.revision})).json();settings=await (await req('/api/firecrawl/settings','PUT',{enabled:true,favoritesOnly:false,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:settings.revision})).json();
 const source=(await (await req('/api/firecrawl/sources')).json()).sources.find(s=>s.sourceId==='companySite');let response=await req('/api/firecrawl/sources/'+source.companyId+'/'+source.sourceId,'PUT',{sourceUrl:source.url,enabled:true,revision:source.revision,config:source.config});assert.equal(response.status,200,await response.clone().text());
 const testBody={companyId:source.companyId,sourceId:source.sourceId};
 const setScope=async favoritesOnly=>{const current=await (await req('/api/firecrawl/settings')).json();const response=await req('/api/firecrawl/settings','PUT',{enabled:true,favoritesOnly,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:current.revision});assert.equal(response.status,200,await response.clone().text());};
 await db.prepare("INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)").bind(source.companyName).run();
 await setScope(true);let denied=await req('/api/firecrawl/test','POST',testBody);assert.equal(denied.status,403);assert.equal(calls,0);
 await db.prepare("INSERT OR REPLACE INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").bind(source.companyName).run();
 hold=true;const operation=req('/api/firecrawl/test','POST',testBody);for(let i=0;i<2000&&!pending;i++)await new Promise(r=>setTimeout(r,1));if(!pending){const early=await operation;throw new Error('Provider not reached: '+early.status+' '+await early.text());}
 assert.equal((await req('/api/firecrawl/test','POST',testBody)).status,409);assert.equal(calls,1);hold=false;release();response=await operation;assert.equal(response.status,200,await response.clone().text());const result=await response.json();assert.equal(result.found,1);assert.equal((await db.prepare('SELECT count(*) n FROM vacancy_candidates').first()).n,0);
 response=await req('/api/firecrawl/runs/'+result.runId+'/save','POST',{});assert.equal(response.status,200,await response.clone().text());assert.equal((await response.json()).savedNew,1);assert.equal(calls,1);
 response=await req('/api/firecrawl/runs/'+result.runId+'/save','POST',{});assert.equal((await response.json()).savedExisting,1);
 await db.prepare("UPDATE company_user_state SET favorite=0 WHERE user_name='Eugene' AND company_name=?").bind(source.companyName).run();
 assert.equal((await req('/api/firecrawl/runs/'+result.runId+'/save','POST',{})).status,403);assert.equal(calls,1);
 await setScope(false);assert.equal((await req('/api/firecrawl/runs/'+result.runId+'/save','POST',{})).status,200);
 await db.prepare("UPDATE company_user_state SET favorite=1 WHERE user_name='Eugene' AND company_name=?").bind(source.companyName).run();await setScope(true);
 hold=true;pending=false;const unfavorited=req('/api/firecrawl/test','POST',testBody);for(let i=0;i<2000&&!pending;i++)await new Promise(r=>setTimeout(r,1));assert.ok(pending);
 await db.prepare("UPDATE company_user_state SET favorite=0 WHERE user_name='Eugene' AND company_name=?").bind(source.companyName).run();hold=false;release();response=await unfavorited;assert.equal(response.status,403,await response.clone().text());assert.equal((await response.json()).code,'favorites_only');
 await db.prepare("UPDATE company_user_state SET favorite=1 WHERE user_name='Eugene' AND company_name=?").bind(source.companyName).run();
 const catalog=await api.readAppCatalog(db);catalog.companies.find(c=>c.name===source.companyName).vacancySources.rabota='https://www.rabota.ru/company/sberbank/';await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data_json=excluded.data_json').bind(JSON.stringify(catalog)).run();
 const rabota=(await api.readVacancyParsingSources(db,{companyId:source.companyId})).find(s=>s.sourceId==='rabota');assert.equal(rabota.supported,true);
 await api.saveVacancyParsingSource(db,rabota.companyId,'rabota',{enabled:true,revision:rabota.revision,sourceUrl:rabota.url,config:rabota.config});
 const partialResponse=await req('/api/firecrawl/test','POST',{companyId:rabota.companyId,sourceId:'rabota'}),partial=await partialResponse.json();assert.equal(partialResponse.status,200,JSON.stringify(partial));assert.equal(partial.status,'partial',JSON.stringify(partial));assert.equal(partial.found,1);assert.equal(partial.errors[0].code,'listing_incomplete');assert.equal(partial.source.url,rabota.url,'catalog URL/revision fence retained');assert.equal(partial.pages[0].finalUrl,'https://www.rabota.ru/company/sberbank/vacancies/');
 const savedPartial=await api.saveVacancyDiscoveryRun(db,partial.runId,'Eugene');assert.equal(savedPartial.savedNew,1,'valid partial pairs remain available to the normal pipeline');
 const backup=await api.createBackup(db);assert.ok(!backup.storage.tables.some(t=>t.name==='vacancy_discovery_runs'));
 // A restore while provider is suspended must fence its final preview writes.
 hold=true;pending=false;const interrupted=req('/api/firecrawl/test','POST',testBody);for(let i=0;i<2000&&!pending;i++)await new Promise(r=>setTimeout(r,1));assert.ok(pending);await api.restoreBackup(db,backup);hold=false;release();response=await interrupted;assert.equal(response.status,409,await response.clone().text());assert.equal((await response.json()).code,'restored');assert.equal((await req('/api/firecrawl/runs/'+result.runId+'/save','POST',{})).status,410);
 assert.equal((await db.prepare('SELECT count(*) n FROM company_vacancies').first()).n,0);assert.equal((await db.prepare('PRAGMA foreign_key_check').all()).results.length,0);
 console.log('Native workerd/D1 Firecrawl: sequential lease across requests, no preview staging writes, server-owned save/idempotency/no extra scrape, backup and restore-in-flight fencing passed');
}finally{await mf.dispose();}
