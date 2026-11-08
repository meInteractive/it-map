import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

const {default:CopyAllDiagnostics}=await moduleFor('frontend/features/settings/CopyAllDiagnostics.tsx');
const saved=Object.fromEntries(['fetch','window','document','navigator','IS_REACT_ACT_ENVIRONMENT'].map(k=>
  [k,Object.getOwnPropertyDescriptor(globalThis,k)]));
const now=Date.parse('2026-11-08T10:00:00Z'),writes=[],requests=[];
let authorized=true,clipboardAllowed=true;
const dashboard={
  settings:{enabled:true,dayBudgetUsd:10,monthBudgetUsd:100},
  counts:{pending:2},cloudStatus:'signal_received',lastSignalAt:now,lastSuccessAt:now,
  statistics:{sources:1,found:3,accepted:2,imported:1},
  spending:{dayUsd:0.2},runs:[],attempts:[],
};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.document={visibilityState:'hidden',addEventListener(){},removeEventListener(){}};
Object.defineProperty(globalThis,'navigator',{configurable:true,value:{
  clipboard:{async writeText(json){if(!clipboardAllowed)throw Error('denied');writes.push(JSON.parse(json));}},
}});
globalThis.fetch=async(url,options={})=>{
  assert.ok(!options.method||options.method==='GET','no paid POST or mutations');
  const path=String(url);requests.push(path);
  if(path==='/api/scheduler/queue/debug?page=1&status=&type=&search=')
    return Response.json({checkedAt:now,total:1,coverage:'server_latest',items:[
      {jobId:'J_1',createdAt:now,status:'pending',type:'classify_title',sourceId:'hh',message:'CI_SECRET'},
    ]});
  if(path==='/api/firecrawl/settings')return Response.json({mode:'free'});
  if(path==='/api/firecrawl/usage?mode=free')return Response.json({error:'not ready'},{status:503});
  if(['/api/scheduler/ai-spending/debug','/api/apify/usage','/api/scheduler/issues',
     '/api/scheduler/funnel?sourceId=all&parser=all'].includes(path))
    return Response.json({error:'not ready'},{status:503});
  throw Error('unexpected GET '+path);
};
let root=null;
const flush=async()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
try{
  await act(async()=>{root=create(h(CopyAllDiagnostics,{dashboard,isAuthorized:()=>authorized}));});
  await flush();await flush();await flush();
  const button=()=>root.root.findAllByType('button')[0];
  assert.equal(button().props.disabled,false,'partial API failures must not block the full export');
  const before=requests.length;
  await act(async()=>{await button().props.onClick();});
  assert.equal(requests.length,before,'click must use cached read-only data');
  assert.equal(writes.length,1);
  assert.equal(writes[0].sections.queue.recentItems.items[0].jobId,'J_1');
  assert.equal(writes[0].sections.aiSpending.available,false);
  assert.equal(writes[0].sections.firecrawl.available,false);
  assert.equal(writes[0].sections.schedulerOverview.summary.statistics.imported,1);
  assert.ok(!JSON.stringify(writes).includes('CI_SECRET'));

  authorized=false;
  await act(async()=>{await button().props.onClick();});
  assert.equal(writes.length,1,'logout or loss of admin rights must prevent copying');
  authorized=true;clipboardAllowed=false;
  await act(async()=>{await button().props.onClick();});
  assert.equal(writes.length,1,'clipboard denial must not silently claim success');
  console.log('S11-DIAG-05 UI: cached internal GETs, partial errors, safe clipboard, rights revoked and denial passed');
}finally{
  if(root)await act(async()=>root.unmount());
  for(const [key,desc] of Object.entries(saved))if(desc)Object.defineProperty(globalThis,key,desc);else delete globalThis[key];
}
