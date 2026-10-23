import {candidateDb} from '../repositories/vacancy-candidates.js';
import {parserCompanyDb} from '../repositories/parser-company-scope.js';
import {generationBoundDb,boundGeneration} from '../repositories/generation-bound-db.js';
import {readVacancyParsingSources} from '../repositories/vacancy-source-parsing.js';
import {schedulerSettings,schedulerError,enqueueScheduledJob} from '../repositories/scheduler.js';
import {firecrawlSettings,withFirecrawlLease} from '../repositories/firecrawl-settings.js';
import {createFirecrawlUsageSession,firecrawlScrape,preflightListingUrl} from '../integrations/firecrawl.js';
import {fetchVacancyListing} from './vacancy-discovery.js';
import {checkVacancyMarkdown} from './vacancy-fulltext.js';
import {readFirecrawlBudgetView} from './firecrawl-budget-view.js';
import {extractVacancyListing} from './vacancy-listing-extractor.js';
import {rabotaListingCoverage,rabotaListingHosts,rabotaListingUrl} from './rabota-listing.js';
import {leafRoleContext,currentLeafRoles} from './vacancy-leaf-roles.js';
import {readClassificationSettings} from '../repositories/classification-prompts.js';
import {rabotaDetailMarkdown,rabotaBodyPresent} from './rabota-detail.js';

