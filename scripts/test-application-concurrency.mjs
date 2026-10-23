import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend,applicationTestIntent} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();
const backend=await import('../.tests/backend.mjs');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'application-concurrency-synthetic'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'concurrency-synthetic-secret'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try {
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Concurrent A',jobs:[]}],agencies:[]})).run();
 const cookies={};for(const name of ['Eugene','Nastya']) {const r=await mf.dispatchFetch('https://concurrency.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(path,method='GET',body,user='Nastya')=>mf.dispatchFetch('https://concurrency.test'+path,{method,headers:{cookie:cookies[user],origin:'https://concurrency.test','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 const input={companyName:'Concurrent A',status:'Откликнулся',comment:'Initial',applicationSources:['hcm']};
 let response=await req('/api/applications','POST',{...input,...await applicationTestIntent(()=>req('/api/applications/create-context'))});assert.equal(response.status,201,await response.clone().text());
 const original=(await response.json()).records[0],id=original.id,path='/api/applications/'+id;
 assert.match(original.revision,/^[a-f0-9]{64}$/);
 const read=async()=>(await (await req('/api/applications?company=Concurrent%20A')).json()).records.find(r=>r.id===id);
 const snapshot=async()=>Promise.all(['company_applications','company_application_status_history'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY id').all()).results));
 const ownDetail=await req(path);assert.equal(ownDetail.status,200);assert.deepEqual((await ownDetail.json()).record,original);
 assert.equal((await req(path,'GET',undefined,'Eugene')).status,404);
 const before=await snapshot();assert.equal((await req(path,'PUT',input)).status,428);assert.equal((await req(path,'PUT',{...input,revision:1})).status,428);assert.deepEqual(await snapshot(),before);
 assert.equal((await req(path,'PUT',original,'Eugene')).status,404);
 const globalRecords=(await (await req('/api/crm/applications')).json()).records;assert.equal(globalRecords.find(r=>r.id===id).revision,original.revision);
 response=await req(path,'PUT',{...original,status:'Офер'});assert.equal(response.status,200,await response.clone().text());
 const offer=(await response.json()).records.find(r=>r.id===id);assert.notEqual(offer.revision,original.revision);
 const afterOffer=await snapshot();assert.equal((await req(path,'PUT',{...original,comment:'Old draft'})).status,409);assert.deepEqual(await snapshot(),afterOffer);
 // A current client can explicitly reconcile its comment without reverting the offer.
 assert.equal((await req(path,'PUT',{...offer,comment:'Reconciled draft'})).status,200);
 assert.equal((await read()).status,'Офер');
 // Persisted field changes alter the revision even when updated_at is unchanged.
 const baseline=await read();await db.prepare('UPDATE company_applications SET recruiter_phone=? WHERE id=?').bind('New phone',id).run();
 const changed=await read();assert.notEqual(changed.revision,baseline.revision);assert.equal(changed.updatedAt,baseline.updatedAt);
 assert.equal((await req(path,'PUT',{...baseline,comment:'Stale phone'})).status,409);
 // Force a change AFTER the actual repository preflight and before the actual D1
 // batch. This would bypass a hash-only check without the SQL compare-and-swap.
 const barrierBase=await read();const guardedDb={prepare:db.prepare.bind(db),async batch(statements){await db.prepare('UPDATE company_applications SET comment_md=? WHERE id=?').bind('Concurrent winner',id).run();return db.batch(statements);}};
 const historyBefore=(await db.prepare('SELECT * FROM company_application_status_history ORDER BY id').all()).results;
 await assert.rejects(()=>backend.updateCompanyApplication(guardedDb,'Nastya',id,null,'Скрининг','Losing draft',backend.normalizeRecruiterContact(barrierBase),barrierBase.applicationSources,barrierBase.revision),error=>error.status===409);
 assert.equal((await read()).comment,'Concurrent winner');assert.equal((await read()).status,'Офер');
 assert.deepEqual((await db.prepare('SELECT * FROM company_application_status_history ORDER BY id').all()).results,historyBefore,'Losing CAS must not append history');
 // Two real HTTP requests with the same version: exactly one winner.
 const raceBase=await read();const race=await Promise.all(['Скрининг','Собес 2'].map(status=>req(path,'PUT',{...raceBase,status})));
 assert.deepEqual(race.map(r=>r.status).sort(),[200,409]);
 const raceHistory=(await db.prepare('SELECT * FROM company_application_status_history ORDER BY id').all()).results;assert.equal(raceHistory.length,historyBefore.length+1);
 // A history failure must roll the matching UPDATE and its new token back.
 const rollbackBase=await read(),rollbackBefore=await snapshot();
 await db.prepare("CREATE TRIGGER fail_update_history BEFORE INSERT ON company_application_status_history BEGIN SELECT RAISE(ABORT,'synthetic history failure'); END").run();
 assert.equal((await req(path,'PUT',{...rollbackBase,status:'Отказ'})).status,400);assert.deepEqual(await snapshot(),rollbackBefore);assert.equal((await read()).revision,rollbackBase.revision);
 await db.prepare('DROP TRIGGER fail_update_history').run();assert.equal((await req(path,'PUT',{...rollbackBase,status:'Отказ'})).status,200);
 assert.equal((await req(path,'DELETE')).status,200);assert.equal((await req(path,'PUT',rollbackBase)).status,404);assert.equal((await req(path)).status,404);
 console.log('D1/workerd: required state token, stale saves, same-second changes, forced post-preflight race, parallel HTTP writes, history rollback and user isolation passed.');
} finally {await mf.dispose();}
