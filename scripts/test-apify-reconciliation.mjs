import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=fetch;
const {reconcileApifyRuns,executeApifyRun,apifyDatasetInfo}=await moduleFor('server/services/apify-runs.js');
const {apifyRequest}=await moduleFor('server/integrations/apify.js');
const token='apify_api_SYNTHETIC_RECONCILE_1234',hash=await api.candidateHash(token);
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token));
const generation=(await api.candidateDb(db)).generation||sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation;
for(let n=1;n<=4;n++)sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,run_id,dataset_id,created_at,updated_at) VALUES(?,?,?,'free','account',?,'s','e',.25,?,?,?,1,1)").run('r'+n,generation,'detail:'+n,hash,n===1?'running':n===3?'uncertain':'succeeded','run'+n,'data'+n);
let reads=0,unknown=true,wrong=false;
globalThis.fetch=async(url,o)=>{
 assert.equal(o.method,'GET','reconciliation cannot POST');reads++;
 if(url.endsWith('/datasets/data1'))return Response.json({data:{id:'data1',itemCount:12,cleanItemCount:10,secret:'PRIVATE'}});
 const n=Number(new URL(url).pathname.split('/').at(-1).slice(3));
 return Response.json({data:{id:wrong?'other':'run'+n,userId:'account',defaultDatasetId:'data'+n,status:'SUCCEEDED',...(unknown&&n===2?{}:{usageTotalUsd:n/100})}});
};
try{
 const out=await reconcileApifyRuns(env);assert.equal(out.checked,4);assert.equal(reads,4);assert.equal(sqlite.prepare("SELECT state FROM apify_runs WHERE id='r1'").get().state,'succeeded');assert.equal(sqlite.prepare("SELECT charged_usd FROM apify_runs WHERE id='r2'").get().charged_usd,null);
 assert.deepEqual(await apifyDatasetInfo(token,'data1'),{itemCount:12,cleanItemCount:10});
 unknown=false;sqlite.prepare("UPDATE apify_runs SET updated_at=1 WHERE id='r2'").run();
 await executeApifyRun(env,{operationKey:'detail:2',input:{}});assert.equal(sqlite.prepare("SELECT charged_usd FROM apify_runs WHERE id='r2'").get().charged_usd,.02,'terminal cost omitted in first ACK is retried');
 const before=sqlite.prepare('SELECT * FROM apify_runs ORDER BY id').all();wrong=true;
 sqlite.prepare("UPDATE apify_runs SET updated_at=1 WHERE id='r1'").run();
 const changed=await reconcileApifyRuns(env);assert.equal(changed.results[0].code,'apify_data');assert.equal(sqlite.prepare("SELECT charged_usd FROM apify_runs WHERE id='r1'").get().charged_usd,before[0].charged_usd);
 await assert.rejects(apifyRequest(token,'/datasets/data1',{method:'POST'}),e=>e.code==='apify_scope');
 await assert.rejects(apifyRequest(token,'/datasets/data1/delete'),e=>e.code==='apify_scope');
 sqlite.prepare('UPDATE apify_settings SET free_key_encrypted=?').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_CHANGED_RECONCILE_1234'));reads=0;
 await reconcileApifyRuns(env);assert.equal(reads,0,'changed key never reads previous account resources');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,4);
 sqlite.prepare('UPDATE apify_settings SET free_key_encrypted=?').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token));wrong=false;sqlite.exec('UPDATE apify_runs SET updated_at=1');
 await assert.rejects(api.runScheduledSeriesTick(env,'apify-reconcile:invalid'),e=>e.code==='input');
 const key='apify-reconcile:11111111-1111-4111-8111-111111111111',receipt=await api.runScheduledSeriesTick(env,key);assert.equal(receipt.actorStarts,0);assert.equal(receipt.trigger,'probe');assert.equal(receipt.processed,4);const count=reads;
 assert.equal((await api.runScheduledSeriesTick(env,key)).reused,true);assert.equal(reads,count,'receipt replay performs no GET or POST');assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_jobs').get().n,0,'maintenance does not claim unrelated jobs');
 console.log('Apify reconciliation: bounded GET-only, stale running/uncertain, delayed terminal cost, ID/account fencing, unknown reserves, no starts and dataset metadata route scope passed.');
}finally{globalThis.fetch=old;f.close();}
