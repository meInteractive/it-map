import {agencyCandidateDb} from '../repositories/parser-owner.js';
import {candidateDb,candidateId,candidateError,candidateHash,candidateClassificationContext,getVacancyCandidate} from '../repositories/vacancy-candidates.js';
import {classifyVacancyTitle} from './vacancy-classification.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {JEV_MODEL_ID} from '../integrations/jev.js';

const nowSql="(CAST(strftime('%s','now') AS INTEGER)*1000)";
const changed=()=>Object.assign(candidateError('Заголовок, настройки или владелец обработки изменились. Результат не сохранён. Повторите проверку.',409),{code:'candidate_changed'});

export async function classifyStoredCandidate(env,id,{force=false,forceRoleIds=[],recordTechnicalTimeout=false,signal}={}){
 if(!Array.isArray(forceRoleIds)||forceRoleIds.some(id=>!Number.isSafeInteger(id)||id<1))throw candidateError('Некорректные ID принудительных направлений.');
 if(typeof force!=='boolean')throw candidateError('force должен быть логическим значением.');
 id=candidateId(id);const db=await agencyCandidateDb(await candidateDb(env.DB),id);env={...env,DB:db};
 const initial=await getVacancyCandidate(db,id);if(!initial)throw candidateError('Первичная вакансия не найдена.',404);
 const snapshot=initial.candidate,roles=await candidateClassificationContext(db),current=new Map(initial.currentResults.map(row=>[row.roleId,row]));
 const needed=roles.filter(role=>force||forceRoleIds.includes(role.roleId)||!current.get(role.roleId)?.decision||current.get(role.roleId).promptVersion!==role.promptVersion);
 if(!needed.length)return {...await getVacancyCandidate(db,id),reused:true,requestedRoleIds:[],attemptId:null};
 const owner=crypto.randomUUID(),generation=boundGeneration(db);
 // A durable lease prevents parallel workers from billing the same candidate.
 await db.prepare(`INSERT INTO vacancy_candidate_classification_leases(candidate_id,generation,owner_token,expires_at)
  SELECT ?,?,?,${nowSql}+300000 WHERE EXISTS(SELECT 1 FROM vacancy_candidates WHERE id=? AND title_revision=? AND title_hash=?)
  ON CONFLICT(candidate_id) DO UPDATE SET generation=excluded.generation,owner_token=excluded.owner_token,expires_at=excluded.expires_at
  WHERE vacancy_candidate_classification_leases.generation<>excluded.generation OR vacancy_candidate_classification_leases.expires_at<=${nowSql}`)
 .bind(id,generation,owner,id,snapshot.titleRevision,snapshot.titleHash).run();
 const acquired=await db.prepare(`SELECT owner_token FROM vacancy_candidate_classification_leases WHERE candidate_id=? AND owner_token=? AND generation=? AND expires_at>${nowSql}`).bind(id,owner,generation).first();
 if(!acquired)throw Object.assign(candidateError('Эта первичная вакансия уже классифицируется или была изменена. Повторите позже.',409),{code:'candidate_busy'});
 try{
  signal?.throwIfAborted();
  if(await candidateHash(snapshot.titleRaw.trim())!==snapshot.titleHash)throw changed();
  // Recheck after acquiring ownership: an immediately preceding worker may
  // have completed between the first cache read and the atomic lease claim.
  const refreshed=await getVacancyCandidate(db,id),fresh=new Map(refreshed.currentResults.map(row=>[row.roleId,row]));
  if(refreshed.candidate.titleRevision!==snapshot.titleRevision||refreshed.candidate.titleHash!==snapshot.titleHash)throw changed();
  const selected=needed.filter(role=>force||forceRoleIds.includes(role.roleId)||!fresh.get(role.roleId)?.decision||fresh.get(role.roleId).promptVersion!==role.promptVersion);
  if(!selected.length)return {...refreshed,reused:true,requestedRoleIds:[],attemptId:null};
  const requestStarted=Date.now();let response;
  try{response=await classifyVacancyTitle(env,snapshot.titleRaw,{signal,roleIds:selected.map(role=>role.roleId),expectedPromptVersions:Object.fromEntries(selected.map(role=>[role.roleId,role.promptVersion]))});}
  catch(error){if(!recordTechnicalTimeout||!['TimeoutError','AbortError'].includes(error.name))throw error;response={model:null,durationMs:Date.now()-requestStarted,costUsd:null,results:selected.map(role=>({roleId:role.roleId,promptVersion:role.promptVersion,promptRevision:role.revision,decision:null,error:{code:'timeout',message:'Истекло время ожидания JEV. Успешные решения сохранены; продолжите пакет позже.'}}))};}
  const idToRole=new Map(selected.map(role=>[role.roleId,role]));
  const errors=response.results.filter(row=>row.error).map(row=>({roleId:row.roleId,promptVersion:row.promptVersion||idToRole.get(row.roleId)?.promptVersion,code:row.error.code,message:row.error.message}));
  const decisions=response.results.filter(row=>!row.error&&['match','reject'].includes(row.decision));
  const attemptId=crypto.randomUUID(),classifiedAt=new Date().toISOString(),outcome=!errors.length?'complete':decisions.length?'partial':'error';
  const statements=[
   db.prepare(`SELECT CASE WHEN EXISTS(SELECT 1 FROM vacancy_candidates c JOIN vacancy_candidate_classification_leases l ON l.candidate_id=c.id WHERE c.id=? AND c.title_revision=? AND c.title_hash=? AND l.owner_token=? AND l.generation=? AND l.expires_at>${nowSql}) THEN 1 ELSE abs(-9223372036854775808) END AS candidate_guard`).bind(id,snapshot.titleRevision,snapshot.titleHash,owner,generation),
   db.prepare(`INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,errors_json,outcome,classified_at,duration_ms,cost_usd) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`)
    .bind(attemptId,id,snapshot.titleRaw,snapshot.titleHash,snapshot.titleRevision,JEV_MODEL_ID,response.model,JSON.stringify(selected.map(({roleId,name,promptVersion,revision})=>({roleId,name,promptVersion,promptRevision:revision}))),JSON.stringify(errors),outcome,classifiedAt,response.requestDurationMs??response.durationMs,response.costUsd)
  ];
  for(const decision of decisions){
   const role=idToRole.get(decision.roleId);
   if(!role||decision.promptVersion!==role.promptVersion||!response.model)throw changed();
   statements.push(db.prepare(`INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`)
    .bind(id,attemptId,role.roleId,role.name,decision.decision,JEV_MODEL_ID,response.model,decision.promptVersion,decision.promptRevision,snapshot.titleHash,snapshot.titleRevision,classifiedAt));
  }
  if(!recordTechnicalTimeout)signal?.throwIfAborted();
  try{await db.batch(statements);}catch(error){if(error.code==='restored')throw error;if(/overflow/i.test(error.message))throw changed();throw error;}
  return {...await getVacancyCandidate(db,id),reused:false,requestedRoleIds:selected.map(role=>role.roleId),attemptId};
 }finally{
  // Generation and owner matching cannot delete a successor's lease. This
  // cleanup also succeeds after restore without writing an obsolete result.
  await env.DB.prepare('DELETE FROM vacancy_candidate_classification_leases WHERE candidate_id=? AND generation=? AND owner_token=?').bind(id,generation,owner).run().catch(()=>{});
 }
}
