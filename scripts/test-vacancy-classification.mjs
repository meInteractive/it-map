import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?vacancy-roles'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);



const previousFetch=globalThis.fetch,calls=[];let mode='normal',release,pending;
await request('/api/classification/settings');
const roles=sqlite.prepare('SELECT role_id,revision FROM vacancy_role_classification ORDER BY role_id').all();
const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-CLASSIFICATION-KEY');
await api.orSettings(env);sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').run(encrypted);
const unchanged=()=>JSON.stringify(['company_vacancies','vacancy_role_assignments','system_prompts','prompt_model_settings'].map(table=>sqlite.prepare('SELECT * FROM '+table).all()));
const before=unchanged();
globalThis.fetch=async(url,options={})=>{
 const body=options.body?JSON.parse(options.body):null;calls.push({url:String(url),body});
 if(String(url).includes('/models')){assert.match(String(url),/output_modalities=decisions/);return Response.json({data:[{id:api.JEV_MODEL_ID,pricing:{prompt:'0.000000042'}}]});}
 assert.ok(String(url).endsWith('/api/alpha/decisions'));assert.deepEqual(body.state,{title:'Senior Product Manager'});
 assert.equal(Object.keys(body.questions).length,2);for(const q of Object.values(body.questions)){assert.equal(q.type,'choice');assert.deepEqual(Object.keys(q.criteria),['match','reject']);assert.ok(q.instructions);}
 if(mode==='held')await new Promise(resolve=>{pending=true;release=resolve;});
 if(mode==='network')throw new Error('network');
 if(mode==='credits')return Response.json({error:{code:402}},{status:402});
 const answers=Object.fromEntries(roles.map((role,index)=>['role_'+role.role_id,{type:'choice',choice:mode==='reject'?'reject':mode==='both'?'match':index===0?'match':'reject'}]));
 if(mode==='invalid-all')for(const key in answers)answers[key]={type:'score',score:7};
 if(mode==='partial')delete answers['role_'+roles[1].role_id];
 if(mode==='partial-no-match'){answers['role_'+roles[0].role_id].choice='reject';delete answers['role_'+roles[1].role_id];}
 return Response.json({model:mode==='mismatch'?'different/model':api.JEV_MODEL_ID+'-20260917',answers,...(mode==='no-cost'?{}:{usage:{cost:0.00002,input_tokens:400,output_tokens:50}})});
};
const test=(cookie=admin,body={title:'Senior Product Manager'})=>request('/api/classification/test','POST',body,cookie);
try{
 assert.equal((await test(user)).status,403);assert.equal(calls.length,0);
 assert.equal((await request('/api/classification/test')).status,405);
 assert.equal((await worker.fetch(new Request(base+'/api/classification/test',{method:'POST',headers:{cookie:admin,origin:'https://evil.invalid'},body:JSON.stringify({title:'x'})}),env)).status,403);
 assert.equal((await test(admin,{title:'😀'.repeat(501)})).status,400);assert.equal((await test(admin,{title:'x',roleId:2})).status,400);assert.equal(calls.length,0);
 sqlite.exec('UPDATE vacancy_role_classification SET parsing_enabled=0');
 let data=await (await test()).json();assert.equal(data.status,'no_active_roles');assert.equal(data.accepted,null);assert.equal(calls.length,0);
 sqlite.exec('UPDATE vacancy_role_classification SET parsing_enabled=1');
 data=await (await test()).json();assert.equal(data.status,'complete');assert.equal(data.accepted,true);assert.deepEqual(data.results.map(r=>r.decision),['match','reject']);assert.equal(data.costRub,.00002*85);assert.equal(calls.filter(c=>c.body).length,1);
 for(const [selected,accepted,status] of [['reject',false,'complete'],['both',true,'complete'],['partial',true,'incomplete'],['partial-no-match',null,'incomplete'],['invalid-all',null,'incomplete'],['credits',null,'incomplete'],['network',null,'incomplete'],['mismatch',null,'incomplete']]){
  mode=selected;data=await (await test()).json();assert.equal(data.accepted,accepted,selected);assert.equal(data.status,status,selected);if(['credits','network','mismatch'].includes(mode))assert.ok(data.results.every(r=>r.decision===null&&r.error));
 }
 mode='no-cost';data=await (await test()).json();assert.equal(data.costRub,null);
 for(const update of ['UPDATE vacancy_role_classification SET revision=revision+1 WHERE role_id='+roles[0].role_id,'UPDATE vacancy_role_classification SET parsing_enabled=0 WHERE role_id='+roles[0].role_id]){
  mode='held';pending=false;const running=test();while(!pending)await new Promise(resolve=>setTimeout(resolve,1));sqlite.exec(update);release();data=await (await running).json();assert.equal(data.status,'incomplete');assert.equal(data.accepted,null);assert.equal(data.results[0].decision,null);
  sqlite.exec('UPDATE vacancy_role_classification SET parsing_enabled=1');
 }
 mode='held';pending=false;const running=test();while(!pending)await new Promise(resolve=>setTimeout(resolve,1));sqlite.exec("UPDATE app_runtime_generation SET generation='00000000-0000-0000-0000-000000000002' WHERE id=1");release();assert.equal((await running).status,409,'restore invalidates in-flight result');
 assert.equal(unchanged(),before,'classification never changes vacancies, assignments, legacy prompts or provider choices');
 console.log('Title classification: batched independent choices, exact IDs/prompts, auth/CSRF/limits/no-call, multi-match, errors never reject, known/unknown cost, config/restore fencing and no vacancy mutations passed');
}finally{globalThis.fetch=previousFetch;sqlite.close();}
