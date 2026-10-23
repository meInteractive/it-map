import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
const worker=(await import('../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const file of fs.readdirSync('drizzle').filter(name=>name.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
// A populated migration must preserve private letter content and original date.
{
 const migrationDb=new DatabaseSync(':memory:');for(const file of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')&&!f.startsWith('0050')).sort())migrationDb.exec(fs.readFileSync('drizzle/'+file,'utf8'));
 migrationDb.exec("INSERT INTO company_crm(company_name) VALUES('Migration fixture');INSERT INTO company_vacancies(id,company_name,title,url) VALUES(990001,'Migration fixture','Role','https://fixture.test');INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown,created_at) VALUES('Nastya',990001,'Saved subject','Saved body','2026-10-01 01:02:03');");
 const before=migrationDb.prepare('SELECT * FROM vacancy_cover_letters').get();migrationDb.exec(fs.readFileSync('drizzle/0050_cover_letter_regeneration.sql','utf8'));
 assert.deepEqual({...migrationDb.prepare('SELECT * FROM vacancy_cover_letters').get()},{...before,override_shared:0,revision:0});assert.throws(()=>migrationDb.exec('UPDATE vacancy_cover_letters SET override_shared=2'));assert.throws(()=>migrationDb.exec('UPDATE vacancy_cover_letters SET revision=-1'));migrationDb.close();
}
const db={prepare(sql){let args=[];return {bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){const p=sqlite.prepare(sql);if(p.columns().length)return {results:p.all(...args)};return {meta:p.run(...args)}}}},async batch(statements){sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(e){sqlite.exec('ROLLBACK');throw e}}};
const env={DB:db,SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',DEMO_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'};
const base='https://letter-test.example';
async function login(name){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'})}),env);assert.equal(r.status,303);return r.headers.get('set-cookie').split(';')[0]}
const cookies={Eugene:await login('Eugene'),Nastya:await login('Nastya'),Demo:await login('Demo')};
const req=(name,path,method='GET',body,origin=base,extraHeaders={})=>worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[name],origin,'content-type':'application/json',...extraHeaders},...(body?{body:JSON.stringify(body)}:{})}),env);
const letter=(name,id=900001)=>req(name,`/api/vacancies/${id}/cover-letter`,'POST');
const originalFetch=globalThis.fetch;let calls=[],mode='normal',releaseDeferred,answer=JSON.stringify({subject:'Тема отклика',markdown:'Здравствуйте, **мой опыт** подходит.'});
globalThis.fetch=async(url,options)=>{
 if(url.includes('/models?'))return Response.json({data:['one','two','three'].map(id=>({id:'test/'+id,name:id,context_length:262144,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:0,completion:0}}))});
 if(url.includes('/benchmarks?'))return Response.json({data:['one','two','three'].map((id,index)=>({source:'artificial-analysis',model_permaslug:'test/'+id,intelligence_index:3-index}))});
 if(url.endsWith('/chat/completions')){const body=JSON.parse(options.body);calls.push(body);
  if(body.messages[0].content.includes('Ответь одним словом')&&body.model==='test/one')return Response.json({error:{message:'Unavailable'}},{status:503});
  if(mode==='deferred'&&body.model==='test/two')await new Promise(resolve=>{releaseDeferred=resolve});
  const content=mode==='fallback-format'&&body.model==='test/two'?'bad JSON':answer;
  return Response.json({model:body.model,choices:[{message:{content}}]});}
 throw new Error('unexpected external request: '+url);
};
try{
 sqlite.exec("INSERT INTO company_crm(company_name) VALUES('Synthetic'); INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(900001,'Synthetic','https://example.invalid/1','Инженер данных','## Требования\\nSQL и Python'),(900002,'Synthetic','https://example.invalid/2','Аналитик','Полный текст'),(900003,'Synthetic','https://example.invalid/3','Менеджер','Текст'),(900004,'Synthetic','https://example.invalid/4','Директор','Полный текст')");
 assert.equal((await req('Nastya','/api/vacancies/1/cover-letter')).status,405);
 assert.equal((await req('Nastya','/api/vacancies/1/cover-letter','POST',null,'https://evil.invalid')).status,403);
 assert.equal((await letter('Nastya',9000)).status,404);
 let r=await letter('Nastya');assert.equal(r.status,409);assert.match((await r.json()).error,/резюме/);
 await req('Nastya','/api/profile/resume','PUT',{resumeMd:'# Резюме Насти\nОпыт SQL',revision:0});
 r=await letter('Nastya');assert.equal(r.status,409);assert.match((await r.json()).error,/Промт/);
 const key='FAKE_TEST_TOKEN_81efd70a208d';r=await req('Eugene','/api/openrouter/key','PUT',{apiKey:key});assert.equal(r.status,200,await r.clone().text());
 r=await req('Eugene','/api/openrouter/test','POST');assert.equal(r.status,200);assert.equal((await r.json()).model,'test/two');calls=[];
 r=await req('Eugene','/api/profile/prompts/cover_letter','PUT',{markdown:'Пиши кратко и по делу.',revision:0});assert.equal(r.status,200);
 r=await letter('Nastya');assert.equal(r.status,200,await r.clone().text());let data=await r.json();assert.equal(data.source,'personal');assert.equal(data.coverLetterSubject,'Тема отклика');assert.equal(data.coverLetterMd,'Здравствуйте, **мой опыт** подходит.');
 assert.equal(calls.length,1);assert.equal(calls[0].model,'test/two','Use last successful test model, not ranked #1');assert.match(calls[0].messages[0].content,/Пиши кратко/);assert.deepEqual(JSON.parse(calls[0].messages[1].content),{resumeMd:'# Резюме Насти\nОпыт SQL',vacancyTitle:'Инженер данных',vacancyMd:'## Требования\\nSQL и Python'});
 assert.equal((await letter('Nastya')).status,200);assert.equal(calls.length,1,'Saved letter must not regenerate');
 assert.equal((await letter('Demo')).status,409,'Other candidate cannot read Nastya’s letter');
 await req('Demo','/api/profile/resume','PUT',{resumeMd:'Резюме Demo',revision:0});r=await letter('Demo');assert.equal(r.status,200);assert.equal(calls.length,2);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_cover_letters WHERE vacancy_id=900001').get().n,2);
 assert.equal((await (await req('Demo','/api/vacancies')).json()).vacancies.find(v=>v.id===900001).coverLetterMd,'','List must not expose personal letter');
 sqlite.prepare("UPDATE company_vacancies SET cover_letter_subject='Общая тема',cover_letter_md='Письмо вручную' WHERE id=900002").run();r=await letter('Nastya',900002);data=await r.json();assert.equal(data.source,'vacancy');assert.equal(data.coverLetterMd,'Письмо вручную');assert.equal(calls.length,2);
 answer='bad JSON';r=await letter('Nastya',900003);assert.equal(r.status,502);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_cover_letters WHERE vacancy_id=900003').get().n,0);
 answer=JSON.stringify({subject:'Новая тема',markdown:'Новый текст'});mode='fallback-format';calls=[];r=await letter('Nastya',900003);assert.equal(r.status,200);assert.equal((await r.json()).coverLetterMd,'Новый текст');assert.deepEqual(calls.map(call=>call.model),['test/two','test/three']);
 const failedAttempt=sqlite.prepare("SELECT error_code FROM openrouter_ai_logs WHERE task='cover_letter_attempt' AND actual_model='test/two' AND success=0 ORDER BY id DESC LIMIT 1").get();assert.equal(failedAttempt.error_code,'invalid_output');
 const log=sqlite.prepare("SELECT requested_primary_model,actual_model,fallback_used,success FROM openrouter_ai_logs WHERE task='cover_letter' ORDER BY id DESC LIMIT 1").get();assert.equal(log.requested_primary_model,'test/two');assert.equal(log.actual_model,'test/three');assert.equal(log.fallback_used,1);assert.equal(log.success,1);
 mode='deferred';calls=[];const streamed=await req('Nastya','/api/vacancies/900004/cover-letter','POST',null,base,{accept:'application/x-ndjson'});assert.equal(streamed.status,200);const streamReader=streamed.body.getReader();const first=await streamReader.read();assert.match(new TextDecoder().decode(first.value),/progress/);for(let i=0;i<30&&!releaseDeferred;i++)await new Promise(resolve=>setTimeout(resolve,1));assert.equal(typeof releaseDeferred,'function');assert.deepEqual(calls.map(call=>call.model),['test/two'],'Slow active model remains in progress without advancing');releaseDeferred();let rest='';for(;;){const part=await streamReader.read();if(part.done)break;rest+=new TextDecoder().decode(part.value)}assert.match(rest,/"type":"done"/);assert.match(rest,/"stage":"model"/);assert.match(rest,/"model":"two"/);assert.match(rest,/"stage":"saving"/);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_cover_letters WHERE vacancy_id=900004').get().n,1);const timing=sqlite.prepare("SELECT success_count,average_ms FROM (SELECT success_count,total_duration_ms/success_count average_ms FROM openrouter_model_metrics WHERE model_id='test/two')").get();assert.ok(timing.success_count>=3);assert.ok(timing.average_ms>=0);const publicState=await (await req('Eugene','/api/openrouter')).json();assert.equal(publicState.models.find(m=>m.id==='test/two').letter_timing.success_count,timing.success_count);
 mode='normal';answer='```markdown\n# Цепляющая тема\n\nПисьмо кандидата\n```';r=await letter('Demo',900004);assert.equal(r.status,200);assert.equal((await r.json()).coverLetterSubject,'Цепляющая тема');
 // Explicit regeneration replaces only the current candidate after success.
 const regenerate=(id=900001)=>req('Nastya',`/api/vacancies/${id}/cover-letter/regenerate`,'POST');
 assert.equal((await req('Nastya','/api/vacancies/900001/cover-letter/regenerate')).status,405);
 assert.equal((await req('Nastya','/api/vacancies/900001/cover-letter/regenerate','POST',null,'https://evil.invalid')).status,403);
 sqlite.prepare("UPDATE user_profiles SET resume_md='UPDATED RESUME',revision=revision+1 WHERE user_name='Nastya'").run();
 sqlite.prepare("UPDATE system_prompts SET markdown='UPDATED PROMPT',revision=revision+1 WHERE kind='cover_letter'").run();
 answer=JSON.stringify({subject:'REGENERATED SUBJECT',markdown:'REGENERATED BODY'});calls=[];
 r=await regenerate();assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).coverLetterMd,'REGENERATED BODY');assert.equal(calls.length,1);
 assert.match(calls[0].messages[0].content,/UPDATED PROMPT/);assert.equal(JSON.parse(calls[0].messages[1].content).resumeMd,'UPDATED RESUME');
 assert.equal(sqlite.prepare("SELECT revision FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=900001").get().revision,1);
 assert.equal((await (await req('Nastya','/api/vacancies/900001')).json()).vacancy.coverLetterMd,'REGENERATED BODY');
 assert.equal((await (await req('Demo','/api/vacancies/900001')).json()).vacancy.coverLetterMd,'Здравствуйте, **мой опыт** подходит.');
 const savedBefore=sqlite.prepare("SELECT * FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=900001").get();
 answer='bad JSON';r=await regenerate();assert.equal(r.status,502);assert.deepEqual(sqlite.prepare("SELECT * FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=900001").get(),savedBefore,'Failure keeps previous text');
 answer=JSON.stringify({subject:'REGENERATED SUBJECT',markdown:'REGENERATED BODY'});mode='deferred';calls=[];releaseDeferred=undefined;
 const firstRegen=regenerate();while(!releaseDeferred)await new Promise(resolve=>setTimeout(resolve,1));const secondRegen=regenerate();
 for(let i=0;i<20;i++)await new Promise(resolve=>setTimeout(resolve,1));assert.equal(calls.length,1,'Parallel regeneration shares the lease');releaseDeferred();
 assert.equal((await firstRegen).status,200);assert.equal((await secondRegen).status,200);assert.equal(calls.length,1);
 assert.equal(sqlite.prepare("SELECT revision FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=900001").get().revision,2,'Identical output still advances revision');
 // Prompt mutation during inference rejects stale output and retains both fields.
 releaseDeferred=undefined;const staleRegen=regenerate();while(!releaseDeferred)await new Promise(resolve=>setTimeout(resolve,1));
 sqlite.prepare("UPDATE system_prompts SET revision=revision+1 WHERE kind='cover_letter'").run();releaseDeferred();assert.equal((await staleRegen).status,409);
 assert.equal(sqlite.prepare("SELECT revision FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=900001").get().revision,2);
 // A shared legacy letter gets a private override, never a global overwrite.
 mode='normal';answer=JSON.stringify({subject:'PRIVATE REPLACEMENT',markdown:'PRIVATE OVERRIDE'});r=await regenerate(900002);assert.equal(r.status,200,await r.clone().text());
 assert.equal((await (await req('Nastya','/api/vacancies/900002')).json()).vacancy.coverLetterMd,'PRIVATE OVERRIDE');
 assert.equal((await (await req('Demo','/api/vacancies/900002')).json()).vacancy.coverLetterMd,'Письмо вручную');
 assert.equal((await (await letter('Nastya',900002)).json()).coverLetterMd,'PRIVATE OVERRIDE');
 assert.equal(sqlite.prepare('SELECT cover_letter_md FROM company_vacancies WHERE id=900002').get().cover_letter_md,'Письмо вручную');
 await (await import('./test-react.mjs')).testCoverLetter();
 assert.equal((await req('Nastya','/api/profile/prompts')).status,403);
 console.log('Cover letters verified: four inputs, active tested model, prompt output repair, streamed progress for slow model, timing metrics, fallback for invalid models, personal isolation, manual priority, no regeneration, missing inputs and retry.');
}finally{globalThis.fetch=originalFetch;sqlite.close()}
