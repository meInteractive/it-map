// Исполняет неизменённые JEV функции только с synthetic DB/fetch/billing.
import {runJevRelevance,runJevTitleClassification,JEV_MODEL_ID} from '../server/integrations/jev.js';
import {runJevLeafRoles} from '../server/integrations/jev-leaf-roles.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {encryptGoogleToken} from '../server/integrations/google-calendar.js';
const secret='CI_SYNTHETIC_SECRET',cipher=await encryptGoogleToken(secret+':openrouter:v1','CI_SYNTHETIC_KEY'),original=globalThis.fetch,frames=[];
const roles=[{roleId:2,name:'CI_ROLE',markdown:'CI_RULES',revision:3},{roleId:1,name:'CI_OTHER',markdown:'CI_OTHER_RULES',revision:4}];
const leafRoles=[{id:2,path:'CI / Other'},{id:1,path:'CI / Developer'}];
const scenarios=[
 ...[0,4.5,9,9.01,'4',null].map(score=>({task:'relevance',answers:{relevance:{type:'score',score}}})),
 {task:'relevance',model:'foreign',answers:{relevance:{type:'score',score:5}}},
 {task:'title_classification',answers:{role_2:{type:'choice',choice:'match'},role_1:{type:'choice',choice:'reject'}}},
 {task:'title_classification',answers:{role_2:{type:'choice',choice:'reject'}}},
 {task:'title_classification',answers:{role_2:{type:'score',score:3},role_1:{type:'choice',choice:1}}},
 {task:'vacancy_role',answers:{leaf_2:{type:'choice',choice:'match'},leaf_1:{type:'choice',choice:'match'}}},
 {task:'vacancy_role',answers:{leaf_2:{type:'choice',choice:'reject'},leaf_1:{type:'choice',choice:'reject'}}},
 {task:'vacancy_role',answers:{leaf_2:{type:'choice',choice:'match'}}},
 {task:'vacancy_role',answers:{leaf_2:{type:'choice',choice:'match'},alien:{type:'choice',choice:'reject'}}},
 {task:'vacancy_role',answers:{leaf_2:{type:'choice',choice:'maybe'},leaf_1:{type:'choice',choice:'reject'}}},
 {task:'vacancy_role',contextLength:100,answers:{}},
];
try{for(const scenario of scenarios){
 const response={model:scenario.model||JEV_MODEL_ID,answers:scenario.answers,usage:{cost:0.001}},calls=[];let billing;
 const db={prepare(sql){return {sql,bind(){return this},async run(){return{meta:{changes:1}}},async first(){return sql.includes('SELECT generation')?{generation:'CI_GENERATION'}:{key_encrypted:cipher,revision:1}}}},async batch(statements){return statements.map(s=>({meta:{changes:1},results:s.sql.includes('SELECT * FROM openrouter_settings')?[{key_encrypted:cipher,revision:1}]:s.sql.includes('SELECT generation')?[{generation:'CI_GENERATION'}]:[]}))}};
 const env={DB:generationBoundDb(db,'CI_GENERATION'),SESSION_SECRET:secret,SCHEDULER_BILLING:{run(meta,work){billing=meta;return work()}}};
 globalThis.fetch=async(url,opts)=>{if(!opts?.body)return Response.json({data:[{id:JEV_MODEL_ID,context_length:scenario.contextLength||262144,pricing:{prompt:'0.01',completion:'0'}}]});calls.push({url:String(url),payload:JSON.parse(opts.body)});return Response.json(response)};
 const options={task:scenario.task,instructions:'CI_INSTRUCTIONS',state:{text:'CI_VACANCY'},roles:leafRoles};let expected;
 try{let r= scenario.task==='relevance'?await runJevRelevance(env,options):scenario.task==='vacancy_role'?await runJevLeafRoles(env,options):await runJevTitleClassification(env,{title:'CI_TITLE',roles});delete r.duration_ms;delete r.requestDurationMs;delete r.decision;expected={ok:true,result:r}}catch(e){expected={ok:false,code:e.code,status:e.status,message:e.message}}
 frames.push({scenario,response,calls,billing,...expected});
}}finally{globalThis.fetch=original}
console.log(JSON.stringify(frames));
