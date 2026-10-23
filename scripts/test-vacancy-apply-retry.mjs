// Regression promoted from the P1 diagnostic: old shortcut cannot bypass receipts.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();
const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'apply-retry-audit'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:()=>new Response('Disabled',{status:503})};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'}]};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
const base='https://apply-retry-audit.test';
try {
 const db=await mf.getD1Database('DB','first'),workers=[await mf.getWorker('first'),await mf.getWorker('second')];
 await db.batch(buildBackupSchema(process.cwd()).objects.map(object=>db.prepare(object.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title) VALUES(1,'Synthetic','https://synthetic.invalid/vacancy','Synthetic')").run();
 const login=await workers[0].fetch(base+'/auth',{method:'POST',body:new URLSearchParams({login:'Nastya',password:'synthetic'}),redirect:'manual'});
 assert.equal(login.status,303);const cookie=login.headers.get('set-cookie').split(';')[0];
 const req=(index,path,method='POST',body)=>workers[index].fetch(base+path,{method,headers:{cookie,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 const counts=async()=>Promise.all(['company_applications','company_application_status_history','application_creation_receipts'].map(async table=>(await db.prepare('SELECT count(*) n FROM '+table).first()).n));
 assert.equal((await req(0,'/api/vacancies/1/apply')).status,428);assert.deepEqual(await counts(),[0,0,0]);
 const context=async()=>(await(await req(0,'/api/applications/create-context','GET')).json()).creationContext;
 const input={companyName:'Synthetic',idempotencyKey:crypto.randomUUID(),creationContext:await context()};
 const backup=await api.createBackup(db);
 let response=await req(0,'/api/vacancies/1/apply','POST',input);assert.equal(response.status,200);const first=(await response.json()).vacancy.applicationId;
 response=await req(1,'/api/vacancies/1/apply/create','POST',input);assert.equal(response.status,200);assert.equal((await response.json()).vacancy.applicationId,first);assert.deepEqual(await counts(),[1,1,1]);
 assert.equal((await req(0,'/api/applications/'+first,'DELETE')).status,200);assert.deepEqual(await counts(),[0,0,1]);
 assert.equal((await req(1,'/api/vacancies/1/apply','POST',input)).status,410);assert.deepEqual(await counts(),[0,0,1]);
 await api.restoreBackup(db,backup);assert.deepEqual(await counts(),[0,0,1]);
 assert.equal((await req(0,'/api/vacancies/1/apply','POST',input)).status,409);assert.deepEqual(await counts(),[0,0,1]);
 const fresh={...input,idempotencyKey:crypto.randomUUID(),creationContext:await context()};response=await req(1,'/api/vacancies/1/apply','POST',fresh);assert.equal(response.status,200);const next=(await response.json()).vacancy.applicationId;assert.equal(next,first,'Restore can reuse IDs, so the generation guard is essential');assert.deepEqual(await counts(),[1,1,2]);
 assert.equal((await req(0,'/api/vacancies/1/apply','POST',input)).status,409);assert.deepEqual(await counts(),[1,1,2]);
 response=await req(0,'/api/vacancies/1/apply','POST',fresh);assert.equal(response.status,200);assert.equal((await response.json()).createdApplicationId,next);assert.deepEqual(await counts(),[1,1,2]);
 console.log('S03c former diagnostic regression: legacy shortcut missing428, cross-URL replay, delete410/no recreation, restore409/no writes, reused ID isolation and fresh intent/replay passed on native D1/two Workers.');

} finally {await mf.dispose();}
