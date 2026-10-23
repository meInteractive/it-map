import assert from 'node:assert/strict';
import {moduleFor} from './test-support.mjs';
const {runScheduler}=await moduleFor('frontend/features/settings/scheduler-run.ts');
const oldFetch=globalThis.fetch,oldWindow=globalThis.window;globalThis.window={dispatchEvent(){}};
const key='11111111-1111-4111-8111-111111111111',signal=new AbortController().signal;
let writes=0,reads=0;
try{
 globalThis.fetch=async(url,options={})=>{
  if(options.method==='POST'){writes++;return new Response('{"type":"progress","processed":2,"errors":0}\n',{headers:{'content-type':'application/x-ndjson'}});}
  assert.equal(url,'/api/scheduler/run?requestId='+key);reads++;return Response.json({status:'interrupted',finished_at:1,processed:2,errors:0,reason:'timeout'});
 };
 const recovered=await runScheduler(key,()=>{},signal);assert.equal(recovered.processed,2);assert.equal(recovered.status,'interrupted');assert.deepEqual([writes,reads],[1,1],'lost stream receipt performs read-only recovery with identical intent');
 writes=0;reads=0;globalThis.fetch=async(_,options={})=>{if(options.method==='POST')writes++;else reads++;return Response.json({error:'Доступ запрещён'},{status:403});};
 await assert.rejects(runScheduler(key,()=>{},signal),/Доступ запрещён/);assert.deepEqual([writes,reads],[1,0],'authorization errors are not masked');
 const stopped=new AbortController();globalThis.fetch=async()=>{stopped.abort(new DOMException('Closed','AbortError'));throw stopped.signal.reason;};await assert.rejects(runScheduler(key,()=>{},stopped.signal),e=>e.name==='AbortError');
 console.log('Manual run client: interrupted NDJSON recovers persisted status with one POST, no duplicate work, explicit authorization/cancellation preserved');
}finally{globalThis.fetch=oldFetch;globalThis.window=oldWindow;}
