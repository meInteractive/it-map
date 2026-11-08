import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

const {default:Scheduler}=await moduleFor('frontend/features/settings/Scheduler.tsx');
const saved=Object.fromEntries(['fetch','window','document','navigator','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,Object.getOwnPropertyDescriptor(globalThis,k)]));
const writes=[],requests=[];
const now=Date.parse('2026-11-07T15:00:00.000Z');
const info={settings:{enabled:true,ownerName:'Owner private',dayBudgetUsd:10,monthBudgetUsd:100,revision:1,
  favoritesOnly:true,agenciesEnabled:false,sourceIds:[],intervalDays:7,vacancyAgeDays:3,priorities:{},cloudConnected:true},
  counts:{pending:2,running:0},runs:Array.from({length:14},(_,i)=>({id:'R_'+i,trigger:'automatic',started_at:now+i*1000,finished_at:now+i*1000+200,status:'completed',ready:2,processed:i,succeeded:i,errors:0,remaining:0,reason:'empty'})),
  attempts:Array.from({length:14},(_,i)=>({id:'A_'+i,job_id:'J_'+i,run_id:'R_13',type:'classify_title',source_id:'hh',started_at:now+i*1000,finished_at:now+i*1000+200,status:'completed',reason_code:'success',provider:'openrouter',model:'gpt-4o-mini',reserved_usd:0.003,cost_usd:i===13?null:0,credits:null,message:'PRIVATE_RESULT',result_json:'PRIVATE_RESULT'})),
  statistics:{sources:10,found:35,accepted:15,imported:9},
  spending:{dayUsd:0.8,monthUsd:1.2,timezone:'Europe/Moscow',byType:[]},
  sourceOptions:[],lastSignalAt:now,lastSuccessAt:now,nextSignalAt:null,cloudStatus:'signal_received',
};
const queue={total:25,limit:10,coverage:'server_latest',checkedAt:now,items:Array.from({length:10},(_,i)=>({jobId:'Q_'+(25-i),type:'classify_title',status:'failed',sourceId:'hh',reasonCode:'retry_due',createdAt:now-i*1000,updatedAt:now-i*1000,priority:10,payload:'PRIVATE_RESULT'}))};
Object.defineProperty(globalThis,'navigator',{value:{clipboard:{async writeText(v){writes.push(JSON.parse(v));}}},configurable:true});
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.document={visibilityState:'hidden',addEventListener(){},removeEventListener(){},getElementById(){return null;}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async (url,options={})=>{
  assert.ok(!options.method||options.method==='GET','diagnostic export must not POST');
  requests.push(String(url));
  const value=String(url);
  if(value==='/api/session')return Response.json({isAdmin:true,name:'Owner private'});
  if(value==='/api/scheduler')return Response.json(info);
  if(value.startsWith('/api/scheduler/queue/debug?'))return Response.json(queue);
  if(value.startsWith('/api/scheduler/queue?'))return Response.json({jobs:[],total:0,page:1,pageSize:30});
  if(value.startsWith('/api/scheduler/issues')||value.startsWith('/api/scheduler/funnel'))return Response.json({error:'not ready'},{status:503});
  throw new Error('unexpected network: '+value);
};
let root=null;
const flush=async()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
try{
  await act(async()=>{root=create(h(Scheduler,{}));});await flush();await flush();
  const buttons=()=>root.root.findAllByProps({'aria-label':'Копировать JSON'});
  assert.equal(buttons().length,1,'overview has one export button');
  await act(async()=>{await buttons()[0].props.onClick();});
  assert.equal(writes.length,1);
  assert.equal(writes[0].section,'scheduler_overview');
  assert.equal(writes[0].summary.statistics.imported,9);
  assert.equal(writes[0].summary.spending.upperEstimateUsd,0.8);

  await act(async()=>{root.root.findByProps({id:'parser-tab-diagnostics'}).props.onClick();});
  await flush();await flush();
  // Two additional read-only controls now appear in Diagnostics, and both
  // must fail closed when Go issues/funnel endpoints return 503. Keep the
  // old queue/run/attempt checks rather than dropping them.
  assert.equal(buttons().length,6,'overview, unavailable funnel/issues, queue, runs and attempts');
  const before=requests.length;
  for(const b of buttons().slice(1)) await act(async()=>{await b.props.onClick();});
  assert.equal(requests.length,before,'copy does not fetch or run stages');
  assert.equal(writes.length,6);
  assert.deepEqual(writes.slice(1).map(v=>v.section),
    ['parser_funnel','parser_issues','scheduler_queue','scheduler_runs','scheduler_attempts']);
  assert.equal(writes[1].available,false,'unavailable funnel not represented by zeros');
  assert.equal(writes[1].errorCode,'funnel_unavailable');
  assert.equal(writes[2].available,false,'unavailable P1/P2 not represented by zeros');
  assert.equal(writes[2].errorCode,'issues_unavailable');
  assert.equal(writes[3].recentItems.total,25);
  assert.equal(writes[3].dataAsOf,new Date(now).toISOString());
  assert.equal(writes[3].recentItems.returned,10);
  assert.equal(writes[4].recentItems.items[0].runId,'R_13');
  assert.equal(writes[5].recentItems.items[0].costUsd,null);
  assert.ok(!JSON.stringify(writes).includes('PRIVATE_RESULT'));
  assert.ok(requests.some(x=>x.startsWith('/api/scheduler/queue/debug?')));
  console.log('S11-DIAG-02 UI: overview, queue, runs/attempts, read-only export and safe clipboard passed');
} finally {
  if(root) await act(async()=>root.unmount());
  for(const [key,desc] of Object.entries(saved))if(desc)Object.defineProperty(globalThis,key,desc);else delete globalThis[key];
}
