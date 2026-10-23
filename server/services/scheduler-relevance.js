import {parserOwner,parserOwnerScopeSql} from '../repositories/parser-owner.js';
import {candidateDb,candidateHash} from '../repositories/vacancy-candidates.js';
import {boundGeneration,generationBoundDb} from '../repositories/generation-bound-db.js';
import {parserCompanyScopeSql} from '../repositories/parser-company-scope.js';
import {schedulerSettings,enqueueScheduledJob,schedulerError} from '../repositories/scheduler.js';
import {readVacancyRelevanceInput,recalculateVacancyRelevance} from './vacancy-relevance.js';

// Scores are personal to the scheduler owner. Queue payloads contain IDs/hashes,
// never the resume or source text. A saved zero is a completed score too.
export async function relevanceStage(env,{vacancyId,candidateId,companyId,agencyId=null,sourceId,ownerName}){
 const db=await candidateDb(env.DB),input=await readVacancyRelevanceInput(db,ownerName,vacancyId);
 if(!input||input.relevance!=null||input.archived||!['company','agency'].includes(input.entityType)||!input.resumeMd?.trim()||!input.promptMd?.trim()||input.provider!=='openrouter'||input.openrouterModel!=='jev')return null;
 if(input.entityType==='agency'&&!(await schedulerSettings(db)).agenciesEnabled)return null;
 const hash=await candidateHash(input.snapshot);
 return {type:'score_relevance',key:'relevance:'+ownerName+':'+vacancyId+':'+hash,companyId,agencyId,candidateId,sourceId,payload:{vacancyId,ownerName}};
}
export async function enqueueImportedRelevance(env,candidateId,ownerName){
 const c=await env.DB.prepare('SELECT company_id,agency_id,source_id,main_vacancy_id FROM vacancy_candidates WHERE id=?').bind(candidateId).first();
 if(!c?.main_vacancy_id)return null;
 const stage=await relevanceStage(env,{vacancyId:c.main_vacancy_id,candidateId,companyId:c.company_id,agencyId:c.agency_id,sourceId:c.source_id,ownerName});
 return stage?enqueueScheduledJob(env.DB,stage):null;
}
export async function enqueueMissingRelevance(env,{limit=5,deadline=Infinity}={}){
 const db=await candidateDb(env.DB),s=await schedulerSettings(db);if(!s.enabled||!s.sourceIds.length)return 0;
 const configured=await db.prepare("SELECT 1 FROM user_profiles p JOIN prompt_model_settings m ON m.kind='relevance' AND m.provider='openrouter' AND m.openrouter_model='jev' JOIN system_prompts prompt ON prompt.kind='relevance' WHERE p.user_name=? AND trim(p.resume_md)<>'' AND trim(prompt.markdown)<>''").bind(s.ownerName).first();if(!configured)return 0;
 // Existing imported records must progress even while scans keep the queue busy.
 // Previously queued failures remain visible and are never restarted in a loop.
 const scope=parserOwnerScopeSql('c');
 const rows=(await db.prepare(`SELECT c.id,c.company_id,c.agency_id,c.source_id,c.main_vacancy_id FROM vacancy_candidates c JOIN company_vacancies v ON v.id=c.main_vacancy_id LEFT JOIN company_identity i ON i.id=c.company_id LEFT JOIN agency_identity ai ON ai.id=c.agency_id LEFT JOIN company_vacancy_user_state p ON p.vacancy_id=v.id AND p.user_name=? WHERE p.relevance IS NULL AND v.archived=0 AND ((v.entity_type='company' AND c.agency_id IS NULL AND i.company_name=v.company_name) OR (v.entity_type='agency' AND c.company_id IS NULL AND ai.agency_name=v.company_name)) AND c.source_id IN (SELECT value FROM json_each(?)) AND ${scope} AND NOT EXISTS(SELECT 1 FROM scheduler_jobs j WHERE j.generation=? AND j.type='score_relevance' AND json_extract(j.payload,'$.vacancyId')=v.id AND json_extract(j.payload,'$.ownerName')=?) GROUP BY v.id ORDER BY v.id DESC LIMIT ?`).bind(s.ownerName,JSON.stringify(s.sourceIds),s.ownerName,boundGeneration(db),s.ownerName,Math.min(10,Math.max(0,limit))).all()).results||[];
 let added=0;for(const c of rows){if(Date.now()>=deadline)break;const job=await enqueueImportedRelevance({...env,DB:db},c.id,s.ownerName);if(job)added++;}return added;
}
export async function executeRelevanceStage(env,claim,{owner,signal}){
 const {vacancyId,ownerName}=JSON.parse(claim.job.payload);
 if(!Number.isSafeInteger(vacancyId)||vacancyId<1||ownerName!==owner)throw schedulerError('relevance_scope','Владелец задания релевантности изменился.',403);
 const ownerRef=parserOwner(claim.job);
 const db=generationBoundDb(env.DB,boundGeneration(env.DB),{sql:`EXISTS(SELECT 1 FROM company_vacancies v JOIN ${ownerRef.identityTable} i ON i.${ownerRef.nameColumn}=v.company_name WHERE v.id=? AND i.id=? AND v.archived=0 AND v.entity_type=?) AND EXISTS(SELECT 1 FROM firecrawl_settings WHERE id=1 AND json_extract(scheduler_json,'$.ownerName')=?)`,bindings:[vacancyId,ownerRef.id,ownerRef.type,owner],failure:()=>schedulerError('relevance_scope','Вакансия или владелец задания изменились.',403)});
 const r=await recalculateVacancyRelevance(db,{...env,DB:db},owner,vacancyId,signal,{onlyMissing:true,requireJev:true});
 if(!r)throw schedulerError('not_found','Вакансия удалена.',404);return r;
}
