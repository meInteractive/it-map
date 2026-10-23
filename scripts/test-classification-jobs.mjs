import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?vacancy-roles'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);



await request('/api/classification/settings');
const roots=(await (await request('/api/classification/settings')).json()).roles,it=roots.find(r=>r.name==='IT Manager'),journalist=roots.find(r=>r.name==='Журналист');
await api.orSettings(env);const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-JOB-KEY');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').run(encrypted);
const priorFetch=globalThis.fetch;let mode='normal',paid=0,hold=false,releases=[],active=0,maxActive=0;
globalThis.fetch=async(url,options={})=>{
 if(!options.body)return Response.json({data:[{id:api.JEV_MODEL_ID,pricing:{prompt:'0.000000042'}}]});
 assert.equal(url,'https://openrouter.ai/api/alpha/decisions');paid++;active++;maxActive=Math.max(maxActive,active);const body=JSON.parse(options.body);
 if(hold)await new Promise(r=>releases.push(r));active--;
 if(mode==='timeout')await new Promise((resolve,reject)=>{const keepAlive=setTimeout(()=>{},2000);const abort=()=>{clearTimeout(keepAlive);reject(options.signal.reason)};if(options.signal.aborted)abort();else options.signal.addEventListener('abort',abort,{once:true});});
 if(mode==='credits')return Response.json({error:{code:402}},{status:402});
 if(mode==='rate')return Response.json({error:{code:429}},{status:429});
 const answers=Object.fromEntries(Object.keys(body.questions).map(k=>[k,{type:'choice',choice:k==='role_'+it.roleId?'match':'reject'}]));if(mode==='partial')delete answers['role_'+journalist.roleId];
 return Response.json({model:api.JEV_MODEL_ID,answers,usage:{cost:0.00002}});
};
const createCandidate=async(key,titleRaw='Head of Engineering')=>(await api.upsertVacancyCandidate(db,{titleRaw,vacancyUrl:'https://isolated.example/jobs/'+key})).id;
const enqueue=async(ids,extra={},receipt=crypto.randomUUID())=>api.enqueueClassification(env,{ids,...extra},receipt);
const run=async id=>api.runClassificationJob(env,id);
async function until(predicate){for(let i=0;i<1000&&!predicate();i++)await new Promise(r=>setTimeout(r,1));assert.ok(predicate());}
try{
 const ids=[];for(const [n,title] of ['Head of Engineering','Выпускающий редактор','Менеджер по продажам автомобилей'].entries())ids.push(await createCandidate(n,title));
 const receipt=crypto.randomUUID(),queued=await enqueue(ids,{},receipt),again=await enqueue(ids,{},receipt);assert.deepEqual(again.jobIds,queued.jobIds);assert.equal(queued.queued,3);
 let result=await api.runClassificationJob(env,queued.jobIds[0],{sliceMs:0});assert.equal(result.state,'paused');assert.equal(paid,0);result=await run(result.id);assert.equal(result.state,'completed');assert.equal(result.completed,3);assert.equal(paid,3);assert.ok(Math.abs(result.costUsd-0.00006)<1e-12);assert.equal(result.unknownCostRequests,0);assert.ok(maxActive<=2);
 const before=paid;assert.equal((await enqueue(ids)).queued,0);assert.equal((await run(result.id)).state,'completed');assert.equal(paid,before);
 const stats=await api.classificationCandidateStatistics(db);assert.equal(stats.total,3);assert.equal(stats.pending,0);assert.equal(stats.accepted,3);
 sqlite.prepare('UPDATE vacancy_role_classification SET markdown=?,revision=revision+1 WHERE role_id=?').run('Changed IT prompt',it.roleId);assert.equal((await api.classificationCandidateStatistics(db)).pending,3);const changed=await enqueue(ids);result=await run(changed.jobIds[0]);assert.equal(result.state,'completed');assert.ok(result.items.every(i=>i.log[0].roleCount===1));
 const partial=await createCandidate('partial');mode='partial';const partialJob=await enqueue([partial]);result=await run(partialJob.jobIds[0]);assert.equal(result.state,'paused');assert.equal(result.errors,1);assert.equal((await api.getVacancyCandidate(db,partial)).candidate.processingStatus,'accepted');assert.equal((await api.classificationCandidateStatistics(db)).pending,1);mode='normal';result=await api.runClassificationJob(env,result.id,{resume:true});assert.equal(result.completed,1);assert.equal(result.items[0].log.at(-1).roleCount,1);
 const forced=await enqueue([partial],{force:true});mode='partial';result=await run(forced.jobIds[0]);assert.equal(result.errors,1);mode='normal';result=await api.runClassificationJob(env,result.id,{resume:true});assert.equal(result.state,'completed');assert.equal(result.items[0].log.at(-1).roleCount,1,'force retries only failed roles');
 const errIds=[await createCandidate('fatal1'),await createCandidate('fatal2'),await createCandidate('fatal3')];mode='credits';const failed=await enqueue(errIds);result=await run(failed.jobIds[0]);assert.equal(result.state,'paused');assert.equal(result.completed,0);assert.ok(result.requests<=2,'fatal stops starting remaining jobs');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_classifications WHERE candidate_id=?').get(errIds[0]).n,0);mode='normal';result=await api.runClassificationJob(env,result.id,{resume:true});assert.equal(result.state,'completed');
 const retry=await createCandidate('retry');mode='rate';const retryJob=await enqueue([retry]);result=await run(retryJob.jobIds[0]);assert.equal(result.errors,1);assert.equal(result.items[0].attempts,4,'initial + at most3 retries');mode='normal';
 const busy=await createCandidate('busy'),busyJob=await enqueue([busy]);hold=true;const running=run(busyJob.jobIds[0]);await until(()=>releases.length>0);await assert.rejects(()=>run(busyJob.jobIds[0]),e=>e.code==='job_busy');const same=await enqueue([busy]);const overlap=await run(same.jobIds[0]);assert.equal(overlap.state,'paused');assert.equal(overlap.requests,0);hold=false;releases.splice(0).forEach(r=>r());await running;const paidNow=paid;assert.equal((await run(overlap.id)).state,'completed');assert.equal(paid,paidNow,'overlapping job reuses successful result');
 const slotIds=[await createCandidate('slot1'),await createCandidate('slot2')],third=await createCandidate('slot3'),slotsJob=await enqueue(slotIds);hold=true;const slotRunning=run(slotsJob.jobIds[0]);await until(()=>releases.length===2);const beforeThird=paid;const thirdResult=await api.classifyStoredCandidate(env,third);assert.equal(thirdResult.attempts.items[0].outcome,'error');assert.equal(JSON.parse(thirdResult.attempts.items[0].errors_json)[0].code,'classification_busy');assert.equal(paid,beforeThird);assert.equal(maxActive,2);hold=false;releases.splice(0).forEach(r=>r());await slotRunning;
 // Crash recovery: a saved forced decision is detected using the previous ID.
 const recovered=await enqueue([partial],{force:true});const row=sqlite.prepare('SELECT * FROM vacancy_classification_jobs WHERE id=?').get(recovered.jobIds[0]);const items=JSON.parse(row.items_json),detail=await api.getVacancyCandidate(db,partial);items[0].outcome='running';items[0].forceBaseline=Object.fromEntries(detail.currentResults.map(r=>[r.roleId,r.id]));await api.classifyStoredCandidate(env,partial,{force:true});sqlite.prepare("UPDATE vacancy_classification_jobs SET state='running',owner_token='old',lease_until=1,items_json=? WHERE id=?").run(JSON.stringify(items),row.id);const recoveryPaid=paid;assert.equal((await run(row.id)).state,'completed');assert.equal(paid,recoveryPaid,'recovery of forced job does not repeat committed roles');
 const timeoutId=await createCandidate('timeout'),timeoutJob=await enqueue([timeoutId]);mode='timeout';const timed=await api.runClassificationJob(env,timeoutJob.jobIds[0],{sliceMs:800});assert.equal(timed.state,'paused');assert.equal(timed.unknownCostRequests,1);const timedDetail=await api.getVacancyCandidate(db,timeoutId);assert.equal(timedDetail.candidate.processingStatus,'classification_error');assert.equal(timedDetail.history.total,0);assert.ok(sqlite.prepare('SELECT count(*) n FROM vacancy_classification_slots WHERE expires_at>?').get(Date.now()).n>=1,'uncertain request retains concurrency exposure');sqlite.exec('UPDATE vacancy_classification_slots SET expires_at=1');mode='normal';assert.equal((await api.runClassificationJob(env,timed.id,{resume:true})).state,'completed');
 assert.equal((await request('/api/classification/jobs','GET',undefined,user)).status,403);assert.equal((await request('/api/classification/jobs','POST',{requestId:crypto.randomUUID(),limit:51})).status,400);assert.equal((await request('/api/classification/jobs','POST',{requestId:crypto.randomUUID(),force:true})).status,400);
 const bgId=await createCandidate('background'),tasks=[];const ctx={waitUntil(p){tasks.push(p)}};const response=await worker.fetch(new Request(base+'/api/classification/jobs',{method:'POST',headers:{cookie:admin,origin:base,'content-type':'application/json'},body:JSON.stringify({requestId:crypto.randomUUID(),candidateId:bgId})}),env,ctx);assert.equal(response.status,202);assert.equal((await response.json()).execution,'background');await Promise.all(tasks);assert.equal((await api.getVacancyCandidate(db,bgId)).needsClassification,false,'server background finishes without UI polling');
 const backup=await api.createBackup(db);assert.ok(!backup.storage.tables.some(t=>t.name==='vacancy_classification_jobs'));await api.restoreBackup(db,backup);await assert.rejects(()=>api.readClassificationJob(db,result.id),e=>e.status===404);
 sqlite.exec('UPDATE vacancy_role_classification SET parsing_enabled=0');const noPaid=paid;assert.equal((await enqueue([ids[0]])).noActiveRoles,true);assert.equal(paid,noPaid);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Classification jobs: typed stored decisions, current stats/cache, selective prompt/partial/force retry, durable pause/crash recovery, global2, idempotency/overlap, fatal stop, bounded backoff, ACL/background/restore and no Firecrawl/main passed');
}finally{globalThis.fetch=priorFetch;sqlite.close();}
