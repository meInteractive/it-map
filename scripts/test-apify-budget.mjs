import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{db,env,api,sqlite}=f,original=fetch;
const {apifyBudget,refreshApifyAccount}=await moduleFor('server/services/apify-account.js');
const {executeApifyRun}=await moduleFor('server/services/apify-runs.js');
const token='apify_api_SYNTHETIC_BUDGET_12345',start=new Date(Date.UTC(new Date().getUTCFullYear(),new Date().getUTCMonth(),1)).toISOString(),end=new Date(Date.UTC(new Date().getUTCFullYear(),new Date().getUTCMonth()+1,1)).toISOString();
const account={accountId:'account',planCreditsUsd:5,providerLimitUsd:5,providerUsedUsd:2,periodStart:start,periodEnd:end};
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token));
let generation=sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation;
function run(id,cost,{period=start,accountId='account',profile='free'}={}){sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,created_at,updated_at) VALUES(?,?,?,?,?,'hash',?,?,.25,?,'succeeded',1,1)").run(id,generation,id,profile,accountId,period,end,cost);}
let used=2,concurrent=false,posts=0;
globalThis.fetch=async(url,o)=>{
 if(url.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5,id:'FREE'}}});
 if(url.endsWith('/users/me/limits')){if(concurrent)sqlite.prepare('UPDATE apify_settings SET free_account_json=?').run(JSON.stringify({...account,providerUsedUsd:4}));return Response.json({data:{monthlyUsageCycle:{startAt:start,endAt:end},current:{monthlyUsageUsd:used},limits:{maxMonthlyUsageUsd:5}}});}
 if(o.method==='POST'){posts++;return Response.json({data:{id:'newRun',defaultDatasetId:'newData',userId:'account',status:'RUNNING'}});}
 if(url.includes('/actor-runs/'))return Response.json({data:{id:'newRun',defaultDatasetId:'newData',userId:'account',status:'RUNNING'}});
 throw Error('Unexpected request');
};
try{
 run('known',.6);run('unknown',null);run('other-profile',.1,{profile:'starter'});run('old-period',4,{period:'old'});run('foreign-account',4,{accountId:'foreign'});
 let b=await apifyBudget(db,'free',account);
 assert.ok(Math.abs(b.ownConfirmedUsd-.7)<1e-9);assert.equal(b.unknownReserveUsd,.25);assert.equal(b.unknownRuns,1);assert.equal(b.readReserveUsd,.03);assert.ok(Math.abs(b.remainingUsd-2.72)<1e-9);assert.equal(b.coverageStatus,'estimated_overlap');
 assert.ok(Math.abs(b.localUpperUsd-(b.ownConfirmedUsd+b.unknownReserveUsd+b.readReserveUsd))<1e-9);
 // Account total includes third-party spending; confirmed local total is a
 // floor if provider usage lags. Unknown caps/read reserves remain additive.
 const lag=await apifyBudget(db,'free',{...account,providerUsedUsd:.7});assert.ok(Math.abs(lag.remainingUsd-4.02)<1e-9);
 sqlite.prepare("UPDATE apify_runs SET charged_usd=.9 WHERE id='known'").run();b=await apifyBudget(db,'free',account);assert.ok(Math.abs(b.remainingUsd-2.72)<1e-9,'confirmed cost already covered by account usage is not subtracted again');
 const localFloor=await apifyBudget(db,'free',{...account,providerUsedUsd:.2});assert.ok(Math.abs(localFloor.remainingUsd-3.72)<1e-9,'late local charges remain a floor when provider usage lags');
 assert.equal((await apifyBudget(db,'free',{...account,periodStart:'new'})).localUpperUsd,0,'period isolation');
 concurrent=true;const fresh=await refreshApifyAccount(env,'free');assert.equal(fresh.account.providerUsedUsd,4,'SQL high-water survives a newer concurrent snapshot');concurrent=false;
 const backup=await api.createBackup(db);assert.ok(!backup.storage.tables.some(t=>t.name==='apify_account_usage'));await api.restoreBackup(db,backup);assert.equal(sqlite.prepare('SELECT provider_used_usd FROM apify_account_usage').get().provider_used_usd,4);assert.equal((await refreshApifyAccount(env,'free')).account.providerUsedUsd,4,'restore/cache reset cannot release known account spending');sqlite.exec('UPDATE apify_settings SET enabled=1');generation=sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation;
 sqlite.exec('DELETE FROM apify_account_usage');
 sqlite.prepare('UPDATE apify_settings SET free_account_json=?').run(JSON.stringify({...account,periodStart:'old',providerUsedUsd:5}));assert.equal((await refreshApifyAccount(env,'free')).account.providerUsedUsd,2,'previous period cannot contaminate current usage');
 sqlite.prepare('UPDATE apify_settings SET free_account_json=?').run(JSON.stringify({...account,accountId:'other',providerUsedUsd:5}));assert.equal((await refreshApifyAccount(env,'free')).account.providerUsedUsd,2,'previous account cannot contaminate current usage');
 sqlite.exec('DELETE FROM apify_runs');used=0;let raced=false;
 const raceDb={...db,prepare(sql){if(sql.startsWith('INSERT OR IGNORE INTO apify_runs')&&!raced){raced=true;sqlite.prepare('UPDATE apify_settings SET free_account_json=?').run(JSON.stringify({...account,providerUsedUsd:4.9}));}return db.prepare(sql);}};
 await assert.rejects(executeApifyRun({...env,DB:raceDb},{operationKey:'stale-snapshot',input:{}}),e=>e.code==='apify_busy');assert.equal(posts,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,0,'atomic admission rechecks latest account usage');
 sqlite.exec("UPDATE apify_settings SET free_account_json=''");sqlite.exec('DELETE FROM apify_account_usage');used=4.7;
 const results=await Promise.allSettled(['a','b'].map(operationKey=>executeApifyRun(env,{operationKey,input:{}})));
 assert.equal(results.filter(r=>r.status==='fulfilled').length,1);assert.equal(posts,1,'concurrent claims cannot double-start');
 const accepted=await apifyBudget(db,'free',{...account,providerUsedUsd:4.7});assert.ok(Math.abs(accepted.remainingUsd-.04)<1e-9);assert.ok(accepted.remainingUsd<accepted.minimumReserveUsd,'UI and admission agree on remaining budget');
 console.log('Apify budget: explicit known/unknown/read reserves, third-party/lag, account/period isolation, late cost, atomic provider high-water and concurrent admission passed.');
}finally{globalThis.fetch=original;f.close();}
