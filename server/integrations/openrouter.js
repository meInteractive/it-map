import {withSchedulerBilling} from './scheduler-billing.js';
import {structuredTask,rankStructuredModels,recordStructuredModel} from './structured-model-health.js';
import {operationErrorCode} from '../services/operation-error.js';
// Server-only; extracted from the v218 Worker.
import {decryptGoogleToken,encryptGoogleToken} from './google-calendar.js';
import {jsonResponse} from '../http.js';
import {ensureRuntimeGeneration, restoredDuringOperation} from '../repositories/runtime-generation.js';
import {generationBoundDb, boundGeneration} from '../repositories/generation-bound-db.js';

export const OR_CACHE_MS = 6 * 60 * 60 * 1000;

export const OR_TABLES = [
 `CREATE TABLE IF NOT EXISTS openrouter_settings (id INTEGER PRIMARY KEY NOT NULL, key_encrypted TEXT NOT NULL DEFAULT '', key_suffix TEXT NOT NULL DEFAULT '', connection_status TEXT NOT NULL DEFAULT 'missing', last_error TEXT NOT NULL DEFAULT '', cache_json TEXT NOT NULL DEFAULT '[]', primary_model_id TEXT, models_synced_at TEXT, last_used_model TEXT, last_used_at TEXT, revision INTEGER NOT NULL DEFAULT 0)`,
 `CREATE TABLE IF NOT EXISTS openrouter_ai_logs (id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, created_at TEXT NOT NULL, task TEXT NOT NULL, requested_primary_model TEXT, actual_model TEXT, fallback_used INTEGER NOT NULL DEFAULT 0, success INTEGER NOT NULL, error_code TEXT)`,
 `CREATE TABLE IF NOT EXISTS openrouter_model_metrics (model_id TEXT PRIMARY KEY NOT NULL, success_count INTEGER NOT NULL DEFAULT 0, total_duration_ms INTEGER NOT NULL DEFAULT 0, last_duration_ms INTEGER, last_success_at TEXT, failure_count INTEGER NOT NULL DEFAULT 0, last_failure_duration_ms INTEGER, last_failure_at TEXT)`
];

export class OpenRouterError extends Error { constructor(code,message,status=502){super(message);this.code=code;this.status=status;} }

export function orError(code,status=502){const messages={missing_key:'Укажите и сохраните API-ключ OpenRouter.',invalid_key:'OpenRouter отклонил API-ключ. Проверьте ключ и сохраните его заново.',decrypt:'Не удалось расшифровать ключ. Сохраните API-ключ заново.',no_models:'Нет бесплатных текстовых моделей с контекстом от 262 144 токенов и поддержкой рассуждений.',unavailable:'Все подходящие модели сейчас недоступны. Повторите запрос позже.',invalid_output:'Ни одна доступная модель не вернула письмо в нужном формате. Повторите попытку позже.',sync:'Не удалось обновить модели. Сохранён предыдущий список.',network:'OpenRouter не ответил вовремя. Повторите попытку.',changed:'Настройки изменились во время запроса. Повторите действие.'};return new OpenRouterError(code,messages[code]||'Ошибка подключения к OpenRouter.',status);}

export async function orSettings(env){if(!env?.DB)throw new OpenRouterError('storage','Хранилище настроек пока не подключено.',503);for(const sql of OR_TABLES)await env.DB.prepare(sql).run();await env.DB.prepare('INSERT OR IGNORE INTO openrouter_settings(id) VALUES(1)').run();return env.DB.prepare('SELECT * FROM openrouter_settings WHERE id=1').first();}

export async function orKey(env,s){if(!s.key_encrypted)throw orError('missing_key',400);try{return await decryptGoogleToken(env.SESSION_SECRET+':openrouter:v1',s.key_encrypted);}catch{throw orError('decrypt',400);}}

export async function orFetch(key,path,body,signal,surface='v1'){
 let response;try{response=await fetch('https://openrouter.ai/api/'+surface+'/'+path,{method:body?'POST':'GET',headers:{Authorization:'Bearer '+key,'Content-Type':'application/json'},...(body?{body:JSON.stringify(body)}:{}),signal:signal?AbortSignal.any([signal,AbortSignal.timeout(body?210000:25000)]):AbortSignal.timeout(body?210000:25000)});}catch{signal?.throwIfAborted();throw orError('network');}
 let data;try{data=await response.json();}catch{throw orError('network');}
 if(!response.ok||data.error){const status=response.ok?Number(data.error?.code)||502:response.status;if(status===401||(status===403&&!body))throw orError('invalid_key',400);const parameterError=status===400&&/reasoning|effort/i.test(String(data.error?.message||''));const e=orError(parameterError?'reasoning_parameter':'provider_'+status);e.providerStatus=status;throw e;}
 return data;
}

