import {runAI} from '../server/integrations/openrouter.js';
import {encryptGoogleToken} from '../server/integrations/google-calendar.js';
const fetchOriginal=globalThis.fetch,clock=Date.now,at='2026-10-30T00:00:00.000Z',secret='CI_SYNTHETIC_SECRET';Date.now=()=>Date.parse(at);
const encrypted=await encryptGoogleToken(secret+':openrouter:v1','CI_SYNTHETIC_KEY');
const models=['vendor/a','vendor/b'].map((id,i)=>({id,name:id,canonical_slug:id,rank:i+1,context_length:262144,pricing:{prompt:0,completion:0},supports_reasoning:true,supports_structured_outputs:true,is_eligible:true,fetched_at:at}));
const success=model=>({model,choices:[{finish_reason:'stop',message:{role:'assistant',content:'готово'}}],usage:{prompt_tokens:2,completion_tokens:1}});
const cases=[ [success('vendor/a')], [success('wrong'),success('vendor/b')], [{status:400,error:{code:400,message:'reasoning unavailable'}},success('vendor/a')], [{...success('vendor/a'),choices:[{finish_reason:'length',message:{content:'partial'}}]},success('vendor/b')], [success('wrong'),success('wrong')], [{...success('vendor/a'),choices:[{finish_reason:'stop',message:{refusal:'no'}}]},success('vendor/b')] ];
const rows=[];
try{
 for(const responses of cases){
  const row={id:1,key_encrypted:encrypted,key_suffix:'xxxx',revision:1,connection_status:'connected',last_error:'',cache_json:JSON.stringify(models),primary_model_id:models[0].id,models_synced_at:at,last_used_model:null,last_used_at:null};
  const generation='00000000-0000-4000-8000-000000000001';
  const db={prepare(sql){const stmt={bind(){return stmt},async run(){return{meta:{changes:1},results:result()}},async first(){return result()[0]||null},async all(){return{results:result()}}};function result(){if(sql.startsWith('SELECT CASE'))return[{generation_guard:1}];if(sql.includes('SELECT generation FROM app_runtime_generation'))return[{generation}];if(sql.includes('SELECT * FROM openrouter_settings'))return[row];return[];}return stmt},async batch(stmts){return Promise.all(stmts.map(s=>s.run()))}};
  const payloads=[];let n=0;
  globalThis.fetch=async(_url,opts)=>{payloads.push(JSON.parse(opts.body));const r=responses[n++]||success('wrong');return Response.json(r,{status:r.status||200});};
  let expected;
  try{expected={ok:true,result:await runAI({DB:db,SESSION_SECRET:secret},{task:'connection_test',messages:[{role:'user',content:'CI_USER'}],maxTokens:1024})};}
  catch(e){expected={ok:false,code:e.code,status:e.status,message:e.message};}
  rows.push({models,responses,payloads,...expected});
 }
}finally{globalThis.fetch=fetchOriginal;Date.now=clock;}
console.log(JSON.stringify(rows));
