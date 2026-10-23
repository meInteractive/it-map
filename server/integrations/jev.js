import {withSchedulerBilling} from './scheduler-billing.js';
import {withClassificationSlot} from '../repositories/classification-slots.js';
// Jev returns typed decisions through OpenRouter, not chat completions.
import {orSettings,orKey,orFetch,orNumeric,orLog,OpenRouterError,orError} from './openrouter.js';
import {ensureRuntimeGeneration} from '../repositories/runtime-generation.js';
import {generationBoundDb,boundGeneration} from '../repositories/generation-bound-db.js';

export const JEV_MODEL_ID='typesafe/jev-1.13';
export async function jevCatalogModel(env,signal){
 const settings=await orSettings(env),key=await orKey(env,settings);
 // The models endpoint defaults to text output. JEV is a decision model, so it
 // must be requested explicitly or it is omitted even while available.
 const catalog=await orFetch(key,'models?output_modalities=decisions',undefined,signal);
 const model=catalog.data?.find(m=>m.id===JEV_MODEL_ID);
 if(!model||!(orNumeric(model.pricing?.prompt)>0))throw new OpenRouterError('jev_unavailable','Платная JEV отсутствует в каталоге OpenRouter. Выберите «Лучшая бесплатная» или повторите позже.',409);
 return {id:model.id,name:model.name||'JEV',contextLength:orNumeric(model.context_length),pricing:model.pricing,source:'https://openrouter.ai/'+JEV_MODEL_ID,fetchedAt:new Date().toISOString()};
}

const relevanceLevels=[
 'Совпадений с профилем кандидата нет (0 из 100).',
 'Почти не соответствует профилю (11 из 100).',
 'Соответствует по отдельным второстепенным требованиям (22 из 100).',
 'Есть несколько совпадений, существенные требования не закрыты (33 из 100).',
 'Частичное соответствие с заметными пробелами (44 из 100).',
 'Соответствует примерно половине существенных требований (56 из 100).',
 'Хорошее соответствие, остаются существенные пробелы (67 из 100).',
 'Большинство существенных требований подтверждены (78 из 100).',
 'Очень высокое соответствие, небольшие пробелы (89 из 100).',
 'Полное соответствие основным требованиям и ответственности (100 из 100).'
];

export async function runJevRelevance(env,options){
 if(options.task!=='relevance')throw new OpenRouterError('unsupported_task','JEV доступна только для оценки релевантности.',400);
 const db=generationBoundDb(env.DB,boundGeneration(env.DB)||await ensureRuntimeGeneration(env.DB));
 env={...env,DB:db};
 const settings=await orSettings(env),key=await orKey(env,settings);
 const instructions=options.instructions||options.messages?.filter(m=>m.role==='system'||m.role==='developer').map(m=>m.content).join('\n');
 const state=options.state??options.messages?.filter(m=>m.role!=='system'&&m.role!=='developer');
 if(typeof instructions!=='string'||!instructions.trim()||state==null)throw new OpenRouterError('input','Передайте промт и данные для оценки релевантности.',400);
 const started=Date.now();let actual=null;
 try{
  options.signal?.throwIfAborted();await options.checkActive?.();
  const catalog=await jevCatalogModel(env,options.signal);
  options.onProgress?.({stage:'model',model:'JEV',attempt:1,total:1});
  const payload={model:JEV_MODEL_ID,state,questions:{relevance:{type:'score',instructions:instructions+'\n\nОпредели положение на упорядоченной шкале criteria от наименьшего к наибольшему соответствию. Используй типизированный ответ score Decisions API; перевод оценки в 0–100 выполняет приложение.',criteria:relevanceLevels}}};
  const response=await withSchedulerBilling(env,{provider:'openrouter',model:JEV_MODEL_ID,pricing:catalog.pricing,contextLength:catalog.contextLength,payload},()=>orFetch(key,'decisions',payload,options.signal,'alpha'));
  actual=response.model;
  if(actual!==JEV_MODEL_ID&&!actual?.startsWith(JEV_MODEL_ID+'-'))throw new OpenRouterError('model_mismatch','OpenRouter вернул другую модель вместо JEV.',502);
  const answer=response.answers?.relevance;
  if(answer?.type!=='score'||typeof answer.score!=='number'||!Number.isFinite(answer.score)||answer.score<0||answer.score>9)throw new OpenRouterError('invalid_output','JEV вернула некорректную оценку релевантности.',502);
  const relevance=Math.round(answer.score*100/9),content=JSON.stringify({relevance});
  if(options.validateContent&&!options.validateContent(content))throw new OpenRouterError('invalid_output','Ответ JEV не соответствует ожидаемому формату.',502);
  options.signal?.throwIfAborted();await options.checkActive?.();
  const current=await orSettings(env);
  if(current.revision!==settings.revision)throw orError('changed',409);
  await env.DB.prepare('UPDATE openrouter_settings SET last_used_model=?,last_used_at=? WHERE id=1 AND revision=?').bind(actual,new Date().toISOString(),settings.revision).run();
  await orLog(env,{task:'relevance',primary:JEV_MODEL_ID,actual,success:true});
  return {model:actual,relevance,content,decision:answer,usage:response.usage,fallback_used:false,attempted_models:[JEV_MODEL_ID],duration_ms:Date.now()-started};
 }catch(error){
  options.signal?.throwIfAborted();
  if(error.code==='restored'||error.code==='relevance_changed')throw error;
  await orLog(env,{task:'relevance',primary:JEV_MODEL_ID,actual,success:false,error:error.code||'internal'});
  if(error.providerStatus===402)throw new OpenRouterError('credits','Недостаточно средств на балансе OpenRouter для JEV.',402);
  throw error;
 }
}

