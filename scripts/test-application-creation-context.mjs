import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const secret='synthetic-secret',worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'creation-context'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',DEMO_PASSWORD:'synthetic',SESSION_SECRET:secret},outboundService:()=>new Response('Disabled',{status:503})};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'},{...worker,name:'no-db',d1Databases:{}}]};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB','first'),workers={first:await mf.getWorker('first'),second:await mf.getWorker('second'),noDb:await mf.getWorker('no-db')};
 await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 const cookies={};for(const name of ['Eugene','Nastya','Demo']){const response=await workers.first.fetch('https://context.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(response.status,303);cookies[name]=response.headers.get('set-cookie').split(';')[0];}
 const req=(user='Nastya',workerName='first',method='GET',query='')=>workers[workerName].fetch('https://context.test/api/applications/create-context'+query,{method,headers:{cookie:cookies[user]||''}});
 assert.equal((await req('unknown')).status,401);assert.equal((await req('Nastya','first','POST')).status,405);assert.equal((await req('Nastya','noDb')).status,503);
 const before=(await api.createBackup(db)).storage;
 const first=await req();assert.equal(first.status,200);assert.match(first.headers.get('cache-control'),/no-store/);const context=await first.json();assert.deepEqual(Object.keys(context),['creationContext']);assert.match(context.creationContext,/^ac1\.[A-Za-z0-9_-]{43}$/);
 const generation=await api.ensureRuntimeGeneration(db);assert.ok(!context.creationContext.includes(generation));assert.ok(!context.creationContext.includes('Nastya'));assert.ok(!context.creationContext.includes(secret));assert.equal(await api.verifyApplicationCreationContext(db,'Nastya',secret,context.creationContext),generation);
 assert.deepEqual(await(await req('Nastya','second')).json(),context,'Two actual Workers share the same live generation');assert.deepEqual(await(await req('Nastya','first','GET','?userName=Eugene')).json(),context,'Identity comes only from the verified session');
 for(const name of ['Eugene','Demo']){const other=await(await req(name)).json();assert.notEqual(other.creationContext,context.creationContext);await assert.rejects(()=>api.verifyApplicationCreationContext(db,name,secret,context.creationContext),e=>e.status===409);assert.equal(await api.verifyApplicationCreationContext(db,name,secret,other.creationContext),generation);}
 assert.deepEqual((await api.createBackup(db)).storage,before);assert.equal((await db.prepare('SELECT count(*) n FROM application_creation_receipts').first()).n,0);
 const verify=token=>api.verifyApplicationCreationContext(db,'Nastya',secret,token);
 for(const missing of [undefined,null,''])await assert.rejects(()=>verify(missing),e=>e.status===428);
 for(const bad of [17,{},'bad',context.creationContext.replace('ac1.','ac2.'),context.creationContext+'=', 'ac1.'+'a'.repeat(10000)])await assert.rejects(()=>verify(bad),e=>e.status===400);
 const sig=context.creationContext.slice(4),alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';const noncanonical='ac1.'+sig.slice(0,-1)+alphabet[alphabet.indexOf(sig.at(-1))^1];await assert.rejects(()=>verify(noncanonical),e=>e.status===400);
 await assert.rejects(()=>verify('ac1.'+(sig[0]==='a'?'b':'a')+sig.slice(1)),e=>e.status===409);await assert.rejects(()=>api.verifyApplicationCreationContext(db,'Nastya','other-secret',context.creationContext),e=>e.status===409);
 const sessionDomain='ac1.'+await api.sign(secret,'v2.synthetic-session-payload');await assert.rejects(()=>verify(sessionDomain),e=>e.status===409);await assert.rejects(()=>api.issueApplicationCreationContext(db,'Nastya',''),e=>e.status===503);
 const backup=await api.createBackup(db);await api.restoreBackup(db,backup);await assert.rejects(()=>verify(context.creationContext),e=>e.status===409&&e.code==='creation_context_expired');let current=await api.issueApplicationCreationContext(db,'Nastya',secret);assert.notEqual(current.creationContext,context.creationContext);assert.equal(await verify(current.creationContext),await api.ensureRuntimeGeneration(db));
 await api.restoreBackup(db,backup);await assert.rejects(()=>verify(current.creationContext),e=>e.status===409);current=await api.issueApplicationCreationContext(db,'Nastya',secret);
 // Native D1 forced restore immediately before final guard, after crypto work.
 function proxy(hook){const originals=new WeakMap();function wrap(native,sql){const statement={bind(...args){return wrap(native.bind(...args),sql);},first(...args){return native.first(...args);},all(){return native.all();},run(){return native.run();}};originals.set(statement,{native,sql});return statement;}return {prepare(sql){return wrap(db.prepare(sql),sql);},async batch(statements){if(statements.some(statement=>originals.get(statement).sql==='SELECT 1 AS creation_context_ready'))await hook();return db.batch(statements.map(statement=>originals.get(statement).native));}};}
 let restores=0;const during=proxy(async()=>{restores++;await api.restoreBackup(db,backup);});await assert.rejects(()=>api.issueApplicationCreationContext(during,'Nastya',secret),e=>e.status===409&&e.code==='restored');current=await api.issueApplicationCreationContext(db,'Nastya',secret);await assert.rejects(()=>api.verifyApplicationCreationContext(during,'Nastya',secret,current.creationContext),e=>e.status===409&&e.code==='restored');assert.equal(restores,2);
 // Failed restore must leave the previously issued context valid.
 current=await api.issueApplicationCreationContext(db,'Nastya',secret);await db.prepare("CREATE TRIGGER fail_context_restore BEFORE UPDATE ON app_runtime_generation BEGIN SELECT RAISE(ABORT,'Synthetic failure'); END").run();const failed=proxy(async()=>{await assert.rejects(()=>api.restoreBackup(db,backup),/Synthetic failure/);});assert.deepEqual(await api.issueApplicationCreationContext(failed,'Nastya',secret),current);assert.equal(await api.verifyApplicationCreationContext(failed,'Nastya',secret,current.creationContext),await api.ensureRuntimeGeneration(db));await db.prepare('DROP TRIGGER fail_context_restore').run();
 assert.deepEqual((await api.createBackup(db)).storage,backup.storage);assert.equal((await db.prepare('SELECT count(*) n FROM application_creation_receipts').first()).n,0);
 console.log('Real authenticated HTTP/two workerd Workers + native D1: no-store opaque user-bound HMAC context, query identity ignored, auth/method/storage errors, canonical/malformed/forged/cross-user/secret/domain rejection, identical/repeated restore and final guard races, failed-restore validity and no application data writes passed. Production POST not wired yet.');
}finally{await mf.dispose();}
