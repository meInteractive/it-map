import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {ALIGNED_RELEVANCE_PROMPT} from '../server/services/relevance-prompt.js';
import {prepareBackend} from './test-support.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs'),worker=api.default;
const sqlite=new DatabaseSync(':memory:');
for(const f of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+f,'utf8'));
const db={prepare(sql){let values=[];return {bind(...v){values=v;return this;},async first(){return sqlite.prepare(sql).get(...values)||null;},async all(){return {results:sqlite.prepare(sql).all(...values)};},async run(){const q=sqlite.prepare(sql);return q.columns().length?{results:q.all(...values)}:{meta:q.run(...values)};}};},async batch(statements){sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
const env={DB:db,SITE_PASSWORD:'fixture',NASTYA_PASSWORD:'fixture',DEMO_PASSWORD:'fixture',SESSION_SECRET:'fixture-secret'};
const base='https://relevance.invalid',cookies={};
for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'fixture'})}),env);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
const req=(name='Nastya',id=91001,method='POST',origin=base,body)=>worker.fetch(new Request(base+'/api/vacancies/'+id+'/relevance',{method,headers:{...(name?{cookie:cookies[name]}:{}),origin,'content-type':'application/json'},body:body?JSON.stringify(body):undefined}),env);
sqlite.exec("INSERT INTO company_crm(company_name) VALUES('Fixture'); INSERT INTO company_vacancies(id,company_name,title,url,description_md,salary_from,salary_to) VALUES(91001,'Fixture','Development manager','https://fixture.invalid/1','FULL VACANCY',300000,500000),(91002,'Fixture','Another vacancy','https://fixture.invalid/2','SECOND FULL VACANCY',NULL,NULL); INSERT INTO user_profiles(user_name,resume_md) VALUES('Eugene','EUGENE PRIVATE RESUME'),('Nastya','NASTYA PRIVATE RESUME'); INSERT INTO system_prompts(kind,markdown) VALUES('relevance','ADMIN RELEVANCE PROMPT'); INSERT INTO prompt_model_settings(kind,provider,openrouter_model) VALUES('relevance','openrouter','jev'); INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite,hidden) VALUES('Nastya',91001,25,1,1),('Eugene',91001,91,0,0);");
sqlite.prepare("UPDATE system_prompts SET markdown=? WHERE kind='relevance'").run(ALIGNED_RELEVANCE_PROMPT);
await api.orSettings(env);const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','sk-or-FIXTURE-ONLY-JEV');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').run(encrypted);
const oldFetch=fetch,calls=[];let mode='normal',score=7.2,release,pending;
globalThis.fetch=async(url,opts={})=>{
 const body=opts.body?JSON.parse(opts.body):undefined;calls.push({url:String(url),body});
 if(String(url).includes('/models')){
  if(/[?&]output_modalities=decisions(?:&|$)/.test(String(url)))return Response.json({data:[{id:api.JEV_MODEL_ID,architecture:{output_modalities:['decisions']},pricing:{prompt:'0.000000042'}}]});
  return Response.json({data:[{id:'fixture/free:free',name:'Free',context_length:300000,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:'0',completion:'0'}}]});
 }
 if(String(url).includes('/benchmarks'))return Response.json({data:[]});
 if(body){
  if(mode==='held')await new Promise(resolve=>{pending=true;release=resolve;});
  if(mode==='credits')return Response.json({error:{code:402}},{status:402});
  if(String(url).endsWith('/decisions'))return Response.json({model:api.JEV_MODEL_ID,answers:{relevance:{type:'score',score}},usage:{cost:.00002}});
  return Response.json({model:body.model,choices:[{message:{content:JSON.stringify({relevance:mode==='bad-free'?101:66})}}]});
 }
 throw new Error('Unexpected request '+url);
};
const personal=(name='Nastya',id=91001)=>sqlite.prepare('SELECT * FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=?').get(name,id);
try{
 assert.equal((await req(null)).status,401);assert.equal((await req('Nastya',91001,'GET')).status,405);assert.equal((await req('Nastya',91001,'POST','https://evil.invalid')).status,403);
 assert.equal((await req('Nastya',91001,'POST',base,{userName:'Eugene'})).status,400);
 assert.equal((await req('Nastya',999999)).status,404);assert.equal((await req('Demo')).status,409);assert.equal(calls.length,0,'Missing resume does not bill a request');
 const emptyStream=await worker.fetch(new Request(base+'/api/vacancies/91001/relevance',{method:'POST',headers:{cookie:cookies.Nastya,origin:base},body:new Uint8Array(0)}),env);assert.equal(emptyStream.status,200,await emptyStream.clone().text());
 let r=await req();assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).relevance,80);assert.equal(personal().relevance,80);assert.equal(personal().favorite,1);assert.equal(personal().hidden,1);assert.equal(personal('Eugene').relevance,91);
 const body=calls.at(-1).body;assert.deepEqual(body.state,{resumeMd:'NASTYA PRIVATE RESUME',vacancyTitle:'Development manager',vacancyMd:'FULL VACANCY',salary:{from:300000,to:500000,currency:'RUB',period:'month'}});assert.ok(body.questions.relevance.instructions.startsWith(ALIGNED_RELEVANCE_PROMPT));assert.equal(body.questions.relevance.type,'score');assert.equal(body.questions.relevance.criteria.length,10);assert.match(body.questions.relevance.instructions,/шкале criteria/);assert.doesNotMatch(body.questions.relevance.instructions,/Верни только JSON/);assert.ok(!JSON.stringify(body).includes('EUGENE PRIVATE RESUME'));
 score=0;r=await req('Nastya',91002);assert.equal((await r.json()).relevance,0);assert.equal(personal('Nastya',91002).relevance,0);assert.ok(!Object.hasOwn(calls.at(-1).body.state,'salary'));
 score=9;assert.equal((await (await req('Nastya',91002)).json()).relevance,100);
 mode='credits';r=await req();assert.equal(r.status,402);assert.equal(personal().relevance,80);assert.match((await r.json()).error,/Недостаточно средств/);
 mode='normal';score=100;assert.equal((await req()).status,502);assert.equal(personal().relevance,80);score=7.2;
 // A changed description/resume/model/personal score cannot be replaced by an obsolete response.
 for(const update of ["UPDATE company_vacancies SET description_md=description_md||' UPDATED' WHERE id=91001", "UPDATE user_profiles SET resume_md=resume_md||' UPDATED',revision=revision+1 WHERE user_name='Nastya'", "UPDATE system_prompts SET revision=revision+1 WHERE kind='relevance'", "UPDATE prompt_model_settings SET revision=revision+1 WHERE kind='relevance'", "UPDATE company_vacancy_user_state SET relevance=45 WHERE user_name='Nastya' AND vacancy_id=91001", "UPDATE app_runtime_generation SET generation='00000000-0000-0000-0000-000000000001' WHERE id=1"]){
  mode='held';pending=false;const inflight=req();while(!pending)await new Promise(r=>setTimeout(r,1));sqlite.exec(update);const expected=personal().relevance;release();r=await inflight;assert.equal(r.status,409,await r.clone().text());assert.equal(personal().relevance,expected);
 }
 mode='normal';sqlite.exec("UPDATE prompt_model_settings SET openrouter_model='best_free',revision=revision+1 WHERE kind='relevance'");r=await req();assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).relevance,66);const free=calls.at(-1).body;assert.equal(free.model,'fixture/free:free');assert.equal(free.provider.max_price.prompt,0);assert.deepEqual(JSON.parse(free.messages[1].content).salary,{from:300000,to:500000,currency:'RUB',period:'month'});
 mode='bad-free';r=await req();assert.equal(r.status,502);assert.match((await r.json()).error,/оценку от 0 до 100/);assert.equal(personal().relevance,66);mode='normal';
 // Historical profile backfill preserves explicitly saved zero and other existing scores.
 sqlite.exec("UPDATE company_vacancy_user_state SET relevance=0 WHERE user_name='Eugene' AND vacancy_id=91001");await api.applyEugeneResumeRelevanceScores(db);assert.equal(personal('Eugene').relevance,0);
 for(const value of ['101','-1','80%','{"relevance":"80"}','null'])assert.equal(api.parseRelevanceAnswer(value),null);assert.equal(api.parseRelevanceAnswer('0'),0);assert.equal(api.parseRelevanceAnswer('{"relevance":100}'),100);
 console.log('Vacancy relevance: exact saved inputs/salary omission, private scores, 0/100, selected paid/free model, auth/CSRF, errors keep old score, atomic input/config/restore fencing and backfill preservation passed.');
}finally{globalThis.fetch=oldFetch;sqlite.close();}
