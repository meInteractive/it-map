import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

const {default:ParserIssues}=await moduleFor('frontend/features/settings/ParserIssues.tsx');
const {default:ParserFunnel}=await moduleFor('frontend/features/settings/ParserFunnel.tsx');
const descriptors=Object.fromEntries(['fetch','window','document','navigator','IS_REACT_ACT_ENVIRONMENT'].map(k=>[
  k,Object.getOwnPropertyDescriptor(globalThis,k)]));
const now=Date.parse('2026-11-07T15:00:00.000Z'),writes=[],requests=[];
const dates=Array.from({length:30},(_,i)=>new Date(Date.UTC(2026,9,9+i)).toISOString().slice(0,10));
const counts={received:2,accepted:1,fulltext:2,structured:1,roles:1,published:1};
const report={
  checkedAt:now,timezone:'Europe/Moscow',from:dates[0],to:dates[29],days:30,
  filters:{sourceId:'all',parser:'all'},
  sourceOptions:[{id:'hh',name:'HH'}],parserOptions:[{id:'apify',name:'Apify'}],
  totals:counts,averages:Object.fromEntries(Object.entries(counts).map(([k,v])=>[k,v/30])),
  cohort:{...counts,rejected:0,error:0,waiting:1,created:1,linked:0,undatedPublished:0},
  daily:dates.map((day,i)=>({day,...Object.fromEntries(Object.entries(counts).map(([k,v])=>[k,i===29?v:0]))})),
};
const issues={checkedAt:now,issues:[{
  id:'p2_fail',priority:2,status:'observed',title:'CI PRIVATE TITLE',
  description:'CI SECRET DETAILS',nextAction:'Review',target:'queue',
  stage:'scan_source',sourceId:'hh',reasonCode:'invalid_output',
  observedAt:now,jobs:[{id:'CI_PRIVATE',message:'CI SECRET DETAILS',companyName:'CI CUSTOMER'}],
}],historicalFailed:1,sampleLimited:false,limit:120,activeCounts:{p1:0,p2:1},
history:{timezone:'Europe/Moscow',metric:'daily_peak_active',days:30,observedDays:1,
  average:{p1:0,p2:1},points:dates.map((day,i)=>({
    day,p1:i===29?0:null,p2:i===29?1:null,observedAt:i===29?now:null,
  }))}};
let authorized=true;
Object.defineProperty(globalThis,'navigator',{configurable:true,value:{
  clipboard:{async writeText(value){writes.push(JSON.parse(value));}},
}});
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.document={visibilityState:'hidden',addEventListener(){},removeEventListener(){},getElementById(){return null;}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{
  assert.ok(!options.method||options.method==='GET','export must never initiate paid POST');
  const path=String(url);requests.push(path);
  if(path.startsWith('/api/scheduler/funnel?')){
    const params=new URL(path,'https://fixture').searchParams;
    return Response.json({...report,filters:{sourceId:params.get('sourceId'),parser:params.get('parser')}});
  }
  throw new Error('unexpected request '+path);
};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
let issueRoot=null,funnelRoot=null;
try {
  await act(async()=>{issueRoot=create(h(ParserIssues,{
    report:issues,loading:false,onReload(){},onOpen(){},isAuthorized:()=>authorized,
  }));});
  const issueButton=issueRoot.root.findByProps({'aria-label':'Копировать JSON'});
  assert.equal(issueButton.props.disabled,false);
  await act(async()=>{await issueButton.props.onClick();});
  assert.equal(writes.length,1);
  assert.equal(writes[0].section,'parser_issues');
  assert.equal(writes[0].daily.length,30);
  assert.equal(writes[0].summary.p2Breakdown.available,false);
  assert.ok(!JSON.stringify(writes[0]).includes('CI PRIVATE'));
  assert.ok(!JSON.stringify(writes[0]).includes('CI SECRET'));
  assert.equal(requests.length,0,'P1/P2 clipboard reads passed report only');
  authorized=false;
  await act(async()=>{await issueButton.props.onClick();});
  assert.equal(writes.length,1,'copy must recheck admin privileges');
  authorized=true;

  await act(async()=>{funnelRoot=create(h(ParserFunnel,{isAuthorized:()=>authorized}));});
  await flush();await flush();
  let button=funnelRoot.root.findByProps({'aria-label':'Копировать JSON'});
  assert.equal(button.props.disabled,false);
  const before=requests.length;
  await act(async()=>{await button.props.onClick();});
  assert.equal(requests.length,before,'funnel clipboard must reuse cached GET');
  assert.equal(writes[1].section,'parser_funnel');
  assert.deepEqual(writes[1].filters,{sourceId:'all',parser:'all'});
  assert.equal(writes[1].daily.length,30);
  assert.equal(writes[1].summary.newVacanciesPublished,1);

  await act(async()=>{funnelRoot.root.findByProps({'aria-label':'Источник воронки'})
    .props.onChange({target:{value:'hh'}});});
  await flush();await flush();
  button=funnelRoot.root.findByProps({'aria-label':'Копировать JSON'});
  assert.equal(button.props.disabled,false,'new server response matches current source filter');
  await act(async()=>{await button.props.onClick();});
  assert.deepEqual(writes.at(-1).filters,{sourceId:'hh',parser:'all'});

  await act(async()=>{funnelRoot.root.findByProps({'aria-label':'Парсер воронки'})
    .props.onChange({target:{value:'apify'}});});
  await flush();await flush();
  await act(async()=>{await funnelRoot.root.findByProps({'aria-label':'Копировать JSON'}).props.onClick();});
  assert.deepEqual(writes.at(-1).filters,{sourceId:'hh',parser:'apify'});
  assert.ok(requests.every(path=>path.startsWith('/api/scheduler/funnel?')));
  console.log('S11-DIAG-04 UI: P1/P2 and funnel clipboard safe, filters correct, 30 days, admin recheck, no extra GET or POST');
} finally {
  if(issueRoot)await act(async()=>issueRoot.unmount());
  if(funnelRoot)await act(async()=>funnelRoot.unmount());
  for(const [k,d] of Object.entries(descriptors))if(d)Object.defineProperty(globalThis,k,d);else delete globalThis[k];
}