export const orNumeric=value=>(typeof value==='number'||(typeof value==='string'&&value.trim()!==''))&&Number.isFinite(Number(value))?Number(value):null;

export const orBase=id=>String(id||'').replace(/:free$/,'');

export function orRankModels(models,benchmarks,fetchedAt){
 const bySlug=new Map(benchmarks.filter(b=>b.source==='artificial-analysis').map(b=>[b.model_permaslug,b]));const seen=new Set();
 const rows=models.filter(m=>{
  if(typeof m.id!=='string'||m.id==='openrouter/free'||seen.has(m.id))return false;seen.add(m.id);
  return orNumeric(m.context_length)>=262144&&m.supported_parameters?.includes('reasoning')&&m.architecture?.output_modalities?.includes('text')&&orNumeric(m.pricing?.prompt)===0&&orNumeric(m.pricing?.completion)===0&&(m.pricing?.request===undefined||orNumeric(m.pricing.request)===0);
 }).map(m=>{const b=bySlug.get(m.id)||bySlug.get(m.canonical_slug)||bySlug.get(orBase(m.id))||bySlug.get(orBase(m.canonical_slug));return {id:m.id,name:typeof m.name==='string'?m.name:m.id,canonical_slug:m.canonical_slug||m.id,context_length:Number(m.context_length),pricing:{prompt:0,completion:0,...(m.pricing.request!==undefined?{request:0}:{})},supports_reasoning:true,supports_structured_outputs:m.supported_parameters?.includes('structured_outputs')===true,intelligence_index:orNumeric(b?.intelligence_index),agentic_index:orNumeric(b?.agentic_index),coding_index:orNumeric(b?.coding_index),is_eligible:true,fetched_at:fetchedAt};});
 rows.sort((a,b)=>{for(const field of ['intelligence_index','agentic_index','coding_index','context_length']){if(a[field]===b[field])continue;if(a[field]===null)return 1;if(b[field]===null)return -1;return b[field]-a[field];}return a.id.localeCompare(b.id);});return rows.map((m,i)=>({...m,rank:i+1}));
}

export async function orRefresh(env,s=undefined){
 s=s||await orSettings(env);const key=await orKey(env,s);
 try{
  const [models,benchmarks]=await Promise.all([orFetch(key,'models?context=262144&supported_parameters=reasoning&output_modalities=text&sort=intelligence-high-to-low'),orFetch(key,'benchmarks?source=artificial-analysis')]);
  if(!Array.isArray(models.data)||!Array.isArray(benchmarks.data))throw orError('sync');
  const at=new Date().toISOString(),ranked=orRankModels(models.data,benchmarks.data,at);
  const result=await env.DB.prepare("UPDATE openrouter_settings SET cache_json=?,primary_model_id=?,models_synced_at=?,connection_status='connected',last_error='' WHERE id=1 AND revision=?").bind(JSON.stringify(ranked),ranked[0]?.id||null,at,s.revision).run();
  if(!result.meta?.changes)throw orError('changed',409);
 }catch(e){if(e.code==='changed')throw e;await env.DB.prepare('UPDATE openrouter_settings SET last_error=?,connection_status=? WHERE id=1 AND revision=?').bind(e.code==='invalid_key'?e.message:orError('sync').message,e.code==='invalid_key'?'error':s.connection_status,s.revision).run();throw e;}
 return orSettings(env);
}