// Independent choice questions share one title and one billed request. The
// existing relevance score path, provider choice and settings stay untouched.
export async function runJevTitleClassification(env,options) {env={...env,DB:boundGeneration(env.DB)?env.DB:generationBoundDb(env.DB,await ensureRuntimeGeneration(env.DB))};return withClassificationSlot(env,()=>runTitleRequest(env,options));}
async function runTitleRequest(env,{title,roles,signal}) {
 const settings=await orSettings(env),key=await orKey(env,settings);let actual=null;
 try{
  const catalog=await jevCatalogModel(env,signal);
  const questions=Object.fromEntries(roles.map(role=>['role_'+role.roleId,{
   type:'choice',instructions:role.markdown,
   criteria:{match:'Название вакансии подходит профессиональному направлению по правилам промта.',reject:'Название вакансии не подходит профессиональному направлению по правилам промта.'}
  }]));
  const requestStarted=Date.now();
  const payload={model:JEV_MODEL_ID,state:{title},questions};
  const response=await withSchedulerBilling(env,{provider:'openrouter',model:JEV_MODEL_ID,pricing:catalog.pricing,contextLength:catalog.contextLength,payload},()=>orFetch(key,'decisions',payload,signal,'alpha'));
  const requestDurationMs=Date.now()-requestStarted;
  actual=response.model;
  if(actual!==JEV_MODEL_ID&&!actual?.startsWith(JEV_MODEL_ID+'-'))throw new OpenRouterError('model_mismatch','OpenRouter вернул другую модель вместо JEV.',502);
  signal?.throwIfAborted();
  if((await orSettings(env)).revision!==settings.revision)throw orError('changed',409);
  const results=roles.map(role=>{
   const answer=response.answers?.['role_'+role.roleId];
   const valid=answer?.type==='choice'&&['match','reject'].includes(answer.choice);
   return {roleId:role.roleId,name:role.name,promptRevision:role.revision,
    decision:valid?answer.choice:null,
    error:valid?null:{code:'invalid_output',message:'JEV не вернула корректное бинарное решение для этой роли.'}};
  });
  await env.DB.prepare('UPDATE openrouter_settings SET last_used_model=?,last_used_at=? WHERE id=1 AND revision=?').bind(actual,new Date().toISOString(),settings.revision).run();
  await orLog(env,{task:'title_classification',primary:JEV_MODEL_ID,actual,success:results.every(r=>!r.error),error:results.some(r=>r.error)?'invalid_output':null});
  const cost=orNumeric(response.usage?.cost);
  return {model:actual,results,requestDurationMs,usage:response.usage||null,costUsd:cost!==null&&cost>=0?cost:null};
 }catch(error){
  signal?.throwIfAborted();
  if(error.code==='restored')throw error;
  await orLog(env,{task:'title_classification',primary:JEV_MODEL_ID,actual,success:false,error:error.code||'internal'});
  if(error.providerStatus===402)throw new OpenRouterError('credits','Недостаточно средств на балансе OpenRouter для JEV.',402);
  throw error;
 }
}
