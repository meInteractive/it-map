import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

const {ApifyBudgets}=await moduleFor('frontend/features/settings/Apify.tsx');
const {FirecrawlBudget}=await moduleFor('frontend/features/settings/Firecrawl.tsx');
const saved=Object.fromEntries(['fetch','window','document','navigator','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,Object.getOwnPropertyDescriptor(globalThis,k)]));
const requests=[],writes=[];
const now=Date.now();
const apify={
  providerLimitMode:true,
  profiles:{
    free:{providerUsedUsd:3,ownConfirmedUsd:2,unknownReserveUsd:0.5,readReserveUsd:0.1,
      unknownRuns:1,limitUsd:10,remainingUsd:6,periodStart:'2026-11-01T00:00:00Z',
      periodEnd:'2026-12-01T00:00:00Z',checkedAt:now,accountId:'HIDDEN_ACCOUNT'},
    starter:null,
  },
  active:[{id:'OP01',profile:'free',state:'uncertain',run_id:'HIDDEN_RUN',created_at:now}],
};
const firecrawl={mode:'free',fresh:true,checkedAt:now,status:'available',reason:'SECRET_REASON',
  limits:{credits:1000,scrapePerMinute:10,appConcurrency:1},
  budget:{providerPlanCredits:1000,providerRemaining:900,providerUsedEstimate:100,
    localReported:30,localEstimated:45,reserved:15,effectiveUsed:100,remaining:900,
    periodStart:'2026-11-01T00:00:00Z',periodEnd:'2026-12-01T00:00:00Z'}};
Object.defineProperty(globalThis,'navigator',{value:{clipboard:{async writeText(value){writes.push(JSON.parse(value));}}},configurable:true});
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.document={visibilityState:'hidden',addEventListener(){},removeEventListener(){},getElementById(){return null;}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async (url,opts={})=>{
  assert.ok(!opts.method||opts.method==='GET','JSON export must never invoke provider POST');
  const path=String(url);
  requests.push(path);
  if(path==='/api/session')return Response.json({isAdmin:true});
  if(path==='/api/apify/usage')return Response.json(apify);
  if(path==='/api/firecrawl/usage?mode=free')return Response.json(firecrawl);
  throw new Error('Unexpected request: '+path);
};
const flush=async()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
let a=null,f=null;
try{
  await act(async()=>{a=create(h(ApifyBudgets));});
  await flush();await flush();
  const ab=a.root.findByProps({'aria-label':'Копировать JSON'});
  assert.equal(ab.props.disabled,false);
  const before=requests.length;
  await act(async()=>{await ab.props.onClick();});
  assert.equal(requests.length,before,'copy has no network side effects');
  assert.equal(writes[0].section,'apify_usage');
  assert.equal(writes[0].summary.profiles.free.ownConfirmedUsd,2);
  assert.equal(writes[0].summary.operationHistoryAvailable,false);
  assert.ok(!JSON.stringify(writes[0]).includes('HIDDEN_RUN'));
  assert.ok(!JSON.stringify(writes[0]).includes('HIDDEN_ACCOUNT'));

  await act(async()=>{f=create(h(FirecrawlBudget,{mode:'free'}));});
  await flush();await flush();
  const fb=f.root.findByProps({'aria-label':'Копировать JSON'});
  assert.equal(fb.props.disabled,false);
  const beforeFirecrawl=requests.length;
  await act(async()=>{await fb.props.onClick();});
  assert.equal(requests.length,beforeFirecrawl,'copy must not refresh Firecrawl usage');
  assert.equal(writes[1].section,'firecrawl_usage');
  assert.equal(writes[1].summary.reportedCredits,30);
  assert.equal(writes[1].summary.reservedCredits,15);
  assert.ok(!JSON.stringify(writes[1]).includes('SECRET_REASON'));
  assert.ok(!requests.includes('/api/firecrawl/usage/refresh'));
  assert.equal(writes.length,2);
  console.log('S11-DIAG-03 UI: Apify and Firecrawl clipboard from cached GET, no POST or private fields passed');
}finally{
  if(a)await act(async()=>a.unmount());
  if(f)await act(async()=>f.unmount());
  for(const [k,v] of Object.entries(saved))if(v)Object.defineProperty(globalThis,k,v);else delete globalThis[k];
}