// Owner MCP diagnostics: one saved listing preview OR one explicit numeric card.
// Queue explicitly adds one ordinary source scan; it does not execute providers.
// No candidate decisions, model calls, main imports or changes to settings.
export async function checkRabotaSource(env,companyId,target,{signal}={}){
 const db=await candidateDb(env.DB),settings=await schedulerSettings(db),sources=await readVacancyParsingSources(db,{companyId});
 const source=sources.find(s=>s.sourceId==='rabota');
 const history=(await db.prepare("SELECT company_id,type,status,attempts,reason_code,last_error,updated_at FROM scheduler_jobs WHERE generation=? AND source_id='rabota' ORDER BY updated_at DESC LIMIT 8").bind(boundGeneration(db)).all()).results||[];
 const counts=(await db.prepare("SELECT company_id,count(*) total,sum(CASE WHEN main_vacancy_id IS NOT NULL THEN 1 ELSE 0 END) imported FROM vacancy_candidates WHERE source_id='rabota' GROUP BY company_id").all()).results||[];
 const summary={source:source??null,selected:settings.sourceIds.includes('rabota'),history,counts,titlePrompts:await readClassificationSettings(db),budget:await readFirecrawlBudgetView({...env,DB:db})};
 if(target==='inspect')return summary;
 const explicitSlug=target.match(/^list-([a-z0-9][a-z0-9_-]{0,39})$/)?.[1];
 const pageNumber=target.match(/^page-([1-9]\d{0,2})$/)?.[1],candidateId=target.match(/^candidate-([1-9]\d{0,8})$/)?.[1];
 if(!summary.selected||source&&(!source.supported||!source.enabled)||['list','queue'].includes(target)&&!source)throw schedulerError('source_scope','Нужен выбранный источник rabota.ru; сохранённая ссылка должна быть включена.');
 if(source&&!/(^|\.)rabota\.ru$/.test(new URL(source.url).hostname))throw schedulerError('source_scope','Ссылка источника должна вести на rabota.ru.');
 let scoped=await parserCompanyDb(db,companyId,settings.ownerName);
 scoped=generationBoundDb(scoped,boundGeneration(scoped),{sql:"EXISTS(SELECT 1 FROM firecrawl_settings f WHERE f.id=1 AND EXISTS(SELECT 1 FROM json_each(json_extract(f.scheduler_json,'$.sourceIds')) WHERE value='rabota') AND (?=1 OR EXISTS(SELECT 1 FROM vacancy_source_parsing s WHERE s.company_id=? AND s.source_id='rabota' AND s.source_url=? AND s.revision=? AND s.enabled=1)))",bindings:[source?0:1,companyId,source?.url??'',source?.revision??0],failure:()=>schedulerError('source_scope','Источник rabota.ru изменился.')});
 await scoped.prepare('SELECT 1').first();
 if(candidateId){
  const candidate=await scoped.prepare("SELECT company_id FROM vacancy_candidates WHERE id=? AND source_id='rabota'").bind(Number(candidateId)).first();
  if(!candidate||candidate.company_id!==companyId)throw schedulerError('source_scope','Кандидат не относится к текущему источнику компании.');
  const x=await leafRoleContext(scoped,Number(candidateId)),cached=await currentLeafRoles(scoped,x);
  return {...summary,roleInput:{candidateId:Number(candidateId),title:x.candidate.titleRaw,allowed:x.allowed,prompt:x.prompt,config:x.config,markdown:x.raw.slice(0,24000),markdownLength:x.raw.length,truncated:x.raw.length>24000,extractedDescription:x.data.description_md?.slice(0,18000)??null,cachedRoleIds:cached?JSON.parse(cached.role_ids):null},externalRequests:0,writes:'diagnostic_receipt_only'};
 }
 if(target==='queue'){
  if(!settings.enabled)throw schedulerError('disabled','Планировщик выключен.');
  const active=await scoped.prepare("SELECT id,status FROM scheduler_jobs WHERE generation=? AND company_id=? AND source_id='rabota' AND type='scan_source' AND status IN ('pending','running') ORDER BY created_at LIMIT 1").bind(boundGeneration(scoped),companyId).first();
  // The key belongs to the adapter revision, not the probe UUID: a new diagnostic
  // request cannot replay a completed scan or reset a failed historical job.
  const job=active??await enqueueScheduledJob(scoped,{type:'scan_source',key:'rabota-pages-v2:'+companyId+':'+source.revision+':'+source.url+':'+settings.revision,companyId,sourceId:'rabota',payload:{manual:true,sourceUrl:source.url,sourceRevision:source.revision}});
  return {...summary,queued:{jobId:job.id,status:job.status,reused:!!active},writes:'ordinary_scan_job_only',externalRequests:0};
 }
 if(target==='list'){
  const result=await fetchVacancyListing({...env,DB:scoped},companyId,'rabota',{userName:settings.ownerName,signal,maxPagesPerSlice:1});
  return {...summary,listing:{runId:result.runId,status:result.status,found:result.found,requests:result.requests,loadedPages:result.loadedPages,creditsReported:result.creditsReported,errors:result.errors,pages:result.pages,jobs:result.jobs.slice(0,5)},writes:'listing_preview_only'};
 }
 if(pageNumber&&!source)throw schedulerError('source_scope','Для проверки страницы нужна сохранённая ссылка компании.');
 if(!pageNumber&&!explicitSlug&&!/^[1-9]\d{1,10}$/.test(target))throw schedulerError('input','Для проверки нужен slug компании или числовой ID rabota.ru.');
 return withFirecrawlLease(scoped,async(assertOwned,owned)=>{
  const state=await firecrawlSettings(owned);if(!state.enabled)throw schedulerError('disabled','Firecrawl выключен.');
  const listing=!!(explicitSlug||pageNumber),pageUrl=pageNumber?new URL(rabotaListingUrl(source.url)):null;if(pageUrl)pageUrl.searchParams.set('page',pageNumber);
  const url=pageUrl?.href??(explicitSlug?'https://www.rabota.ru/company/'+explicitSlug+'/vacancies/':'https://www.rabota.ru/vacancy/'+target),hosts=rabotaListingHosts('www.rabota.ru'),session=await createFirecrawlUsageSession({...env,DB:owned},{signal,expectedRevision:state.revision});
  const final=await preflightListingUrl(url,hosts,{signal});await assertOwned();
  const result=await firecrawlScrape(session,final,{hosts,markdownOnly:false,html:true,onlyMainContent:false,timeoutMs:Math.min(45000,state.timeout_ms),proxy:'basic',signal,budget:{remaining:1}});await assertOwned();
  const markdown=listing?typeof result.data.markdown==='string'?result.data.markdown:'':rabotaDetailMarkdown(result.data,result.finalUrl);
  if(listing){const list=extractVacancyListing(result.data,{pageUrl:result.finalUrl,hosts,config:{adapter:'rabota',html:true}}),coverage=rabotaListingCoverage(markdown,{nextUrl:list.nextUrl}),samples=(markdown.match(/^.*(?:vacancy\/|\/vacancy).*$/mg)||[]).slice(0,5).map(line=>line.slice(0,600));return {...summary,listing:{url:result.finalUrl,status:!list.ok?'error':coverage.incomplete?'partial':'complete',found:list.jobs.length,requests:result.requests,creditsReported:result.creditsReported,coverage,diagnostics:list.diagnostics,markdownLinkSamples:samples,paginationSamples:[...String(result.data.html||'').matchAll(/.{0,200}(?:pagination|page=|data-page|pager).{0,400}/gi)].slice(0,5).map(m=>m[0]),errors:list.ok?[]:[{code:'extraction'}],jobs:list.jobs.slice(0,5)},employerVerified:false,writes:'diagnostic_receipt_only'};}
  const validation=checkVacancyMarkdown(markdown,{sourceUrl:url,finalUrl:result.finalUrl,targetStatus:result.targetStatus,listingUrl:source?.url});
  const closedEvidence=(markdown.slice(0,3000).match(/.{0,40}(?:ваканси[яи].{0,30}(?:закрыта|удалена|в архиве)|captcha|access denied|страница не найдена).{0,60}/gi)||[]).slice(0,3);
  return {...summary,detail:{vacancyId:target,url:result.finalUrl,title:markdown.match(/^#\s+(.+)$/m)?.[1]?.slice(0,500)??null,markdownLength:Array.from(markdown).length,bodyPresent:rabotaBodyPresent(markdown),bodySample:markdown.slice(0,4000),htmlLength:String(result.data.html||'').length,providerStatus:result.providerStatus,targetStatus:result.targetStatus,creditsReported:result.creditsReported,validation,closedEvidence,employerVerified:false},writes:'diagnostic_receipt_only'};
 });
}
