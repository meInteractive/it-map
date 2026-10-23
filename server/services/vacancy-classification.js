import {candidateHash,candidateError} from '../repositories/vacancy-candidates.js';
import {activeClassificationRoles} from '../repositories/classification-prompts.js';
import {runJevTitleClassification,JEV_MODEL_ID} from '../integrations/jev.js';
import {ensureRuntimeGeneration} from '../repositories/runtime-generation.js';
import {generationBoundDb,boundGeneration} from '../repositories/generation-bound-db.js';

export function validateVacancyTitle(value) {
 if(typeof value!=='string'||!value.trim()||value.includes('\0')||Array.from(value).length>500)throw Object.assign(new Error('Укажите название вакансии, не более 500 символов.'),{status:400});
 return value.trim();
}

export async function classifyVacancyTitle(env,title,options={}) {
 title=validateVacancyTitle(title);
 const started=Date.now(),generation=boundGeneration(env.DB)||await ensureRuntimeGeneration(env.DB);
 env={...env,DB:boundGeneration(env.DB)?env.DB:generationBoundDb(env.DB,generation)};
 let roles=await activeClassificationRoles(env.DB);
 if(options.roleIds!==undefined){
  if(!Array.isArray(options.roleIds)||options.roleIds.some(id=>!Number.isSafeInteger(id)||id<1))throw candidateError('Некорректные ID направлений.');
  const selected=new Set(options.roleIds);roles=roles.filter(role=>selected.has(role.roleId));
  if(roles.length!==selected.size)throw candidateError('Состав активных направлений изменился. Повторите проверку.',409);
 }
 roles=await Promise.all(roles.map(async role=>({...role,promptVersion:await candidateHash(role.markdown)})));
 if(options.expectedPromptVersions&&roles.some(role=>options.expectedPromptVersions[role.roleId]!==role.promptVersion))throw candidateError('Промты изменились до классификации. Повторите проверку.',409);
 const base={title,checkedRoleIds:roles.map(r=>r.roleId),model:null,results:[],accepted:null,status:'no_active_roles',durationMs:Date.now()-started,requestDurationMs:null,costUsd:null,costRub:null,rubPerUsd:85};
 if(!roles.length)return base;
 try{
  const response=await runJevTitleClassification(env,{title,roles,signal:options.signal});
  // Discard answers for directions whose prompt/toggle/root changed in flight.
  const current=await activeClassificationRoles(env.DB),byId=new Map(current.map(role=>[role.roleId,role]));
  const results=response.results.map(result=>{
   const role=byId.get(result.roleId);result={...result,promptVersion:roles.find(item=>item.roleId===result.roleId)?.promptVersion};
   return role&&role.revision===result.promptRevision?result:{...result,decision:null,error:{code:'changed',message:'Промт или направление изменены во время проверки. Повторите запрос.'}};
  });
  const complete=results.every(r=>!r.error),matched=results.some(r=>r.decision==='match');
  return {...base,model:response.model,results,accepted:matched?true:complete?false:null,status:complete?'complete':'incomplete',durationMs:Date.now()-started,requestDurationMs:response.requestDurationMs,costUsd:response.costUsd,costRub:response.costUsd===null?null:response.costUsd*85};
 }catch(error){
  options.signal?.throwIfAborted();
  if(error.code==='restored')throw error;
  const technical={code:error.code||'internal',message:error.code?error.message:'Не удалось выполнить классификацию. Повторите запрос.'};
  return {...base,status:'incomplete',requestedModel:JEV_MODEL_ID,results:roles.map(role=>({roleId:role.roleId,name:role.name,promptRevision:role.revision,promptVersion:role.promptVersion,decision:null,error:technical})),durationMs:Date.now()-started,error:technical};
 }
}