export async function orPublic(env,refresh=false){let s=await orSettings(env);if(s.key_encrypted&&(refresh||!s.models_synced_at||Date.now()-Date.parse(s.models_synced_at)>OR_CACHE_MS)){try{s=await orRefresh(env,s);}catch{s=await orSettings(env);}}
 let models=[];try{models=JSON.parse(s.cache_json);}catch{}
 const timings=(await env.DB.prepare('SELECT * FROM openrouter_model_metrics').all()).results||[];
 const timingByModel=new Map(timings.map(row=>[row.model_id,row]));
 models=models.map(model=>{const row=timingByModel.get(model.id);return {...model,letter_timing:row?{success_count:row.success_count,average_ms:row.success_count?Math.round(row.total_duration_ms/row.success_count):null,last_ms:row.last_duration_ms,last_success_at:row.last_success_at,failure_count:row.failure_count,last_failure_ms:row.last_failure_duration_ms,last_failure_at:row.last_failure_at}:null};});
 const tested=await env.DB.prepare("SELECT actual_model FROM openrouter_ai_logs WHERE task='connection_test' AND success=1 AND actual_model IS NOT NULL ORDER BY id DESC LIMIT 1").first();
 const active=models.find(model=>orBase(model.id)===orBase(tested?.actual_model)||orBase(model.canonical_slug)===orBase(tested?.actual_model));
 return {configured:!!s.key_encrypted,key_mask:s.key_suffix?'••••'+s.key_suffix:'',connection_status:s.connection_status,warning:s.last_error,models,primary_model_id:s.primary_model_id,active_test_model_id:active?.id||null,models_synced_at:s.models_synced_at,last_used_model:s.last_used_model,last_used_at:s.last_used_at,empty_message:s.key_encrypted&&!models.length?orError('no_models').message:'',cache_hours:6};}

export async function orLog(env,{task,primary,actual=null,success,error=null,fallback=false}){await env.DB.batch([env.DB.prepare('INSERT INTO openrouter_ai_logs(created_at,task,requested_primary_model,actual_model,fallback_used,success,error_code) VALUES(?,?,?,?,?,?,?)').bind(new Date().toISOString(),String(task||'unspecified').replace(/[^a-z0-9_-]/gi,'').slice(0,64),primary,actual,fallback?1:0,success?1:0,error),env.DB.prepare('DELETE FROM openrouter_ai_logs WHERE id NOT IN (SELECT id FROM openrouter_ai_logs ORDER BY id DESC LIMIT 200)')]);}

export async function orRecordLetterTiming(env,modelId,started,success){
 const elapsed=Math.max(0,Date.now()-started),now=new Date().toISOString();
 await env.DB.prepare('INSERT OR IGNORE INTO openrouter_model_metrics(model_id) VALUES(?)').bind(modelId).run();
 await env.DB.prepare(success?'UPDATE openrouter_model_metrics SET success_count=success_count+1,total_duration_ms=total_duration_ms+?,last_duration_ms=?,last_success_at=? WHERE model_id=?':'UPDATE openrouter_model_metrics SET failure_count=failure_count+1,last_failure_duration_ms=?,last_failure_at=? WHERE model_id=?').bind(...(success?[elapsed,elapsed,now,modelId]:[elapsed,now,modelId])).run();
}

