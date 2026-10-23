import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {rankStructuredModels,recordStructuredModel} from '../server/integrations/structured-model-health.js';
const f=await schedulerFixture(),originalFetch=globalThis.fetch;
try{
 const {api,db,env,sqlite}=f;await api.orSettings(env);
 const key=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','sk-or-SYNTHETIC-NOT-A-KEY');
 const models=['slow/free:free','fast/free:free','unsupported/free:free'].map((id,i)=>({id,name:id,canonical_slug:id,context_length:300000,pricing:{prompt:0,completion:0},supports_structured_outputs:i<2,rank:i+1}));
 sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=?,cache_json=?,models_synced_at=? WHERE id=1').run(key,JSON.stringify(models),new Date().toISOString());
 let failure=true;const calls=[];
 globalThis.fetch=async(url,options)=>{
  assert.match(String(url),/chat\/completions$/);const body=JSON.parse(options.body);calls.push(body.model);assert.deepEqual(body.provider.max_price,{prompt:0,completion:0,request:0});assert.equal(body.provider.require_parameters,true);assert.ok(options.signal);assert.ok(!body.models);
  if(failure)return Response.json({error:{code:502}},{status:502});
  return Response.json({model:body.model,choices:[{finish_reason:'stop',message:{content:'{"role_ids":[1]}'}}],usage:{cost:0}});
 };
 const options={task:'vacancy_extraction',maxAttempts:1,messages:[{role:'user',content:'SYNTHETIC PRIVATE INPUT'}],validateContent:()=>true};
 const activeEnv={...env,SCHEDULER_BILLING:{run:(_,work)=>work()}};
 await assert.rejects(()=>api.runAI(activeEnv,options),e=>e.code==='provider_502');assert.deepEqual(calls,['slow/free:free']);
 failure=false;const retry=await api.runAI(activeEnv,options);assert.equal(retry.model,'fast/free:free');assert.equal(retry.fallback_used,true);assert.deepEqual(calls,['slow/free:free','fast/free:free']);
 await api.runAI(activeEnv,{...options,task:'vacancy_role'});assert.equal(calls.at(-1),'slow/free:free','roles have independent health');
 const before=sqlite.prepare('SELECT * FROM openrouter_model_metrics').all();assert.ok(before.some(x=>x.failure_count===1));assert.ok(!JSON.stringify(before).includes('SYNTHETIC PRIVATE INPUT'));assert.ok(!before.some(x=>x.model_id==='slow/free:free'),'letter metrics untouched');
 // Semantic validation failures count against this model and keep their cause.
 await assert.rejects(()=>api.runAI(activeEnv,{...options,validateContent:()=>{throw Object.assign(Error('Unconfirmed field'),{code:'invalid_output',status:422});}}),e=>e.code==='invalid_output'&&e.message==='Unconfirmed field');
 // Global key failures and explicit user cancellation are not model failures.
 const bound=api.generationBoundDb(db,await api.ensureRuntimeGeneration(db));
 const rows=()=>sqlite.prepare('SELECT * FROM openrouter_model_metrics ORDER BY model_id').all();const snapshot=rows();
 await recordStructuredModel(bound,'vacancy_extraction','fast/free:free',0,10,{code:'invalid_key'});await recordStructuredModel(bound,'vacancy_extraction','fast/free:free',0,10,new DOMException('Cancelled','AbortError'));assert.deepEqual(rows(),snapshot);
 sqlite.prepare('UPDATE openrouter_settings SET revision=revision+1 WHERE id=1').run();await api.runAI(activeEnv,options);assert.equal(calls.at(-1),'slow/free:free','new key revision ignores old circuit state');
 const newGeneration=crypto.randomUUID();sqlite.prepare('UPDATE app_runtime_generation SET generation=? WHERE id=1').run(newGeneration);await api.runAI(activeEnv,options);assert.equal(calls.at(-1),'slow/free:free','restore epoch ignores old circuit state');
 // Healthy measured fast models win after two samples; unknown stays eligible.
 const current=api.generationBoundDb(db,newGeneration);await recordStructuredModel(current,'vacancy_extraction','fast/free:free',1,1000);await recordStructuredModel(current,'vacancy_extraction','fast/free:free',1,1200);
 assert.equal((await rankStructuredModels(current,'vacancy_extraction',models.slice(0,2),1))[0].id,'fast/free:free');
 await recordStructuredModel(current,'vacancy_extraction','fast/free:free',1,90000,new DOMException('Deadline','TimeoutError'));assert.equal((await rankStructuredModels(current,'vacancy_extraction',models.slice(0,2),1))[0].id,'slow/free:free');
 assert.equal((await rankStructuredModels(current,'vacancy_extraction',models.slice(0,2),1,Date.now()+900001))[0].id,'fast/free:free','cooldown expires');
 const timeout=AbortSignal.timeout,keepAlive=setInterval(()=>{},1000);
 try{
  AbortSignal.timeout=ms=>timeout(ms===90000?1:ms);
  globalThis.fetch=async(_,o)=>{if(o.signal.aborted)throw o.signal.reason;return new Promise((_,reject)=>o.signal.addEventListener('abort',()=>reject(o.signal.reason),{once:true}));};
  await assert.rejects(()=>api.runAI(activeEnv,options),e=>e.name==='TimeoutError','local model deadline keeps canonical timeout rather than unavailable');
 }finally{AbortSignal.timeout=timeout;clearInterval(keepAlive);}
 console.log('Structured model health: real router switches after502/invalid schema, task isolation, cooldown/latency, free-only exact model/no provider fallback, key revision/restore isolation, no private content or letter changes passed');
}finally{globalThis.fetch=originalFetch;f.close();}
