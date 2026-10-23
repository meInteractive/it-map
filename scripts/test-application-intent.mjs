import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx'),oldFetch=fetch;
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const contextA='ac1.'+'A'.repeat(43),contextB='ac1.'+'B'.repeat(42)+'A';
let root,contextMode='hold',postMode='lost',saved=0;const contexts=[],sent=[];let contextGets=0;
const props={companyName:'Synthetic',onSaved(){saved++;}};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const mount=async(extra={})=>{await act(async()=>{root=create(h(ui.ApplicationForm,{...props,...extra}));});await flush();};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=null;};
const submit=()=>act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
const type=comment=>act(async()=>root.root.findByType('textarea').props.onChange({target:{value:comment}}));
const saveDisabled=()=>root.root.findByProps({className:'crm-save'}).props.disabled;
const retry=()=>root.root.findAllByType('button').find(button=>button.children.includes('Повторить подготовку'));
try{
 globalThis.fetch=async(url,options={})=>{
  if(url==='/api/applications/create-context'){
   contextGets++;if(contextMode==='hold')return new Promise(resolve=>contexts.push(resolve));
   return Response.json({creationContext:contextA});
  }
  assert.ok(options.method==='POST'||options.method==='PUT');sent.push({url,method:options.method,body:JSON.parse(options.body)});
  if(postMode==='lost')throw new TypeError('Synthetic lost ACK');
  if(postMode==='conflict')return Response.json({error:'Synthetic intent conflict',code:'intent_conflict'},{status:409});
  if(postMode==='restored')return Response.json({error:'Synthetic stale context',code:'creation_context_expired'},{status:409});
  return Response.json({records:[],createdApplicationId:1,replayed:false},{status:options.method==='POST'?201:200});
 };
 // Inputs remain editable while GET is pending; even a direct submit sends no POST.
 await mount();assert.equal(contextGets,1);assert.equal(saveDisabled(),true);assert.equal(root.root.findByProps({className:'action-form-fields'}).props.disabled,false);await type('Draft during context load');await submit();assert.equal(sent.length,0);assert.equal(root.root.findByType('textarea').props.value,'Draft during context load');
 await act(async()=>contexts.shift()(Response.json({error:'Synthetic context failure'},{status:503})));await flush();assert.equal(saveDisabled(),true);assert.ok(retry());
 // Explicit retry only for acquisition failure; two clicks from one render = one GET.
 const click=retry().props.onClick;await act(async()=>{click();click();});assert.equal(contextGets,2);await type('Draft during retry');await act(async()=>contexts.shift()(Response.json({creationContext:contextA})));await flush();assert.equal(saveDisabled(),false);
 await submit();assert.equal(sent.length,1);assert.equal(sent[0].url,'/api/applications/create');assert.match(sent[0].body.idempotencyKey,/^[a-f0-9-]{36}$/);assert.equal(sent[0].body.creationContext,contextA);assert.equal(saved,0);
 // Cache invalidation and refreshed props cannot redirect/reset the original draft.
 ui.clearCache();await act(async()=>root.update(h(ui.ApplicationForm,{...props,companyName:'Other',vacancyId:999})));await type('Changed after lost ACK');postMode='conflict';await submit();assert.equal(saved,0);assert.equal(saveDisabled(),false);assert.equal(root.root.findByType('textarea').props.value,'Changed after lost ACK');assert.equal(sent[1].body.companyName,'Synthetic');assert.equal(sent[1].body.vacancyId,null);assert.equal(sent[1].body.idempotencyKey,sent[0].body.idempotencyKey);assert.equal(sent[1].body.creationContext,contextA);assert.equal(contextGets,2);
 postMode='restored';await submit();assert.equal(contextGets,2);assert.equal(sent[2].body.idempotencyKey,sent[0].body.idempotencyKey);assert.equal(sent[2].body.creationContext,contextA);assert.equal(root.root.findByType('textarea').props.value,'Changed after lost ACK');assert.equal(saved,0);await stop();
 // Unmounted pending GET must not populate a newly opened draft.
 contextMode='hold';await mount();const old=contexts.shift();await type('Abandoned');await stop();await mount();await type('New draft');const current=contexts.shift();await act(async()=>old(Response.json({creationContext:contextA})));await flush();assert.equal(saveDisabled(),true);assert.equal(root.root.findByType('textarea').props.value,'New draft');await act(async()=>current(Response.json({creationContext:contextB})));await flush();postMode='success';await submit();assert.equal(sent.at(-1).body.creationContext,contextB);assert.notEqual(sent.at(-1).body.idempotencyKey,sent[0].body.idempotencyKey);assert.equal(saved,1);await stop();
 // Existing PUT/revision obtains no creation key/context and keeps editing behavior.
 const gets=contextGets,record={id:7,revision:'a'.repeat(64),companyName:'Synthetic',status:'Откликнулся',comment:'Existing',vacancyId:null,applicationSources:['hh'],history:[]};await mount({record});await type('Edit');await submit();assert.equal(contextGets,gets);assert.equal(sent.at(-1).url,'/api/applications/7');assert.equal(sent.at(-1).method,'PUT');assert.equal(sent.at(-1).body.revision,record.revision);assert.ok(!Object.hasOwn(sent.at(-1).body,'creationContext'));assert.ok(!Object.hasOwn(sent.at(-1).body,'idempotencyKey'));
 console.log('Actual React: context wait/error/explicit double-click retry, editable draft, no premature POST, frozen UUID/context/target after lost ACK/props/cache/409/restore, late unmount response, fresh draft and unchanged PUT revision passed.');
}finally{await stop();ui.clearCache();globalThis.fetch=oldFetch;}
