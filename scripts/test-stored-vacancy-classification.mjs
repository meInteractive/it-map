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
await api.orSettings(env);const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-STORED-CLASSIFICATION');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').run(encrypted);
const previousFetch=globalThis.fetch,calls=[];let mode='normal',release,pending;
globalThis.fetch=async(url,options={})=>{
 const body=options.body?JSON.parse(options.body):null;calls.push({url:String(url),body});
 if(!body){assert.match(String(url),/output_modalities=decisions/);return Response.json({data:[{id:api.JEV_MODEL_ID,pricing:{prompt:'0.000000042'}}]});}
 assert.ok(String(url).endsWith('/api/alpha/decisions'));assert.deepEqual(Object.keys(body.state),['title']);
 if(mode==='held')await new Promise(resolve=>{pending=true;release=resolve;});
 if(mode==='credits')return Response.json({error:{code:402}},{status:402});
 const answers=Object.fromEntries(Object.keys(body.questions).map(key=>[key,{type:'choice',choice:mode==='reject'?'reject':key==='role_'+it.roleId?'match':'reject'}]));
 if(mode==='partial'&&answers['role_'+journalist.roleId])delete answers['role_'+journalist.roleId];
 return Response.json({model:api.JEV_MODEL_ID+'-20260917',answers,usage:{cost:.00002}});
};
const inferences=()=>calls.filter(call=>call.body);
const createCandidate=async(key,titleRaw='Senior Engineering Manager')=>{const r=await request('/api/vacancy-candidates','POST',{titleRaw,vacancyUrl:'https://isolated.example/'+key});assert.equal(r.status,201,await r.clone().text());return (await r.json()).id;};
const classify=(id,force=false)=>request('/api/vacancy-candidates/'+id+'/classify','POST',{force});
const detail=async id=>(await request('/api/vacancy-candidates/'+id)).json();
const read=async r=>{assert.equal(r.status,200,await r.clone().text());return r.json();};
async function waitPending(){for(let i=0;i<1000&&!pending;i++)await new Promise(resolve=>setTimeout(resolve,1));assert.ok(pending,'model request started');}
try{
 const id=await createCandidate('first',' Senior Engineering Manager ');
 assert.equal((await request('/api/vacancy-candidates/'+id+'/classify','POST',{force:false},user)).status,403);
 let result=await read(await classify(id));assert.equal(result.candidate.processingStatus,'accepted');assert.equal(result.history.total,2);assert.equal(result.attempts.total,1);assert.equal(result.needsClassification,false);
 assert.equal(inferences().at(-1).body.state.title,'Senior Engineering Manager');assert.equal(inferences().length,1);assert.equal(result.attempts.items[0].cost_usd,.00002);
 await request('/api/vacancy-candidates','POST',{titleRaw:'Senior Engineering Manager',vacancyUrl:'https://isolated.example/first'});assert.equal((await detail(id)).candidate.titleRevision,1,'trim-only title update preserves decisions');const cacheCalls=calls.length;result=await read(await classify(id));assert.equal(result.reused,true);assert.equal(calls.length,cacheCalls);assert.equal(result.history.total,2);
 await request('/api/classification/roles/'+it.roleId,'PUT',{enabled:false,revision:it.revision});assert.equal((await detail(id)).candidate.processingStatus,'rejected');
 await request('/api/classification/roles/'+it.roleId,'PUT',{enabled:true,revision:it.revision+1});result=await read(await classify(id));assert.equal(result.reused,true);assert.equal(calls.length,cacheCalls,'toggle off/on reuses unchanged prompt hash');
 await request('/api/classification/prompts/'+journalist.roleId,'PUT',{markdown:'New independent journalist prompt',revision:journalist.revision});
 result=await read(await classify(id));assert.deepEqual(result.requestedRoleIds,[journalist.roleId]);assert.deepEqual(Object.keys(inferences().at(-1).body.questions),['role_'+journalist.roleId]);assert.equal(result.history.total,3);assert.equal(result.attempts.total,2);
 await request('/api/vacancy-candidates','POST',{titleRaw:'Changed Engineering Manager',vacancyUrl:'https://isolated.example/first'});assert.equal((await detail(id)).candidate.processingStatus,'pending_classification');
 result=await read(await classify(id));assert.equal(result.history.total,5);assert.equal(result.currentResults.every(r=>r.decision),true);assert.ok(result.history.items.some(r=>r.title_revision===1));assert.ok(result.history.items.some(r=>r.title_revision===2));
 mode='reject';result=await read(await classify(id,true));assert.equal(result.candidate.processingStatus,'rejected');assert.equal(result.history.total,7);mode='normal';
 const partial=await createCandidate('partial');mode='partial';result=await read(await classify(partial));assert.equal(result.candidate.processingStatus,'accepted');assert.equal(result.needsClassification,true);assert.equal(result.history.total,1);assert.equal(result.attempts.items[0].outcome,'partial');assert.equal(JSON.parse(result.attempts.items[0].errors_json).length,1);
 const pendingSelection=await api.selectClassificationCandidates(db,{ids:[partial]});assert.equal(pendingSelection.candidates.length,1);assert.equal(pendingSelection.candidates[0].processingStatus,'accepted');assert.equal(pendingSelection.candidates[0].classificationComplete,0);assert.ok((await api.classificationCandidateStatistics(db)).pending>0);mode='normal';result=await read(await classify(partial));assert.deepEqual(result.requestedRoleIds,[journalist.roleId]);assert.equal(result.history.total,2);assert.equal(result.needsClassification,false);assert.equal((await api.selectClassificationCandidates(db,{ids:[partial]})).candidates.length,0);assert.equal((await api.selectClassificationCandidates(db,{ids:[partial],force:true})).candidates.length,1);await assert.rejects(()=>api.selectClassificationCandidates(db,{limit:51}),e=>e.status===400);
 const errorId=await createCandidate('error');mode='credits';result=await read(await classify(errorId));assert.equal(result.candidate.processingStatus,'classification_error');assert.equal(result.history.total,0);assert.ok(JSON.parse(result.attempts.items[0].errors_json).every(e=>e.code==='credits'));mode='normal';result=await read(await classify(errorId));assert.equal(result.candidate.processingStatus,'accepted');assert.equal(result.attempts.total,2);
 // A change in configured model invalidates only matching model keys.
 sqlite.prepare("UPDATE vacancy_classifications SET model_key='obsolete-model' WHERE candidate_id=? AND root_role_id=?").run(partial,it.roleId);result=await read(await classify(partial));assert.deepEqual(result.requestedRoleIds,[it.roleId]);
 const beforeRename=inferences().length;await request('/api/vacancy-roles/'+journalist.roleId,'PUT',{name:'Renamed journalist'});result=await read(await classify(partial));assert.equal(result.reused,true);assert.equal(inferences().length,beforeRename);
 await request('/api/vacancy-roles/'+journalist.roleId,'PUT',{parentId:it.roleId});result=await detail(partial);assert.equal(result.currentResults.length,1);assert.ok(result.history.items.some(r=>r.root_role_id===journalist.roleId));await request('/api/vacancy-roles/'+journalist.roleId,'PUT',{parentId:null});
 const concurrent=await createCandidate('concurrent');mode='held';pending=false;const running=classify(concurrent);await waitPending();const whileHeld=inferences().length;assert.equal((await classify(concurrent)).status,409);assert.equal(inferences().length,whileHeld);mode='normal';release();result=await read(await running);assert.equal(result.history.total,2);assert.equal(result.attempts.total,1);
 const atomic=await createCandidate('atomic');sqlite.exec("CREATE TRIGGER injected_classification_failure BEFORE INSERT ON vacancy_classifications BEGIN SELECT RAISE(ABORT,'synthetic failure'); END");assert.equal((await classify(atomic)).status,400);result=await detail(atomic);assert.equal(result.attempts.total,0);assert.equal(result.history.total,0);sqlite.exec('DROP TRIGGER injected_classification_failure');
 const changed=await createCandidate('changed');mode='held';pending=false;const changedRequest=classify(changed);await waitPending();await request('/api/vacancy-candidates','POST',{titleRaw:'Updated in flight',vacancyUrl:'https://isolated.example/changed'});mode='normal';release();assert.equal((await changedRequest).status,409);assert.equal((await detail(changed)).history.total,0);
 const superseded=await createCandidate('superseded');mode='held';pending=false;const obsolete=classify(superseded);await waitPending();sqlite.prepare('UPDATE vacancy_candidate_classification_leases SET expires_at=1 WHERE candidate_id=?').run(superseded);mode='normal';result=await read(await classify(superseded));release();assert.equal((await obsolete).status,409);assert.equal(result.attempts.total,1);
 // Configuration changes in flight preserve independent successful outcomes.
 const promptRace=await createCandidate('prompt-race');mode='held';pending=false;const race=classify(promptRace);await waitPending();sqlite.prepare('UPDATE vacancy_role_classification SET markdown=?,revision=revision+1 WHERE role_id=?').run('Changed during request',journalist.roleId);mode='normal';release();result=await read(await race);assert.equal(result.candidate.processingStatus,'accepted');assert.equal(result.needsClassification,true);assert.equal(result.history.total,1);
 const restored=await createCandidate('restore');mode='held';pending=false;const oldOperation=classify(restored);await waitPending();const backup=await api.createBackup(db);await api.restoreBackup(db,backup);mode='normal';release();assert.equal((await oldOperation).status,409);assert.equal((await detail(restored)).history.total,0);
 sqlite.exec('UPDATE vacancy_role_classification SET parsing_enabled=0');const disabledCalls=calls.length;result=await read(await classify(partial));assert.equal(result.candidate.processingStatus,'no_active_roles');assert.equal(calls.length,disabledCalls);assert.ok(result.history.total>0);assert.equal((await api.selectClassificationCandidates(db)).candidates.length,0);assert.equal((await api.classificationCandidateStatistics(db)).noActiveRoles,(await api.classificationCandidateStatistics(db)).total);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Stored candidates: cache/role hashes/toggle reuse, selective rerun, title/model revisions, append-only history, partial/error semantics, lease concurrency/recovery, atomic rollback, config/title/restore fencing and main isolation passed');
}finally{globalThis.fetch=previousFetch;sqlite.close();}
