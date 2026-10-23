import {databaseExportTicket} from '../import-export/database-dump.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
import {schedulerSettings} from '../repositories/scheduler.js';
import {runScheduledSeriesTick} from './scheduler-series.js';
import {schedulerDashboard,schedulerProbe} from './scheduler-route.js';
import {readClassificationInput} from '../repositories/classification-prompts.js';
import {jsonResponse} from '../http.js';
import {streamSchedulerMcpCall} from './scheduler-mcp-stream.js';
import {parserIssues} from './parser-issues.js';
import {parserDiagnostics} from './parser-diagnostics.js';
import {parserFunnel} from './parser-funnel.js';
const supportedVersions=['2026-07-28','2025-11-25','2025-06-18','2025-03-26','2024-11-05'];
const serverInfo={name:'it-map-scheduler',version:'1.2.0'};
const tools=[
 {name:'scheduler_tick',description:'Без requestId подготовить новую серверную серию it-map без обработки. Передавать continuationRequestId в следующий вызов requestId до null. До пяти рабочих тиков после подготовки. При потере ответа рабочего тика повторить тот же requestId. Очередь и бюджеты на сервере.',inputSchema:{type:'object',properties:{requestId:{type:'string',maxLength:100}},additionalProperties:false}},
 {name:'scheduler_status',description:'Прочитать состояние серверного планировщика it-map и сохранённые результаты последнего цикла.',inputSchema:{type:'object',properties:{},additionalProperties:false}},
 {name:'scheduler_probe',description:'Безопасно проверить запись диагностического задания и выполнение диспетчером. Без Firecrawl, JEV, LLM и импорта.',inputSchema:{type:'object',properties:{},additionalProperties:false}}
];
export async function schedulerMcpRoute(request,env,ctx){
 if(request.method!=='POST')return jsonResponse({error:'Use POST'},405);
 let rpc;try{rpc=await readClassificationInput(request)}catch{return jsonResponse({error:'Invalid JSON-RPC'},400)}
 const modern=rpc.method==='server/discover'||(request.headers.get('mcp-protocol-version')||rpc.params?._meta?.['io.modelcontextprotocol/protocolVersion'])==='2026-07-28';
 const reply=result=>jsonResponse({jsonrpc:'2.0',id:rpc.id??null,result:modern?{resultType:'complete',...result,_meta:{'io.modelcontextprotocol/serverInfo':serverInfo}}:result});
 if(rpc.method==='server/discover')return reply({supportedVersions,capabilities:{tools:{listChanged:false}},ttlMs:300000,cacheScope:'public'});
 if(rpc.method==='notifications/initialized'||rpc.method==='notifications/cancelled')return new Response(null,{status:204});
 if(rpc.method==='initialize')return reply({protocolVersion:supportedVersions.includes(rpc.params?.protocolVersion)?rpc.params.protocolVersion:'2025-03-26',capabilities:{tools:{listChanged:false}},serverInfo});
 if(rpc.method==='ping')return reply({});
 if(rpc.method==='tools/list')return reply(modern?{tools,ttlMs:300000,cacheScope:'public'}:{tools});
 if(rpc.method!=='tools/call')return jsonResponse({jsonrpc:'2.0',id:rpc.id??null,error:{code:-32601,message:'Method not found'}},400);
 const id=request.headers.get('oai-authenticated-user-id');if(!id)return jsonResponse({error:'Требуется подключение ChatGPT.'},401);
 try{
  env={...env,DB:await candidateDb(env.DB)};const settings=await schedulerSettings(env.DB);
  if(!settings.cloudOwnerId||settings.cloudOwnerId!==id)return jsonResponse({error:'Облачный вызов не привязан к администратору it-map.'},403);
  const name=rpc.params?.name,args=rpc.params?.arguments||{};
  if(!tools.some(t=>t.name===name)||Object.keys(args).some(k=>k!=='requestId'||name!=='scheduler_tick')||args.requestId!==undefined&&(typeof args.requestId!=='string'||args.requestId.length>100))return reply({isError:true,content:[{type:'text',text:'Некорректный инструмент или параметры.'}]});
  const runner=(_,options)=>name==='scheduler_tick'?runScheduledSeriesTick(env,args.requestId,options):schedulerProbe(env,undefined,options);
  if(name!=='scheduler_status'&&request.headers.get('accept')?.includes('text/event-stream'))return streamSchedulerMcpCall(request,env,ctx,rpc,runner);
  const work=name==='scheduler_status'?Promise.all([schedulerDashboard(env.DB),parserDiagnostics(env,{signal:request.signal}),parserFunnel(env.DB)]).then(async([dashboard,diagnostics,funnel])=>({...dashboard,diagnostics,funnel,issues:await parserIssues(env.DB,dashboard)})):runner(env,{signal:request.signal});
  if(name!=='scheduler_status')ctx?.waitUntil?.(work.catch(()=>{}));
  const value=await work;
  if(name==='scheduler_status')value.databaseExport=await databaseExportTicket(env,id);
  return reply({content:[{type:'text',text:JSON.stringify(value)}]});
 }catch(e){return reply({isError:true,content:[{type:'text',text:e.status?e.message:'Не удалось выполнить серверную операцию.'}]});}
}
