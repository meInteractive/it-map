import assert from 'node:assert/strict';
import {serializeDebugJSON} from '../shared/debug-json.mjs';
import {apifyUsageSnapshot} from '../shared/provider-debug.mjs';

const now=Date.parse('2026-11-07T15:00:00Z');
const secret=['apify','api','PRIVATE'].join('_');
const ops=Array.from({length:10},(_,i)=>({
  operationId:i.toString(16).padStart(32,'0'),
  profile:i%2?'starter':'free',
  state:i===9?'uncertain':i===8?'failed':'succeeded',
  createdAt:now-i*1000,
  updatedAt:now-i*1000+30,
  chargedUsd:i===9?null:i===8?0:0.1,
  reservedUsd:0.25,
  errorCode:i===9?'apify_uncertain':null,
  hasProviderRunId:i<9,
  startOutcomeKnown:i<9,
  run_id:secret,
  dataset_id:secret,
  operation_key:secret,
  account_id:secret,
  key_hash:secret,
  token:secret,
}));
const usage={
  providerLimitMode:false,
  profiles:{free:{providerUsedUsd:4,ownConfirmedUsd:0.9,readReserveUsd:0.01,
    unknownReserveUsd:0.25,unknownRuns:1,limitUsd:5,remainingUsd:0.75,
    periodStart:'2026-11-01T00:00:00Z',periodEnd:'2026-12-01T00:00:00Z',
    checkedAt:now-2000,accountId:secret},starter:null},
  active:[{id:'SHOULD_NOT_APPEAR',profile:'free',state:'running',
    run_id:secret,created_at:now+10000}],
  operationHistoryAvailable:true,operationsTotal:28,operationsCheckedAt:now,
  recentOperations:ops,
};
const result=JSON.parse(serializeDebugJSON({
  section:'apify_usage',capturedAt:now,...apifyUsageSnapshot(usage),
}));
assert.equal(result.section,'apify_usage');
assert.equal(result.summary.operationHistoryAvailable,true);
assert.equal(result.summary.activeOnly,false);
assert.equal(result.summary.actorStartCountAvailable,false);
assert.equal(result.summary.reconcileReadCountAvailable,false);
assert.equal(result.recentItems.total,28);
assert.equal(result.recentItems.returned,10);
assert.equal(result.recentItems.coverage,'server_latest');
assert.equal(result.recentItems.items[0].operationId,ops[0].operationId);
assert.equal(result.recentItems.items[9].operationId,ops[9].operationId);
assert.equal(result.recentItems.items[9].chargedUsd,null);
assert.equal(result.recentItems.items[8].chargedUsd,0,'confirmed zero must not become unknown');
assert.equal(result.recentItems.items[9].hasProviderRunId,false);
assert.equal(result.recentItems.items[9].startOutcomeKnown,false);
assert.equal(result.summary.profiles.free.ownConfirmedUsd,0.9);
assert.equal(result.summary.profiles.free.providerUsedUsd,4);
assert.equal(result.summary.totalUsd,undefined,'do not add overlapping account and local costs');
assert.equal(result.dataAsOf,new Date(now).toISOString());
const output=JSON.stringify(result);
for(const term of [secret,'SHOULD_NOT_APPEAR','account_id','key_hash','operation_key','dataset_id','run_id']){
 assert.ok(!output.includes(term),'sensitive record field was exported: '+term);
}
const incomplete={...usage,recentOperations:ops.slice(0,9)};
assert.throws(()=>serializeDebugJSON({
  section:'apify_usage',capturedAt:now,...apifyUsageSnapshot(incomplete),
}),/incomplete_debug_latest/);
const fallback=JSON.parse(serializeDebugJSON({
  section:'apify_usage',capturedAt:now,...apifyUsageSnapshot({
    ...usage,operationHistoryAvailable:false,recentOperations:undefined,
  }),
}));
assert.equal(fallback.summary.activeOnly,true);
assert.equal(fallback.summary.operationHistoryAvailable,false);
assert.equal(fallback.recentItems.items[0].operationId,'SHOULD_NOT_APPEAR');
assert.equal(fallback.recentItems.coverage,'complete');
console.log('S11-DIAG-03 Apify: last 10 from full ledger, private fields redacted, confirmed zero/unknown, no invented POST/GET counts');
