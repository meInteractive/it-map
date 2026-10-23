import {parserCompanyDb} from '../repositories/parser-company-scope.js';
import {generationBoundDb,boundGeneration} from '../repositories/generation-bound-db.js';
import {readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {schedulerSettings,schedulerError} from '../repositories/scheduler.js';
import {firecrawlSettings,withFirecrawlLease} from '../repositories/firecrawl-settings.js';
import {createFirecrawlUsageSession,firecrawlScrape,preflightListingUrl,safeListingUrl} from '../integrations/firecrawl.js';
import {extractVacancyListing} from './vacancy-listing-extractor.js';

// Owner diagnostics of the configured public listing only. No URL parameter,
// pagination, AI, candidate writes, freshness updates or source configuration edits.
export async function checkListingSource(env,companyId,sourceId,target,{signal}={}){
 const settings=await schedulerSettings(env.DB),source=await readVacancyParsingSource(env.DB,companyId,sourceId);
 let db=await parserCompanyDb(env.DB,companyId,settings.ownerName);
 const row=await db.prepare('SELECT auto_enabled,interval_days,next_run_at,last_attempt_at,last_result,config_json FROM vacancy_source_parsing WHERE company_id=? AND source_id=?').bind(companyId,sourceId).first();
 const scans=(await db.prepare("SELECT id,status,reason_code code,run_after runAfter,updated_at checkedAt,attempts,json_extract(payload,'$.sourceRevision') sourceRevision,json_extract(payload,'$.runId') discoveryRunId FROM scheduler_jobs WHERE generation=? AND company_id=? AND source_id=? AND type='scan_source' ORDER BY updated_at DESC,id DESC LIMIT 3").bind(boundGeneration(db),companyId,sourceId).all()).results||[];
 const summary={source,selected:settings.sourceIds.includes(sourceId),schedule:row?{autoEnabled:!!row.auto_enabled,intervalDays:row.interval_days,nextRunAt:row.next_run_at,lastAttemptAt:row.last_attempt_at,lastResult:row.last_result,storedAdapter:(()=>{try{return JSON.parse(row.config_json).adapter??null;}catch{return null;}})()}:null,scans};
 if(target==='inspect')return {...summary,externalRequests:0,writes:'diagnostic_receipt_only'};
 if(!['companySite','getmatch'].includes(sourceId)||!['page','next-page'].includes(target))throw schedulerError('input','Нужен поддерживаемый тип проверки списка.');
 if(!summary.selected||!source.enabled||!source.supported)throw schedulerError('source_scope','Нужен выбранный и включённый источник компании.');
 let pageUrl=source.url,continuationReceipt=null;
 if(target==='next-page'){
  const previous=await db.prepare("SELECT a.id,a.result_json FROM scheduler_attempts a JOIN scheduler_runs r ON r.id=a.run_id WHERE r.generation=? AND r.reason='source_page_check' AND a.type='diagnostic' AND a.company_id=? AND a.source_id=? AND json_extract(a.result_json,'$.report.source.url')=? AND json_extract(a.result_json,'$.report.source.revision')=? AND json_type(a.result_json,'$.report.page')='object' ORDER BY a.started_at DESC,a.id DESC LIMIT 1").bind(boundGeneration(db),companyId,sourceId,source.url,source.revision).first();
  const page=previous?JSON.parse(previous.result_json).report.page:null,next=page?.configured?.ok&&page.configured.nextUrl;
  if(!next)throw schedulerError('source_continuation','У последней проверки этого источника нет подтверждённой ссылки продолжения.');
  pageUrl=safeListingUrl(next,[new URL(source.url).hostname]);
  if(new URL(pageUrl).origin!==new URL(source.url).origin||pageUrl===page.url)throw schedulerError('source_continuation','Продолжение должно вести на другую страницу того же источника.');
  continuationReceipt=previous.id;
 }
 const generation=boundGeneration(db),sourceGuard={sql:"EXISTS(SELECT 1 FROM firecrawl_settings f WHERE f.id=1 AND EXISTS(SELECT 1 FROM json_each(json_extract(f.scheduler_json,'$.sourceIds')) WHERE value=?) AND EXISTS(SELECT 1 FROM vacancy_source_parsing s WHERE s.company_id=? AND s.source_id=? AND s.source_url=? AND s.revision=? AND s.enabled=1))",bindings:[sourceId,companyId,sourceId,source.url,source.revision],failure:()=>schedulerError('source_scope','Источник изменился во время проверки.')};
 db=generationBoundDb(db,generation,sourceGuard);await db.prepare('SELECT 1').first();
 // Release the global lease through its scheduler/epoch facade even if this
 // source is disabled while the response is in flight.
 return withFirecrawlLease(env.DB,async(assertLease,leased)=>{
  const owned=generationBoundDb(await parserCompanyDb(leased,companyId,settings.ownerName),generation,sourceGuard);
  const assertOwned=async()=>{await assertLease();await owned.prepare('SELECT 1').first();};
  const state=await firecrawlSettings(owned);if(!state.enabled)throw schedulerError('disabled','Firecrawl выключен.');
  const hosts=[new URL(source.url).hostname,...source.config.allowedHosts],session=await createFirecrawlUsageSession({...env,DB:owned},{signal,expectedRevision:state.revision});
  const final=await preflightListingUrl(pageUrl,hosts,{signal});await assertOwned();
  const loaded=await firecrawlScrape(session,final,{hosts,html:true,onlyMainContent:true,timeoutMs:Math.min(45000,state.timeout_ms),proxy:'basic',signal,budget:{remaining:1}});await assertOwned();
  const data=loaded.data,markdown=typeof data.markdown==='string'?data.markdown:'',html=typeof data.html==='string'?data.html:'';
  // Compare the configured mode with HTML evidence without changing that config.
  const configured=extractVacancyListing(source.config.html?data:{...data,html:''},{pageUrl:loaded.finalUrl,hosts,config:source.config});
  const withHtml=extractVacancyListing(data,{pageUrl:loaded.finalUrl,hosts,config:{...source.config,html:true}});
  const view=x=>({errorCode:x.errorCode,ok:x.ok,emptyConfirmed:x.emptyConfirmed,found:x.jobs.length,nextUrl:x.nextUrl,diagnostics:x.diagnostics,jobs:x.jobs.slice(0,8)});
  const htmlSamples=[...html.matchAll(/<a\b[^>]*\bhref\s*=\s*["'][^"']*(?:vacanc|jobs?|positions?|openings?|hh\.ru\/employer)[^"']*["'][^>]*>[\s\S]{0,1600}?<\/a>/gi)].slice(0,12).map(m=>m[0].slice(0,1200));
  const paginationSamples=[...html.matchAll(/.{0,180}(?:storepartuid|data-storepart|loadmore|load-more|t_store__init|t_store_init|productslist).{0,500}/gi)].slice(0,12).map(m=>m[0]);
  const storeScripts=[...html.matchAll(/<script\b[^>]*>[\s\S]*?<\/script>/gi)].filter(m=>/t_store|storepart|productslist/i.test(m[0])).slice(0,4).map(m=>m[0].slice(0,4000));
  return {...summary,continuationReceipt,page:{url:loaded.finalUrl,providerStatus:loaded.providerStatus,targetStatus:loaded.targetStatus,requests:loaded.requests,creditsReported:loaded.creditsReported,markdownLength:markdown.length,htmlLength:html.length,configured:view(configured),withHtml:view(withHtml),markdownSample:markdown.slice(0,12000),markdownTruncated:markdown.length>12000,htmlSamples,paginationSamples,storeScripts},writes:'diagnostic_receipt_and_usage_only'};
 });
}
