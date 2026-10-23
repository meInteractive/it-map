import {withSchedulerBilling} from './scheduler-billing.js';
import {runJevRelevance} from './jev.js';
import {runJevLeafRoles} from './jev-leaf-roles.js';
// Direct Responses API. Admin key is encrypted at rest and only decrypted server-side.
import {encryptGoogleToken,decryptGoogleToken} from './google-calendar.js';
import {ensureRuntimeGeneration} from '../repositories/runtime-generation.js';
import {generationBoundDb} from '../repositories/generation-bound-db.js';
import {jsonResponse} from '../http.js';
import {openaiModelProfile,openaiTextModel,openaiDefault,REQUESTED_OPENAI_DEFAULT,OPENAI_RECOMMENDED,validateOpenaiChoice} from './openai-models.js';
import {runAI as runOpenRouter,orRecordLetterTiming,orLog} from './openrouter.js';
import {readPromptModels} from '../repositories/prompt-models.js';
const CACHE_MS=6*60*60*1000;
let cached;
export class OpenAIError extends Error{constructor(code,message,status=502){super(message);this.code=code;this.status=status;}}
export async function openaiSettings(env){
 const row=env.DB?await env.DB.prepare('SELECT key_encrypted,key_suffix,revision,updated_at FROM openai_settings WHERE id=1').first():null;
 return row||{key_encrypted:'',key_suffix:'',revision:0,updated_at:null};
}
async function keyFor(env){
 const settings=await openaiSettings(env);
 if(settings.key_encrypted){
  if(!env.SESSION_SECRET)throw new OpenAIError('encryption','Серверный ключ шифрования не настроен.',503);
  try{return await decryptGoogleToken(env.SESSION_SECRET+':openai:v1',settings.key_encrypted);}
  catch{throw new OpenAIError('decrypt','Не удалось расшифровать ключ OpenAI. Сохраните его заново.',409);}
 }
 if(typeof env.OPENAI_API_KEY==='string'&&env.OPENAI_API_KEY.trim())return env.OPENAI_API_KEY.trim();
 throw new OpenAIError('missing_key','Сохраните API-ключ OpenAI во вкладке «Нейросети» → «ChatGPT».',409);
}
async function readKeyInput(request){
 const reader=request.body?.getReader();if(!reader)throw new OpenAIError('input','Передайте API-ключ.',400);
 let text='',bytes=0;const decoder=new TextDecoder();
 try{for(;;){const {done,value}=await reader.read();if(done)break;bytes+=value.byteLength;if(bytes>2048){await reader.cancel();throw new OpenAIError('input','Слишком длинный ключ.',413);}text+=decoder.decode(value,{stream:true});}}finally{reader.releaseLock();}
 let input;try{input=JSON.parse(text+decoder.decode());}catch{throw new OpenAIError('input','Некорректный JSON.',400);}
 const key=typeof input?.apiKey==='string'?input.apiKey.trim():'';
 if(!input||Array.isArray(input)||Object.keys(input).some(k=>k!=='apiKey')||!/^sk-[A-Za-z0-9_-]{12,512}$/.test(key)||key.startsWith('sk-or-'))throw new OpenAIError('input','Введите корректный API-ключ OpenAI (sk-…).',400);
 return key;
}
export async function openaiFetch(env,path,body,signal,resolvedKey){
 const key=resolvedKey||await keyFor(env),timeout=AbortSignal.timeout(body?300000:25000),combined=signal?AbortSignal.any([signal,timeout]):timeout;
 try{
  const response=await fetch('https://api.openai.com/v1/'+path,{method:body?'POST':'GET',headers:{Authorization:'Bearer '+key,'Content-Type':'application/json'},...(body?{body:JSON.stringify(body)}:{}),signal:combined});
  let data;try{data=await response.json();}catch{throw new OpenAIError('invalid_response','OpenAI вернул некорректный ответ.');}
  if(!response.ok||data.error){
   const status=response.status;
   const message=status===401?'OpenAI отклонил API-ключ. Проверьте серверное подключение.':status===403?'Доступ к модели OpenAI запрещён для этого проекта.':status===404?'Выбранная модель OpenAI недоступна. Обновите список моделей.':status===429?'OpenAI ограничил запрос: проверьте лимиты и баланс проекта.':status===400?'OpenAI отклонил параметры запроса. Проверьте модель и уровень рассуждений.':'OpenAI сейчас недоступен. Повторите запрос позже.';
   throw new OpenAIError('provider_'+status,message,status===401?409:status===429?429:502);
  }
  return data;
 }catch(error){signal?.throwIfAborted();if(error instanceof OpenAIError)throw error;throw new OpenAIError('network','OpenAI не ответил за 5 минут или соединение прервано. Повторите запрос.');}
}
export async function openaiModels(env,refresh=false,signal){
 const key=await keyFor(env);
 if(!refresh&&cached?.key===key&&Date.now()-cached.at<CACHE_MS)return cached.models;
 const data=await openaiFetch(env,'models',undefined,signal,key);
 if(!Array.isArray(data.data))throw new OpenAIError('invalid_models','OpenAI вернул некорректный список моделей.');
 const seen=new Set();const models=data.data.filter(m=>openaiTextModel(m.id)&&!seen.has(m.id)&&seen.add(m.id)).map(m=>openaiModelProfile(m.id));
 const priority=[REQUESTED_OPENAI_DEFAULT,...OPENAI_RECOMMENDED];
 models.sort((a,b)=>(priority.indexOf(a.id)<0?Infinity:priority.indexOf(a.id))-(priority.indexOf(b.id)<0?Infinity:priority.indexOf(b.id))||a.id.localeCompare(b.id));
 cached={key,at:Date.now(),models};return models;
}
export async function openaiPublic(env,refresh=false){
 const settings=await openaiSettings(env),configured=!!settings.key_encrypted||!!env.OPENAI_API_KEY;
 const base={configured,key_mask:settings.key_suffix?'••••'+settings.key_suffix:'',requestedDefault:REQUESTED_OPENAI_DEFAULT};
 if(!configured)return {...base,models:[],defaultModel:'',warning:'Сохраните API-ключ OpenAI, чтобы загрузить доступные модели.'};
 let models;try{models=await openaiModels(env,refresh);}catch(error){if(refresh)throw error;return {...base,models:[],defaultModel:'',warning:error instanceof OpenAIError?error.message:'Не удалось загрузить модели OpenAI.'};}
 const metrics=env.DB?(await env.DB.prepare("SELECT * FROM openrouter_model_metrics WHERE model_id LIKE 'openai:%'").all()).results||[]:[];
 models=models.map(model=>{const timing=metrics.find(row=>row.model_id==='openai:'+model.id);return {...model,letterTiming:timing?{successCount:timing.success_count,failureCount:timing.failure_count,averageMs:timing.success_count?Math.round(timing.total_duration_ms/timing.success_count):null,lastMs:timing.last_duration_ms}:null};});
 const defaultModel=openaiDefault(models);
 return {...base,models,defaultModel,warning:models.some(m=>m.id===REQUESTED_OPENAI_DEFAULT)?'':`GPT-6.1 Luna отсутствует в доступных API-моделях.${defaultModel?' По умолчанию: '+openaiModelProfile(defaultModel).name+'.':''}`};
}
export async function runOpenAI(env,options,config){
 const {signal,checkActive,onProgress,validateContent}=options;
 signal?.throwIfAborted();await checkActive?.();
 const models=await openaiModels(env,false,signal),model=config.openaiModel||openaiDefault(models),profile=models.find(m=>m.id===model);
 if(!profile)throw new OpenAIError('model_unavailable','Выбранная модель OpenAI недоступна. Администратору нужно обновить список и выбрать модель.',409);
 // Unknown capabilities use the model default; no invented effort parameter.
 const effort=config.openaiModel?config.reasoningEffort:(profile.reasoningEfforts.includes('medium')?'medium':'auto');
 validateOpenaiChoice(model,effort);
 onProgress?.({stage:'model',model:profile.name,attempt:1,total:1});
 const started=Date.now();let success=false;
 try{
 const body={model,input:options.messages.map(m=>({role:m.role==='system'?'developer':m.role,content:m.content})),max_output_tokens:Math.max(options.maxTokens||4096,32768),store:false,...(options.responseFormat?.type==='json_schema'?{text:{format:{type:'json_schema',...options.responseFormat.json_schema}}}:{}),...(effort==='auto'?{}:{reasoning:{effort}})};
 const data=await withSchedulerBilling(env,{provider:'openai',model,payload:body},()=>openaiFetch(env,'responses',body,signal));
 signal?.throwIfAborted();await checkActive?.();
 const messages=(Array.isArray(data.output)?data.output:[]).filter(o=>o.type==='message'&&o.role==='assistant');
 const content=messages.flatMap(o=>o.content||[]).filter(c=>c.type==='output_text'&&typeof c.text==='string').map(c=>c.text).join('\n');
 if(data.status!=='completed'||messages.some(o=>(o.content||[]).some(c=>c.type==='refusal'))||!content.trim()||(validateContent&&!validateContent(content)))throw new OpenAIError('invalid_output','OpenAI не вернул полный ответ в нужном формате. Результат не сохранён.');
 if(typeof data.model!=='string'||!(data.model===model||new RegExp('^'+model.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'-\\d{4}-\\d{2}-\\d{2}$').test(data.model)))throw new OpenAIError('model_mismatch','OpenAI вернул ответ другой модели. Результат не сохранён.');
 success=true;
 // Only metadata in logs; no resume, prompt, output or key.
 console.info(JSON.stringify({event:'openai_generation',task:options.task,model:data.model,reasoning_effort:effort,duration_ms:Date.now()-started,success:true}));
 return {model:data.model,message:{content},usage:data.usage,fallback_used:false,attempted_models:[model]};
 }finally{
  if(options.task==='cover_letter'&&env.DB){
   signal?.throwIfAborted();await checkActive?.();
   await orRecordLetterTiming(env,'openai:'+model,started,success);
   await orLog(env,{task:'openai_cover_letter',primary:model,actual:model,success,error:success?null:'failed'});
  }
 }
}
export async function runPromptAI(env,options,config){
 config=config||(await readPromptModels(env.DB))[options.task];
 if(config?.provider==='openai')return runOpenAI(env,options,config);
 if(config?.openrouterModel==='jev'){
  if(options.task==='vacancy_role')return runJevLeafRoles(env,options);
  if(options.task!=='relevance')throw new OpenAIError('unsupported_task','JEV доступна только для оценки релевантности.',400);
  return runJevRelevance(env,options);
 }
 return runOpenRouter(env,options);
}
export async function openaiRoute(request,env,url){
 const action=url.pathname.slice('/api/openai'.length);
 if(!['','/refresh','/test','/key'].includes(action))return jsonResponse({error:'Метод API не найден.'},404);
 if(request.method!==(action==='/key'?'PUT':action?'POST':'GET'))return jsonResponse({error:'Метод не поддерживается.'},405);
 if(action&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  if(action==='/key'){
   if(!env.DB||!env.SESSION_SECRET)throw new OpenAIError('storage','Серверное хранилище ключей не настроено.',503);
   const guardedDb=generationBoundDb(env.DB,await ensureRuntimeGeneration(env.DB));
   const key=await readKeyInput(request),encrypted=await encryptGoogleToken(env.SESSION_SECRET+':openai:v1',key);
   await guardedDb.prepare(`INSERT INTO openai_settings(id,key_encrypted,key_suffix,revision,updated_at) VALUES(1,?,?,1,CURRENT_TIMESTAMP)
    ON CONFLICT(id) DO UPDATE SET key_encrypted=excluded.key_encrypted,key_suffix=excluded.key_suffix,revision=openai_settings.revision+1,updated_at=CURRENT_TIMESTAMP`).bind(encrypted,key.slice(-4)).run();
   cached=undefined;
   return jsonResponse({configured:true,key_mask:'••••'+key.slice(-4)});
  }
  if(action==='/test'){
   const config=(await readPromptModels(env.DB)).cover_letter;
   const result=await runOpenAI(env,{task:'connection_test',messages:[{role:'user',content:'Ответь одним словом: готово.'}],maxTokens:1024},config);
   return jsonResponse({ok:true,model:result.model,state:await openaiPublic(env)});
  }
  return jsonResponse(await openaiPublic(env,action==='/refresh'));
 }catch(error){return jsonResponse({error:error instanceof OpenAIError?error.message:'Не удалось выполнить операцию OpenAI.',code:error.code||'internal'},error.status||500);}
}
