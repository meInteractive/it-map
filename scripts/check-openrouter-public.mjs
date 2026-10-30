// Actual orPublic над синтетической БД; fetch запрещён полностью.
import {orPublic} from '../server/integrations/openrouter.js';
const original=globalThis.fetch,clock=Date.now,at='2026-10-30T00:00:00.000Z';let calls=0;
globalThis.fetch=async()=>{calls++;throw Error('Network forbidden in source oracle');};Date.now=()=>Date.parse(at);
const cases=[];
try{
 for(const configured of [false,true])for(const metric of [false,true]){
  const model={id:'vendor/model:free',canonical_slug:'vendor/model',name:'Synthetic Model',rank:1};
  const row={id:1,key_encrypted:configured?'synthetic-envelope':'',key_suffix:configured?'xxxx':'',revision:1,connection_status:configured?'connected':'missing',last_error:'',cache_json:JSON.stringify([model]),primary_model_id:model.id,models_synced_at:at,last_used_model:null,last_used_at:null};
  const metrics=metric?[{model_id:model.id,success_count:2,total_duration_ms:11,last_duration_ms:5,last_success_at:at,failure_count:1,last_failure_duration_ms:3,last_failure_at:at}]:[];
  const db={prepare(sql){return{async run(){return{meta:{changes:1}}},async first(){return sql.includes('openrouter_settings')?row:{actual_model:'vendor/model'}},async all(){return{results:metrics}}}}};
  const expected=await orPublic({DB:db});cases.push({row,metrics,tested:'vendor/model',expected});
 }
}finally{globalThis.fetch=original;Date.now=clock;}
if(calls)throw Error('source oracle attempted network');
console.log(JSON.stringify(cases));
