import assert from 'node:assert/strict';
import {moduleFor} from './test-support.mjs';
const {streamSchedulerRun}=await moduleFor('server/services/scheduler-stream.js');
const wait=[];let finish,started;const entered=new Promise(r=>started=r),gate=new Promise(r=>finish=r);
const response=streamSchedulerRun(new Request('https://fixture.test/run'),{},{waitUntil:p=>wait.push(p)},'fixture',{heartbeatMs:5,runner:async(_,o)=>{started();o.onProgress({processed:1,errors:0});await gate;return {processed:2,errors:0}}});
assert.match(response.headers.get('content-type'),/ndjson/);const reader=response.body.getReader(),decoder=new TextDecoder();let text=decoder.decode((await reader.read()).value);assert.match(text,/progress/);await entered;finish();for(;;){const chunk=await reader.read();if(chunk.done)break;text+=decoder.decode(chunk.value);}await Promise.all(wait);assert.match(text,/"processed":1/);assert.match(text,/"type":"done"/);assert.match(text,/"processed":2/);
let aborted=false,cleanup=false;const active=[];let enteredAbort;const ready=new Promise(r=>enteredAbort=r);
const cancelled=streamSchedulerRun(new Request('https://fixture.test/run'),{},{waitUntil:p=>active.push(p)},'cancel',{runner:async(_,o)=>{enteredAbort();await new Promise(resolve=>o.signal.addEventListener('abort',()=>{aborted=true;resolve();},{once:true}));cleanup=true;return {processed:0,errors:0};}});const cr=cancelled.body.getReader();await cr.read();await ready;await cr.cancel();await Promise.all(active);assert.equal(aborted,true);assert.equal(cleanup,true);
console.log('Manual scheduler stream: immediate progress, live counts, terminal result, waitUntil cleanup and disconnected-client abort passed');