export class OpenRouterModelRouter {
 constructor(env){this.env=env;}
 async runAI({messages,task,responseFormat,maxTokens=4096,validateContent,onProgress,generation,signal,checkActive,maxAttempts}){
  if(!this.env?.DB)throw new OpenRouterError('storage','Хранилище настроек пока не подключено.',503);
  const existing=boundGeneration(this.env.DB),epoch=generation??existing??await ensureRuntimeGeneration(this.env.DB);
  if(existing&&existing!==epoch)throw restoredDuringOperation();
  const env=existing?this.env:{...this.env,DB:generationBoundDb(this.env.DB,epoch)};let primary=null;
  try{
   signal?.throwIfAborted();
   await checkActive?.();
   const state=await orPublic(env);if(!state.configured)throw orError('missing_key',400);if(['vacancy_extraction','vacancy_role'].includes(task)&&state.models.some(m=>m.supports_structured_outputs===undefined)){await orRefresh(env);state.models=(await orPublic(env)).models;}if(!state.models.length)throw orError('no_models',503);
   const s=await orSettings(env),key=await orKey(env,s);let candidates=state.models,structuredPrimary;
   if(task==='cover_letter'||task==='relevance'){
    const selected=candidates.findIndex(model=>model.id===state.active_test_model_id);
    if(selected>=0)candidates=candidates.slice(selected);
   }
   if(structuredTask(task)){candidates=candidates.filter(m=>m.supports_structured_outputs);if(!candidates.length)throw new OpenRouterError('no_structured_models','Нет доступной бесплатной модели с поддержкой JSON Schema. Обновите модели или выберите OpenAI.',409);structuredPrimary=candidates[0].id;candidates=(await rankStructuredModels(env.DB,task,candidates,s.revision)).slice(0,maxAttempts||1);}
   primary=structuredPrimary||candidates[0].id;let result,actual,invalidOutput=false;const attemptedModels=[];
   // Keep one ranked snapshot for the request. Only this server advances the model;
   // never delegate inter-model fallback with a models array to OpenRouter.
   for(const candidate of candidates){
    attemptedModels.push(candidate.id);
    const candidateStarted=Date.now();
    let failureCode='empty_response',finishReason=null,completionTokens=null,reasoningTokens=null;
    onProgress?.({stage:'model',model:candidate.name||candidate.id,attempt:attemptedModels.length,total:candidates.length});
    let structuredError;
    try{
    for(const reasoning of (['vacancy_extraction','vacancy_role'].includes(task)?[{effort:'medium',exclude:true}]:[{effort:task==='cover_letter'?'medium':'high',exclude:true},{enabled:true,exclude:true}])){
     try{
      signal?.throwIfAborted();
      await checkActive?.();
      const modelSignal=structuredTask(task)&&env.SCHEDULER_BILLING?signal?AbortSignal.any([signal,AbortSignal.timeout(90000)]):AbortSignal.timeout(90000):signal;
      const response=await withSchedulerBilling(env,{provider:'openrouter',model:candidate.id,free:true},()=>orFetch(key,'chat/completions',{model:candidate.id,messages,reasoning,provider:{max_price:{prompt:0,completion:0,request:0},require_parameters:true},max_tokens:maxTokens,stream:false,...(responseFormat?{response_format:responseFormat}:{})},modelSignal));
      signal?.throwIfAborted();
      await checkActive?.();
      const choice=response?.choices?.[0],message=choice?.message;
      finishReason=choice?.finish_reason||null;completionTokens=response?.usage?.completion_tokens??null;reasoningTokens=response?.usage?.completion_tokens_details?.reasoning_tokens??null;
      const matches=typeof response?.model==='string'&&(orBase(candidate.id)===orBase(response.model)||orBase(candidate.canonical_slug)===orBase(response.model));
      // A different model or an empty/error response cannot mark this attempt successful.
      if(!matches){failureCode='model_mismatch';break;}
      if(choice?.error){failureCode='provider_response_error';break;}
      if(['error','content_filter','length'].includes(choice?.finish_reason)){failureCode='finish_'+choice.finish_reason;break;}
      if(message?.refusal){failureCode='refusal';break;}
      if(typeof message?.content!=='string'||!message.content.trim()){failureCode='empty_response';break;}
      if(validateContent&&!validateContent(message.content)){failureCode='invalid_output';invalidOutput=true;break;}
      result=response;actual=candidate;break;
     }catch(e){signal?.throwIfAborted();if(e.code==='restored'||e.code==='generation_lease_lost'||e.code==='relevance_changed')throw e;failureCode=e.code||'internal';if(e.code==='invalid_key'||['vacancy_extraction','vacancy_role'].includes(task))throw e;if(reasoning.effort&&e.code==='reasoning_parameter')continue;break;}
    }
    }catch(error){structuredError=error;throw error;}
    finally{if(structuredTask(task))await recordStructuredModel(env.DB,task,candidate.id,s.revision,Date.now()-candidateStarted,result?null:structuredError||new OpenRouterError(failureCode,'Модель не вернула корректный ответ.')).catch(()=>{});}
    if(task==='cover_letter'){
     await orRecordLetterTiming(env,candidate.id,candidateStarted,!!result);
     await orLog(env,{task:'cover_letter_attempt',primary,actual:candidate.id,success:!!result,error:result?null:failureCode,fallback:candidate.id!==primary});
     console.info(JSON.stringify({event:'cover_letter_attempt',model:candidate.id,success:!!result,error:result?null:failureCode,duration_ms:Date.now()-candidateStarted,finish_reason:finishReason,completion_tokens:completionTokens,reasoning_tokens:reasoningTokens}));
    }
    if(result)break;
   }
   if(!result){
    if(['vacancy_extraction','vacancy_role'].includes(task))throw new OpenRouterError(invalidOutput?'invalid_output':'unavailable','Модель не вернула полный структурированный ответ. Markdown сохранён.',502);
    if(task==='relevance'&&invalidOutput)throw new OpenRouterError('invalid_output','Модели OpenRouter не вернули корректную оценку от 0 до 100. Прежняя оценка сохранена.',502);
    throw orError(invalidOutput?'invalid_output':'unavailable',invalidOutput?502:503);
   }
   await env.DB.prepare('UPDATE openrouter_settings SET last_used_model=?,last_used_at=? WHERE id=1 AND revision=?').bind(result.model,new Date().toISOString(),s.revision).run();
   const fallback=actual.id!==primary;
   await orLog(env,{task,primary,actual:result.model,success:true,fallback});return {model:result.model,message:result.choices[0].message,usage:result.usage,fallback_used:fallback,attempted_models:attemptedModels};
  }catch(e){signal?.throwIfAborted();if(e.code==='restored'||e.code==='generation_lease_lost'||e.code==='relevance_changed'||e.name==='TimeoutError'||e.name==='AbortError')throw e;await orLog(env,{task,primary,success:false,error:e.code||'internal'});if(e instanceof OpenRouterError||e.code?.startsWith('scheduler_')||e.code==='budget_unknown'||e.code==='invalid_output')throw e;throw orError('unavailable',503);}
 }
}

