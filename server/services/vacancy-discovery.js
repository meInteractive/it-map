import {parserOwner,parserOwnerKey,parserOwnerColumns,parserOwnerScopeSql} from '../repositories/parser-owner.js';
import {loadYooMoneyPublicPage,usesYooMoneyPublicListing} from './yoomoney-public-listing.js';
import {fetchApifyListing} from './apify-discovery.js';
import {saveApifySnapshot} from './apify-vacancies.js';
import {parserCompanyDb,parserCompanyScopeSql} from '../repositories/parser-company-scope.js';
import {createFirecrawlUsageSession,firecrawlScrape,preflightListingUrl,fcError,FirecrawlError} from '../integrations/firecrawl.js';
import {firecrawlSettings,withFirecrawlLease} from '../repositories/firecrawl-settings.js';
import {readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {candidateDb,candidateHash,canonicalCandidateUrl,upsertVacancyCandidate} from '../repositories/vacancy-candidates.js';
import {generationBoundDb,boundGeneration} from '../repositories/generation-bound-db.js';
import {extractVacancyListing} from './vacancy-listing-extractor.js';
import {rabotaListingUrl,rabotaListingCoverage,rabotaListingHosts} from './rabota-listing.js';
const MAX_JOBS=200,PREVIEW_TTL=60*60*1000;
const errorInfo=e=>({code:e.code||'storage',message:e instanceof FirecrawlError?e.message:e.status===400||e.status===404||e.status===409||e.status===422?e.message:'Ошибка сохранения первичной вакансии.',providerStatus:e.providerStatus??null,targetStatus:e.targetStatus??null});
export async function knownCandidates(db,jobs,source){
 const namespace=parserOwnerKey(source)+':'+source.sourceId;
 const ids=await Promise.all(jobs.map(async j=>{const keys=['url:'+await candidateHash(canonicalCandidateUrl(j.url))];if(j.externalJobId)keys.push('external:'+await candidateHash(JSON.stringify([namespace+':'+new URL(j.url).origin,j.externalJobId])));return {j,keys};}));
 const keys=JSON.stringify(ids.flatMap(r=>r.keys));
 const rows=(await db.prepare('SELECT identity_key,candidate_id FROM vacancy_candidate_identities WHERE identity_key IN (SELECT value FROM json_each(?))').bind(keys).all()).results||[],known=new Map(rows.map(r=>[r.identity_key,r.candidate_id]));
 return ids.map(({j,keys})=>({...j,existingId:keys.map(k=>known.get(k)).find(Boolean)||null,saveState:'unsaved'}));
}
async function cleanupRuns(db){await db.batch([db.prepare("DELETE FROM vacancy_discovery_runs WHERE expires_at<=? AND json_extract(result_json,'$.paused') IS NULL").bind(Date.now()),db.prepare("DELETE FROM vacancy_discovery_runs WHERE json_extract(result_json,'$.paused') IS NULL AND id NOT IN (SELECT id FROM vacancy_discovery_runs WHERE json_extract(result_json,'$.paused') IS NULL ORDER BY created_at DESC LIMIT 50)")]);}
async function verifySnapshot(db,source){
 const fresh=await readVacancyParsingSource(db,source,source.sourceId);
 if(fresh.url!==source.url||fresh.revision!==source.revision||!fresh.enabled)throw fcError('changed',409);
 return fresh;
}
export async function fetchVacancyListing(env,companyId,sourceId,{userName,signal,resume,maxPagesPerSlice,discoveryId}={}){
 env={...env,DB:await candidateDb(env.DB)};let db=await parserCompanyDb(env.DB,companyId,userName);
 const source=await readVacancyParsingSource(db,companyId,sourceId),s=await firecrawlSettings(db);
 if(!source.supported)throw Object.assign(new Error(source.reason),{status:422});
 if(sourceId==='hh')return fetchApifyListing(env,companyId,sourceId,{userName,signal,resume,discoveryId});
 if(resume&&(parserOwnerKey(resume.source)!==parserOwnerKey(source)||resume.source.sourceId!==source.sourceId||resume.source.url!==source.url||resume.sourceRevision!==source.revision))throw fcError('changed',409);
 if(!s.enabled||!source.enabled)throw fcError('disabled',400);
 const publicSite=usesYooMoneyPublicListing(source)&&(!resume||resume.provider==='yoomoney-public');
 const session=publicSite?null:await createFirecrawlUsageSession(env,{signal,expectedRevision:s.revision});
 return withFirecrawlLease(env.PARSER_RESOURCE_DB||env.DB,async (assertOwned,ownedDb)=>{
  db=await parserCompanyDb(ownedDb,companyId,userName);
  await cleanupRuns(db);const started=Date.now(),generation=boundGeneration(db),runId=resume?.runId||crypto.randomUUID(),hosts=[...(sourceId==='rabota'?rabotaListingHosts(new URL(source.url).hostname):[new URL(source.url).hostname]),...source.config.allowedHosts],listingConfig=sourceId==='rabota'?{...source.config,html:true}:source.config;
  const fullRabota=sourceId==='rabota',continuable=fullRabota||publicSite,budget={remaining:s.max_pages+1};const remainingPages=continuable?Infinity:resume?.paused?.remainingPages??s.max_pages,pageLimit=Math.min(s.max_pages,remainingPages,maxPagesPerSlice??s.max_pages);let newPages=0;const visited=new Set(resume?.paused?.visited||[]),found=new Map((resume?.jobs||[]).map(j=>[canonicalCandidateUrl(j.url),j])),pages=(resume?.pages||[]).filter(p=>!p.paused),errors=[],warnings=[];let paused=null;let url=resume?.paused?.pageUrl||source.url,requests=resume?.requests||0,loadedPages=resume?.loadedPages||0,creditsReported=resume?.creditsReported||0,allCreditsKnown=!resume?.requests||resume.creditsReported!==null,creditsEstimate=resume?.creditsEstimate||0,yoomoney=resume?.yoomoney||{phase:'source'};
  while(url&&newPages<pageLimit){
   if(visited.has(url)){warnings.push('Повторная страница пропущена.');break;}visited.add(url);
   await assertOwned();await verifySnapshot(db,source);
   let loaded,requestedUrl=url;
   try{
    if(publicSite){loaded=await loadYooMoneyPublicPage(url,yoomoney,{signal,timeoutMs:s.timeout_ms,assertActive:async()=>{await assertOwned();await verifySnapshot(db,source);}});requestedUrl=loaded.finalUrl;yoomoney=loaded.cursor;}
    else{
    const finalUrl=await preflightListingUrl(sourceId==='rabota'?rabotaListingUrl(url):url,hosts,{signal});requestedUrl=finalUrl;
    if(finalUrl!==url&&visited.has(finalUrl))break;visited.add(finalUrl);
    loaded=await firecrawlScrape(session,finalUrl,{hosts,html:listingConfig.html,onlyMainContent:sourceId!=='rabota',timeoutMs:s.timeout_ms,proxy:s.mode==='hobby'&&source.config.useEnhancedProxy&&s.enhanced_allowed?'enhanced':'basic',enhancedAllowed:s.mode==='hobby'&&!!s.enhanced_allowed,signal,budget});
    }
    requests+=loaded.requests;loadedPages++;newPages++;creditsEstimate+=loaded.creditsEstimate;
    if(loaded.creditsReported===null)allCreditsKnown=false;else creditsReported+=loaded.creditsReported;
    const extraction=loaded.extraction||extractVacancyListing(loaded.data,{pageUrl:loaded.finalUrl,hosts,config:listingConfig});
    pages.push({url,finalUrl:loaded.finalUrl,providerStatus:loaded.providerStatus,targetStatus:loaded.targetStatus,durationMs:loaded.durationMs,profile:loaded.profile,found:extraction.jobs.length,diagnostics:extraction.diagnostics,emptyConfirmed:extraction.emptyConfirmed});
    if(!extraction.ok){errors.push(errorInfo(fcError(extraction.errorCode||'extraction',422)));break;}
    const coverage=publicSite?loaded.coverage:fullRabota?rabotaListingCoverage(loaded.data.markdown,{nextUrl:extraction.nextUrl}):null;
    const previousLast=pages.at(-2)?.coverage?.last??resume?.coverage?.last??0;
    if(coverage)coverage.total??=pages.at(-2)?.coverage?.total??resume?.coverage?.total??null;
    if(fullRabota&&previousLast&&(coverage.first!==previousLast+1||coverage.last<=previousLast)||fullRabota&&resume?.jobs?.length&&extraction.jobs.every(j=>found.has(canonicalCandidateUrl(j.url)))){errors.push({code:'listing_incomplete',message:'Следующая страница rabota.ru повторилась или диапазон вакансий не продолжает предыдущий. Полнота не подтверждена.'});break;}
    if(coverage)pages.at(-1).coverage=coverage;
    for(const job of extraction.jobs){const canonical=canonicalCandidateUrl(job.url);if((fullRabota||found.size<MAX_JOBS)&&!found.has(canonical))found.set(canonical,{...job,sourceUrl:loaded.listingUrl||url});}
    if(extraction.diagnostics.limitReached||!fullRabota&&found.size>=MAX_JOBS){warnings.push('Лимит200 вакансий на странице или проверке. Список частичный.');if(fullRabota)errors.push({code:'listing_incomplete',message:'Достигнут защитный лимит извлечения, полнота не подтверждена.'});break;}
    if(extraction.diagnostics.incomplete)warnings.push(extraction.diagnostics.incomplete);
    if(extraction.diagnostics.unpaired||extraction.diagnostics.ambiguous)warnings.push('Часть ссылок не сопоставлена с однозначным заголовком и не включена.');
    url=extraction.nextUrl;
    if(fullRabota&&!url&&coverage.last!==null&&coverage.total!==null&&coverage.last<coverage.total){errors.push({code:'listing_incomplete',message:'rabota.ru сообщает о следующих вакансиях, но ссылка продолжения не получена. Сохранена только доступная порция.',providerStatus:loaded.providerStatus,targetStatus:loaded.targetStatus});break;}
    if(url&&newPages>=pageLimit){warnings.push(continuable?'Продолжение сохранено для следующей порции.':'Достигнут лимит страниц; следующие страницы не загружались.');if(continuable||maxPagesPerSlice&&remainingPages>newPages)paused={code:'slice',pageUrl:url,visited:[...visited],...(continuable?{}:{remainingPages:remainingPages-newPages})};}
   }catch(e){
    if(e.code==='changed'||e.code==='restored')throw e;
    if(['free_exhausted','hobby_exhausted','rate_limit','usage_unavailable','hobby_access','profile_changed','credits','rate','network','timeout','auth','busy'].includes(e.code))paused={code:e.code,pageUrl:url,visited:[...visited].filter(v=>v!==url&&v!==requestedUrl),...(continuable?{}:{remainingPages:Math.max(1,remainingPages-newPages)})};
    requests+=e.requests||0;creditsEstimate+=e.creditsEstimate||0;if(e.requests){if(e.creditsReported==null)allCreditsKnown=false;else creditsReported+=e.creditsReported;}
    pages.push({url,paused:!!paused,providerStatus:e.providerStatus??null,targetStatus:e.targetStatus??null,error:errorInfo(e)});errors.push(errorInfo(e));break;
   }
  }
  await assertOwned();await verifySnapshot(db,source);
  if(publicSite&&!paused&&!url&&yoomoney.phase==='complete'&&found.size!==yoomoney.total)errors.push({code:'listing_incomplete',message:'Число уникальных вакансий ЮMoney не совпало с объявленным итогом. Полнота не подтверждена.'});
  const oldJobs=new Map((resume?.jobs||[]).map(j=>[canonicalCandidateUrl(j.url),j]));
  const jobs=(await knownCandidates(db,[...found.values()],source)).map(j=>oldJobs.get(canonicalCandidateUrl(j.url))?.saveState&&!['unsaved','error'].includes(oldJobs.get(canonicalCandidateUrl(j.url)).saveState)?oldJobs.get(canonicalCandidateUrl(j.url)):j);
  // Earlier pages may contain rejected pairs even after a later slice succeeds.
  if(pages.some(p=>p.diagnostics?.unpaired||p.diagnostics?.ambiguous)&&!warnings.some(w=>w.startsWith('Часть ссылок')))warnings.push('Часть ссылок не сопоставлена с однозначным заголовком и не включена.');
  for(const page of pages)if(page.diagnostics?.incomplete&&!warnings.includes(page.diagnostics.incomplete))warnings.push(page.diagnostics.incomplete);
  const partial=errors.length>0&&jobs.length>0||warnings.length>0;
  const result={...(publicSite?{provider:'yoomoney-public',yoomoney}:{}),runId,sourceRevision:source.revision,paused,coverage:pages.at(-1)?.coverage??resume?.coverage??null,status:errors.length&& !jobs.length?'error':partial?'partial':'complete',source:{...parserOwnerColumns(source),companyName:source.companyName,sourceId:source.sourceId,sourceName:source.sourceName,url:source.url},mode:publicSite?'public':'scrape',adapter:source.config.adapter,proxy:publicSite?'none':s.mode==='hobby'&&source.config.useEnhancedProxy&&s.enhanced_allowed?'enhanced':'basic',maxPages:s.max_pages,requests,loadedPages,found:jobs.length,newCount:jobs.filter(j=>!j.existingId).length,existingCount:jobs.filter(j=>j.existingId).length,savedNew:0,savedExisting:0,saveErrors:[],durationMs:(resume?.durationMs||0)+Date.now()-started,creditsReported:allCreditsKnown&&requests>0?creditsReported:null,creditsEstimate,creditEstimateNote:'Оценка: 1 кредит за возвращённую страницу. Не является точным списанием.',pages,errors,warnings,jobs,expiresAt:new Date(started+PREVIEW_TTL).toISOString()};
  // Guard config snapshot and preview insertion in one transaction. A changed
  // source must not accept a result with obsolete URL/config. Mode changes
  // preserve already received pairs; the usage policy stops further requests.
  await db.batch([
   db.prepare(`SELECT CASE WHEN EXISTS(SELECT 1 FROM vacancy_source_parsing WHERE ${parserOwner(source).column}=? AND source_id=? AND revision=? AND source_url=? AND enabled=1) THEN 1 ELSE abs(-9223372036854775808) END AS snapshot_guard`).bind(parserOwner(source).id,source.sourceId,source.revision,source.url),
   db.prepare('INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,agency_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET result_json=excluded.result_json,settings_revision=excluded.settings_revision,expires_at=excluded.expires_at WHERE vacancy_discovery_runs.user_name=excluded.user_name AND vacancy_discovery_runs.generation=excluded.generation').bind(runId,userName,generation,source.companyId,source.agencyId,source.sourceId,source.url,source.revision,s.revision,JSON.stringify(result),started,started+PREVIEW_TTL),
   db.prepare(`UPDATE vacancy_source_parsing SET last_error=? WHERE ${parserOwner(source).column}=? AND source_id=? AND revision=? AND source_url=?`).bind(errors[0]?.message||warnings[0]||'',parserOwner(source).id,source.sourceId,source.revision,source.url)
  ]);
  return result;
 },(s.max_pages+1)*(s.timeout_ms+28000)+30000);
}
export async function readVacancyDiscoveryRun(db,runId,userName){
 db=await candidateDb(db);const row=await db.prepare("SELECT * FROM vacancy_discovery_runs WHERE id=? AND user_name=? AND generation=? AND (expires_at>? OR json_extract(result_json,'$.paused') IS NOT NULL)").bind(runId,userName,boundGeneration(db),Date.now()).first();if(!row)throw fcError('expired',410);
 await parserCompanyDb(db,row,userName);
 const result=JSON.parse(row.result_json);if(result.kind==='apify-listing-replay')throw fcError('changed',409);return {row,result,db};
}
export async function saveVacancyDiscoveryRun(db,runId,userName,{maxJobs=Infinity,resourceDb}={}){
 const {row,result,db:bound}=await readVacancyDiscoveryRun(db,runId,userName);db=bound;
 return withFirecrawlLease(resourceDb||db,async (assertOwned,ownedDb)=>{
  db=await parserCompanyDb(ownedDb,row,userName);
  const source=await readVacancyParsingSource(db,row,row.source_id);
  await verifySnapshot(db,source);
  if(source.url!==row.source_url||source.revision!==row.source_revision)throw fcError('changed',409);
  db=generationBoundDb(db,boundGeneration(db),{sql:`EXISTS(SELECT 1 FROM vacancy_source_parsing WHERE ${parserOwner(source).column}=? AND source_id=? AND revision=? AND source_url=? AND enabled=1)`,bindings:[parserOwner(source).id,source.sourceId,row.source_revision,row.source_url],failure:()=>fcError('changed',409)});
  // Only server-owned pairs from this active/paused preview. Browser sends UUID only.
  const saved=[];let savedNew=0,savedExisting=0,processed=0;const saveErrors=[];
  for(const job of result.jobs){
   if(job.existingId&&['new','existing'].includes(job.saveState)){saved.push(job);savedExisting++;continue;}
   if(processed>=maxJobs){saved.push(job);continue;}processed++;
   await assertOwned();await verifySnapshot(db,source);
   try{const response=await upsertVacancyCandidate(db,{titleRaw:job.title,vacancyUrl:job.url,...parserOwnerColumns(source),companyNameRaw:source.companyName,sourceId:source.sourceId,sourceUrl:job.sourceUrl,sourceNamespace:parserOwnerKey(source)+':'+source.sourceId+':'+new URL(job.url).origin,externalJobId:job.externalJobId,acquisitionMethod:['apify','yoomoney-public'].includes(result.provider)?'site':'firecrawl'},{preserveCompany:true,intakeParser:result.provider==='apify'?'apify':result.provider==='yoomoney-public'?'http':'firecrawl'});if(parserOwnerKey(response.candidate)!==parserOwnerKey(source))throw Object.assign(new Error('Публикация уже принадлежит другой организации.'),{status:409,code:'owner_conflict'});if(result.provider==='apify')await saveApifySnapshot(db,response.id,job,{profile:result.apifyProfile,runId:result.apifyRunId,buildId:result.apifyBuildId});response.created?savedNew++:savedExisting++;saved.push({...job,existingId:response.id,saveState:response.created?'new':'existing',companyConflict:parserOwnerKey(response.candidate)!==parserOwnerKey(source),processingStatus:response.candidate.processingStatus});}
   catch(e){if(e.code==='restored'||e.code==='changed')throw e;const error=errorInfo(e);saveErrors.push({title:job.title,url:job.url,...error});saved.push({...job,saveState:'error',saveError:error.message});}
  }
  const completeSaved=result.status==='complete'&&(result.provider!=='apify'||result.coverage?.hhComplete===true)&&!result.paused&&!result.errors.length&&!saveErrors.length&&saved.every(j=>['new','existing'].includes(j.saveState));
  const alreadySaved=!!result.savedAt&&!result.savePending&&!result.saveErrors.length&&result.jobs.every(j=>['new','existing'].includes(j.saveState));
  const fullSavedAt=result.fullSavedAt||(completeSaved?(alreadySaved?result.savedAt:new Date().toISOString()):null);
  if(completeSaved&&!result.fullSavedAt&&!alreadySaved)await db.prepare(`UPDATE vacancy_source_parsing SET last_success_at=?,last_error='' WHERE ${parserOwner(source).column}=? AND source_id=? AND revision=?`).bind(fullSavedAt,parserOwner(source).id,source.sourceId,source.revision).run();
  const next={...result,...(fullSavedAt?{fullSavedAt}:{}),jobs:saved,savedNew,savedExisting,saveErrors,savePending:saved.some(j=>!['new','existing'].includes(j.saveState)),savedAt:new Date().toISOString()};
  await db.prepare("UPDATE vacancy_discovery_runs SET result_json=? WHERE id=? AND user_name=? AND generation=? AND (expires_at>? OR json_extract(result_json,'$.paused') IS NOT NULL)").bind(JSON.stringify(next),runId,userName,boundGeneration(db),Date.now()).run();return next;
 });
}

export async function resumeVacancyDiscoveryRun(env,runId,userName,{signal,maxPagesPerSlice}={}){
 const {row,result,db}=await readVacancyDiscoveryRun(env.DB,runId,userName);if(!result.paused)throw Object.assign(new Error('Сбор не приостановлен.'),{status:409});
 return fetchVacancyListing({...env,DB:db},row,row.source_id,{userName,signal,maxPagesPerSlice,resume:{...result,sourceRevision:row.source_revision}});
}
export async function listPausedVacancyDiscoveryRuns(db,userName,filters={}){
 db=await candidateDb(db);const owner=filters.companyId!==undefined||filters.agencyId!==undefined?parserOwner(filters):null;
 const rows=(await db.prepare(`SELECT id,result_json,created_at FROM vacancy_discovery_runs WHERE user_name=? AND generation=? AND coalesce(json_extract(result_json,'$.kind'),'')<>'apify-listing-replay' AND json_extract(result_json,'$.paused') IS NOT NULL AND (? IS NULL OR ${owner?.column||'company_id'}=?) AND (? IS NULL OR source_id=?) AND ${parserOwnerScopeSql('vacancy_discovery_runs')} ORDER BY created_at DESC LIMIT 50`).bind(userName,boundGeneration(db),owner?.id??null,owner?.id??null,filters.sourceId??null,filters.sourceId??null,userName).all()).results||[];
 return {runs:rows.map(row=>{const r=JSON.parse(row.result_json);return {runId:row.id,source:r.source,found:r.found,paused:r.paused,createdAt:new Date(row.created_at).toISOString()};})};
}
