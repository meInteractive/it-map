import {runDueTasks} from './scheduler-dispatcher.js';
import {securityHeaders} from '../http.js';
// Stream a manual cycle while it runs. After disconnect, abort external work
// and use waitUntil only to persist cleanup, never as a long-running queue.
export function streamSchedulerRun(request,env,ctx,requestId,{runner=runDueTasks,heartbeatMs=8000}={}){
 const abort=new AbortController(),signal=AbortSignal.any([request.signal,abort.signal]),encoder=new TextEncoder();let closed=false,timer,work,latest={processed:0,errors:0};
 const body=new ReadableStream({
  start(controller){
   const started=Date.now();
   const send=(type,data)=>{if(closed)return;try{controller.enqueue(encoder.encode(JSON.stringify({type,...data})+'\n'));}catch{closed=true;abort.abort(new DOMException('Соединение прервано','AbortError'));clearInterval(timer);}};
   const progress=data=>{latest={...latest,...data};send('progress',{...latest,elapsedSeconds:Math.floor((Date.now()-started)/1000)});};
   progress({});timer=setInterval(()=>progress({}),heartbeatMs);
   work=Promise.resolve().then(()=>runner(env,{trigger:'manual',requestId,signal,onProgress:progress})).then(run=>send('done',{run})).catch(error=>send('error',{error:error.status?error.message:'Не удалось завершить цикл. Сохранённые этапы доступны в очереди.',status:error.status||500,code:error.code||'storage'})).finally(()=>{clearInterval(timer);if(!closed){closed=true;try{controller.close();}catch{}}});
   ctx?.waitUntil?.(work);
  },
  cancel(){closed=true;clearInterval(timer);abort.abort(new DOMException('Соединение прервано','AbortError'));return work;}
 });
 return new Response(body,{headers:{...securityHeaders,'content-type':'application/x-ndjson; charset=utf-8','cache-control':'private, no-cache, no-transform','x-accel-buffering':'no'}});
}
