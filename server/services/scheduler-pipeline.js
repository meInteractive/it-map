import {parserOwner,parserOwnerKey,parserOwnerColumns,parserOwnerScopeSql} from '../repositories/parser-owner.js';
import {candidateDb,candidateHash,getVacancyCandidate,candidateClassificationContext} from '../repositories/vacancy-candidates.js';
import {readVacancyParsingSources,readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {schedulerSettings,enqueueScheduledJob,enqueueScheduledJobs,schedulerError} from '../repositories/scheduler.js';
import {boundGeneration,generationBoundDb} from '../repositories/generation-bound-db.js';
import {parserCompanyDb,parserCompanyScopeSql} from '../repositories/parser-company-scope.js';
import {fetchVacancyListing,resumeVacancyDiscoveryRun,saveVacancyDiscoveryRun,readVacancyDiscoveryRun} from './vacancy-discovery.js';
import {classifyStoredCandidate} from './stored-vacancy-classification.js';
import {currentRawSnapshot,fetchCandidateMarkdown} from './vacancy-fulltext.js';
import {extractionContext,currentExtraction,extractCandidateMarkdown} from './vacancy-extraction.js';
import {leafRoleContext,currentLeafRoles,analyzeVacancyLeafRoles} from './vacancy-leaf-roles.js';
import {prepareVacancyImport,importVacancyCandidate} from './vacancy-import.js';
import {relevanceStage,enqueueImportedRelevance,executeRelevanceStage} from './scheduler-relevance.js';
import {RABOTA_MISSING_BODY} from './rabota-detail.js';
import {schedulerWaitDelay} from './scheduler-wait.js';
export async function schedulerCompanyAllowed(db,id,owner){if(id==null||typeof id==='object'&&id.companyId==null&&id.company_id==null&&id.agencyId==null&&id.agency_id==null)return false;try{await parserCompanyDb(db,id,owner);return true}catch(e){if(e.code==='favorites_only')return false;throw e;}}
export async function scheduleDueSources(db,owner,now=Date.now(),{deadline=Infinity,limit=50,sourceIds}={}){
 const settings=await schedulerSettings(db);if(!settings.sourceIds.length)return;
 const all=await readVacancyParsingSources(db,{favoritesOnly:settings.favoritesOnly,userName:owner});
 const stored=(await db.prepare('SELECT * FROM vacancy_source_parsing').all()).results||[],byKey=new Map(stored.map(r=>[parserOwnerKey(r)+':'+r.source_id,r]));
 const history=(await db.prepare("SELECT j.company_id,j.agency_id,j.source_id,j.dedupe_key,j.status FROM scheduler_jobs j LEFT JOIN vacancy_source_parsing s ON s.company_id IS j.company_id AND s.agency_id IS j.agency_id AND s.source_id=j.source_id WHERE j.generation=(SELECT generation FROM app_runtime_generation WHERE id=1) AND j.type='scan_source' AND (j.company_id IS NOT NULL OR j.agency_id IS NOT NULL) AND (j.status IN ('pending','running') OR (json_extract(j.payload,'$.scheduleRevision')=? AND json_extract(j.payload,'$.dueAt')=coalesce(s.next_run_at,0)))").bind(settings.revision).all()).results||[];
 const active=new Set(history.filter(j=>['pending','running'].includes(j.status)).map(r=>parserOwnerKey(r)+':'+r.source_id)),known=new Set(history.map(j=>j.dedupe_key));
 const candidates=all.filter(s=>s.supported&&settings.sourceIds.includes(s.sourceId)&&(!sourceIds||sourceIds.includes(s.sourceId))&&!active.has(parserOwnerKey(s)+':'+s.sourceId)).map(source=>{
  const row=byKey.get(parserOwnerKey(source)+':'+source.sourceId),fresh=row?.source_url===source.url;
  const last=fresh&&row.last_success_at?Date.parse(row.last_success_at):0;
  const next=fresh?(row.interval_days!==settings.intervalDays&&last?last+settings.intervalDays*86400000:row.next_run_at||0):0;
  let adapterUpgrade=false;try{adapterUpgrade=!!(fresh&&source.config.adapter==='yoomoney'&&JSON.parse(row.config_json).adapter==='generic');}catch{/* invalid custom config is not an adapter upgrade */}
  return {source,row,next,adapterUpgrade};
 }).filter(s=>s.next<=now).sort((a,b)=>Number(a.source.sourceId==='hh')-Number(b.source.sourceId==='hh')||a.next-b.next||(a.row?.last_attempt_at||0)-(b.row?.last_attempt_at||0)||parserOwner(a.source).key.localeCompare(parserOwner(b.source).key)||a.source.sourceId.localeCompare(b.source.sourceId));
 const due=[];
 for(const candidate of candidates){const {source,row,next,adapterUpgrade}=candidate,revision=row?row.revision+(row.source_url!==source.url||!row.enabled||adapterUpgrade?1:0):1;
  const key='scan:'+(source.agencyId?'agency:'+source.agencyId:source.companyId)+':'+source.sourceId+':'+revision+':'+next+':'+settings.revision;
  if(known.has(await candidateHash(key)))continue;due.push(candidate);if(due.length>=limit)break;
 }
 for(const {source,row,next,adapterUpgrade} of due){
  if(Date.now()>=deadline)break;
  // Selecting a source type authorizes it for every eligible company. Keep its
  // per-company extraction config and progress, without individual opt-ins.
  const r=await db.prepare(`INSERT INTO vacancy_source_parsing(${parserOwner(source).column},source_id,source_url,enabled,config_json,revision,auto_enabled,interval_days,next_run_at) VALUES(?,?,?,1,?,1,1,?,?) ON CONFLICT(${parserOwner(source).column},source_id) DO UPDATE SET source_url=excluded.source_url,enabled=1,config_json=excluded.config_json,auto_enabled=1,interval_days=excluded.interval_days,next_run_at=excluded.next_run_at,revision=vacancy_source_parsing.revision+CASE WHEN vacancy_source_parsing.source_url<>excluded.source_url OR vacancy_source_parsing.enabled=0 OR ? THEN 1 ELSE 0 END WHERE ?=0 OR (vacancy_source_parsing.revision=? AND vacancy_source_parsing.source_url=? AND vacancy_source_parsing.config_json=?) RETURNING revision`).bind(parserOwner(source).id,source.sourceId,source.url,JSON.stringify(source.config),settings.intervalDays,next,adapterUpgrade?1:0,adapterUpgrade?1:0,row?.revision??0,row?.source_url??source.url,row?.config_json??'{}').all();
  if(!r.results.length)continue; // An administrator changed the legacy config.
  const revision=r.results[0].revision;
  await enqueueScheduledJob(db,{type:'scan_source',key:'scan:'+(source.agencyId?'agency:'+source.agencyId:source.companyId)+':'+source.sourceId+':'+revision+':'+next+':'+settings.revision,...parserOwnerColumns(source),sourceId:source.sourceId,payload:{sourceRevision:revision,sourceUrl:source.url,manual:false,scheduleRevision:settings.revision,dueAt:next}});
 }
}
export async function enqueueCandidateClassifications(env,ids){
 const db=await candidateDb(env.DB),settings=await schedulerSettings(db);if(!ids.length||!settings.sourceIds.length)return 0;
 const roles=await candidateClassificationContext(db);if(!roles.length)return 0;
 const rows=(await db.prepare(`SELECT id,company_id,agency_id,source_id,title_hash,title_revision FROM vacancy_candidates WHERE id IN (SELECT value FROM json_each(?)) AND source_id IN (SELECT value FROM json_each(?)) AND ${parserOwnerScopeSql('vacancy_candidates')}`).bind(JSON.stringify(ids),JSON.stringify(settings.sourceIds),settings.ownerName).all()).results||[];
 const jobs=await Promise.all(rows.map(async c=>({type:'classify_title',key:'classify_title:'+c.id+':'+await candidateHash(JSON.stringify([c.title_hash,c.title_revision,roles.map(r=>[r.roleId,r.promptVersion,r.modelKey])])),companyId:c.company_id,agencyId:c.agency_id,candidateId:c.id,sourceId:c.source_id})));
 return enqueueScheduledJobs(db,jobs);
}
function selectedSourceDb(db,sourceId){
 return generationBoundDb(db,boundGeneration(db),{sql:"EXISTS(SELECT 1 FROM firecrawl_settings f WHERE f.id=1 AND (EXISTS(SELECT 1 FROM json_each(json_extract(f.scheduler_json,'$.sourceIds')) WHERE value=?) OR (json_type(f.scheduler_json,'$.sourceIds') IS NULL AND EXISTS(SELECT 1 FROM vacancy_source_parsing WHERE auto_enabled=1 AND source_id=?))))",bindings:[sourceId,sourceId],failure:()=>schedulerError('source_scope','Источник исключён из общего списка парсинга.',403)});
}
export async function nextCandidateStage(env,id,{resolveImport=true}={}){
 const db=await candidateDb(env.DB),detail=await getVacancyCandidate(db,id);if(!detail)return null;const c=detail.candidate,settings=await schedulerSettings(db);
 if(!settings.sourceIds.includes(c.sourceId)||!await schedulerCompanyAllowed(db,c,settings.ownerName))return null;
 const roles=await candidateClassificationContext(db);if(!roles.length)return null;
 const version=await candidateHash(JSON.stringify([c.titleHash,c.titleRevision,roles.map(r=>[r.roleId,r.promptVersion,r.modelKey])]));
 const base={...parserOwnerColumns(c),candidateId:c.id,sourceId:c.sourceId},make=(type,key,payload={})=>({...base,type,key:type+':'+id+':'+key,payload});
 if(detail.currentResults.some(r=>!r.decision))return make('classify_title',version);
 if(!detail.currentResults.some(r=>r.decision==='match'))return null;
 const raw=await currentRawSnapshot(db,c),last=await db.prepare("SELECT started_at FROM vacancy_content_fetches WHERE candidate_id=? AND status='success' ORDER BY started_at DESC LIMIT 1").bind(id).first();
 const refresh=!!(raw&&c.titleUpdatedAt&&Date.parse(c.titleUpdatedAt)>(last?.started_at||Date.parse(raw.fetched_at)));
 if(!raw||refresh)return make('fetch_full_text',version+(c.sourceId==='rabota'?':rabota-body-v3':''),{refresh});
 const extraction=await currentExtraction(db,c,raw),x=await extractionContext(db);
 if(!extraction)return make('extract_vacancy',version+':'+raw.id+':'+x.configHash+':'+x.promptVersion);
 const leaf=await leafRoleContext(db,id),result=await currentLeafRoles(db,leaf);
 if(!result)return make('assign_roles',leaf.inputHash);
 // An empty successful selection is the role prompt's explicit no-match.
 // Keep that cached decision; no import or repeated model call is needed.
 const selectedRoles=JSON.parse(result.role_ids);if(Array.isArray(selectedRoles)&&selectedRoles.length===0)return null;
 // A new candidate has no existing main record to skip. Import itself owns
 // duplicate resolution and the guarded plan; handoff needs only dependencies.
 if(resolveImport&&c.mainVacancyId){const plan=await prepareVacancyImport(env,id);if(plan.status==='unchanged')return relevanceStage(env,{vacancyId:c.mainVacancyId,candidateId:c.id,...parserOwnerColumns(c),sourceId:c.sourceId,ownerName:settings.ownerName});}
 return make('import_vacancy',leaf.inputHash+':'+extraction.id+':'+result.id+':auto-separate-v1');
}
export async function enqueueNextStage(env,id){const next=await nextCandidateStage(env,id);return next?enqueueScheduledJob(env.DB,next):null;}
const dependencyCodes=['roles_pending','missing_extraction','missing_markdown','not_accepted'];
export async function candidateTerminalDecision(db,id){
 const detail=await getVacancyCandidate(db,id);if(!detail)return null;
 if(detail.candidate.mainVacancyId)return {state:'imported',vacancyId:detail.candidate.mainVacancyId};
 if(!detail.currentResults.length||detail.currentResults.some(r=>!r.decision))return null;
 if(!detail.currentResults.some(r=>r.decision==='match'))return {state:'filtered',reason:'title_no_match'};
 const x=await leafRoleContext(db,id),result=await currentLeafRoles(db,x);
 if(result&&JSON.parse(result.role_ids).length===0)return {state:'filtered',reason:'roles_no_match',roleResultId:result.id,inputHash:x.inputHash};
 return null;
}
// Repair known missing dependencies, title-only imports and the exact legacy
// missing-body defect without retrying arbitrary failures. The old failure/attempt remains an audit receipt.
export async function reconcileFailedCandidateDependencies(env,{limit=3,deadline=Infinity}={}){
 const db=await candidateDb(env.DB),s=await schedulerSettings(db);if(!s.enabled||!s.sourceIds.length||Date.now()>=deadline)return 0;
 const scope=parserOwnerScopeSql('c');
 const rows=(await db.prepare(`SELECT j.id,c.id AS candidate_id,c.company_id,c.agency_id,c.source_id FROM scheduler_jobs j JOIN vacancy_candidates c ON c.id=j.candidate_id WHERE j.generation=? AND j.status='failed' AND ((j.type='import_vacancy' AND j.reason_code IN (SELECT value FROM json_each(?))) OR (j.type='fetch_full_text' AND c.source_id='rabota' AND j.reason_code='invalid_content' AND EXISTS(SELECT 1 FROM vacancy_raw_snapshots s WHERE s.id=(SELECT id FROM vacancy_raw_snapshots WHERE candidate_id=c.id ORDER BY created_at DESC,rowid DESC LIMIT 1) AND s.content_status='invalid' AND json_array_length(s.diagnostics,'$.errors')=1 AND json_extract(s.diagnostics,'$.errors[0]')=?))) AND json_type(j.payload,'$.dependencyRecovery') IS NULL AND c.main_vacancy_id IS NULL AND c.source_id IN (SELECT value FROM json_each(?)) AND ${scope} AND NOT EXISTS(SELECT 1 FROM scheduler_jobs active WHERE active.generation=j.generation AND active.candidate_id=c.id AND active.status IN ('pending','running')) ORDER BY j.created_at,j.id LIMIT ?`).bind(boundGeneration(db),JSON.stringify([...dependencyCodes,'possible_duplicate','roles_unresolved','roles_stale']),RABOTA_MISSING_BODY,JSON.stringify(s.sourceIds),s.ownerName,Math.min(3,Math.max(0,limit))).all()).results||[];
 let repaired=0;
 for(const row of rows){
  if(Date.now()>=deadline)break;
  const scoped=selectedSourceDb(await parserCompanyDb(db,row,s.ownerName),row.source_id),next=await nextCandidateStage({...env,DB:scoped},row.candidate_id);
  if(!next){
   const terminal=await candidateTerminalDecision(scoped,row.candidate_id);if(!terminal)continue;
   await scoped.prepare("UPDATE scheduler_jobs SET payload=json_set(payload,'$.dependencyRecovery',json(?)) WHERE id=? AND generation=? AND status='failed' AND json_type(payload,'$.dependencyRecovery') IS NULL").bind(JSON.stringify({...terminal,at:Date.now()}),row.id,boundGeneration(db)).run();repaired++;continue;
  }
  const job=await enqueueScheduledJob(scoped,next);if(!job)continue;
  // An exhausted current dependency must not monopolize all three recovery
  // slots on every tick or be relaunched with a new paid operation identity.
  const state=['pending','running'].includes(job.status)?'queued':'blocked';
  await scoped.prepare("UPDATE scheduler_jobs SET payload=json_set(payload,'$.dependencyRecovery',json(?)) WHERE id=? AND generation=? AND status='failed' AND json_type(payload,'$.dependencyRecovery') IS NULL").bind(JSON.stringify({jobId:job.id,type:job.type,state,...(state==='blocked'?{reason:job.reason_code||job.status}:{}),at:Date.now()}),row.id,boundGeneration(db)).run();repaired++;
 }
 return repaired;
}
export async function executeScheduledStage(env,claim,{owner,signal}){
 const {job}=claim,payload=JSON.parse(job.payload);let db=claim.db;env={...env,DB:db,PARSER_RESOURCE_DB:claim.db};
 if(job.type==='diagnostic')return {diagnostic:true,nonce:payload.nonce||'',externalRequests:0};
 if(!await schedulerCompanyAllowed(db,job,owner))throw schedulerError('scope','Компания исключена фильтром избранного.',403);
 db=selectedSourceDb(await parserCompanyDb(db,job,owner),job.source_id);
 if(job.candidate_id){const ownerRef=parserOwner(job);db=generationBoundDb(db,boundGeneration(db),{sql:'EXISTS(SELECT 1 FROM vacancy_candidates WHERE id=? AND company_id IS ? AND agency_id IS ?)',bindings:[job.candidate_id,ownerRef.companyId,ownerRef.agencyId],failure:()=>schedulerError('scope','Первичная вакансия относится к другой организации.',403)});}
 await db.prepare('SELECT 1').first();env={...env,DB:db};
 if(job.type==='scan_source'){
  const source=await readVacancyParsingSource(db,job,job.source_id),row=await db.prepare(`SELECT auto_enabled,interval_days FROM vacancy_source_parsing WHERE ${parserOwner(job).column}=? AND source_id=?`).bind(parserOwner(job).id,job.source_id).first();
  if(!source.enabled||!source.supported||source.revision!==payload.sourceRevision||source.url!==payload.sourceUrl||!payload.manual&&!row?.auto_enabled)throw schedulerError('source_changed','Источник выключен или его настройки изменились.');
  await db.prepare(`UPDATE vacancy_source_parsing SET last_attempt_at=?,last_result='running' WHERE ${parserOwner(job).column}=? AND source_id=?`).bind(Date.now(),parserOwner(job).id,job.source_id).run();
  const previous=payload.runId?(await readVacancyDiscoveryRun(db,payload.runId,owner)).result:null,oldCredits=payload.credits??0;
  const scan=previous?(previous.savePending||previous.saveErrors?.length?previous:previous.paused?await resumeVacancyDiscoveryRun(env,payload.runId,owner,{signal,maxPagesPerSlice:1}):previous):await fetchVacancyListing(env,job,job.source_id,{userName:owner,signal,maxPagesPerSlice:1,discoveryId:job.id});
  const credits=scan.creditsReported===null?null:Math.max(0,scan.creditsReported-oldCredits);payload.credits=scan.creditsReported;payload.runId=scan.runId;await db.prepare('UPDATE scheduler_jobs SET payload=? WHERE id=? AND locked_by=?').bind(JSON.stringify(payload),job.id,claim.owner).run();
  const saved=await saveVacancyDiscoveryRun(db,scan.runId,owner,{maxJobs:10,resourceDb:env.PARSER_RESOURCE_DB});
  if(saved.saveErrors.length)throw schedulerError('storage','Не все найденные вакансии сохранены. Порция доступна для повтора.');
  await enqueueCandidateClassifications(env,saved.jobs.filter(j=>j.existingId).map(j=>j.existingId));
  if(saved.savePending)return {defer:true,code:'slice',delayMs:1000,payload,found:scan.found,credits};
  if(scan.paused)return {defer:true,code:scan.paused.code,delayMs:schedulerWaitDelay(scan.paused.code),payload,found:scan.found,credits};
  if(scan.errors.length&&(scan.errors.every(e=>e.code==='listing_unavailable')||(job.source_id==='rabota'||scan.provider==='yoomoney-public')&&scan.found>0&&scan.errors.every(e=>e.code==='listing_incomplete'))){
   // Preserve the incomplete receipt and last_success_at. Schedule a future fresh
   // check so an honest partial listing does not disable this source forever.
   await db.prepare(`UPDATE vacancy_source_parsing SET next_run_at=? WHERE ${parserOwner(job).column}=? AND source_id=? AND revision=? AND source_url=?`).bind(Date.now()+(await schedulerSettings(db)).intervalDays*86400000,parserOwner(job).id,job.source_id,source.revision,source.url).run();
  }
  if(scan.status==='error'||scan.errors.length)throw schedulerError(scan.errors[0]?.code||'source_error',scan.errors[0]?.message||'Не удалось обработать источник.');
  await db.prepare(`UPDATE vacancy_source_parsing SET next_run_at=?,last_result=?,last_error=? WHERE ${parserOwner(job).column}=? AND source_id=? AND revision=?`).bind(Date.now()+(await schedulerSettings(db)).intervalDays*86400000,scan.status==='complete'?'success':'partial',scan.status==='complete'?'':scan.warnings[0]||'Полнота списка не подтверждена.',parserOwner(job).id,job.source_id,source.revision).run();
  return {found:scan.found,savedNew:saved.savedNew,credits};
 }
 if(job.type==='score_relevance')return executeRelevanceStage(env,claim,{owner,signal});
 const id=job.candidate_id;
 if(job.type==='classify_title'){
  const r=await classifyStoredCandidate(env,id,{signal,recordTechnicalTimeout:true});const missing=r.currentResults.some(x=>!x.decision);if(missing){const attempt=r.attemptId?await db.prepare('SELECT errors_json FROM vacancy_classification_attempts WHERE id=?').bind(r.attemptId).first():null,error=attempt?JSON.parse(attempt.errors_json)[0]:null;throw schedulerError(error?.code||'no_active_roles',error?.message||'Нет актуальных решений JEV.');}
  await enqueueNextStage(env,id);return {accepted:r.currentResults.some(r=>r.decision==='match'),reused:r.reused};
 }
 if(job.type==='fetch_full_text'){const r=await fetchCandidateMarkdown(env,id,{signal,allowImported:true,refresh:!!payload.refresh,confirmed:!!payload.refresh});await enqueueNextStage(env,id);return {snapshotId:r.snapshot.id,reused:r.reused,credits:r.reused?0:r.snapshot.firecrawl_credits};}
 if(job.type==='extract_vacancy'){const r=await extractCandidateMarkdown(env,id,{signal,allowImported:true});await enqueueNextStage(env,id);return {extractionId:r.extraction.id,reused:r.reused};}
 if(job.type==='assign_roles'){const r=await analyzeVacancyLeafRoles(env,id,{signal});await enqueueNextStage(env,id);return {roleIds:r.roleIds,filtered:r.roleIds.length===0,reused:r.reused};}
 if(job.type==='import_vacancy'){
  const scoped=await parserCompanyDb(db,job,owner),scopedEnv={...env,DB:scoped};
  const supersede=async next=>{if(next)await enqueueScheduledJob(scoped,next);return {superseded:true,nextStage:next?.type??null};};
  const sameImport=async next=>next?.type==='import_vacancy'&&await candidateHash(next.key)===job.dedupe_key;
  // A queued import belongs to its exact model/prompt/snapshot hash. Settings
  // can change while a previous model request or its handoff is in flight.
  const next=await nextCandidateStage(scopedEnv,id,{resolveImport:false});if(!await sameImport(next))return supersede(next);
  let plan=await prepareVacancyImport(scopedEnv,id);
  // Title equality is not publication identity. The unattended policy keeps
  // unconfirmed publications separate, while exact URL/ID conflicts still fail.
  const separate=plan.status==='possible_duplicate';
  const possibleDuplicateIds=separate?plan.possibleDuplicates.map(v=>v.id):[];
  if(separate)plan=await prepareVacancyImport(scopedEnv,id,{createNew:true});
  if(dependencyCodes.includes(plan.code)){
   const current=await nextCandidateStage(scopedEnv,id,{resolveImport:false});if(!await sameImport(current))return supersede(current);
   throw schedulerError('pipeline_changed','Зависимости изменились во время проверки импорта. Этап будет проверен повторно.');
  }
  if(!['create','update','unchanged'].includes(plan.status))throw schedulerError(plan.code||'import_blocked',plan.reason||'Автоматическое безопасное решение невозможно.');
  const result=await importVacancyCandidate(scopedEnv,id,{token:plan.token,resolution:plan.resolution,operationId:job.id,batchId:'scheduler:'+claim.runId});
  await enqueueImportedRelevance(scopedEnv,id,owner);return {...result,...(separate?{duplicateResolution:'separate',possibleDuplicateIds}:{})};
 }
 throw schedulerError('type','Неизвестный обработчик.');
}
