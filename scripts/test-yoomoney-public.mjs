import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {yooMoneyStoreFromHtml,yooMoneyCatalogUrl} from '../server/services/yoomoney-public-listing.js';
const fixture=JSON.parse(fs.readFileSync('scripts/fixtures/yoomoney-public-catalog.json','utf8')),f=await schedulerFixture(),{api,db,env,sqlite}=f,originalFetch=fetch;
let mode='normal',requests=[],changed;
const store=yooMoneyStoreFromHtml(fixture.storeHtml);assert.equal(store.partuid,'459966367011');assert.equal(store.recid,'1588513391');assert.equal(store.size,6);assert.equal(store.slice,1);
assert.throws(()=>yooMoneyStoreFromHtml(fixture.storeHtml+fixture.storeHtml),/единственный/);assert.throws(()=>yooMoneyCatalogUrl({...store,partuid:'https://evil.org'}));
globalThis.fetch=async(url,options={})=>{
 const u=new URL(url);assert.ok(['cloudflare-dns.com','jobs.yoomoney.ru','store.tildaapi.com'].includes(u.hostname),'only current public source/DNS/pinned catalog, no Firecrawl/AI');
 if(u.hostname==='cloudflare-dns.com')return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});if(options.method==='HEAD')return new Response(null,{status:200});
 assert.equal(options.redirect,'manual');assert.equal(options.headers?.authorization,undefined);requests.push(url);if(changed)await changed();
 if(u.hostname==='jobs.yoomoney.ru')return new Response(u.pathname==='/'?'<a href="/vacancies">Хочу в команду</a>':mode==='missing-store'?'<html>no catalog</html>':fixture.storeHtml,{headers:{'content-type':'text/html'}});
 assert.equal(u.pathname,'/api/getproductslist/');assert.equal(u.searchParams.get('storepartuid'),'459966367011');assert.equal(u.searchParams.get('recid'),'1588513391');assert.equal(u.searchParams.get('size'),'6');assert.equal(u.searchParams.has('filters'),false);
 if(mode==='blocked')return new Response('Forbidden',{status:403});
 const page=Number(u.searchParams.get('slice')||1),data=structuredClone(fixture['page'+page]);
 if(mode==='repeat'){Object.assign(data,structuredClone(fixture.page1),{slice:page,total:12});if(page===2)delete data.nextslice;}
 if(mode==='total-change'&&page===2)data.total=10;
 if(mode==='bad-link'&&page===1)data.products[0].url='https://evil.org/manager';
 if(mode==='wrong-part')data.partuid=123;
 if(mode==='malformed')data.products=[null];
 if(mode==='empty'){data.total=0;data.products=[];delete data.nextslice;}
 return Response.json(data);
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=[{...catalog.companies[0],name:'ЮMoney',vacancySources:{companySite:'https://jobs.yoomoney.ru/'}}];catalog.agencies=[];sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 let fc=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC-YOOMONEY',revision:fc.revision});fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,favoritesOnly:false,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 let settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,favoritesOnly:false,sourceIds:['companySite'],dayBudgetUsd:0,monthBudgetUsd:0,revision:settings.revision},'Eugene');
 let source=(await api.readVacancyParsingSources(db)).find(s=>s.companyName==='ЮMoney'&&s.sourceId==='companySite');source=await api.saveVacancyParsingSource(db,source.companyId,source.sourceId,{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});
 const fresh=()=>sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=?').get(source.companyId).last_success_at;
 let scan=await api.fetchVacancyListing(env,source.companyId,source.sourceId,{userName:'Eugene',maxPagesPerSlice:1});assert.equal(scan.provider,'yoomoney-public');assert.equal(scan.requests,1);assert.equal(scan.found,0);assert.equal(scan.paused.code,'slice');await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(fresh(),null);
 scan=await api.resumeVacancyDiscoveryRun(env,scan.runId,'Eugene',{maxPagesPerSlice:1});assert.equal(scan.yoomoney.phase,'catalog');assert.equal(scan.found,0);assert.equal(scan.requests,2);await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(fresh(),null);
 scan=await api.resumeVacancyDiscoveryRun(env,scan.runId,'Eugene',{maxPagesPerSlice:1});assert.equal(scan.found,6);assert.equal(scan.status,'partial');assert.equal(scan.requests,3);assert.equal(scan.yoomoney.slice,2);let saved=await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene',{maxJobs:3});assert.equal(saved.savePending,true);await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(requests.length,3);assert.equal(fresh(),null);
 scan=await api.resumeVacancyDiscoveryRun(env,scan.runId,'Eugene',{maxPagesPerSlice:1});assert.equal(scan.found,9);assert.equal(scan.status,'complete');assert.equal(scan.paused,null);assert.equal(scan.requests,4);assert.equal(scan.creditsReported,0);assert.equal(scan.yoomoney.total,9);assert.equal(scan.coverage.complete,true);assert.deepEqual(scan.errors,[]);assert.deepEqual(scan.warnings,[]);
 saved=await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene',{maxJobs:1});assert.equal(saved.savePending,true);assert.equal(fresh(),null);saved=await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(saved.savePending,false);const success=fresh();assert.ok(success);
 await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(requests.length,4,'save/replay never repeats public acquisition');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,9);assert.equal(sqlite.prepare("SELECT count(*) n FROM vacancy_candidates WHERE acquisition_method='site' AND source_url='https://jobs.yoomoney.ru/vacancies'").get().n,9);assert.equal(sqlite.prepare('SELECT count(*) n FROM firecrawl_usage_events').get().n,0,'no paid Firecrawl events');
 const full=async()=>{let r=await api.fetchVacancyListing(env,source.companyId,source.sourceId,{userName:'Eugene'});while(r.paused)r=await api.resumeVacancyDiscoveryRun(env,r.runId,'Eugene');return r;};
 mode='repeat';scan=await full();assert.equal(scan.status,'partial');assert.equal(scan.found,6);assert.match(scan.errors[0].message,/повторил/);await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(fresh(),success);
 mode='total-change';scan=await full();assert.equal(scan.status,'partial');assert.equal(scan.found,6);assert.match(scan.errors[0].message,/изменилось/);
 mode='bad-link';scan=await full();assert.equal(scan.status,'partial');assert.equal(scan.found,8);assert.ok(scan.pages.some(p=>p.diagnostics?.unpaired));assert.equal(scan.errors[0].code,'listing_incomplete');
 mode='blocked';const at=requests.length;scan=await full();assert.equal(scan.status,'error');assert.equal(scan.errors[0].code,'blocked');assert.equal(requests.length-at,3,'one blocked request, no proxy or retry');assert.equal(fresh(),success);
 mode='wrong-part';scan=await full();assert.equal(scan.status,'error');assert.equal(scan.found,0);assert.equal(scan.errors[0].code,'listing_incomplete');
 mode='malformed';scan=await full();assert.equal(scan.status,'error');assert.equal(scan.errors[0].code,'listing_incomplete');
 mode='empty';scan=await full();assert.equal(scan.status,'complete');assert.equal(scan.found,0);assert.equal(scan.pages.at(-1).emptyConfirmed,true);
 mode='missing-store';scan=await full();assert.equal(scan.status,'error');assert.equal(scan.found,0);
 mode='normal';changed=async()=>sqlite.prepare('UPDATE vacancy_source_parsing SET enabled=0 WHERE company_id=?').run(source.companyId);await assert.rejects(api.fetchVacancyListing(env,source.companyId,source.sourceId,{userName:'Eugene'}),e=>e.code==='changed');assert.equal(sqlite.prepare('SELECT count(*) n FROM firecrawl_request_lease').get().n,0,'source changes release the global lease');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);
 console.log('YooMoney public catalog: observed seed/store/6+3 pages, durable slicing/save/replay, exact identity/host/continuation/total/repeated-page guards, honest freshness, no Firecrawl/AI charges and source-change lease cleanup passed.');
}finally{globalThis.fetch=originalFetch;f.close();}