export async function runAI(env,options){return new OpenRouterModelRouter(env).runAI(options);}

export async function openRouterRoute(request,env,url){
 const action=url.pathname.slice('/api/openrouter'.length);
 const methods={'':'GET','/key':'PUT','/check':'POST','/refresh':'POST','/test':'POST','/logs':'GET'};
 if(!methods[action])return jsonResponse({error:'Метод API не найден.'},404);
 if(request.method!==methods[action])return jsonResponse({error:'Метод не поддерживается.'},405);
 if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  if(!env?.DB)throw new OpenRouterError('storage','Хранилище настроек пока не подключено.',503);
  env={...env,DB:generationBoundDb(env.DB,await ensureRuntimeGeneration(env.DB))};
  let s=await orSettings(env);
  if(action==='/key'){
   const reader=request.body?.getReader();let text='',size=0;const decoder=new TextDecoder();if(reader){while(true){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>2048){await reader.cancel();throw new OpenRouterError('input','Слишком длинный ключ.',413);}text+=decoder.decode(value,{stream:true});}text+=decoder.decode();}let input;try{input=JSON.parse(text);}catch{throw new OpenRouterError('input','Некорректный JSON.',400);}
   const key=typeof input.apiKey==='string'?input.apiKey.trim():'';if(!/^sk-or-[A-Za-z0-9_-]{12,512}$/.test(key))throw new OpenRouterError('input','Введите корректный API-ключ OpenRouter (sk-or-…).',400);
   const encrypted=await encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1',key);
   await env.DB.prepare("UPDATE openrouter_settings SET key_encrypted=?,key_suffix=?,connection_status='unchecked',last_error='',revision=revision+1 WHERE id=1").bind(encrypted,key.slice(-4)).run();return jsonResponse(await orPublic(env,true));
  }
  if(action==='/check'){try{await orFetch(await orKey(env,s),'key');await env.DB.prepare("UPDATE openrouter_settings SET connection_status='connected',last_error='' WHERE id=1 AND revision=?").bind(s.revision).run();}catch(e){await env.DB.prepare("UPDATE openrouter_settings SET connection_status='error',last_error=? WHERE id=1 AND revision=?").bind(e.message,s.revision).run();throw e;}}
  if(action==='/refresh')await orRefresh(env,s);
  if(action==='/test'){const result=await runAI(env,{task:'connection_test',messages:[{role:'user',content:'Ответь одним словом: готово.'}],maxTokens:1024});return jsonResponse({ok:true,model:result.model,fallback_used:result.fallback_used,attempted_models:result.attempted_models,state:await orPublic(env)});}
  if(action==='/logs')return jsonResponse({logs:(await env.DB.prepare('SELECT * FROM openrouter_ai_logs ORDER BY id DESC LIMIT 50').all()).results||[]});
  return jsonResponse(await orPublic(env));
 }catch(e){return jsonResponse({error:e instanceof OpenRouterError?e.message:'Не удалось выполнить операцию OpenRouter.',code:e.code||'internal'},e.status||500);}
}
