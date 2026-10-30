import {jevCatalogModel,JEV_MODEL_ID} from '../server/integrations/jev.js';
import {encryptGoogleToken} from '../server/integrations/google-calendar.js';
const original=globalThis.fetch,secret='CI_SYNTHETIC_SECRET',cipher=await encryptGoogleToken(secret+':openrouter:v1','CI_SYNTHETIC_KEY'),rows=[];
try{for(const price of ['0.01',0,null]){
 const models=[{id:JEV_MODEL_ID,name:'JEV',context_length:262144,pricing:{prompt:price,completion:'0'}}];let url;
 const db={prepare(sql){return{async run(){return{meta:{changes:1}}},async first(){return{key_encrypted:cipher,revision:1}}}}};
 globalThis.fetch=async(u)=>{url=String(u);return Response.json({data:models});};let expected;
 try{const v=await jevCatalogModel({DB:db,SESSION_SECRET:secret});delete v.fetchedAt;expected={ok:true,result:v};}catch(e){expected={ok:false,code:e.code,status:e.status,message:e.message};}
 rows.push({models,url,...expected});
}}finally{globalThis.fetch=original;}
console.log(JSON.stringify(rows));
