import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';
const {default:TargetHH}=await moduleFor('frontend/features/settings/TargetHH.tsx');
const previous={fetch:globalThis.fetch,window:globalThis.window,sessionStorage:globalThis.sessionStorage,IS_REACT_ACT_ENVIRONMENT:globalThis.IS_REACT_ACT_ENVIRONMENT};
const storage=new Map(),calls=[];let failed=false,root,stageResults=[],blockNext=false,unblock,stageFail=false;
globalThis.IS_REACT_ACT_ENVIRONMENT=true;globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.sessionStorage={getItem:k=>storage.get(k)??null,setItem:(k,v)=>storage.set(k,v),removeItem:k=>storage.delete(k)};
globalThis.fetch=async(url,options={})=>{if(!options.method)return Response.json({sources:[{companyId:1,companyName:'CI Employer',enabled:true,supported:true}]});const body=JSON.parse(options.body);calls.push({url,body});if(url.endsWith('/stage')&&blockNext){blockNext=false;await new Promise(resolve=>{unblock=resolve;});}if(url.endsWith('/stage')&&stageFail){stageFail=false;return Response.json({error:'retry'},{status:503});}if(url==='/api/scheduler/target-hh'&&!failed){failed=true;return Response.json({error:'retry'},{status:503});}return Response.json({...(url==='/api/scheduler/target-hh'?{jobId:'12345678-1234-4234-8234-123456789abc'}:{}),processed:url.endsWith('/stage')&&stageResults.length?stageResults.shift():1,errors:0,status:'completed'});};
const button=name=>root.root.findAllByType('button').find(b=>b.children.join('')===name);
try{
 await act(async()=>{root=create(h(TargetHH));});await act(async()=>root.root.findByType('select').props.onChange({target:{value:'1'}}));
 await act(async()=>{button('Запустить проверку HH').props.onClick();await new Promise(r=>setTimeout(r,0));});
 assert.ok(button('Повторить запрос с тем же идентификатором'));await act(async()=>root.unmount());await act(async()=>{root=create(h(TargetHH));});
 await act(async()=>{button('Повторить запрос с тем же идентификатором').props.onClick();await new Promise(r=>setTimeout(r,0));});
 await act(async()=>{button('Продолжить получение списка').props.onClick();await new Promise(r=>setTimeout(r,0));});
 await act(async()=>{button('Обработать следующий этап').props.onClick();await new Promise(r=>setTimeout(r,0));});
 assert.equal(calls.length,4);assert.equal(calls[0].body.requestId,calls[1].body.requestId);assert.equal(calls[2].body.scanRequestId,calls[0].body.requestId);assert.equal(calls[3].body.scanJobId,'12345678-1234-4234-8234-123456789abc');assert.equal(calls[3].body.companyId,1);
 stageResults=[1,1,0];await act(async()=>{button('Обработать доступные этапы').props.onClick();await new Promise(r=>setTimeout(r,20));});
 assert.equal(calls.length,7);assert.ok(calls.slice(4).every(c=>c.url==='/api/scheduler/target-hh/stage'&&c.body.companyId===1&&c.body.scanJobId==='12345678-1234-4234-8234-123456789abc'));assert.equal(new Set(calls.slice(4).map(c=>c.body.requestId)).size,3);assert.match(JSON.stringify(root.toJSON()),/Выполнено этапов: 2/);
 const beforeCap=calls.length;await act(async()=>{button('Обработать доступные этапы').props.onClick();await new Promise(r=>setTimeout(r,25));});assert.equal(calls.length-beforeCap,30);assert.match(JSON.stringify(root.toJSON()),/Выполнено этапов: 30/);
 blockNext=true;const beforeStop=calls.length;await act(async()=>{button('Обработать доступные этапы').props.onClick();await new Promise(r=>setTimeout(r,0));});assert.equal(typeof unblock,'function');await act(async()=>button('Остановить после текущего этапа').props.onClick());await act(async()=>{unblock();await new Promise(r=>setTimeout(r,10));});assert.equal(calls.length-beforeStop,1);assert.match(JSON.stringify(root.toJSON()),/Остановлено после этапов: 1/);
 stageFail=true;await act(async()=>{button('Обработать доступные этапы').props.onClick();await new Promise(r=>setTimeout(r,10));});const lost=calls.at(-1).body.requestId;assert.ok(button('Повторить запрос с тем же идентификатором'));await act(async()=>{button('Повторить запрос с тем же идентификатором').props.onClick();await new Promise(r=>setTimeout(r,10));});assert.equal(calls.at(-1).body.requestId,lost);


 console.log('Target HH UI: scoped batch stops on empty, caps at 30, stops on request and reuses lost request IDs.');
}finally{if(root)await act(async()=>root.unmount());Object.assign(globalThis,previous);}
