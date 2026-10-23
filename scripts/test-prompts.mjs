import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {ALIGNED_RELEVANCE_PROMPT,ALIGN_RELEVANCE_PROMPT_SQL} from '../server/services/relevance-prompt.js';
const worker=(await import('../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec(fs.readFileSync('drizzle/0048_prompt_model_settings.sql','utf8'));sqlite.exec(fs.readFileSync('drizzle/0051_prompt_openrouter_model.sql','utf8'));
const db={prepare(sql){let args=[];return {bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){const query=sqlite.prepare(sql);if(query.columns().length)return {results:query.all(...args)};return {success:true,meta:query.run(...args)}}}},async batch(statements){sqlite.exec('BEGIN');try{const result=[];for(const s of statements)result.push(await s.run());sqlite.exec('COMMIT');return result;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
const env={DB:db,SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',DEMO_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'};
const base='https://prompts-test.example';
const cookies={};
for(const name of ['Eugene','Nastya','Demo']){const result=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'})}),env);assert.equal(result.status,303);cookies[name]=result.headers.get('set-cookie').split(';')[0]}
const req=(name,path='/api/profile/prompts',method='GET',payload,origin=base)=>worker.fetch(new Request(base+path,{method,headers:{...(name?{cookie:cookies[name]}:{}),origin,'content-type':'application/json'},...(payload===undefined?{}:{body:JSON.stringify(payload)})}),env);
try{
 // The data correction targets only the observed old revision, preserving any
 // concurrent/custom edit and the unrelated letter. Runtime and SQL agree.
 const alignment=new DatabaseSync(':memory:');
 try{
  alignment.exec(fs.readFileSync('drizzle/0041_calm_satana.sql','utf8'));
  const insert=alignment.prepare('INSERT INTO system_prompts(kind,markdown,revision,updated_at) VALUES(?,?,?,?)');
  insert.run('cover_letter','Existing letter',3,'2026-10-01 21:20:45');
  insert.run('relevance','# Оценка релевантности вакансии\n\nLegacy prompt',1,'2026-10-16 19:51:15');
  const migration=fs.readFileSync('drizzle/0066_relevance_prompt_alignment.sql','utf8');
  alignment.exec(migration);
  const row=()=>alignment.prepare("SELECT * FROM system_prompts WHERE kind='relevance'").get();
  assert.equal(row().markdown,ALIGNED_RELEVANCE_PROMPT);assert.equal(row().revision,2);
  const corrected=row();alignment.exec(ALIGN_RELEVANCE_PROMPT_SQL);assert.deepEqual(row(),corrected);
  assert.equal(alignment.prepare("SELECT markdown FROM system_prompts WHERE kind='cover_letter'").get().markdown,'Existing letter');
  for(const [text,revision,date] of [['# Оценка релевантности вакансии\nCustom edit',2,'2026-10-16 19:51:15'],['# Оценка релевантности вакансии\nOther date',1,'2026-10-17 00:00:00'],['Different prompt',1,'2026-10-16 19:51:15']]){
   alignment.prepare("UPDATE system_prompts SET markdown=?,revision=?,updated_at=? WHERE kind='relevance'").run(text,revision,date);
   const before=row();alignment.exec(migration);alignment.exec(ALIGN_RELEVANCE_PROMPT_SQL);assert.deepEqual(row(),before);
  }
  assert.ok(Array.from(ALIGNED_RELEVANCE_PROMPT).length<=5000);
 }finally{alignment.close();}
 // Owner-authorized calibration changes the observed prompt only, not stored scores.
 const calibrated=new DatabaseSync(':memory:');
 try{
  calibrated.exec(fs.readFileSync('drizzle/0041_calm_satana.sql','utf8'));
  calibrated.exec("CREATE TABLE company_vacancy_user_state(user_name TEXT,vacancy_id INTEGER,relevance INTEGER); INSERT INTO company_vacancy_user_state VALUES('Fixture',1,0),('Fixture',2,77)");
  const insert=calibrated.prepare('INSERT INTO system_prompts(kind,markdown,revision,updated_at) VALUES(?,?,?,?)');
  insert.run('cover_letter','Preserved letter prompt',5,'2026-10-21 18:52:51');
  insert.run('relevance',ALIGNED_RELEVANCE_PROMPT,2,'2026-10-16 22:06:11');
  const get=()=>calibrated.prepare("SELECT * FROM system_prompts WHERE kind='relevance'").get();
  const scores=calibrated.prepare('SELECT * FROM company_vacancy_user_state').all(),letter=calibrated.prepare("SELECT * FROM system_prompts WHERE kind='cover_letter'").get();
  const migration=fs.readFileSync('drizzle/0080_relevance_prompt_calibration.sql','utf8');calibrated.exec(migration);
  const after=get();assert.equal(after.revision,3);assert.match(after.markdown,/Профессиональное соответствие: функция/);assert.ok(Array.from(after.markdown).length<=5000);
  assert.deepEqual(calibrated.prepare('SELECT * FROM company_vacancy_user_state').all(),scores);
  assert.deepEqual(calibrated.prepare("SELECT * FROM system_prompts WHERE kind='cover_letter'").get(),letter);
  calibrated.exec(migration);assert.deepEqual(get(),after,'Idempotent calibration');
  for(const [text,revision,date] of [[ALIGNED_RELEVANCE_PROMPT,3,'2026-10-16 22:06:11'],[ALIGNED_RELEVANCE_PROMPT,2,'2026-10-17 00:00:00'],['Custom prompt',2,'2026-10-16 22:06:11']]){
   calibrated.prepare("UPDATE system_prompts SET markdown=?,revision=?,updated_at=? WHERE kind='relevance'").run(text,revision,date);
   const before=get();calibrated.exec(migration);assert.deepEqual(get(),before,'Preserve concurrent/other edits');
  }
 }finally{calibrated.close();}
 // Upgrade populated old table without changing text, revision or timestamps.
 const upgraded=new DatabaseSync(':memory:');
 try{
  upgraded.exec(fs.readFileSync('drizzle/0041_calm_satana.sql','utf8'));
  const insert=upgraded.prepare('INSERT INTO system_prompts(kind,markdown,revision,updated_at) VALUES(?,?,?,?)');
  insert.run('cover_letter','# Existing **letter**',7,'2026-10-01 10:20:30');insert.run('relevance','## Existing relevance',9,'2026-10-02 10:20:30');
  const before=upgraded.prepare('SELECT * FROM system_prompts ORDER BY kind').all();
  upgraded.exec('BEGIN;'+fs.readFileSync('drizzle/0047_cover_letter_prompt_limit.sql','utf8')+'COMMIT;');
  assert.deepEqual(upgraded.prepare('SELECT * FROM system_prompts ORDER BY kind').all(),before);
  const update=upgraded.prepare('UPDATE system_prompts SET markdown=? WHERE kind=?');
  update.run('😀'.repeat(10000),'cover_letter');assert.throws(()=>update.run('x'.repeat(10001),'cover_letter'),/CHECK constraint/);
  const after0047=upgraded.prepare('SELECT * FROM system_prompts ORDER BY kind').all();
  upgraded.exec('BEGIN;'+fs.readFileSync('drizzle/0079_cover_letter_prompt_15000.sql','utf8')+'COMMIT;');
  assert.deepEqual(upgraded.prepare('SELECT * FROM system_prompts ORDER BY kind').all(),after0047);
  update.run('😀'.repeat(15000),'cover_letter');assert.throws(()=>update.run('x'.repeat(15001),'cover_letter'),/CHECK constraint/);
  update.run('😀'.repeat(5000),'relevance');assert.throws(()=>update.run('x'.repeat(5001),'relevance'),/CHECK constraint/);
 }finally{upgraded.close();}
 assert.equal((await req(null)).status,401);
 for(const name of Object.keys(cookies)){
  if(name==='Eugene'){const result=await (await req(name)).json();assert.equal(result.maxLength,5000);assert.deepEqual(result.maxLengths,{cover_letter:15000,relevance:5000,vacancy_extraction:10000,vacancy_role:5000});assert.equal(result.prompts.cover_letter.markdown,'');assert.equal(result.prompts.relevance.revision,0);}else assert.equal((await req(name)).status,403);
  const {default:Settings}=await moduleFor('frontend/features/settings/Settings.tsx');assert.equal(render(Settings,{user:sessionUser(name==='Eugene'),tab:'prompts'}).includes('Промты'),name==='Eugene');

 }
 const extractionPath='/api/profile/prompts/vacancy_extraction';
 const longPrompt='😀'.repeat(10000);
 const extractionSaved=await req('Eugene',extractionPath,'PUT',{markdown:longPrompt,revision:0});assert.equal(extractionSaved.status,200,await extractionSaved.clone().text());assert.equal((await extractionSaved.json()).maxLength,10000);
 assert.equal((await(await req('Eugene')).json()).prompts.vacancy_extraction.markdown,longPrompt);
 assert.equal((await req('Eugene',extractionPath,'PUT',{markdown:'я'.repeat(10001),revision:1})).status,400);
 assert.equal((await req('Eugene',extractionPath,'PUT',{markdown:'valid',revision:0})).status,409);
 assert.equal((await req('Nastya',extractionPath,'PUT',{markdown:'valid',revision:1})).status,403);
 assert.equal((await req('Eugene',extractionPath,'PUT',{markdown:'valid',revision:1},'https://foreign.test')).status,403);
 assert.throws(()=>sqlite.prepare('UPDATE vacancy_detail_settings SET markdown=? WHERE id=1').run('я'.repeat(10001)),/CHECK constraint/);
 const path='/api/profile/prompts/cover_letter';
 assert.equal((await req('Eugene',path,'PUT',{markdown:'# Привет\n\n**Письмо**',revision:0})).status,200);
 assert.equal((await req('Nastya',path,'PUT',{markdown:'Попытка',revision:0})).status,403);
 assert.equal((await req('Demo',path,'PUT',{markdown:'Попытка',revision:0})).status,403);
 assert.equal((await req('Eugene','/api/profile/prompts/relevance','PUT',{markdown:'## Оценка',revision:0})).status,200);
 assert.equal((await (await req('Eugene')).json()).prompts.cover_letter.markdown,'# Привет\n\n**Письмо**');
 assert.equal((await (await req('Eugene')).json()).prompts.relevance.markdown,'## Оценка');
 assert.equal((await req('Nastya')).status,403);
 assert.equal((await req('Eugene',path,'PUT',{markdown:'stale',revision:0})).status,409);
 assert.equal((await req('Eugene',path,'PUT',{markdown:'oversize',revision:1},'https://wrong.test')).status,403);
 for(const payload of [{markdown:'x'.repeat(15001),revision:1},{markdown:'a\0b',revision:1},{markdown:null,revision:1},{markdown:'hack',revision:1,userName:'Nastya'}])assert.equal((await req('Eugene',path,'PUT',payload)).status,400);
 assert.equal((await req('Eugene','/api/profile/prompts/unknown','PUT',{markdown:'x',revision:0})).status,404);
 assert.equal((await req('Eugene',path,'DELETE')).status,405);
 let response=await req('Eugene',path,'PUT',{markdown:'😀'.repeat(15000),revision:1});assert.equal(response.status,200,await response.clone().text());assert.equal((await response.json()).maxLength,15000);
 assert.equal(Array.from((await (await req('Eugene')).json()).prompts.cover_letter.markdown).length,15000);
 assert.equal((await req('Eugene','/api/profile/prompts/relevance','PUT',{markdown:'x'.repeat(5001),revision:1})).status,400);
 assert.equal((await req('Eugene','/api/profile/prompts/relevance','PUT',{markdown:'😀'.repeat(5000),revision:1})).status,200);
 assert.equal((await req('Eugene','/api/profile/prompts/relevance','PUT',{markdown:'## Оценка',revision:2})).status,200);
 assert.equal((await req('Eugene',path,'PUT',{markdown:'',revision:2})).status,200);
 assert.equal((await (await req('Eugene')).json()).prompts.cover_letter.markdown,'');
 assert.equal(sqlite.prepare('SELECT markdown FROM system_prompts WHERE kind=?').get('relevance').markdown,'## Оценка');
 assert.equal((await req('Eugene',path,'PUT',{markdown:'x'.repeat(132000),revision:3})).status,413);
 const modelPath='/api/profile/prompt-models/cover_letter';
 const config={provider:'openrouter',openaiModel:'gpt-6-luna',reasoningEffort:'medium',revision:0};
 for(const name of ['Nastya','Demo']){assert.equal((await req(name,'/api/profile/prompt-models')).status,403);assert.equal((await req(name,modelPath,'PUT',config)).status,403);}
 assert.equal((await req('Eugene',modelPath,'PUT',config,'https://wrong.test')).status,403);
 assert.equal((await req('Eugene',modelPath,'PUT',config)).status,200);
 assert.equal((await req('Eugene',modelPath,'PUT',config)).status,409);
 assert.equal((await req('Eugene',modelPath,'PUT',{...config,provider:'bad',revision:1})).status,400);
 const modelSettings=await (await req('Eugene','/api/profile/prompt-models')).json();assert.equal(modelSettings.models.cover_letter.provider,'openrouter');assert.equal(modelSettings.models.relevance.provider,'openrouter');
 assert.equal(sqlite.prepare('SELECT revision FROM system_prompts WHERE kind=?').get('cover_letter').revision,3,'Model settings never overwrite prompt text/revision');
 sqlite.prepare("UPDATE system_prompts SET markdown=?,revision=1,updated_at='2026-10-16 19:51:15' WHERE kind='relevance'").run('# Оценка релевантности вакансии\n\nLegacy prompt');
 const aligned=await (await req('Eugene')).json();assert.equal(aligned.prompts.relevance.markdown,ALIGNED_RELEVANCE_PROMPT);assert.equal(aligned.prompts.relevance.revision,2);
 assert.equal((await req('Eugene','/api/profile/prompts/relevance','PUT',{markdown:'Later admin edit',revision:1})).status,409);
 assert.equal((await req('Eugene','/api/profile/prompts/relevance','PUT',{markdown:'Later admin edit',revision:2})).status,200);
 assert.equal((await (await req('Eugene')).json()).prompts.relevance.markdown,'Later admin edit');
 console.log('Prompts verified: admin-only editors, shared records, Unicode limit, CAS/CSRF, legacy alignment idempotence and preservation of later edits.');
}finally{sqlite.close()}
