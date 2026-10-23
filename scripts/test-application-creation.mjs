// Primitive integration: actual two workerd isolates + shared native D1.
// Test-only HTTP adapter; production /api/applications is not wired yet.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {build} from 'esbuild';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
await build({stdin:{contents:`import {createApplicationWithReceipt} from './server/repositories/application-creation.js';
 export default {async fetch(request,env){const {user,input,intent}=await request.json();try{return Response.json(await createApplicationWithReceipt(env.DB,user,input,intent));}catch(e){return Response.json({error:e.message,code:e.code},{status:e.status||500});}}};`,resolveDir:process.cwd()},bundle:true,format:'esm',platform:'browser',outfile:'.tests/receipt-worker.mjs',logLevel:'silent'});
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/receipt-worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'creation-once'},outboundService:()=>new Response('Disabled',{status:503})};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'}]};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB','first'),workers=[await mf.getWorker('first'),await mf.getWorker('second')];
 await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:['Synthetic','Limited','Race','Failed','Other'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[]})).run();
 const generation=await api.ensureRuntimeGeneration(db),fresh=()=>({generation,idempotencyKey:crypto.randomUUID()});
 const input={companyName:'Synthetic',status:'Откликнулся',comment:'Synthetic',vacancyId:null,recruiterName:' Recruiter ',applicationSources:['hh','hcm']};
 const create=(intent,body=input,user='Nastya',connection=db)=>api.createApplicationWithReceipt(connection,user,body,intent);
 const snapshot=async()=>Promise.all(['company_applications','company_application_status_history','application_creation_receipts'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 const send=async(workerIndex,intent,body=input,user='Nastya')=>workers[workerIndex].fetch('https://primitive.test/create',{method:'POST',body:JSON.stringify({user,input:body,intent})});
 const key=fresh();let r=await send(0,key);assert.equal(r.status,200,await r.clone().text());const first=await r.json();assert.equal(first.replayed,false);
 // Simulate lost delivery after a fully committed primitive result.
 r=await send(1,key);assert.equal(r.status,200);assert.deepEqual(await r.json(),{applicationId:first.applicationId,replayed:true});let state=await snapshot();assert.deepEqual(state.map(rows=>rows.length),[1,1,1]);assert.equal(state[1][0].application_id,first.applicationId);
 assert.equal(state[0][0].recruiter_name,'Recruiter');assert.deepEqual(JSON.parse(state[0][0].application_sources),['hcm','hh']);
 const normalized={...input,recruiterName:'Recruiter',applicationSources:['hcm','hh','hcm']};assert.equal((await create(key,normalized)).applicationId,first.applicationId);
 let before=await snapshot();await assert.rejects(()=>create(key,{...input,comment:'Changed'}),e=>e.status===409&&e.code==='intent_conflict');assert.deepEqual(await snapshot(),before);
 assert.notEqual((await create(fresh())).applicationId,first.applicationId,'Separate intentional identical applications remain allowed');
 assert.notEqual((await create(key,input,'Eugene')).applicationId,first.applicationId,'Other user has isolated key namespace');
 const empty=await create(fresh(),{...input,status:''});assert.equal((await db.prepare('SELECT count(*) n FROM company_application_status_history WHERE application_id=?').bind(empty.applicationId).first()).n,0);
 // Concurrent same key across independent Workers: one creation, one replay.
 const concurrent=fresh(),parallel=await Promise.all([send(0,concurrent),send(1,concurrent)]);for(const response of parallel)assert.equal(response.status,200,await response.clone().text());const out=await Promise.all(parallel.map(response=>response.json()));assert.equal(out[0].applicationId,out[1].applicationId);assert.deepEqual(out.map(v=>v.replayed).sort(),[false,true]);
 const conflicting=fresh(),conflict=await Promise.all([send(0,conflicting),send(1,conflicting,{...input,comment:'Different intent payload'})]);assert.deepEqual(conflict.map(response=>response.status).sort(),[200,409]);
 // Replay precedes the max10 check, including a winner after the initial receipt read.
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Limited'),('Race')").run();
 for(const company of ['Limited','Race'])await db.batch(Array.from({length:9},()=>db.prepare("INSERT INTO company_applications(user_name,company_name,status) VALUES('Nastya',?,'')").bind(company)));
 const limited={...input,companyName:'Limited'},tenth=fresh(),tenthResult=await create(tenth,limited);assert.equal((await create(tenth,limited)).applicationId,tenthResult.applicationId);await assert.rejects(()=>create(fresh(),limited),/10 откликов/);
 function proxy(hook){
  const entries=new WeakMap();
  function wrap(original,sql){const statement={bind(...args){return wrap(original.bind(...args),sql);},first(...args){return original.first(...args);},all(){return original.all();},run(){return original.run();}};entries.set(statement,{original,sql});return statement;}
  return {prepare(sql){return wrap(db.prepare(sql),sql);},async batch(statements){return hook(statements,()=>db.batch(statements.map(statement=>entries.get(statement).original)),statements.map(statement=>entries.get(statement).sql));}};
 }
 const racing=fresh(),raceBody={...input,companyName:'Race'};let raced=false,winner;
 const countRace=proxy(async(statements,run,sql)=>{const result=await run();if(!raced&&sql.some(s=>s.startsWith('SELECT user_name AS userName'))){raced=true;winner=await create(racing,raceBody);}return result;});
 const raceResult=await create(racing,raceBody,'Nastya',countRace);assert.ok(raced);assert.equal(raceResult.applicationId,winner.applicationId);
 // Different intent keys retain the existing user/vacancy unique rule.
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Other')").run();
 await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title) VALUES(500,'Other','https://primitive.test/vacancy','Synthetic')").run();
 const linked={...input,companyName:'Other',vacancyId:500},linkedBefore=await snapshot();
 const linkedRace=await Promise.all([send(0,fresh(),linked),send(1,fresh(),linked)]);assert.equal(linkedRace.filter(response=>response.ok).length,1);state=await snapshot();assert.deepEqual(state.map((rows,i)=>rows.length-linkedBefore[i].length),[1,1,1]);
 await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title) VALUES(501,'Other','https://primitive.test/moved','Synthetic')").run();
 let moved=false;const moveRace=proxy(async(statements,run,sql)=>{if(sql.some(s=>s.startsWith('INSERT INTO application_creation_receipts'))){await db.prepare("UPDATE company_vacancies SET company_name='Synthetic' WHERE id=501").run();moved=true;}return run();});
 before=await snapshot();await assert.rejects(()=>create(fresh(),{...linked,vacancyId:501},'Nastya',moveRace));assert.ok(moved);assert.deepEqual(await snapshot(),before);
 // Every meaningful late write failure rolls the claim, application and history back.
 const failedBody={...input,companyName:'Failed'};
 for(const trigger of [
  "CREATE TRIGGER fail_creation BEFORE INSERT ON company_applications BEGIN SELECT RAISE(ABORT,'Synthetic app failure'); END",
  "CREATE TRIGGER fail_creation BEFORE UPDATE ON application_creation_receipts BEGIN SELECT RAISE(ABORT,'Synthetic receipt failure'); END",
  "CREATE TRIGGER fail_creation BEFORE INSERT ON company_application_status_history BEGIN SELECT RAISE(ABORT,'Synthetic history failure'); END",
  "CREATE TRIGGER fail_creation BEFORE INSERT ON company_application_status_history BEGIN SELECT abs(-9223372036854775808); END",
  "CREATE TRIGGER fail_creation BEFORE INSERT ON company_application_status_history BEGIN SELECT RAISE(IGNORE); END",
 ]){const failedKey=fresh();before=await snapshot();await db.prepare(trigger).run();await assert.rejects(()=>create(failedKey,failedBody),e=>!e.status);assert.deepEqual(await snapshot(),before);assert.equal(await api.readApplicationReceipt(db,'Nastya',failedKey.idempotencyKey),null);await db.prepare('DROP TRIGGER fail_creation').run();assert.equal((await create(failedKey,failedBody)).replayed,false);}
 // A dropped receipt completion also aborts the entire batch.
 const dropped=fresh();before=await snapshot();await db.prepare('CREATE TRIGGER drop_receipt_completion BEFORE UPDATE ON application_creation_receipts BEGIN SELECT RAISE(IGNORE); END').run();await assert.rejects(()=>create(dropped,failedBody));assert.deepEqual(await snapshot(),before);await db.prepare('DROP TRIGGER drop_receipt_completion').run();
 // Read error AFTER commit: retry recovers via receipt, never via another INSERT.
 const readKey=fresh();let failingReads=0,committed=false;
 const readFailure=proxy(async(statements,run,sql)=>{if(failingReads&&sql.some(s=>s.startsWith('SELECT user_name AS userName'))){failingReads--;throw Error('Synthetic post-commit read failure');}const result=await run();if(sql.some(s=>s.startsWith('INSERT INTO application_creation_receipts'))){committed=true;failingReads=2;}return result;});
 await assert.rejects(()=>create(readKey,failedBody,'Nastya',readFailure),/post-commit/);assert.ok(committed);const afterCommit=await snapshot();assert.equal((await create(readKey,failedBody)).replayed,true);assert.deepEqual(await snapshot(),afterCommit);
 // Replay never overwrites later edits or recreates a deleted result.
 await db.prepare("UPDATE company_applications SET comment_md='New edit' WHERE id=?").bind(first.applicationId).run();assert.equal((await create(key)).applicationId,first.applicationId);assert.equal((await db.prepare('SELECT comment_md FROM company_applications WHERE id=?').bind(first.applicationId).first()).comment_md,'New edit');await db.prepare('DELETE FROM company_applications WHERE id=?').bind(first.applicationId).run();before=await snapshot();await assert.rejects(()=>create(key),e=>e.status===410);assert.deepEqual(await snapshot(),before);
 before=await snapshot();await assert.rejects(()=>create({}),e=>e.status===428);await assert.rejects(()=>create({...fresh(),idempotencyKey:'bad'}),e=>e.status===400);assert.deepEqual(await snapshot(),before);
 const incomplete=fresh(),digest=await api.applicationCreationHash(api.normalizeApplicationCreation(input));await db.prepare('INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash) VALUES(?,?,?,?)').bind('Nastya',incomplete.idempotencyKey,generation,digest).run();before=await snapshot();await assert.rejects(()=>create(incomplete),e=>e.status===503&&e.code==='incomplete_receipt');assert.deepEqual(await snapshot(),before);
 // Restoration before / at batch / just after commit blocks an old intent.
 const backup=await api.createBackup(db);await api.restoreBackup(db,backup);before=await snapshot();await assert.rejects(()=>create(fresh()),e=>e.status===409&&e.code==='restored');assert.deepEqual(await snapshot(),before);
 let activeGeneration=await api.ensureRuntimeGeneration(db);await assert.rejects(()=>create({...key,generation:activeGeneration}),e=>e.status===409&&e.code==='restored');
 for(const when of ['before','after']){const intent={generation:activeGeneration,idempotencyKey:crypto.randomUUID()};let restored=false;const restoreRace=proxy(async(statements,run,sql)=>{const isCreation=sql.some(s=>s.startsWith('INSERT INTO application_creation_receipts'));if(isCreation&&when==='before'){await api.restoreBackup(db,backup);restored=true;}const result=await run();if(isCreation&&when==='after'){await api.restoreBackup(db,backup);restored=true;}return result;});await assert.rejects(()=>create(intent,failedBody,'Nastya',restoreRace),e=>e.status===409&&e.code==='restored');assert.ok(restored);assert.deepEqual((await api.createBackup(db)).storage,backup.storage);activeGeneration=await api.ensureRuntimeGeneration(db);}
 console.log('Native D1/two workerd Workers: same-key replay/conflict, canonical hash/user/intents, tenth-slot race, every late-write rollback, genuine overflow, post-commit read failure, edited/deleted result, missing intent and restore before/at/after batch passed. Production POST remains separate.');
}finally{await mf.dispose();}
