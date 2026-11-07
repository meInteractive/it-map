import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

const {default:AISpendingDebug}=await moduleFor('frontend/features/settings/AISpendingDebug.tsx');
const keys=['fetch','window','document','navigator','IS_REACT_ACT_ENVIRONMENT'];
const saved=Object.fromEntries(keys.map(k=>[k,Object.getOwnPropertyDescriptor(globalThis,k)]));
const requests=[],writes=[];
const now=Date.parse('2026-11-07T15:00:00Z');
const totals={confirmedUsd:0.2,unknownReservedUsd:0.1,upperEstimateUsd:0.3,
  unknownCostOperations:1,unpricedOperations:0};
const report={
 checkedAt:now,dayStart:now-12*3600000,monthStart:now-4*86400000,
 day:totals,month:totals,
 providers:{openai:totals,openrouter:totals},
 models:[{provider:'openrouter',model:'ci-model',type:'classify_title',...totals}],
 latest:[{attemptId:'ATT_01',jobId:'JOB_01',type:'classify_title',
   provider:'openrouter',model:'ci-model',status:'completed',reasonCode:'success',
   startedAt:now,finishedAt:null,reservedUsd:0.1,costUsd:null,
   message:'CI_PRIVATE_MESSAGE'}],
 total:1,duplicateAICallsAvailable:false,
};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.document={visibilityState:'hidden',addEventListener(){},removeEventListener(){}};
Object.defineProperty(globalThis,'navigator',{configurable:true,value:{
  clipboard:{async writeText(s){writes.push(JSON.parse(s));}},
}});
globalThis.fetch=async(url,options={})=>{
 assert.ok(!options.method||options.method==='GET','read-only diagnostic must not POST');
 requests.push(String(url));
 if(url==='/api/session')return Response.json({isAdmin:true});
 if(url==='/api/scheduler/ai-spending/debug')return Response.json(report);
 throw new Error('Unexpected request '+url);
};
const flush=async()=>act(async()=>{await new Promise(r=>setTimeout(r,0));});
let root;
try{
 await act(async()=>{root=create(h(AISpendingDebug,{dayBudgetUsd:5,monthBudgetUsd:20}));});
 await flush();await flush();
 const button=root.root.findByProps({'aria-label':'Копировать JSON'});
 assert.equal(button.props.disabled,false);
 const before=requests.length;
 await act(async()=>{await button.props.onClick();});
 assert.equal(requests.length,before,'copy must not perform network calls');
 assert.equal(writes.length,1);
 assert.equal(writes[0].section,'ai_spending');
 assert.equal(writes[0].summary.monthBudgetUsd,20);
 assert.equal(writes[0].summary.monthConfirmedUsd,0.2);
 assert.equal(writes[0].recentItems.items[0].costUsd,null);
 assert.ok(!JSON.stringify(writes).includes('CI_PRIVATE_MESSAGE'));
 console.log('S11-DIAG-03 AI UI: admin, cached SQL JSON, no external calls or raw attempt message passed');
}finally{
 if(root)await act(async()=>root.unmount());
 for(const [key,d] of Object.entries(saved))if(d)Object.defineProperty(globalThis,key,d);else delete globalThis[key];
}
