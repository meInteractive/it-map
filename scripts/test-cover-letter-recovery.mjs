import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const waiting=[],calls=[];let mode='success';
const models=['first','next'].map(id=>({id:'synthetic/'+id,name:id,context_length:262144,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:0,completion:0}}));
async function provider(url,body){
 if(url.includes('/models?'))return Response.json({data:models});
 if(url.includes('/benchmarks?'))return Response.json({data:[]});
 if(url.endsWith('/chat/completions')){
  const request=JSON.parse(body),input=JSON.parse(request.messages[1].content);const ordinal=calls.length+1,answerMode=mode;calls.push({model:request.model,...input});
  await new Promise(resolve=>waiting.push(resolve));
  if(answerMode==='fail')return Response.json({error:{message:'Synthetic provider failure'}},{status:503});
  return Response.json({model:request.model,choices:[{message:{content:JSON.stringify({subject:'Call '+ordinal,markdown:'Letter call '+ordinal})}}]});
 }
 throw new Error('Unexpected provider request');
}
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'letter-recovery'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:async request=>provider(request.url,await request.text())};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'}]};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB','first'),workers={first:await mf.getWorker('first'),second:await mf.getWorker('second')};
 await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 for(const id of [1,2])await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(?,'Synthetic',?,?,'Body')").bind(id,'https://dedupe.test/'+id,'Role '+id).run();
 const cookies={};for(const name of ['Eugene','Nastya']){const r=await workers.first.fetch('https://dedupe.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(path,method='GET',body,user='Eugene',instance='first',headers={})=>workers[instance].fetch('https://dedupe.test'+path,{method,headers:{cookie:cookies[user],'content-type':'application/json',...headers},...(body===undefined?{}:{body:JSON.stringify(body)})});
 for(const user of ['Eugene','Nastya'])assert.equal((await req('/api/profile/resume','PUT',{resumeMd:'CV '+user,revision:0},user)).status,200);
 assert.equal((await req('/api/profile/prompts/cover_letter','PUT',{markdown:'Prompt',revision:0})).status,200);
 assert.equal((await req('/api/openrouter/key','PUT',{apiKey:'sk-or-synthetic-1234567890'})).status,200);
 const letter=(instance='first',user='Nastya',id=1,stream=false)=>req('/api/vacancies/'+id+'/cover-letter','POST',undefined,user,instance,stream?{accept:'application/x-ndjson'}:{});
 const wait=async(n=1)=>{for(let i=0;i<2000&&waiting.length<n;i++)await new Promise(r=>setTimeout(r,1));assert.equal(waiting.length,n)};
 const release=()=>waiting.shift()();

 const clean=async()=>{await db.prepare('DELETE FROM vacancy_cover_letters').run();assert.equal(waiting.length,0);assert.equal((await db.prepare('SELECT count(*) n FROM cover_letter_generation_leases').first()).n,0);};
 const join=async()=>{
  const r=await letter('second','Nastya',1,true),reader=r.body.getReader();let frames='';
  while(!frames.includes('"stage":"waiting"')){const part=await reader.read();assert.ok(!part.done);frames+=new TextDecoder().decode(part.value);}
  return async()=>{for(;;){const part=await reader.read();if(part.done)break;frames+=new TextDecoder().decode(part.value);}return frames.trim().split('\n').map(JSON.parse);};
 };
 const count=async()=>(await db.prepare('SELECT count(*) n FROM vacancy_cover_letters').first()).n;
 const aiState=async()=>({logs:(await db.prepare('SELECT * FROM openrouter_ai_logs ORDER BY id').all()).results,metrics:(await db.prepare('SELECT * FROM openrouter_model_metrics ORDER BY model_id').all()).results});
 // A failed owner alone tries the next model. Joined requests do not retry the whole chain.
 mode='fail';let before=calls.length,owner=letter();await wait();let joined=await join();release();await wait();release();let r=await owner;assert.equal(r.status,503,await r.clone().text());let frames=await joined();assert.ok(frames.some(e=>e.type==='error'));assert.ok(!frames.some(e=>e.type==='done'));assert.equal(calls.length,before+2);assert.deepEqual(calls.slice(before).map(c=>c.model),['synthetic/first','synthetic/next']);assert.equal(await count(),0);await clean();
 // Explicit retry can acquire again, with no leaked ownership left by the failed request.
 mode='success';owner=letter('second');await wait();release();r=await owner;assert.equal(r.status,200,await r.clone().text());assert.equal(calls.length,before+3);assert.equal(await count(),1);await clean();
 // Expired owner is replaced in another Worker while its old model is still pending.
 // Provider failure must NOT trigger fallback or metrics/logs after takeover.
 before=calls.length;mode='fail';owner=letter();await wait();const oldToken=(await db.prepare("SELECT owner_token FROM cover_letter_generation_leases WHERE user_name='Nastya' AND vacancy_id=1").first()).owner_token;
 await db.prepare("UPDATE cover_letter_generation_leases SET expires_at=1 WHERE user_name='Nastya' AND vacancy_id=1").run();
 mode='success';let successor=letter('second');await wait(2);const successorToken=(await db.prepare("SELECT owner_token FROM cover_letter_generation_leases WHERE user_name='Nastya' AND vacancy_id=1").first()).owner_token;assert.notEqual(successorToken,oldToken);const unchanged=await aiState();
 release();r=await owner;assert.equal(r.status,409,await r.clone().text());assert.equal(calls.length,before+2);assert.equal(await count(),0);assert.deepEqual(await aiState(),unchanged,'Old owner must not record failure/timing or start fallback');
 assert.equal((await db.prepare("SELECT owner_token FROM cover_letter_generation_leases WHERE user_name='Nastya' AND vacancy_id=1").first()).owner_token,successorToken,'Old finally cannot release new ownership');release();r=await successor;assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).coverLetterMd,'Letter call '+(before+2));await clean();
 // A waiting request can take over an orphaned lease without any process-local Promise.
 const orphanInput=await api.readCoverLetterInput(db,'Nastya',1),orphan=await api.claimCoverLetterGeneration(db,'Nastya',1,orphanInput);assert.ok(orphan);
 joined=await join();assert.equal(waiting.length,0);await db.prepare("UPDATE cover_letter_generation_leases SET expires_at=1 WHERE user_name='Nastya' AND vacancy_id=1").run();await wait();await api.releaseCoverLetterGeneration(db,orphan);assert.equal((await db.prepare('SELECT count(*) n FROM cover_letter_generation_leases').first()).n,1);release();frames=await joined();assert.ok(frames.some(e=>e.type==='done'));assert.equal(await count(),1);await clean();
 // Native timer renews D1 while provider is held >20 seconds. Near-expiry lease must extend.
 before=calls.length;owner=letter();await wait();await db.prepare("UPDATE cover_letter_generation_leases SET expires_at=(CAST(strftime('%s','now') AS INTEGER)*1000)+25000 WHERE user_name='Nastya' AND vacancy_id=1").run();
 const nearExpiry=(await db.prepare('SELECT expires_at FROM cover_letter_generation_leases').first()).expires_at;
 await new Promise(resolve=>setTimeout(resolve,22000));const renewed=(await db.prepare('SELECT expires_at FROM cover_letter_generation_leases').first()).expires_at;assert.ok(renewed>nearExpiry+50000,'Real Worker heartbeat extends lease while inference is pending');
 joined=await join();assert.equal(calls.length,before+1);release();assert.equal((await owner).status,200);assert.ok((await joined()).some(e=>e.type==='done'));assert.equal(calls.length,before+1);assert.ok((await db.prepare("SELECT last_duration_ms FROM openrouter_model_metrics WHERE model_id='synthetic/first'").first()).last_duration_ms>=20000);await clean();
 // Current manual personal letter completes both owner and joiner, never overwritten.
 before=calls.length;mode='fail';owner=letter();await wait();joined=await join();await db.prepare("INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES('Nastya',1,'Manual','MANUAL PERSONAL')").run();
 frames=await joined();assert.equal(frames.find(e=>e.type==='done').letter.coverLetterMd,'MANUAL PERSONAL');release();r=await owner;assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).coverLetterMd,'MANUAL PERSONAL');assert.equal(calls.length,before+1);await clean();
 // Fence at final SQL, after successful inference: expired ownership cannot INSERT.
 mode='success';before=calls.length;const globalFetch=fetch;
 try{
  globalThis.fetch=async(url,o)=>{const promise=provider(url,o?.body);if(url.endsWith('/chat/completions'))release();return promise;};
  let hits=0;const wrapped={batch:db.batch.bind(db),prepare(sql){const statement=db.prepare(sql);if(!/^INSERT(?: OR IGNORE)? INTO vacancy_cover_letters/.test(sql))return statement;let bound=statement;return{bind(...args){bound=statement.bind(...args);return this},async run(){hits++;await db.prepare('UPDATE cover_letter_generation_leases SET expires_at=1').run();return bound.run()}};}};
  await assert.rejects(()=>api.vacancyCoverLetter(wrapped,{DB:wrapped,SESSION_SECRET:'synthetic-secret'},'Nastya',1),e=>e.code==='generation_lease_lost'&&e.status===409);assert.equal(hits,1);assert.equal(await count(),0);assert.equal(calls.length,before+1);await clean();
 }finally{globalThis.fetch=globalFetch;}
 console.log('Two native Workers/D1 S06 recovery: joined failure no retry storm, explicit retry, expiry takeover, old owner no fallback/metadata/write/release, orphan recovery, real >20s heartbeat, manual priority and execution-time SQL fence passed.');

}finally{for(const release of waiting.splice(0))release();await mf.dispose();}
