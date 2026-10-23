import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();const backend=await import('../.tests/backend.mjs');
const oldFetch=globalThis.fetch,waiting=[],calls=[];let hold=false;
async function provider(url,body){
 if(url.includes('/models?'))return Response.json({data:[{id:'synthetic/model',name:'Synthetic',context_length:262144,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:0,completion:0}}]});
 if(url.includes('/benchmarks?'))return Response.json({data:[]});
 if(url.endsWith('/chat/completions')){
  const request=JSON.parse(body),inputs=JSON.parse(request.messages[1].content);calls.push(inputs);
  if(hold)await new Promise(resolve=>waiting.push(resolve));
  return Response.json({model:request.model,choices:[{message:{content:JSON.stringify({subject:'For '+inputs.vacancyTitle,markdown:'Letter for '+inputs.vacancyTitle})}}]});
 }
 throw new Error('Unexpected synthetic provider request');
}
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'cover-letter-concurrency'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:async request=>provider(request.url,await request.text())};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 const cookies={};for(const name of ['Eugene','Nastya']){const r=await mf.dispatchFetch('https://letter-race.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(path,method='GET',body,user='Eugene',extra={})=>mf.dispatchFetch('https://letter-race.test'+path,{method,headers:{cookie:cookies[user],origin:'https://letter-race.test','content-type':'application/json',...extra},...(body===undefined?{}:{body:JSON.stringify(body)})});
 assert.equal((await req('/api/profile/resume','PUT',{resumeMd:'Synthetic CV',revision:0},'Nastya')).status,200);
 assert.equal((await req('/api/profile/resume','PUT',{resumeMd:'Other CV',revision:0})).status,200);
 assert.equal((await req('/api/profile/prompts/cover_letter','PUT',{markdown:'Synthetic prompt',revision:0})).status,200);
 assert.equal((await req('/api/openrouter/key','PUT',{apiKey:'FAKE_TEST_TOKEN_8219f7325f23'})).status,200);
 let nextId=9000;
 const fresh=async()=>{
  // Each case is isolated; preserve the production ten-vacancy constraint.
  await db.prepare("DELETE FROM company_vacancies WHERE company_name='Synthetic'").run();
  await db.prepare("INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('Nastya','Synthetic CV',1) ON CONFLICT(user_name) DO UPDATE SET resume_md=excluded.resume_md,revision=user_profiles.revision+1").run();
  await db.prepare("INSERT INTO system_prompts(kind,markdown,revision) VALUES('cover_letter','Synthetic prompt',1) ON CONFLICT(kind) DO UPDATE SET markdown=excluded.markdown,revision=system_prompts.revision+1").run();
  const id=++nextId;await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(?,'Synthetic',?,'Initial title','Initial description')").bind(id,'https://letter-race.test/job/'+id).run();return id;
 };
 const letter=id=>req('/api/vacancies/'+id+'/cover-letter','POST',undefined,'Nastya');
 const count=async id=>(await db.prepare("SELECT COUNT(*) AS n FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=?").bind(id).first()).n;
 const wait=async()=>{for(let i=0;i<2000&&!waiting.length;i++)await new Promise(r=>setTimeout(r,1));assert.equal(waiting.length,1,'Synthetic inference must be pending');};
 const release=()=>{assert.equal(waiting.length,1);waiting.shift()();};
 const mutations=[
  id=>db.prepare("UPDATE company_vacancies SET title='Changed title' WHERE id=?").bind(id).run(),
  id=>db.prepare("UPDATE company_vacancies SET description_md='Changed description' WHERE id=?").bind(id).run(),
  ()=>db.prepare("UPDATE user_profiles SET resume_md='Changed CV',revision=revision+1 WHERE user_name='Nastya'").run(),
  ()=>db.prepare("UPDATE user_profiles SET resume_md='',revision=revision+1 WHERE user_name='Nastya'").run(),
  ()=>db.prepare("DELETE FROM user_profiles WHERE user_name='Nastya'").run(),
  ()=>db.prepare("UPDATE system_prompts SET markdown='Changed prompt',revision=revision+1 WHERE kind='cover_letter'").run(),
  ()=>db.prepare("UPDATE system_prompts SET markdown='',revision=revision+1 WHERE kind='cover_letter'").run(),
  ()=>db.prepare("DELETE FROM system_prompts WHERE kind='cover_letter'").run(),
  // Equal text with a newer input revision must also reject (ABA).
  ()=>db.prepare("UPDATE user_profiles SET revision=revision+2 WHERE user_name='Nastya'").run(),
  ()=>db.prepare("UPDATE system_prompts SET revision=revision+2 WHERE kind='cover_letter'").run(),
  id=>db.prepare("UPDATE company_vacancies SET url=url||'/replaced' WHERE id=?").bind(id).run(),
 ];
 for(const change of mutations){const id=await fresh();hold=true;const pending=letter(id);await wait();await change(id);release();const r=await pending;assert.equal(r.status,409,await r.clone().text());assert.match((await r.json()).error,/Устаревший результат не сохранён/);assert.equal(await count(id),0);}
 // Unrelated candidate data, relevance/favorites and another prompt do not cancel.
 let id=await fresh();let pending=letter(id);await wait();
 await db.batch([db.prepare("UPDATE user_profiles SET resume_md='Other changed',revision=revision+1 WHERE user_name='Eugene'"),db.prepare("INSERT INTO system_prompts(kind,markdown,revision) VALUES('relevance','Other prompt',1)"),db.prepare("INSERT INTO company_vacancy_user_state(user_name,vacancy_id,favorite,hidden,relevance) VALUES('Nastya',?,1,1,99)").bind(id)]);
 release();assert.equal((await pending).status,200);assert.equal(await count(id),1);assert.equal(calls.at(-1).resumeMd,'Synthetic CV');
 // Current manual shared/personal text has priority over the delayed model answer.
 for(const scope of ['shared','personal']){id=await fresh();pending=letter(id);await wait();
  if(scope==='shared')await db.prepare("UPDATE company_vacancies SET cover_letter_subject='Manual',cover_letter_md='Manual shared' WHERE id=?").bind(id).run();
  else await db.prepare("INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES('Nastya',?,'Manual','Manual personal')").bind(id).run();
  release();const data=await(await pending).json();assert.equal(data.coverLetterMd,'Manual '+scope);assert.equal(data.source,scope==='shared'?'vacancy':'personal');assert.equal(await count(id),scope==='shared'?0:1);
 }
 id=await fresh();pending=letter(id);await wait();await db.prepare('DELETE FROM company_vacancies WHERE id=?').bind(id).run();release();assert.equal((await pending).status,404);assert.equal(await count(id),0);
 // Real backup restore replaces the same ID with different model inputs.
 id=await fresh();const backup=await(await req('/api/backup')).json();await db.prepare("UPDATE company_vacancies SET title='Pre-restore title' WHERE id=?").bind(id).run();pending=letter(id);await wait();const restored=await req('/api/backup','POST',backup);assert.equal(restored.status,200,await restored.clone().text());release();assert.equal((await pending).status,409);assert.equal(await count(id),0);assert.equal((await db.prepare('SELECT title FROM company_vacancies WHERE id=?').bind(id).first()).title,'Initial title');
 // NDJSON reports the rejected result without a done frame or a persisted letter.
 id=await fresh();const streamed=await req('/api/vacancies/'+id+'/cover-letter','POST',undefined,'Nastya',{accept:'application/x-ndjson'});const reader=streamed.body.getReader();await reader.read();await wait();await mutations[0](id);release();let chunks='';for(;;){const part=await reader.read();if(part.done)break;chunks+=new TextDecoder().decode(part.value);}assert.match(chunks,/"type":"error"/);assert.ok(!chunks.includes('"type":"done"'));assert.equal(await count(id),0);
 // Force input/manual changes at the actual SQL write after all inference work.
 // Real D1 executes the conditional INSERT; no JS preflight can protect this race.
 hold=false;globalThis.fetch=async(url,options)=>provider(url,options?.body);
 for(const change of [mutations[0],mutations[2],mutations[5],async id=>db.prepare("UPDATE company_vacancies SET cover_letter_subject='At write',cover_letter_md='Manual at write' WHERE id=?").bind(id).run()]){
  id=await fresh();let hits=0;
  const wrapped={batch:db.batch.bind(db),prepare(sql){const statement=db.prepare(sql);if(!/^INSERT(?: OR IGNORE)? INTO vacancy_cover_letters/.test(sql))return statement;let bound=statement;return {bind(...args){bound=statement.bind(...args);return this;},async run(){hits++;await change(id);return bound.run();}};}};
  const env={DB:wrapped,SESSION_SECRET:'synthetic-secret'};
  if(change===mutations[0]||change===mutations[2]||change===mutations[5])await assert.rejects(()=>backend.vacancyCoverLetter(wrapped,env,'Nastya',id),error=>error.status===409);
  else assert.equal((await backend.vacancyCoverLetter(wrapped,env,'Nastya',id)).coverLetterMd,'Manual at write');
  assert.equal(hits,1);assert.equal(await count(id),0);
 }
 // JS whitespace semantics are retained; an empty shared letter can include Unicode spaces.
 id=await fresh();await db.prepare('UPDATE company_vacancies SET cover_letter_md=? WHERE id=?').bind('\u00a0\u2003',id).run();assert.equal((await letter(id)).status,200);assert.equal(await count(id),1);
 // SQL failure is not mistaken for stale input; retry writes only one letter.
 id=await fresh();await db.prepare("CREATE TRIGGER fail_letter BEFORE INSERT ON vacancy_cover_letters BEGIN SELECT RAISE(ABORT,'Synthetic failure'); END").run();let r=await letter(id);assert.equal(r.status,502);assert.equal(await count(id),0);await db.prepare('DROP TRIGGER fail_letter').run();r=await letter(id);assert.equal(r.status,200,await r.clone().text());assert.equal(await count(id),1);
 const previous=calls.length;assert.equal((await letter(id)).status,200);assert.equal(calls.length,previous,'Saved text does not regenerate');
 console.log('D1/workerd cover-letter stale-input guard: all four inputs, revisions/ABA, unrelated state, manual priority, deletion, real changed-input backup restore, NDJSON, forced SQL-write races, SQL error/retry and no regeneration passed. Identical-snapshot and metadata restore barrier covered by test-ai-restore-barrier.');
}finally{hold=false;for(const resolve of waiting.splice(0))resolve();globalThis.fetch=oldFetch;await mf.dispose();}
