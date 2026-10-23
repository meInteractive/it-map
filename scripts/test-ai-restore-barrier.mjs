import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const oldFetch=fetch,waiting=[],calls=[];let hold='',mode='success';
const models=['synthetic/first','synthetic/next'].map(id=>({id,name:id,context_length:262144,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:0,completion:0}}));
async function provider(url,body){
 if(url.includes('/models?')){if(hold==='models')await new Promise(resolve=>waiting.push(resolve));return Response.json({data:models});}
 if(url.includes('/benchmarks?'))return Response.json({data:[]});
 if(url.endsWith('/key')){if(hold==='key')await new Promise(resolve=>waiting.push(resolve));return Response.json({data:{}});}
 if(url.endsWith('/chat/completions')){const request=JSON.parse(body);calls.push(request);if(hold==='model')await new Promise(resolve=>waiting.push(resolve));if(mode==='error')return Response.json({error:{message:'Synthetic provider failure'}},{status:503});return Response.json({model:request.model,choices:[{message:{content:JSON.stringify({subject:'Synthetic subject',markdown:'Synthetic letter'})}}]});}
 throw new Error('Unexpected synthetic provider request');
}
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'restore-barrier'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:async request=>provider(request.url,await request.text())};
const options={cf:false,workers:[{...worker,name:'candidate'},{...worker,name:'admin'}]};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB','candidate'),workers={candidate:await mf.getWorker('candidate'),admin:await mf.getWorker('admin')};
 await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));await api.ensureRuntimeGeneration(db);
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(1,'Synthetic','https://restore.test/1','Initial','Body')").run();
 const cookies={};for(const name of ['Eugene','Nastya']){const r=await workers.candidate.fetch('https://restore.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(path,method='GET',body,user='Eugene',workerName='candidate',extra={})=>workers[workerName].fetch('https://restore.test'+path,{method,headers:{cookie:cookies[user],'content-type':'application/json',...extra},...(body===undefined?{}:{body:JSON.stringify(body)})});
 assert.equal((await req('/api/profile/resume','PUT',{resumeMd:'CV',revision:0},'Nastya')).status,200);assert.equal((await req('/api/profile/prompts/cover_letter','PUT',{markdown:'Prompt',revision:0})).status,200);assert.equal((await req('/api/openrouter/key','PUT',{apiKey:'FAKE_TEST_TOKEN_8219f7325f23'})).status,200);
 const backup=async()=>{const r=await req('/api/backup');assert.equal(r.status,200,await r.clone().text());return r.json();};
 const restore=file=>req('/api/backup','POST',file,'Eugene','admin');
 const letter=(name='candidate',stream=false)=>req('/api/vacancies/1/cover-letter','POST',undefined,'Nastya',name,stream?{accept:'application/x-ndjson'}:{});
 const epoch=async()=>(await db.prepare('SELECT generation FROM app_runtime_generation WHERE id=1').first()).generation;
 const wait=async(count=1)=>{for(let i=0;i<2000&&waiting.length<count;i++)await new Promise(r=>setTimeout(r,1));assert.equal(waiting.length,count);};
 const release=()=>{assert.ok(waiting.length);waiting.shift()();};
 // Different Worker handles restore while candidate is blocked in the model.
 const file=await backup(),beforeEpoch=await epoch();hold='model';let running=letter();await wait();assert.equal((await restore(file)).status,200);assert.notEqual(await epoch(),beforeEpoch);release();let r=await running;assert.equal(r.status,409,await r.clone().text());assert.match((await r.json()).error,/База восстановлена/);assert.deepEqual((await backup()).storage,file.storage,'No stale letter, settings, timing or failure-log writes');
 // Both independent Worker instances must stop, regardless of provider outcome.
 const pairCalls=calls.length;let a=letter('candidate');await wait();
 const b=await letter('admin',true),joinedReader=b.body.getReader();let joined='';
 while(!joined.includes('\"stage\":\"waiting\"')){const part=await joinedReader.read();assert.ok(!part.done);joined+=new TextDecoder().decode(part.value);}
 assert.equal(calls.length,pairCalls+1,'Joined second Worker must not call provider');
 assert.equal((await restore(file)).status,200);release();assert.equal((await a).status,409);
 for(;;){const part=await joinedReader.read();if(part.done)break;joined+=new TextDecoder().decode(part.value);}
 assert.match(joined,/База восстановлена/);assert.match(joined,/"type":"error"/);assert.ok(!joined.includes('"type":"done"'));assert.deepEqual((await backup()).storage,file.storage);
 mode='error';const callsBefore=calls.length;running=letter();await wait();assert.equal((await restore(file)).status,200);release();assert.equal((await running).status,409);assert.equal(calls.length,callsBefore+1,'Restore must stop fallback, not call next model');assert.deepEqual((await backup()).storage,file.storage);mode='success';
 // Stream gets error (no done); application tables still match restored content.
 r=await letter('candidate',true);const reader=r.body.getReader();await reader.read();await wait();assert.equal((await restore(file)).status,200);release();let chunks='';for(;;){const part=await reader.read();if(part.done)break;chunks+=new TextDecoder().decode(part.value);}assert.match(chunks,/"type":"error"/);assert.ok(!chunks.includes('"type":"done"'));assert.deepEqual((await backup()).storage,file.storage);
 // Failed restore at its final generation write must NOT invalidate the job.
 running=letter();await wait();const unchangedEpoch=await epoch();await db.prepare("CREATE TRIGGER fail_restore_epoch BEFORE UPDATE ON app_runtime_generation BEGIN SELECT RAISE(ABORT,'Synthetic failed restore'); END").run();assert.equal((await restore(file)).status,400);assert.equal(await epoch(),unchangedEpoch);await db.prepare('DROP TRIGGER fail_restore_epoch').run();release();r=await running;assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).coverLetterMd,'Synthetic letter');
 // Restore an empty-letter snapshot before the next case.
 hold='';assert.equal((await restore(file)).status,200);
 // Restore while model catalogue sync is awaiting network: no cache/error writes.
 await db.prepare("UPDATE openrouter_settings SET models_synced_at=NULL").run();const staleCacheFile=await backup();hold='models';running=letter();await wait();assert.equal((await restore(staleCacheFile)).status,200);release();assert.equal((await running).status,409);assert.deepEqual((await backup()).storage,staleCacheFile.storage);hold='';assert.equal((await restore(file)).status,200);
 // Administrative refresh/check also await network and must share the barrier.
 for(const [action,kind] of [['refresh','models'],['check','key']]){
  hold=kind;running=req('/api/openrouter/'+action,'POST');await wait();assert.equal((await restore(file)).status,200);release();const r=await running;assert.equal(r.status,409,await r.clone().text());assert.deepEqual((await backup()).storage,file.storage);
 }
 hold='';
 // Execution-time races after runAI returns; use actual native D1 and real restore.
 globalThis.fetch=async(url,options)=>provider(url,options?.body);
 for(const afterInsert of [false,true]){
  let hit=0;const wrapped={batch:db.batch.bind(db),prepare(sql){const statement=db.prepare(sql);if(!/^INSERT(?: OR IGNORE)? INTO vacancy_cover_letters/.test(sql))return statement;let bound=statement;return {bind(...args){bound=statement.bind(...args);return this;},async run(){hit++;if(afterInsert){const result=await bound.run();await api.restoreBackup(db,file);return result;}await api.restoreBackup(db,file);return bound.run();}};}};
  await assert.rejects(()=>api.vacancyCoverLetter(wrapped,{DB:wrapped,SESSION_SECRET:'synthetic-secret'},'Nastya',1),error=>error.code==='restored'&&error.status===409);assert.equal(hit,1);assert.deepEqual((await backup()).storage,file.storage);
 }
 // Restored manual personal text must not be returned as this old job's success.
 const withManual=structuredClone(file);withManual.storage.tables.find(t=>t.name==='vacancy_cover_letters').rows.push({user_name:'Nastya',vacancy_id:1,subject:'Restored manual',markdown:'Restored manual',created_at:'2026-10-05',override_shared:0,revision:0});withManual.manifest.find(t=>t.name==='vacancy_cover_letters').rows=1;withManual.integrity.digest=await api.backupDigest(withManual);
 let hit=0;const wrapped={batch:db.batch.bind(db),prepare(sql){const statement=db.prepare(sql);if(!/^INSERT(?: OR IGNORE)? INTO vacancy_cover_letters/.test(sql))return statement;let bound=statement;return {bind(...args){bound=statement.bind(...args);return this;},async run(){hit++;await api.restoreBackup(db,withManual);return bound.run();}};}};
 await assert.rejects(()=>api.vacancyCoverLetter(wrapped,{DB:wrapped,SESSION_SECRET:'synthetic-secret'},'Nastya',1),error=>error.code==='restored');assert.equal(hit,1);assert.equal((await db.prepare('SELECT markdown FROM vacancy_cover_letters WHERE vacancy_id=1').first()).markdown,'Restored manual');
 console.log('Real independent workerd Workers + D1: identical restore stops letter/settings/logs/timing/bootstrap/sync, no fallback or done, both instances stop, failed restore permits completion, before/after INSERT races and restored manual priority passed.');
}finally{hold='';for(const resolve of waiting.splice(0))resolve();globalThis.fetch=oldFetch;await mf.dispose();}
