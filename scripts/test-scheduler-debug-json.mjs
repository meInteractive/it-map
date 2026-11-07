import assert from 'node:assert/strict';
import {serializeDebugJSON,buildDebugJSON} from '../shared/debug-json.mjs';
import {schedulerOverviewSnapshot,schedulerQueueSnapshot,schedulerRunsSnapshot,schedulerAttemptsSnapshot} from '../shared/scheduler-debug.mjs';
const at=Date.parse('2026-11-07T15:00:00.000Z');
const secret=['sk','proj','CANNOT_COPY'].join('-');
const d={
  settings:{enabled:true,ownerName:'Private owner',dayBudgetUsd:10},
  cloudStatus:'signal_received',lastSignalAt:at,lastSuccessAt:null,nextSignalAt:at+3600000,
  counts:{pending:3,running:0,failed:1,token:secret},
  statistics:{sources:11,found:100,accepted:40,imported:28,rawPrompt:secret},
  spending:{dayUsd:1.154459,monthUsd:1.3,providerKey:secret},
  runs:Array.from({length:20},(_,i)=>({
    id:'RUN_'+String(i).padStart(2,'0'),trigger:'automatic',started_at:at+i*1000,
    finished_at:i===19?null:at+i*1000+100,status:'completed',
    ready:10,processed:i,succeeded:i,errors:0,remaining:0,reason:'empty',
    message:secret,
  })),
  attempts:Array.from({length:50},(_,i)=>({
    id:'ATT_'+String(i).padStart(2,'0'),job_id:'JOB_'+String(i).padStart(2,'0'),
    run_id:'RUN_19',type:'classify_title',source_id:'hh',status:'completed',
    reason_code:'success',provider:'openrouter',model:'gpt-4o-mini',
    started_at:at+i*1000,finished_at:null,cost_usd:i===49?null:0,
    reserved_usd:0.003,credits:null,message:secret,result_json:secret,
  })),
};
const envelope=(section,snapshot)=>JSON.parse(serializeDebugJSON({section,capturedAt:at,...snapshot}));
const overview=envelope('scheduler_overview',schedulerOverviewSnapshot(d));
assert.equal(overview.summary.enabled,true);
assert.equal(overview.summary.counts.pending,3);
assert.equal(overview.summary.statistics.imported,28);
assert.equal(overview.summary.spending.upperEstimateUsd,1.154459);
assert.equal(overview.summary.lastSignalAt,new Date(at).toISOString());
assert.equal(overview.summary.lastSuccessAt,null);
assert.equal(overview.summary.nextSignalAt,null,'unknown real systemd schedule must not be fabricated');
assert.equal(overview.summary.spending.confirmedUsd,undefined,'upper estimate is not confirmed cost');
assert.ok(!JSON.stringify(overview).includes(secret));
assert.ok(!JSON.stringify(overview).includes('Private owner'));

const q={total:25,limit:10,coverage:'server_latest',checkedAt:at,items:Array.from({length:10},(_,i)=>({
  jobId:'JOB_'+String(24-i),type:'classify_title',sourceId:'hh',status:'failed',
  priority:5,createdAt:at-i*1000,updatedAt:at-i*1000,reasonCode:'retry_due',
  payload:secret,last_error:secret,companyName:secret,
}))};
const queue=envelope('scheduler_queue',schedulerQueueSnapshot(q,{status:'failed',type:'classify_title',searchApplied:true}));
assert.equal(queue.recentItems.total,25);
assert.equal(queue.dataAsOf,new Date(at).toISOString());
assert.equal(queue.recentItems.returned,10);
assert.equal(queue.recentItems.truncated,true);
assert.equal(queue.filters.searchApplied,true);
assert.equal(queue.recentItems.items[0].jobId,'JOB_24');
assert.ok(!JSON.stringify(queue).includes(secret));
assert.throws(()=>buildDebugJSON({section:'scheduler_queue',capturedAt:at,...schedulerQueueSnapshot({...q,items:q.items.slice(0,2)}, {status:'failed',type:'',searchApplied:false})}),/incomplete_debug_latest/);

const runs=envelope('scheduler_runs',schedulerRunsSnapshot(d));
assert.equal(runs.recentItems.returned,10);
assert.equal(runs.recentItems.items[0].runId,'RUN_19');
assert.equal(runs.recentItems.items[0].finishedAt,null);
assert.equal(runs.summary.totalMatched,null,'unknown total must not be fabricated');
const attempts=envelope('scheduler_attempts',schedulerAttemptsSnapshot(d));
assert.equal(attempts.recentItems.returned,10);
assert.equal(attempts.recentItems.items[0].attemptId,'ATT_49');
assert.equal(attempts.recentItems.items[0].costUsd,null);
assert.equal(attempts.recentItems.items[0].reservedUsd,0.003);
assert.equal(attempts.recentItems.items.at(-1).attemptId,'ATT_40');
assert.ok(!JSON.stringify(attempts).includes(secret));
console.log('S11-DIAG-02: overview, 25->10 queue, runs and attempts, confidential fields, unknown spend passed');
