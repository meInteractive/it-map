import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';

const {default:CopyDebugJSON}=await moduleFor('frontend/features/settings/CopyDebugJSON.tsx');
const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator');
const oldFetch=globalThis.fetch,oldAct=globalThis.IS_REACT_ACT_ENVIRONMENT;
let root=null;
const calls=[],at='2026-11-07T18:00:00.000Z',secret=['sk','proj','PRIVATE'].join('-');
let allowed=true,forceError=false;
Object.defineProperty(globalThis,'navigator',{
 configurable:true,value:{clipboard:{async writeText(value){if(forceError)throw new Error('denied');calls.push(value);}}},
});
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=()=>{throw new Error('copy must never issue HTTP requests')};
const props={
 section:'scheduler_queue',
 getSnapshot:()=>({capturedAt:at,filters:{status:'pending',secret},
   summary:{totalMatched:1},items:[{jobId:'CI_JOB_1',type:'scan_source',status:'pending',createdAt:at,
     sourceId:'hh',message:secret,payload:{token:'secret'}}],total:1}),
 isAuthorized:()=>allowed,
};
try {
 const markup=render(CopyDebugJSON,props);
 assert.match(markup,/type="button"/);
 assert.match(markup,/Копировать JSON/);
 assert.match(markup,/role="status"/);
 assert.match(markup,/aria-live="polite"/);
 await act(async()=>{root=create(h(CopyDebugJSON,props));});
 const button=()=>root.root.findByType('button');
 const status=()=>root.root.findByProps({role:'status'});
 await act(async()=>{await button().props.onClick();});
 assert.equal(calls.length,1);
 assert.equal(status().children.join(''),'JSON скопирован');
 const result=JSON.parse(calls[0]);
 assert.equal(result.section,'scheduler_queue');
 assert.equal(result.recentItems.items[0].jobId,'CI_JOB_1');
 assert.ok(!calls[0].includes('PRIVATE'));
 assert.ok(!calls[0].includes('payload'));
 assert.ok(!calls[0].includes('message'));

 forceError=true;
 await act(async()=>{await button().props.onClick();});
 assert.equal(calls.length,1);
 assert.equal(status().children.join(''),'Не удалось скопировать JSON');
 forceError=false;

 allowed=false;
 await act(async()=>{await button().props.onClick();});
 assert.equal(calls.length,1);
 assert.notEqual(status().children.join(''),'JSON скопирован');

 allowed=true;
 let unblock;
 globalThis.navigator.clipboard.writeText=()=>new Promise(resolve=>{unblock=()=>{calls.push('pending');resolve();};});
 let promise;
 await act(async()=>{promise=button().props.onClick();await Promise.resolve();});
 assert.equal(button().props.disabled,true);
 allowed=false;
 await act(async()=>{unblock();await promise;});
 assert.notEqual(status().children.join(''),'JSON скопирован','cannot claim success after logout');
 await act(async()=>{root.unmount();});root=null;

 const disabled=render(CopyDebugJSON,{...props,disabled:true});
 assert.match(disabled,/disabled=""/);
 console.log('S11-DIAG-01 React UI: button, clipboard success/error, no external requests, auth/session guards, accessible status passed.');
} finally {
 if(root)await act(async()=>root.unmount());
 if(oldNavigator)Object.defineProperty(globalThis,'navigator',oldNavigator);
 else delete globalThis.navigator;
 globalThis.fetch=oldFetch;
 globalThis.IS_REACT_ACT_ENVIRONMENT=oldAct;
}
