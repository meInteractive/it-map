import assert from 'node:assert/strict';
import fs from 'node:fs';
import {extractVacancyListing} from '../server/services/vacancy-listing-extractor.js';
import {schedulerFixture} from './scheduler-test-support.mjs';
const fixture=JSON.parse(fs.readFileSync('scripts/fixtures/yoomoney-listing.json','utf8'));
const config={adapter:'yoomoney',html:true},options={pageUrl:fixture.sourceUrl,hosts:['jobs.yoomoney.ru'],config};
const result=extractVacancyListing(fixture,options);
assert.deepEqual(result.jobs.map(j=>[j.title,new URL(j.url).pathname]),[['Менеджер по продажам','/partner-dev-manager'],['Менеджер B2B для продукта «Выплаты»','/b2b-manager-payments'],['Финансовый менеджер','/financial-manager']]);
assert.equal(result.nextUrl,'https://jobs.yoomoney.ru/vacancies');assert.equal(result.diagnostics.ambiguous,0);assert.equal(result.diagnostics.unpaired,0);assert.equal(result.diagnostics.incomplete,null,'the explicit full-list continuation can be followed');
const partial=extractVacancyListing(fixture,{...options,pageUrl:'https://jobs.yoomoney.ru/vacancies'});assert.ok(partial.diagnostics.incomplete);assert.equal(partial.nextUrl,null,'a load-more button never causes URL guessing');
const unrelated=extractVacancyListing({html:'<a href="/financial-manager"><h3>Salary report</h3></a><a href="https://evil.org/manager"><div class="js-store-prod-name">Wrong host</div></a>',markdown:'[Мошеннический заголовок](/manager)'},options);assert.equal(unrelated.jobs.length,0,'only the enclosing same-host Tilda card can supply a title');
const filteredLink=extractVacancyListing({html:fixture.html.replaceAll('https://jobs.yoomoney.ru/vacancies','https://jobs.yoomoney.ru/vacancies?direction=hr'),markdown:''},options);assert.equal(filteredLink.nextUrl,null,'a filtered subset is never the full-list continuation');assert.ok(filteredLink.diagnostics.incomplete);
const f=await schedulerFixture(),{api,db,env,sqlite}=f,originalFetch=fetch;let scrapes=0;
globalThis.fetch=async(url,request={})=>{
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});if(request.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('/team/credit-usage'))return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');scrapes++;const input=JSON.parse(request.body);assert.deepEqual(input.formats,['markdown','links','html']);assert.ok([fixture.sourceUrl,result.nextUrl].includes(input.url));
 return Response.json({success:true,data:{...fixture,metadata:{statusCode:200,url:input.url,creditsUsed:1}}});
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=[{...catalog.companies[0],name:'ЮMoney',vacancySources:{companySite:fixture.sourceUrl}}];catalog.agencies=[];sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 let fc=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC-YOOMONEY',revision:fc.revision});fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,favoritesOnly:false,concurrency:1,maxPages:3,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 let settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,favoritesOnly:false,sourceIds:['companySite'],dayBudgetUsd:0,monthBudgetUsd:0,revision:settings.revision},'Eugene');
 let source=(await api.readVacancyParsingSources(db)).find(s=>s.companyName==='ЮMoney'&&s.sourceId==='companySite');assert.equal(source.config.adapter,'yoomoney');assert.equal(source.config.html,true);
 source=await api.saveVacancyParsingSource(db,source.companyId,source.sourceId,{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});
 // A stored, previously untouched generic default works without a data migration.
 sqlite.prepare("UPDATE vacancy_source_parsing SET config_json=? WHERE company_id=? AND source_id='companySite'").run(JSON.stringify({...source.config,adapter:'generic',html:false}),source.companyId);
 source=await api.readVacancyParsingSource(db,source.companyId,source.sourceId);assert.equal(source.config.adapter,'yoomoney');assert.equal(source.config.html,true);
 settings=await api.schedulerSettings(db);
 const legacy=await api.enqueueScheduledJob(db,{type:'scan_source',key:'scan:'+source.companyId+':companySite:'+source.revision+':0:'+settings.revision,companyId:source.companyId,sourceId:'companySite',payload:{sourceRevision:source.revision,sourceUrl:source.url,scheduleRevision:settings.revision,dueAt:0,runId:'existing-paid-cursor'}});
 await api.scheduleDueSources(db,'Eugene');assert.equal(sqlite.prepare('SELECT revision FROM vacancy_source_parsing WHERE company_id=?').get(source.companyId).revision,source.revision,'active legacy cursor is not upgraded or restarted');
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='extraction' WHERE id=?").run(legacy.id);
 const prior=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(legacy.id);
 await api.scheduleDueSources(db,'Eugene');source=await api.readVacancyParsingSource(db,source.companyId,source.sourceId);assert.equal(source.revision,2);assert.deepEqual(sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(legacy.id),prior,'failed receipt and prior paid cursor are retained');
 assert.equal(JSON.parse(sqlite.prepare('SELECT config_json FROM vacancy_source_parsing WHERE company_id=?').get(source.companyId).config_json).adapter,'yoomoney');
 const active=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE company_id=? AND source_id='companySite' AND status='pending'").all(source.companyId);assert.equal(active.length,1);assert.equal(JSON.parse(active[0].payload).sourceRevision,source.revision);assert.equal(JSON.parse(active[0].payload).runId,undefined,'new adapter gets one new discovery, not the failed old preview');
 await api.scheduleDueSources(db,'Eugene');assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE company_id=? AND source_id='companySite'").get(source.companyId).n,2,'one upgrade only');assert.equal(scrapes,0,'scheduling is not a paid request');
 const scan=await api.fetchVacancyListing(env,source.companyId,source.sourceId,{userName:'Eugene',resume:{runId:crypto.randomUUID(),source,sourceRevision:source.revision,provider:'firecrawl',paused:{pageUrl:source.url,visited:[]},jobs:[],pages:[]}});assert.equal(scrapes,2,'one homepage and one explicit full-list page, without any dynamic endpoint');assert.equal(scan.found,3);assert.equal(scan.status,'partial');assert.match(scan.warnings.join(' '),/только часть списка/);
 const saved=await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(saved.savedNew,3);assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=?').get(source.companyId).last_success_at,null);await api.saveVacancyDiscoveryRun(db,scan.runId,'Eugene');assert.equal(scrapes,2);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,3);
 sqlite.prepare("UPDATE scheduler_jobs SET status='cancelled' WHERE status='pending'").run();
 sqlite.prepare("UPDATE vacancy_source_parsing SET config_json=? WHERE company_id=? AND source_id='companySite'").run(JSON.stringify({...source.config,adapter:'generic',html:false}),source.companyId);
 const custom=JSON.stringify({...source.config,adapter:'generic',html:true,titleClass:'custom-title'});let raced=false;
 const racingDb={...db,prepare(sql){const statement=db.prepare(sql);if(sql.startsWith('INSERT INTO vacancy_source_parsing')){const all=statement.all;statement.all=async()=>{raced=true;sqlite.prepare("UPDATE vacancy_source_parsing SET config_json=?,revision=revision+1 WHERE company_id=? AND source_id='companySite'").run(custom,source.companyId);return all.call(statement);};}return statement;}};
 await api.scheduleDueSources(racingDb,'Eugene');assert.equal(raced,true);assert.equal(sqlite.prepare('SELECT config_json FROM vacancy_source_parsing WHERE company_id=?').get(source.companyId).config_json,custom,'a concurrent manual edit wins over the built-in upgrade');assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE company_id=? AND source_id='companySite'").get(source.companyId).n,2,'no stale source job is queued after the CAS loses');
 assert.equal((await api.readVacancyParsingSource(db,source.companyId,source.sourceId)).config.adapter,'generic','manual extraction rules are retained');
 console.log('YooMoney: observed Tilda names/root URLs, exact card/host guards, explicit full-list link, no dynamic guessing, partial persistence/freshness, legacy default upgrade and manual config preservation passed.');
}finally{globalThis.fetch=originalFetch;f.close();}
