import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const bindings={SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'};
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'creation-http'},bindings,outboundService:()=>new Response('Disabled',{status:503})};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'}]},mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
const base='https://creation-http.test',path='/api/applications/create';
try{
 const db=await mf.getD1Database('DB','first'),workers=[await mf.getWorker('first'),await mf.getWorker('second')];await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8')),catalog={...seed,companies:['Synthetic','Limited'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[]};await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify(catalog)).run();
 const cookies={};for(const name of ['Eugene','Nastya']){const response=await workers[0].fetch(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(response.status,303);cookies[name]=response.headers.get('set-cookie').split(';')[0];}
 const req=(index,method='POST',body,user='Nastya',route=path)=>workers[index].fetch(base+route,{method,headers:{cookie:cookies[user]||'','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 const context=async(user='Nastya')=>(await(await req(0,'GET',undefined,user,'/api/applications/create-context')).json()).creationContext;
 let creationContext=await context();const intent=()=>({creationContext,idempotencyKey:crypto.randomUUID()});
 const input={companyName:'Synthetic',status:'Откликнулся',comment:'Synthetic',applicationSources:['hh','hcm']};
 const snapshot=async()=>Promise.all(['company_applications','company_application_status_history','application_creation_receipts'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 assert.equal((await req(0,'POST',input,'unknown')).status,401);assert.equal((await req(0,'GET')).status,405);
 assert.equal((await mf.dispatchFetch(base+path,{method:'POST',headers:{cookie:cookies.Nastya,origin:'https://foreign.test','content-type':'application/json'},body:JSON.stringify({...input,...intent()})})).status,403);
 assert.equal((await mf.dispatchFetch(base+path,{method:'POST',headers:{cookie:cookies.Nastya,origin:base,'content-type':'application/json'},body:'{'})).status,400);
 let before=await snapshot();for(const payload of [input,{...input,creationContext},{...input,idempotencyKey:crypto.randomUUID()}])assert.equal((await req(0,'POST',payload)).status,428);assert.deepEqual(await snapshot(),before);
 // The legacy URL is a strict alias, never an unsafe fallback for old clients.
 for(const payload of [input,{...input,creationContext},{...input,idempotencyKey:crypto.randomUUID()}])assert.equal((await req(0,'POST',payload,'Nastya','/api/applications')).status,428);
 assert.equal((await req(0,'POST',input,'unknown','/api/applications')).status,401);
 assert.equal((await req(0,'PATCH',input,'Nastya','/api/applications')).status,405);
 assert.equal((await mf.dispatchFetch(base+'/api/applications',{method:'POST',headers:{cookie:cookies.Nastya,origin:'https://foreign.test','content-type':'application/json'},body:JSON.stringify({...input,...intent()})})).status,403);
 assert.equal((await mf.dispatchFetch(base+'/api/applications',{method:'POST',headers:{cookie:cookies.Nastya,origin:base,'content-type':'application/json'},body:'{'})).status,400);
 assert.equal((await req(0,'POST',{...input,...intent(),idempotencyKey:['00000000-0000-4000-8000-000000000000']},'Nastya','/api/applications')).status,400);
 assert.deepEqual(await snapshot(),before,'Rejected legacy requests must not create applications, history or receipts');
 for(const payload of [{...input,...intent(),idempotencyKey:['00000000-0000-4000-8000-000000000000']},{...input,...intent(),applicationSources:[]},{...input,...intent(),recruiterEmail:'bad'},{...input,...intent(),status:'Invalid'},{...input,...intent(),vacancyId:1.2}])assert.equal((await req(0,'POST',payload)).status,400);assert.deepEqual(await snapshot(),before);
 const original={...input,...intent(),userName:'Eugene'};let response=await mf.dispatchFetch(base+path,{method:'POST',headers:{cookie:cookies.Nastya,origin:base,'content-type':'application/json'},body:JSON.stringify(original)});assert.equal(response.status,201,await response.clone().text());const created=await response.json();assert.equal(created.replayed,false);assert.equal(created.records.length,1);assert.equal(created.createdApplicationId,created.records[0].id);assert.equal(created.limit,10);assert.ok(Array.isArray(created.vacancies));assert.equal(typeof created.favorite,'boolean');assert.ok(!Object.hasOwn(created,'generation'));
 assert.equal((await db.prepare('SELECT user_name FROM company_applications WHERE id=?').bind(created.createdApplicationId).first()).user_name,'Nastya');
 // Successful server response is dropped by a client; another actual Worker retries.
 response=await req(1,'POST',original);assert.equal(response.status,200);const repeated=await response.json();assert.equal(repeated.replayed,true);assert.equal(repeated.createdApplicationId,created.createdApplicationId);assert.deepEqual((await snapshot()).map(rows=>rows.length),[1,1,1]);
 response=await req(1,'POST',original,'Nastya','/api/applications');assert.equal(response.status,200);assert.deepEqual(await response.json(),repeated);
 before=await snapshot();assert.equal((await req(0,'POST',{...original,comment:'Changed'})).status,409);assert.deepEqual(await snapshot(),before);
 const second={...input,...intent()},race=await Promise.all([req(0,'POST',second,'Nastya','/api/applications'),req(1,'POST',second)]);assert.deepEqual(race.map(r=>r.status).sort(),[200,201]);const raceData=await Promise.all(race.map(r=>r.json()));assert.equal(raceData[0].createdApplicationId,raceData[1].createdApplicationId);
 const eugeneContext=await context('Eugene');before=await snapshot();assert.equal((await req(0,'POST',{...input,...intent(),creationContext:eugeneContext})).status,409);assert.deepEqual(await snapshot(),before);
 response=await req(0,'POST',{...original,creationContext:eugeneContext,comment:'Other user private'},'Eugene');assert.equal(response.status,201);assert.ok((await response.json()).records.every(record=>record.comment==='Other user private'));assert.ok(!(await(await req(0,'POST',original)).text()).includes('Other user private'));
 // A replay is valid after the company disappears from the mutable catalog.
 await db.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').bind(JSON.stringify({...catalog,companies:catalog.companies.filter(company=>company.name!=='Synthetic')})).run();assert.equal((await req(0,'POST',original)).status,200);assert.equal((await req(0,'POST',{...input,...intent()})).status,404);await db.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').bind(JSON.stringify(catalog)).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Limited')").run();await db.batch(Array.from({length:9},()=>db.prepare("INSERT INTO company_applications(user_name,company_name,status) VALUES('Nastya','Limited','')")));
 const tenth={...input,...intent(),companyName:'Limited'};assert.equal((await req(0,'POST',tenth)).status,201);assert.equal((await req(1,'POST',tenth)).status,200);assert.equal((await req(0,'POST',{...tenth,...intent()})).status,400);
 await db.prepare('DELETE FROM company_applications WHERE id=?').bind(created.createdApplicationId).run();before=await snapshot();assert.equal((await req(0,'POST',original)).status,410);assert.deepEqual(await snapshot(),before);
 assert.equal((await req(1,'POST',original,'Nastya','/api/applications')).status,410);assert.deepEqual(await snapshot(),before);
 // Native D1 fault injection through the same real HTTP handler in Node.
 function proxy(hook){const entries=new WeakMap();function wrap(native,sql){const statement={bind(...args){return wrap(native.bind(...args),sql);},first(...args){return native.first(...args);},all(){return native.all();},run(){return native.run();}};entries.set(statement,{native,sql});return statement;}return {prepare(sql){return wrap(db.prepare(sql),sql);},async batch(statements){return hook(statements.map(statement=>entries.get(statement).sql),()=>db.batch(statements.map(statement=>entries.get(statement).native)));}};}
 const nodeReq=(connection,payload)=>api.default.fetch(new Request(base+path,{method:'POST',headers:{cookie:cookies.Nastya,origin:base,'content-type':'application/json'},body:JSON.stringify(payload)}),{...bindings,DB:connection});
 let committed=false,failRead=true;const readFailure=proxy(async(sql,run)=>{if(committed&&failRead&&sql.some(s=>s.includes('FROM company_applications AS application')&&s.startsWith('SELECT application.id'))){failRead=false;throw Error('Synthetic response read failure');}const result=await run();if(sql.some(s=>s.startsWith('INSERT INTO application_creation_receipts')))committed=true;return result;});
 const readIntent={...input,...intent()};assert.equal((await nodeReq(readFailure,readIntent)).status,503);assert.ok(committed);before=await snapshot();response=await req(1,'POST',readIntent);assert.equal(response.status,200);assert.equal((await response.json()).replayed,true);assert.deepEqual(await snapshot(),before);
 const backup=await api.createBackup(db);for(const when of ['before','after']){const payload={...input,...intent()};let restored=false;const connection=proxy(async(sql,run)=>{const creates=sql.some(s=>s.startsWith('INSERT INTO application_creation_receipts'));if(creates&&when==='before'){await api.restoreBackup(db,backup);restored=true;}const result=await run();if(creates&&when==='after'){await api.restoreBackup(db,backup);restored=true;}return result;});assert.equal((await nodeReq(connection,payload)).status,409);assert.ok(restored);assert.deepEqual((await api.createBackup(db)).storage,backup.storage);creationContext=await context();}
 const stale={...input,...intent()};await api.restoreBackup(db,backup);before=await snapshot();assert.equal((await req(0,'POST',stale)).status,409);assert.deepEqual(await snapshot(),before);
 assert.equal((await req(1,'POST',stale,'Nastya','/api/applications')).status,409);assert.deepEqual(await snapshot(),before);
 console.log('Protected production HTTP + strict legacy alias, two actual Workers/native D1: auth/Origin, missing intent428/no writes, compatible201/200 and cross-URL parallel replay, user privacy, mutable catalog, tenth slot, deleted410, response-read failure and restore409 passed. Separate vacancy /apply remains to be protected.');
}finally{await mf.dispose();}
