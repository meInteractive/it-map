import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor,nav} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx'),oldFetch=fetch;
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const a='ac1.'+'A'.repeat(43),b='ac1.'+'B'.repeat(42)+'A';
const vacancy={id:1,companyName:'Synthetic',title:'Synthetic',entityType:'company',telegramSources:[],applicationId:null};
let root,gets=0,mode='lost';const pending=[],sent=[];
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const mount=async(v=vacancy)=>{await act(async()=>{root=create(h(ui.VacancyActions,{v,entities:[],admin:false,nav}));});await flush();};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=null;};
const button=()=>root.root.findByProps({className:'vacancy-apply'});
const click=async()=>{await act(async()=>button().props.onClick());await flush();};
try {
 globalThis.fetch=async(url,options={})=>{
  if(url==='/api/applications/create-context'){gets++;return new Promise(resolve=>pending.push(resolve));}
  sent.push({url,body:JSON.parse(options.body)});
  if(mode==='lost')throw new TypeError('Synthetic lost ACK');
  if(mode==='restored')return Response.json({error:'Synthetic stale context'},{status:409});
  if(mode==='deleted')return Response.json({error:'Synthetic deleted result'},{status:410});
  return Response.json({ok:true,vacancy:{...vacancy,applicationId:7}});
 };
 await mount();assert.equal(gets,0,'No eager context GET');
 const handler=button().props.onClick;await act(async()=>{handler();handler();});assert.equal(gets,1);assert.equal(sent.length,0);assert.equal(button().props.disabled,true);
 await act(async()=>pending.shift()(Response.json({error:'Context failed'},{status:503})));await flush();assert.equal(button().props.disabled,false);
 await click();assert.equal(gets,2);await act(async()=>pending.shift()(Response.json({creationContext:a})));await flush();assert.equal(sent.length,1);assert.equal(button().props.disabled,false);assert.match(JSON.stringify(root.toJSON()),/lost ACK/);assert.match(sent[0].body.idempotencyKey,/^[a-f0-9-]{36}$/);
 ui.clearCache();await act(async()=>root.update(h(ui.VacancyActions,{v:{...vacancy,id:2,companyName:'Other'},entities:[],admin:false,nav})));
 await click();assert.equal(gets,2);assert.deepEqual(sent[1],sent[0]);assert.equal(sent[1].url,'/api/vacancies/1/apply/create');
 mode='restored';await click();assert.equal(gets,2);assert.deepEqual(sent[2],sent[0]);assert.match(JSON.stringify(root.toJSON()),/stale context/);
 mode='deleted';await click();assert.equal(gets,2);assert.deepEqual(sent[3],sent[0]);assert.match(JSON.stringify(root.toJSON()),/deleted result/);
 await stop();
 // Late acquisition from an abandoned action must not submit a POST.
 await mount();await click();const late=pending.shift();await stop();await mount();await act(async()=>late(Response.json({creationContext:a})));await flush();assert.equal(sent.length,4);assert.equal(gets,3);
 mode='success';await click();await act(async()=>pending.shift()(Response.json({creationContext:b})));await flush();assert.equal(sent.length,5);assert.equal(sent[4].body.creationContext,b);assert.notEqual(sent[4].body.idempotencyKey,sent[0].body.idempotencyKey);
 await act(async()=>root.update(h(ui.VacancyActions,{v:{...vacancy,applicationId:7},entities:[],admin:false,nav})));assert.equal(button().props.disabled,true);await click();assert.equal(sent.length,5);await stop();
 // Owner payloads come from the vacancy itself for companies, agencies and channels.
 for(const [entityType,companyName] of [['agency','Recruiter'],['channel','__telegram_channel__:1']]){
  await mount({...vacancy,entityType,companyName});const before=gets;assert.equal(gets,before);await click();assert.equal(gets,before+1);await act(async()=>pending.shift()(Response.json({creationContext:a})));await flush();assert.equal(sent.at(-1).body.companyName,companyName);await stop();
 }
 await act(async()=>{root=create(h(ui.VacancyTable,{records:Array.from({length:250},(_,i)=>({...vacancy,id:i+10})),entities:[],nav,admin:false}));});await flush();assert.equal(gets,6,'Rendering 250 rows makes no context requests');
 console.log('Actual React shortcut: lazy GET/no N+1, sync double-click lock, acquisition error/retry, lost ACK and frozen key/body/target/context through props/cache/409/410, unmount late response, fresh action, owners and disabled existing application passed.');
} finally {await stop();ui.clearCache();globalThis.fetch=oldFetch;}
