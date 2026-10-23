import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,originalFetch=fetch;let scrapes=0;
globalThis.fetch=async(url,options={})=>{
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});
 if(options.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('/team/credit-usage'))return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');scrapes++;
 return Response.json({success:true,data:{markdown:'###### Не смогли загрузить данные\nПопробуйте снова.\n##### Вакансий не найдено',links:[],metadata:{statusCode:200,url:JSON.parse(options.body).url,creditsUsed:1}}});
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];catalog.companies[0].vacancySources={getmatch:'https://getmatch.ru/companies/fixture'};sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 let fc=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC-FAILURE',revision:fc.revision});fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,concurrency:1,maxPages:3,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 let settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,favoritesOnly:false,sourceIds:['getmatch'],dayBudgetUsd:0,monthBudgetUsd:0,revision:settings.revision},'Eugene');settings=await api.schedulerSettings(db);
 let source=(await api.readVacancyParsingSources(db)).find(s=>s.sourceId==='getmatch');source=await api.saveVacancyParsingSource(db,source.companyId,source.sourceId,{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});
 const key='source-page-check:'+source.companyId+':getmatch:page:'+crypto.randomUUID(),probe=await api.runScheduledSeriesTick(env,key);assert.equal(probe.code,'listing_unavailable');assert.equal(probe.report.page.configured.emptyConfirmed,false);assert.equal(scrapes,1);
 assert.equal((await api.runScheduledSeriesTick(env,key)).reused,true);assert.equal(scrapes,1);
 // Use the normal dispatcher and saved source result, with no blind paid retry.
 const job=await api.enqueueScheduledJob(db,{type:'scan_source',key:'unavailable-fixture',companyId:source.companyId,sourceId:'getmatch',payload:{manual:true,sourceRevision:source.revision,sourceUrl:source.url}});
 const requestId=crypto.randomUUID(),started=Date.now();await api.runDueTasks(env,{requestId,jobId:job.id,limit:1});
 const failed=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(job.id);assert.equal(failed.status,'failed');assert.equal(failed.reason_code,'listing_unavailable');assert.match(failed.last_error,/Площадка не смогла/);assert.equal(scrapes,2);
 const row=sqlite.prepare("SELECT * FROM vacancy_source_parsing WHERE company_id=? AND source_id='getmatch'").get(source.companyId);assert.equal(row.last_success_at,null);assert.ok(row.next_run_at>=started+settings.intervalDays*86400000);assert.equal(row.last_result,'failed');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,0);
 await api.runDueTasks(env,{requestId,jobId:job.id,limit:1});assert.equal(scrapes,2,'receipt replay cannot repeat a paid fetch');
 console.log('Listing source failures: Getmatch error vs empty, bounded source recheck interval, retained error history, no candidates/freshness or immediate paid retries, diagnostic replay passed.');
}finally{globalThis.fetch=originalFetch;f.close();}
