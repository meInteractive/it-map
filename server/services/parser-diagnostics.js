import {readPromptModels} from '../repositories/prompt-models.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {apifySettings,apifyToken,apifyTokenHash} from '../repositories/apify-settings.js';
import {apifyRequest,hhInput} from '../integrations/apify.js';
import {apifyDatasetItems,apifyDatasetInfo} from './apify-runs.js';
import {apifyVacancy} from './apify-vacancies.js';
import {checkVacancyMarkdown} from './vacancy-fulltext.js';
import {apifyInputEvidence,apifyLogEvidence} from './apify-evidence.js';
import {readHHTitleFilter} from './hh-title-filter.js';

// Owner-only MCP diagnostics: bounded GETs of existing runs, never Actor starts.
// Return field names/lengths and validation, never credentials or source bodies.
export async function parserDiagnostics(env,{signal}={}){
 const db=await candidateDb(env.DB),generation=boundGeneration(db);
 const queue=(await db.prepare('SELECT type,source_id,status,count(*) total,min(created_at) oldest_at FROM scheduler_jobs WHERE generation=? GROUP BY type,source_id,status ORDER BY status,type,source_id').bind(generation).all()).results||[];
 const waitingJobs=(await db.prepare("SELECT j.id jobId,j.company_id companyId,j.source_id sourceId,j.type,j.reason_code code,j.run_after runAfter,j.updated_at checkedAt,j.attempts,(SELECT company_name FROM company_identity WHERE id=j.company_id) companyName FROM scheduler_jobs j WHERE j.generation=? AND j.status='pending' ORDER BY j.run_after,j.created_at,j.id LIMIT 20").bind(generation).all()).results||[];
 const roleRecovery=(await db.prepare("SELECT j.id jobId,c.id candidateId,c.title_raw title,c.main_vacancy_id vacancyId,j.reason_code code,json_extract(j.payload,'$.dependencyRecovery') recovery,(SELECT role_ids FROM vacancy_role_results r WHERE r.candidate_id=c.id AND r.status='success' ORDER BY r.processed_at DESC,r.rowid DESC LIMIT 1) lastRoleIds,(SELECT model_id FROM vacancy_role_results r WHERE r.candidate_id=c.id AND r.status='success' ORDER BY r.processed_at DESC,r.rowid DESC LIMIT 1) lastRoleModel FROM scheduler_jobs j JOIN vacancy_candidates c ON c.id=j.candidate_id WHERE j.generation=? AND j.status='failed' AND j.type='import_vacancy' AND j.reason_code IN ('roles_pending','roles_unresolved','roles_stale') ORDER BY j.created_at,j.id LIMIT 20").bind(generation).all()).results||[];
 const failedJobs=(await db.prepare("SELECT j.id jobId,j.type,j.source_id sourceId,j.candidate_id candidateId,c.title_raw title,c.vacancy_url url,j.reason_code code,j.updated_at updatedAt FROM scheduler_jobs j LEFT JOIN vacancy_candidates c ON c.id=j.candidate_id WHERE j.generation=? AND j.status='failed' AND j.source_id IN ('hh','rabota') ORDER BY j.updated_at DESC,j.id LIMIT 12").bind(generation).all()).results||[];
 const hhContent=(await db.prepare("SELECT f.candidate_id candidateId,c.title_raw title,f.status,f.error_code code,f.firecrawl_mode provider,s.content_status contentStatus,length(s.markdown_raw) markdownLength,json_extract(s.diagnostics,'$.provider') snapshotProvider,f.started_at startedAt FROM vacancy_content_fetches f JOIN vacancy_candidates c ON c.id=f.candidate_id LEFT JOIN vacancy_raw_snapshots s ON s.id=f.snapshot_id WHERE c.source_id='hh' ORDER BY f.started_at DESC,f.id LIMIT 8").all()).results||[];
 const imports=(await db.prepare('SELECT candidate_id,vacancy_id,action,status,processed_at,error_code FROM vacancy_import_operations ORDER BY processed_at DESC LIMIT 10').all()).results||[];
 const rabotaCandidates=(await db.prepare("SELECT c.company_id,i.company_name,count(*) total,sum(CASE WHEN c.main_vacancy_id IS NOT NULL THEN 1 ELSE 0 END) imported FROM vacancy_candidates c LEFT JOIN company_identity i ON i.id=c.company_id WHERE c.source_id='rabota' GROUP BY c.company_id ORDER BY c.company_id LIMIT 20").all()).results||[];
 const rabotaScans=(await db.prepare("SELECT id,company_id,source_revision,json_extract(result_json,'$.loadedPages') loadedPages,json_extract(result_json,'$.found') found,json_extract(result_json,'$.status') status,json_extract(result_json,'$.paused.code') pausedCode,json_extract(result_json,'$.paused.pageUrl') nextPageUrl,json_extract(result_json,'$.errors[0].code') errorCode FROM vacancy_discovery_runs WHERE generation=? AND source_id='rabota' ORDER BY created_at DESC LIMIT 6").bind(generation).all()).results||[];
 const rabotaImports=(await db.prepare("SELECT c.id candidateId,v.id vacancyId,v.company_name,v.title,length(v.description_md) descriptionLength,v.relevance FROM vacancy_candidates c JOIN company_vacancies v ON v.id=c.main_vacancy_id WHERE c.source_id='rabota' ORDER BY v.id DESC LIMIT 10").all()).results||[];
 const publicationDates=(await db.prepare('SELECT entity_type,count(*) total,max(published_at) latest_published_at,max(loaded_at) latest_loaded_at,max(created_at) latest_created_at FROM company_vacancies GROUP BY entity_type').all()).results||[];
 const recentAdditions=(await db.prepare("SELECT date(created_at,'+3 hours') day,count(*) total,min(published_at) oldest_publication,max(published_at) newest_publication FROM company_vacancies WHERE created_at>=datetime('now','-7 days') GROUP BY date(created_at,'+3 hours') ORDER BY day DESC LIMIT 8").all()).results||[];
 const rows=(await db.prepare("SELECT operation_key,profile,key_hash,state,run_id,dataset_id FROM apify_runs WHERE generation=? AND operation_key LIKE 'detail:%' ORDER BY created_at DESC,id DESC LIMIT 4").bind(generation).all()).results||[];
 const listing=(await db.prepare("SELECT operation_key,profile,key_hash,state,run_id,dataset_id FROM apify_runs WHERE generation=? AND operation_key LIKE 'listing:%' ORDER BY created_at DESC,id DESC LIMIT 1").bind(generation).all()).results||[];
 const deepest=(await db.prepare("SELECT operation_key,profile,key_hash,state,run_id,dataset_id FROM apify_runs WHERE generation=? AND operation_key LIKE 'listing:%' ORDER BY CAST(substr(operation_key,46) AS INTEGER) DESC,created_at DESC,id DESC LIMIT 1").bind(generation).all()).results||[];
 for(const row of deepest)if(!listing.some(item=>item.operation_key===row.operation_key))listing.push(row);
 const failedListing=(await db.prepare("SELECT operation_key,profile,key_hash,state,run_id,dataset_id FROM apify_runs WHERE generation=? AND operation_key LIKE 'listing:%' AND state='failed' ORDER BY created_at DESC,id DESC LIMIT 1").bind(generation).all()).results||[];
 for(const row of failedListing)if(!listing.some(item=>item.operation_key===row.operation_key))listing.push(row);
 rows.push(...listing);
 const settings=rows.length?await apifySettings(db):null;
 const apifyDetails=await Promise.all(rows.map(async row=>{
   const result={operationKey:row.operation_key,recordedState:row.state};
  try{
   if(!settings.enabled||row.profile==='starter'&&!settings.paid_allowed)return {...result,diagnosis:'disabled'};
   if(!/^[A-Za-z0-9]+$/.test(row.run_id||'')||!/^[A-Za-z0-9]+$/.test(row.dataset_id||''))return {...result,diagnosis:'unknown_run'};
   if(await apifyTokenHash(env,settings,row.profile)!==row.key_hash)return {...result,diagnosis:'key_changed'};
   result.providerRunId=row.run_id;result.datasetId=row.dataset_id;
   const candidateId=row.operation_key.startsWith('detail:')?Number(row.operation_key.split(':')[1]):null,c=candidateId?await db.prepare('SELECT vacancy_url FROM vacancy_candidates WHERE id=?').bind(candidateId).first():null;
   const token=await apifyToken(env,settings,row.profile),run=(await apifyRequest(token,'/actor-runs/'+row.run_id,{signal})).data;
   result.providerStatus=run?.status;
   result.buildId=run?.buildId;
   result.buildNumber=typeof run?.buildNumber==='string'?run.buildNumber.slice(0,40):null;
   result.costUsd=Number.isFinite(run?.usageTotalUsd)&&run.usageTotalUsd>=0?run.usageTotalUsd:null;
   result.finishedAt=typeof run?.finishedAt==='string'&&Number.isFinite(Date.parse(run.finishedAt))?run.finishedAt:null;
   result.proxyUsage={residentialGb:Number.isFinite(run?.usage?.PROXY_RESIDENTIAL_TRANSFER_GBYTES)?run.usage.PROXY_RESIDENTIAL_TRANSFER_GBYTES:null};
   const details=await Promise.allSettled([
    /^[A-Za-z0-9]+$/.test(run?.defaultKeyValueStoreId||'')?apifyRequest(token,'/key-value-stores/'+run.defaultKeyValueStoreId+'/records/INPUT',{signal,maxBytes:16000}):Promise.resolve(null),
    apifyRequest(token,'/logs/'+row.run_id,{signal,maxBytes:64000,text:true})
   ]);
   const input=details[0].status==='fulfilled'?details[0].value:null;
   if(input)result.input=apifyInputEvidence(input,{operationKey:row.operation_key,expectedVacancyUrl:c?.vacancy_url});
   if(details[1].status==='fulfilled'){
    const log=details[1].value;
    result.logSummary={httpErrors:[...new Set(log.match(/\b(?:401|403|404|407|429|500|502|503|504)\b/g)||[])],captcha:/captcha|challenge|access denied|blocked/i.test(log),proxyError:/proxy.{0,80}(?:error|fail|not|denied|unavailable)/i.test(log),parseError:/json.{0,40}(?:parse|error|invalid)|(?:parse|decode).{0,30}error/i.test(log),warnings:(log.match(/\bWARN(?:ING)?\b/g)||[]).length,errors:(log.match(/\bERROR\b/g)||[]).length,...apifyLogEvidence(log)};
   }
   const partial=run?.status!=='SUCCEEDED';
   if(partial&&(!row.operation_key.startsWith('listing:')||!['FAILED','TIMED-OUT','ABORTED'].includes(run?.status)))return {...result,diagnosis:'provider_not_succeeded'};
   const samples=await Promise.allSettled([
    apifyDatasetInfo(token,row.dataset_id,{signal}),
    apifyRequest(token,'/datasets/'+row.dataset_id+'/items?format=json&clean=false&skipHidden=true&skipEmpty=false&offset=0&limit=10',{signal,maxBytes:1800000})
   ]);
   if(samples[0].status==='fulfilled')result.dataset=samples[0].value;
   if(samples[1].status==='fulfilled'&&Array.isArray(samples[1].value))result.rawSample=samples[1].value.slice(0,10).map(item=>({fields:Object.keys(item||{}).slice(0,40),titleLength:typeof item?.title==='string'?item.title.length:0,textLength:typeof item?.description_text==='string'?item.description_text.length:0,htmlLength:typeof item?.description_html==='string'?item.description_html.length:0}));
   const items=await apifyDatasetItems(token,row.dataset_id,0,{signal,clean:true});
   result.items=items.length;
   if(row.operation_key.startsWith('listing:'))return {...result,diagnosis:partial?'partial_listing_dataset':'listing_sample'};
   if(items.length!==1)return {...result,diagnosis:items.length?'unexpected_item_count':'empty_dataset'};
   const item=items[0];result.fields=Object.keys(item||{}).slice(0,40);
   result.textLength=typeof item?.description_text==='string'?item.description_text.length:0;
   result.htmlLength=typeof item?.description_html==='string'?item.description_html.length:0;
   if(!c)return {...result,diagnosis:'candidate_missing'};
   let same=false;try{same=new URL(hhInput(item?.url,{detail:true}).startUrl).pathname===new URL(c.vacancy_url).pathname&&(item.vacancy_id==null||String(item.vacancy_id)===new URL(item.url).pathname.split('/')[2]);}catch{}
   if(!same||item.archived===true)return {...result,diagnosis:'identity_or_archive'};
   const missingFields=[];if(typeof item.title!=='string'||!item.title.trim())missingFields.push('title');if(![item.description_text,item.description_html].some(v=>typeof v==='string'&&v.trim()))missingFields.push('description');
   if(missingFields.length)return {...result,diagnosis:'incomplete_record',missingFields};
   const job=apifyVacancy(item,{sourceUrl:c.vacancy_url,detail:true});
   const validation=checkVacancyMarkdown(job.description,{sourceUrl:job.url,finalUrl:job.url,targetStatus:200});
   return {...result,diagnosis:!job.description?'empty_description':validation.valid?'valid':'invalid_description',validationErrors:validation.errors};
  }catch(e){return {...result,diagnosis:e.code||'diagnostic_unavailable'};}
 }));
 const roleCatalog=(await db.prepare(`SELECT r.id,r.stable_key stableKey,r.name,r.parent_id parentId,count(a.vacancy_id) usageCount FROM vacancy_roles r LEFT JOIN vacancy_role_assignments a ON a.role_id=r.id GROUP BY r.id ORDER BY r.id LIMIT 100`).all()).results||[];
 const preservedAssignments=(await db.prepare(`SELECT a.vacancy_id vacancyId,a.role_id roleId FROM vacancy_role_assignments a JOIN vacancy_roles r ON r.id=a.role_id WHERE r.parent_id=(SELECT id FROM vacancy_roles WHERE stable_key='root-preserved') ORDER BY a.vacancy_id LIMIT 30`).all()).results||[];
 const titleFilter=await readHHTitleFilter(db);
 return {roleCatalog,preservedAssignments,waitingJobs,roleRecovery,failedJobs,hhContent,rabota:{candidates:rabotaCandidates,scans:rabotaScans,imports:rabotaImports},models:await readPromptModels(db),hhTitleFilter:{version:titleFilter.version,status:titleFilter.status,rootRoleIds:titleFilter.rootRoleIds},queue,imports,freshness:{timezone:'Europe/Moscow',publicationDates,recentAdditions},apifyDetails:apifyDetails.filter(r=>r.operationKey.startsWith('detail:')),apifyListings:apifyDetails.filter(r=>r.operationKey.startsWith('listing:'))};
}
