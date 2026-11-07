import assert from 'node:assert/strict';
import {serializeDebugJSON} from '../shared/debug-json.mjs';
import {aiSpendingSnapshot} from '../shared/ai-spending-debug.mjs';

const now=Date.parse('2026-11-07T15:00:00.000Z');
const secret=['sk','proj','PRIVATE'].join('-');
const sum=(confirmedUsd,unknownReservedUsd,unknownCostOperations=0,unpricedOperations=0)=>({
  confirmedUsd,unknownReservedUsd,upperEstimateUsd:confirmedUsd+unknownReservedUsd,
  unknownCostOperations,unpricedOperations,
});
const models=Array.from({length:12},(_,i)=>({
 provider:i%2?'openrouter':'openai',model:'model-'+String(i).padStart(2,'0'),
 type:'classify_title',...sum(i*0.01,0.002),
 rawPrompt:secret,
}));
const latest=Array.from({length:10},(_,i)=>({
 attemptId:'ATT_'+i,jobId:'JOB_'+i,runId:'RUN_'+i,
 type:'classify_title',provider:i%2?'openrouter':'openai',model:'model-00',
 status:'completed',reasonCode:'success',startedAt:now+i*1000,finishedAt:null,
 reservedUsd:0.1,costUsd:i===9?null:0.01,credits:null,
 message:secret,result_json:secret,payload:secret,apiKey:secret,
}));
const report={
 checkedAt:now,dayStart:now-12*3600000,monthStart:now-7*86400000,
 day:sum(0.5,0.1,1),month:sum(2,0.3,3,2),
 providers:{openai:sum(1.5,0.1,1),openrouter:sum(0.5,0.2,2)},
 models,latest,total:16,duplicateAICallsAvailable:false,
};
const json=JSON.parse(serializeDebugJSON({section:'ai_spending',capturedAt:now,
 ...aiSpendingSnapshot(report,{dayBudgetUsd:10,monthBudgetUsd:100})}));
assert.equal(json.section,'ai_spending');
assert.equal(json.summary.dayBudgetUsd,10);
assert.equal(json.summary.monthConfirmedUsd,2);
assert.equal(json.summary.monthUnknownReservedUsd,0.3);
assert.equal(json.summary.monthUpperEstimateUsd,2.3);
assert.equal(json.summary.providers.openai.confirmedUsd,1.5);
assert.equal(json.summary.providers.openrouter.unknownReservedUsd,0.2);
assert.equal(json.summary.duplicateAICallsAvailable,false);
assert.equal(json.recentItems.total,16);
assert.equal(json.recentItems.returned,10);
assert.equal(json.recentItems.items[0].attemptId,'ATT_9');
assert.equal(json.recentItems.items[0].costUsd,null);
assert.equal(json.breakdown.total,12);
assert.equal(json.breakdown.returned,10);
assert.equal(json.breakdown.truncated,true);
assert.equal(json.breakdown.items[0].model,'model-00');
assert.ok(!JSON.stringify(json).includes(secret));
assert.ok(!JSON.stringify(json).includes('rawPrompt'));
assert.throws(()=>serializeDebugJSON({section:'ai_spending',capturedAt:now,
 ...aiSpendingSnapshot({...report,latest:latest.slice(0,2)},{dayBudgetUsd:10,monthBudgetUsd:100})}),/incomplete_debug_latest/);
console.log('S11-DIAG-03 AI: monthly confirmed/unknown, providers, models, 10 latest, unknown null, no sensitive fields passed');
