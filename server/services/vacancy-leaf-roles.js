import {operationErrorCode} from './operation-error.js';
import {candidateDb,candidateId,candidateHash,getVacancyCandidate,candidateError} from '../repositories/vacancy-candidates.js';
import {currentRawSnapshot,snapshotMarkdown} from './vacancy-fulltext.js';
import {currentExtraction,extractionContext,extractionUsage,withLlmSlot} from './vacancy-extraction.js';
import {validateVacancyExtraction,EXTRACTION_INPUT_LIMIT} from './vacancy-extraction-schema.js';
import {readVacancyRoleCatalog} from '../repositories/vacancy-roles.js';
import {readDetailSettings,detailModel} from '../repositories/vacancy-detail-settings.js';
import {runPromptAI} from '../integrations/openai.js';
import {withDetailLease} from './vacancy-detail-leases.js';
export const importError=(code,message,status=409)=>Object.assign(candidateError(message,status),{code});
export async function preparedVacancyData(db,id){
 db=await candidateDb(db);const detail=await getVacancyCandidate(db,candidateId(id));if(!detail)throw importError('not_found','Первичная вакансия не найдена.',404);
 const matches=detail.currentResults.filter(r=>r.decision==='match');if(!matches.length)throw importError('not_accepted','Нет актуального match активного направления.');
 const snapshot=await currentRawSnapshot(db,detail.candidate);if(!snapshot)throw importError('missing_markdown','Нет проверенного исходного Markdown.');
 const context=await extractionContext(db),extraction=await currentExtraction(db,detail.candidate,snapshot,context);if(!extraction)throw importError('missing_extraction','Нет актуальных структурированных данных.');
 const raw=await snapshotMarkdown(db,snapshot),data=validateVacancyExtraction(extraction.extracted_data,raw);
 return {detail,candidate:detail.candidate,matches,snapshot,extraction,raw,data};
}
export async function leafRoleContext(db,id){
 const p=await preparedVacancyData(db,id),catalog=await readVacancyRoleCatalog(db),roots=new Set(p.matches.map(r=>r.roleId));
 const allowed=catalog.leaves.filter(r=>r.parentId!==null&&p.matches.some(m=>catalog.byId.get(m.roleId)?.descendantIds.includes(r.id))).map(r=>({id:r.id,path:r.path,name:r.name,stableKey:r.stableKey,parentId:r.parentId}));
 const treeHash=await candidateHash(JSON.stringify(allowed.sort((a,b)=>a.id-b.id))),s=await readDetailSettings(db,'vacancy_role'),config=detailModel(s,'vacancy_role'),promptVersion=await candidateHash(s.markdown),configHash=await candidateHash(JSON.stringify([config.provider,config.openaiModel,config.openrouterModel,config.reasoningEffort]));
 const inputHash=await candidateHash(JSON.stringify([config.provider==='openrouter'&&config.openrouterModel==='jev'?2:1,p.candidate.id,p.candidate.titleHash,p.candidate.titleRevision,p.snapshot.id,p.snapshot.content_hash,treeHash,p.matches.map(r=>[r.roleId,r.promptVersion]),promptVersion,configHash]));
 return {...p,allowed,treeHash,prompt:s.markdown,promptVersion,config,configHash,inputHash};
}
export async function currentLeafRoles(db,x){return db.prepare("SELECT * FROM vacancy_role_results WHERE candidate_id=? AND input_hash=? AND status='success' ORDER BY processed_at DESC,rowid DESC LIMIT 1").bind(x.candidate.id,x.inputHash).first();}
export function validateLeafRoleIds(content,allowed){let value;try{value=JSON.parse(content);}catch{throw importError('invalid_output','Ответ модели не является JSON.',422);}if(!value||Array.isArray(value)||Object.keys(value).length!==1||!Array.isArray(value.role_ids)||value.role_ids.length>12||value.role_ids.some(id=>!Number.isSafeInteger(id)||!allowed.some(r=>r.id===id))||new Set(value.role_ids).size!==value.role_ids.length)throw importError('invalid_output','Модель должна выбрать только разрешённые конечные ID (не более12).',422);return value.role_ids.sort((a,b)=>a-b);}
export async function analyzeVacancyLeafRoles(env,id,{signal}={}){
 const db=await candidateDb(env.DB);return withDetailLease(db,'candidate:'+candidateId(id),async owned=>{
 const x=await leafRoleContext(owned,id),cached=await currentLeafRoles(owned,x);if(cached)return {result:cached,roleIds:validateLeafRoleIds(JSON.stringify({role_ids:JSON.parse(cached.role_ids)}),x.allowed),reused:true};
 if(!x.allowed.length)throw importError('no_leaf_roles','В подходящих направлениях нет дочерних конечных ролей.');if(x.raw.length>EXTRACTION_INPUT_LIMIT)throw importError('input_too_large','Markdown сохранён целиком, но превышает безопасный лимит LLM.');if(!x.prompt.trim())throw importError('empty_prompt','Заполните промт определения роли.');
 const started=Date.now(),resultId=crypto.randomUUID();let response,usage={input:null,output:null,cost:null};
 try{
 const schema={type:'object',additionalProperties:false,required:['role_ids'],properties:{role_ids:{type:'array',items:{type:'integer',enum:x.allowed.map(r=>r.id)}}}};
 const decision=x.config.provider==='openrouter'&&x.config.openrouterModel==='jev';
 response=await withLlmSlot(owned,slotDb=>runPromptAI({...env,DB:slotDb},{task:'vacancy_role',maxAttempts:1,maxTokens:4096,signal,validateContent:content=>{validateLeafRoleIds(content,x.allowed);return true;},...(decision?{instructions:x.prompt,state:{title:x.candidate.titleRaw,markdown:x.raw},roles:x.allowed.map(({id,path})=>({id,path}))}:{responseFormat:{type:'json_schema',json_schema:{name:'vacancy_leaf_roles',strict:true,schema}},messages:[{role:'system',content:x.prompt+'\nСхема ответа: '+JSON.stringify(schema)},{role:'user',content:JSON.stringify({title:x.candidate.titleRaw,markdown:x.raw,allowedRoles:x.allowed.map(({id,path})=>({id,path}))})}]})},x.config));
 usage=extractionUsage(response,x.config.provider);const ids=validateLeafRoleIds(response.message.content,x.allowed);
 await owned.prepare("INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,prompt_version,config_hash,model_id,provider,role_ids,status,processed_at,duration_ms,input_tokens,output_tokens,cost_usd) VALUES(?,?,?,?,?,?,?,?,?,?,'success',?,?,?,?,?)").bind(resultId,x.candidate.id,x.snapshot.id,x.inputHash,x.treeHash,x.promptVersion,x.configHash,response.model,x.config.provider,JSON.stringify(ids),new Date().toISOString(),Date.now()-started,usage.input,usage.output,usage.cost).run();
 return {result:await owned.prepare('SELECT * FROM vacancy_role_results WHERE id=?').bind(resultId).first(),roleIds:ids,reused:false};
 }catch(e){if(!['restored','detail_changed','llm_busy'].includes(e.code))await owned.prepare("INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,prompt_version,config_hash,model_id,provider,status,error_code,message,processed_at,duration_ms,input_tokens,output_tokens,cost_usd) VALUES(?,?,?,?,?,?,?,?,?,'error',?,?,?,?,?,?,?)").bind(resultId,x.candidate.id,x.snapshot.id,x.inputHash,x.treeHash,x.promptVersion,x.configHash,response?.model||null,x.config.provider,operationErrorCode(e),e.status?e.message:'Запрос прерван; стоимость неизвестна.',new Date().toISOString(),Date.now()-started,usage.input,usage.output,usage.cost).run();throw e;}
 });
}
