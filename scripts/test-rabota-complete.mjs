import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {rabotaNextPage} from '../server/services/rabota-listing.js';
import {rabotaBodyPresent,rabotaDetailMarkdown,RABOTA_MISSING_BODY,incompleteRabotaBody} from '../server/services/rabota-detail.js';
const pageUrl='https://www.rabota.ru/company/sberbank/vacancies/?city=1';
const pager=n=>'<div class="pagination"><ul><li><a href="https://www.rabota.ru/?page='+n+'">'+n+'</a></li></ul></div>';
assert.equal(rabotaNextPage(pager(2),pageUrl),pageUrl+'&page=2');
assert.equal(rabotaNextPage(pager(4),pageUrl),null);
assert.equal(rabotaNextPage(pager(2).replace('www.rabota.ru/?','evil.org/?'),pageUrl),null);
const empty='# Лидер продукта SberPay Online\nМосква, опыт от 3 лет\nЗадайте вопрос работодателю\n'+'Служебный блок '.repeat(40)+'\n### Смотрите также\n[От 250 000 руб.](/vacancy/250000/)';
assert.equal(rabotaBodyPresent(empty),false);
assert.equal(incompleteRabotaBody({errors:[RABOTA_MISSING_BODY]}),true);
assert.equal(incompleteRabotaBody({errors:[RABOTA_MISSING_BODY,'Страница закрыта']}),false);
assert.equal(incompleteRabotaBody({errors:[]}),false);
const body='Обязанности: управлять продуктом и командой разработки. Требования: опыт в цифровых продуктах. Условия: гибрид. '+'Развитие платёжного продукта и ответственность за результат. '.repeat(8);
const ld=id=>'<script type="application/ld+json">'+JSON.stringify({'@type':'JobPosting',url:'https://www.rabota.ru/vacancy/'+id+'/',title:'Лидер продукта SberPay Online',description:'<p>'+body+'</p>'})+'</script>';
assert.equal(rabotaDetailMarkdown({markdown:empty,html:ld(1)},'https://www.rabota.ru/vacancy/2/'),empty);
assert.equal(rabotaBodyPresent(rabotaDetailMarkdown({markdown:empty,html:ld(2)},'https://www.rabota.ru/vacancy/2/')),true);
const f=await schedulerFixture(),{api,db,env,sqlite}=f,original=fetch;let scrapes=0,repeated=false;
globalThis.fetch=async(input,options={})=>{
 const url=String(input);
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});
 if(options.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('/team/credit-usage'))return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');scrapes++;const request=JSON.parse(options.body),n=Number(new URL(request.url).searchParams.get('page')||1),page=repeated?1:n,start=(page-1)*30+1,end=Math.min(page*30,227);
 return Response.json({success:true,data:{markdown:'Вакансии с '+start+' по '+end+'\nНайдено 227 свежих вакансий',html:Array.from({length:end-start+1},(_,i)=>'<div class="vacancy-preview-card"><h3>Product Owner '+(start+i)+'</h3><a href="/vacancy/'+(100000+start+i)+'/">от 250 000 руб.</a></div>').join('')+(page<8?pager(page+1):''),metadata:{statusCode:200,url:request.url,creditsUsed:1}}});
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,2);catalog.agencies=[];for(const c of catalog.companies)c.vacancySources={rabota:pageUrl};sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 let state=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC_RABOTA_PAGES',revision:state.revision});state=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,favoritesOnly:false,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:state.revision});
 const sources=(await api.readVacancyParsingSources(db)).filter(s=>s.sourceId==='rabota');for(const s of sources)await api.saveVacancyParsingSource(db,s.companyId,'rabota',{enabled:true,revision:s.revision,sourceUrl:s.url,config:s.config});
 const source=sources[0];let result=await api.fetchVacancyListing(env,source.companyId,'rabota',{userName:'Eugene',maxPagesPerSlice:1});
 for(let page=1;page<=8;page++){
  assert.equal(result.loadedPages,page);assert.equal(result.found,Math.min(30*page,227));
  let saved=await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene',{maxJobs:10});let slices=1;
  while(saved.savePending){saved=await api.saveVacancyDiscoveryRun(db,result.runId,'Eugene',{maxJobs:10});slices++;}
  assert.equal(slices,page===8?2:3,'only the new page is saved, without repeating old writes');
  if(page<8){assert.equal(saved.paused.code,'slice');assert.equal(sqlite.prepare("SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id='rabota'").get(source.companyId).last_success_at,null);result=await api.resumeVacancyDiscoveryRun(env,result.runId,'Eugene',{maxPagesPerSlice:1});}
  else{assert.equal(result.paused,null);assert.equal(result.errors.length,0);assert.ok(sqlite.prepare("SELECT last_success_at FROM vacancy_source_parsing WHERE company_id=? AND source_id='rabota'").get(source.companyId).last_success_at);}
 }
 assert.equal(scrapes,8);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,227,'all pages beyond 3 and 200 persisted');
 const first=sqlite.prepare('SELECT * FROM vacancy_candidates ORDER BY id LIMIT 1').get();
 await api.upsertVacancyCandidate(db,{titleRaw:'Different employer title',vacancyUrl:first.vacancy_url,companyId:sources[1].companyId,sourceId:'rabota',acquisitionMethod:'firecrawl'},{preserveCompany:true});
 const after=sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=?').get(first.id);assert.equal(after.company_id,first.company_id);assert.equal(after.title_raw,first.title_raw);
 result=await api.fetchVacancyListing(env,source.companyId,'rabota',{userName:'Eugene',maxPagesPerSlice:1});repeated=true;result=await api.resumeVacancyDiscoveryRun(env,result.runId,'Eugene',{maxPagesPerSlice:1});assert.equal(result.errors[0].code,'listing_incomplete');assert.equal(result.found,30,'ignored page query cannot masquerade as complete');
 console.log('Rabota complete: 8 pages/227 jobs, bounded delta saves, resumable >3 pages/>200 jobs, exact-company pager, repeated-page stop, empty-body/JSON-LD identity and employer preservation passed');
}finally{globalThis.fetch=original;f.close();}
