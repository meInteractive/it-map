import {ApiError,invalidateMutation} from '../../api/client';
export interface SchedulerProgress {processed:number;errors:number;elapsedSeconds?:number;}
async function streamRun<T>(requestId:string,onProgress:(p:SchedulerProgress)=>void,signal:AbortSignal):Promise<T>{
 const response=await fetch('/api/scheduler/run',{method:'POST',credentials:'same-origin',signal,headers:{accept:'application/x-ndjson','content-type':'application/json'},body:JSON.stringify({requestId})});
 if(!response.ok){const data=await response.json().catch(()=>({})) as {error?:string};if(response.status===401)window.dispatchEvent(new Event('session-expired'));throw new ApiError(data.error||'Не удалось запустить цикл.',response.status);}
 if(response.headers.get('content-type')?.includes('application/json')){const run=await response.json() as T;invalidateMutation('/api/scheduler/run','POST');return run;}
 const reader=response.body?.getReader();if(!reader)throw new Error('Сервер не вернул поток состояния.');
 const decoder=new TextDecoder();let buffer='',complete=false,result:T|undefined;
 const process=(line:string)=>{if(!line.trim())return;const event=JSON.parse(line);if(event.type==='error')throw new ApiError(event.error,event.status||500);if(event.type==='progress')onProgress(event);if(event.type==='done'){complete=true;result=event.run as T;}};
 try{for(;;){const chunk=await reader.read();buffer+=decoder.decode(chunk.value,{stream:!chunk.done});let pos;while((pos=buffer.indexOf('\n'))>=0){process(buffer.slice(0,pos));buffer=buffer.slice(pos+1);}if(chunk.done)break;}if(buffer.trim())process(buffer);if(!complete)throw new Error('Соединение прервано. Проверьте сохранённый прогресс в журнале.');invalidateMutation('/api/scheduler/run','POST');return result as T;}
 finally{await reader.cancel().catch(()=>{});reader.releaseLock();}
}

// Recover a lost receipt by reading the same durable run. Never POST again here.
export async function runScheduler<T>(requestId:string,onProgress:(p:SchedulerProgress)=>void,signal:AbortSignal):Promise<T>{
 try{return await streamRun<T>(requestId,onProgress,signal);}
 catch(error){
  if(signal.aborted||error instanceof ApiError&&error.status<500)throw error;
  for(let attempt=0;attempt<3;attempt++){
   if(attempt)await new Promise<void>((resolve,reject)=>{const done=()=>{signal.removeEventListener('abort',cancel);resolve();},timer=setTimeout(done,1000),cancel=()=>{clearTimeout(timer);reject(signal.reason);};signal.addEventListener('abort',cancel,{once:true});if(signal.aborted)cancel();});
   signal.throwIfAborted();
   try{
    const response=await fetch('/api/scheduler/run?requestId='+encodeURIComponent(requestId),{credentials:'same-origin',signal:AbortSignal.any([signal,AbortSignal.timeout(10000)]),headers:{accept:'application/json'}});
    if(!response.ok)break;
    const run=await response.json() as {status?:string;finished_at?:number;processed:number;errors:number};
    if(run.finished_at||run.status==='paused'){invalidateMutation('/api/scheduler/run','POST');return run as T;}
    onProgress(run);
   }catch{if(signal.aborted)throw signal.reason;break;}
  }
  throw error;
 }
}
