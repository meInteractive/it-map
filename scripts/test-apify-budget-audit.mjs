import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=fetch;
const {auditApifyBudget,monthlyApifySummary}=await moduleFor('server/services/apify-budget-audit.js');
const {apifyRequest}=await moduleFor('server/integrations/apify.js');
const token='apify_api_SYNTHETIC_AUDIT_12345',start=new Date(Date.UTC(new Date().getUTCFullYear(),new Date().getUTCMonth(),1)).toISOString(),end=new Date(Date.UTC(new Date().getUTCFullYear(),new Date().getUTCMonth()+1,1)).toISOString();
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted,starter_key_encrypted,paid_allowed) VALUES(1,1,?,?,0)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token),await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_SYNTHETIC_PAID_12345'));
const monthly={usageCycle:{startAt:start,endAt:end},totalUsageCreditsUsdBeforeVolumeDiscount:2,totalUsageCreditsUsdAfterVolumeDiscount:1.8,monthlyServiceUsage:{ACTOR_COMPUTE_UNITS:{quantity:4,baseAmountUsd:1,amountAfterVolumeDiscountUsd:.8,secret:'PRIVATE'},ACTOR_OTHER:{quantity:1,baseAmountUsd:1}},dailyServiceUsages:[{secret:'PRIVATE'}],secret:'PRIVATE'};
let reads=0,change=false,badPeriod=false;
globalThis.fetch=async(url,o)=>{
 assert.equal(o.method,'GET');assert.equal(o.headers.authorization,'Bearer '+token,'unapproved Starter is never read');reads++;
 if(url.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5,id:'FREE'},secret:'PRIVATE'}});
 if(url.endsWith('/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:start,endAt:end},current:{monthlyUsageUsd:2},limits:{maxMonthlyUsageUsd:5}}});
 assert.ok(url.endsWith('/users/me/usage/monthly'));if(change)sqlite.exec('UPDATE apify_settings SET revision=revision+1');return Response.json({data:{...monthly,usageCycle:{startAt:badPeriod?'2000-01-01':start,endAt:end}}});
};
try{
 const key='apify-budget-check:22222222-2222-4222-8222-222222222222';
 const r=await api.runScheduledSeriesTick(env,key);assert.equal(r.status,'completed');assert.equal(r.reason,'apify_budget_audit');assert.equal(r.actorStarts,0);assert.equal(r.processed,1);assert.equal(reads,3);
 const report=r.budgetAudit.profiles.free;assert.equal(report.monthly.totalAfterDiscountUsd,1.8);assert.equal(report.monthly.services.length,2);assert.equal(report.monthly.runCoverageAvailable,false);assert.equal(report.budget.coverageStatus,'estimated_overlap');assert.ok(!JSON.stringify(r).includes('PRIVATE'));assert.ok(!JSON.stringify(r).includes(token));
 assert.equal((await api.runScheduledSeriesTick(env,key)).reused,true);assert.equal(reads,3,'same durable receipt cannot repeat network calls');assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_jobs').get().n,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,0);
 await assert.rejects(api.runScheduledSeriesTick(env,'apify-budget-check:bad'),e=>e.code==='input');
 await assert.rejects(apifyRequest(token,'/users/me/usage/monthly',{method:'POST'}),e=>e.code==='apify_scope');await assert.rejects(apifyRequest(token,'/users/me/usage/monthly/extra'),e=>e.code==='apify_scope');await assert.rejects(apifyRequest(token,'/users/me/usage/daily'),e=>e.code==='apify_scope');
 badPeriod=true;assert.equal((await auditApifyBudget(env)).profiles.free.code,'apify_data');badPeriod=false;change=true;assert.equal((await auditApifyBudget(env)).profiles.free.code,'apify_changed');
 const bounded=monthlyApifySummary({...monthly,monthlyServiceUsage:Object.fromEntries(Array.from({length:150},(_,i)=>['ACTOR_'+i,{quantity:1,baseAmountUsd:1}]))},{periodStart:start,periodEnd:end});assert.equal(bounded.services.length,128);assert.equal(bounded.servicesTruncated,true);
 const unknown=monthlyApifySummary({...monthly,monthlyServiceUsage:{bad:{quantity:'1',baseAmountUsd:1}}},{periodStart:start,periodEnd:end});assert.equal(unknown.services.length,0);assert.equal(unknown.servicesTruncated,true);
 console.log('Apify monthly audit: three GETs/profile, paid/key/period fencing, aggregate-only/no inferred coverage, bounded safe numeric output, durable receipt replay and no Actor/job/model calls passed.');
}finally{globalThis.fetch=old;f.close();}
