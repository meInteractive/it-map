import {securityHeaders} from '../http.js';

// Keep the MCP request attached to its work. waitUntil is for short cleanup
// after a lost connection, not for detached long-running provider requests.
export function streamSchedulerMcpCall(request,env,ctx,rpc,runner,{heartbeatMs=8000}={}){
 const abort=new AbortController(),signal=AbortSignal.any([request.signal,abort.signal]);
 const encoder=new TextEncoder(),started=Date.now(),token=rpc.params?._meta?.progressToken;
 const progressToken=typeof token==='string'||typeof token==='number'&&Number.isFinite(token)?token:undefined;
 const modern=(request.headers.get('mcp-protocol-version')||rpc.params?._meta?.['io.modelcontextprotocol/protocolVersion'])==='2026-07-28';
 const result=value=>modern?{resultType:'complete',...value,_meta:{'io.modelcontextprotocol/serverInfo':{name:'it-map-scheduler',version:'1.2.0'}}}:value;
 let closed=false,timer,work;
 const body=new ReadableStream({
  start(controller){
   const send=text=>{if(closed)return;try{controller.enqueue(encoder.encode(text));}catch{closed=true;clearInterval(timer);abort.abort(new DOMException('MCP connection lost','AbortError'));}};
   const event=value=>send('event: message\ndata: '+JSON.stringify(value)+'\n\n');
   const progress=data=>{if(progressToken!==undefined)event({jsonrpc:'2.0',method:'notifications/progress',params:{progressToken,progress:data.processed??0,message:JSON.stringify({...data,elapsedSeconds:Math.floor((Date.now()-started)/1000)})}});};
   send(': it-map scheduler\n\n');timer=setInterval(()=>send(': keepalive\n\n'),heartbeatMs);
   work=Promise.resolve().then(()=>runner(env,{signal,onProgress:progress})).then(value=>event({jsonrpc:'2.0',id:rpc.id??null,result:result({content:[{type:'text',text:JSON.stringify(value)}]})})).catch(error=>event({jsonrpc:'2.0',id:rpc.id??null,result:result({isError:true,content:[{type:'text',text:error.status?error.message:'Не удалось завершить серверный тик. Повторите тот же requestId; сохранённые этапы не потеряны.'}]})})).finally(()=>{clearInterval(timer);if(!closed){closed=true;try{controller.close();}catch{}}});
   ctx?.waitUntil?.(work);
  },
  cancel(){closed=true;clearInterval(timer);abort.abort(new DOMException('MCP connection lost','AbortError'));}
 });
 return new Response(body,{headers:{...securityHeaders,'content-type':'text/event-stream; charset=utf-8','cache-control':'private, no-cache, no-transform','x-accel-buffering':'no'}});
}
