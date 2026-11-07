import assert from 'node:assert/strict';
import {serializeDebugJSON} from '../shared/debug-json.mjs';
import {apifyUsageSnapshot,firecrawlUsageSnapshot} from '../shared/provider-debug.mjs';

const now=Date.parse('2026-11-07T15:00:00.000Z');
const secret=['sk','proj','NEVER_OUTPUT'].join('-');
const budgets={
  free:{
    accountId:secret,planName:'private',providerUsedUsd:7.25,
    ownConfirmedUsd:2.5,unknownReserveUsd:0.75,readReserveUsd:0.1,
    unknownRuns:3,limitUsd:10,remainingUsd:1.9,
    periodStart:'2026-11-01T00:00:00Z',periodEnd:'2026-12-01T00:00:00Z',
    checkedAt:now-3000,keyHash:secret,
  },
  starter:null,
};
const active=Array.from({length:15},(_,i)=>({
  id:'OP_'+String(i).padStart(2,'0'),profile:'free',
  created_at:now+i*1000,state:i===14?'uncertain':'running',
  run_id:secret,actorInput:secret,payload:secret,
}));
const apify=JSON.parse(serializeDebugJSON({
  section:'apify_usage',capturedAt:now,
  ...apifyUsageSnapshot({profiles:budgets,active,providerLimitMode:true,keyMask:secret}),
}));
assert.equal(apify.section,'apify_usage');
assert.equal(apify.summary.profiles.free.providerUsedUsd,7.25);
assert.equal(apify.summary.profiles.free.ownConfirmedUsd,2.5);
assert.equal(apify.summary.profiles.free.unknownReserveUsd,0.75);
assert.equal(apify.summary.profiles.starter,null);
assert.equal(apify.summary.activeOnly,true);
assert.equal(apify.summary.operationHistoryAvailable,false);
assert.equal(apify.recentItems.returned,10);
assert.equal(apify.recentItems.total,15);
assert.equal(apify.recentItems.items[0].operationId,'OP_14');
assert.equal(apify.recentItems.items[0].chargedUsd,null);
assert.equal(apify.summary.profiles.free.checkedAt,new Date(now-3000).toISOString());
assert.ok(!JSON.stringify(apify).includes(secret));
assert.ok(!JSON.stringify(apify).includes('accountId'));
assert.equal(apify.summary.totalUsd,undefined,'do not add provider-used and own-confirmed');

const history={
  checkedAt:now-1000,coverage:'server_latest',total:12,
  items:Array.from({length:10},(_,i)=>({
    operationId:'PAID_'+String(11-i),profile:'free',
    state:i===0?'uncertain':'succeeded',
    createdAt:now-i*1000,updatedAt:now-i*1000,
    chargedUsd:i===0?null:0.05,reservedUsd:0.25,errorCode:'ci_operation',
    accountId:secret,runId:secret,key_hash:secret,datasetId:secret,
  })),
};
const historic=JSON.parse(serializeDebugJSON({
  section:'apify_usage',capturedAt:now,
  ...apifyUsageSnapshot({profiles:budgets,active,providerLimitMode:true},history),
}));
assert.equal(historic.summary.activeOnly,false);
assert.equal(historic.summary.operationHistoryAvailable,true);
assert.equal(historic.recentItems.coverage,'server_latest');
assert.equal(historic.recentItems.total,12);
assert.equal(historic.recentItems.returned,10);
assert.equal(historic.recentItems.items[0].operationId,'PAID_11');
assert.equal(historic.recentItems.items[0].chargedUsd,null);
assert.equal(historic.recentItems.items[0].reservedUsd,0.25);
assert.equal(historic.dataAsOf,new Date(now-1000).toISOString());
assert.ok(!JSON.stringify(historic).includes(secret));
assert.throws(()=>serializeDebugJSON({
  section:'apify_usage',capturedAt:now,
  ...apifyUsageSnapshot({profiles:budgets,active}, {...history,items:history.items.slice(0,2)}),
}),/incomplete_debug_latest/);

const fb={
  mode:'free',fresh:true,status:'available',checkedAt:now-5000,reason:secret,
  limits:{credits:1000,privateKey:secret},
  budget:{providerPlanCredits:1000,providerRemaining:920,providerUsedEstimate:null,
    localReported:45,localEstimated:63,reserved:18,effectiveUsed:80,remaining:920,
    periodStart:'2026-11-01T00:00:00Z',periodEnd:'2026-12-01T00:00:00Z',
    personalDetails:secret},
};
const snapshot=firecrawlUsageSnapshot(fb,now);
const firecrawl=JSON.parse(serializeDebugJSON({section:'firecrawl_usage',capturedAt:now,...snapshot}));
assert.equal(firecrawl.summary.reportedCredits,45);
assert.equal(firecrawl.summary.estimatedCredits,63);
assert.equal(firecrawl.summary.reservedCredits,18);
assert.equal(firecrawl.summary.providerUsedEstimateCredits,null);
assert.equal(firecrawl.summary.operationHistoryAvailable,false);
assert.equal(firecrawl.summary.fresh,true);
assert.equal(firecrawl.summary.checkedAt,new Date(now-5000).toISOString());
assert.equal(firecrawl.summary.confirmedUsd,undefined);
assert.ok(!JSON.stringify(firecrawl).includes(secret));
const stale=JSON.parse(serializeDebugJSON({section:'firecrawl_usage',capturedAt:now,
  ...firecrawlUsageSnapshot(fb,now+120000)}));
assert.equal(stale.summary.fresh,false);
assert.equal(stale.summary.reportedCredits,45,'old values retain explicit timestamp');
const noBudget=JSON.parse(serializeDebugJSON({section:'firecrawl_usage',capturedAt:now,
  ...firecrawlUsageSnapshot({...fb,budget:null,checkedAt:null,fresh:false,status:'unavailable'},now)}));
assert.equal(noBudget.summary.budgetAvailable,false);
assert.equal(noBudget.summary.reportedCredits,null);
assert.equal(noBudget.summary.checkedAt,null);
console.log('S11-DIAG-03: Apify accounts/active-only, Firecrawl credits, redaction, stale/unknown passed');
