import {parserOwnerKey,parserOwnerColumns} from '../repositories/parser-owner.js';
import {parserIssues} from './parser-issues.js';
import {parserFunnel} from './parser-funnel.js';
import {streamSchedulerRun} from './scheduler-stream.js';
import {schedulerAttemptDetails} from './scheduler-attempt-details.js';
import {candidateHash,candidateDb} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {readVacancyParsingSources,readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {schedulerSettings,publicSchedulerSettings,saveSchedulerSettings,enqueueScheduledJob,readScheduledQueue,schedulerError} from '../repositories/scheduler.js';
import {schedulerCompanyAllowed} from './scheduler-pipeline.js';
import {SCHEDULER_SOURCE_OPTIONS,SCHEDULER_APIFY_POLICY} from '../repositories/scheduler-schema.js';
import {schedulerSpending} from './scheduler-budget.js';
import {runDueTasks} from './scheduler-dispatcher.js';
import {readClassificationInput} from '../repositories/classification-prompts.js';
import {jsonResponse} from '../http.js';
import {timingSafeEqual} from '../auth/session.js';
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const checkKeys=(input,keys)=>{if(!input||Object.keys(input).some(k=>!keys.includes(k)))throw schedulerError('input','Неизвестные параметры запроса.',400);};
export async function schedulerDashboard(db){
 db=await candidateDb(db);const s=await schedulerSettings(db),generation=boundGeneration(db);
 const counts=(await db.prepare('SELECT status,count(*) total FROM scheduler_jobs WHERE generation=? GROUP BY status').bind(generation).all()).results||[];
 const runs=(await db.prepare('SELECT * FROM scheduler_runs WHERE generation=? ORDER BY started_at DESC LIMIT 20').bind(generation).all()).results||[];
 const attempts=((await db.prepare('SELECT a.id,a.job_id,a.run_id,a.type,a.company_id,j.agency_id,a.source_id,j.candidate_id,a.started_at,a.finished_at,a.status,a.reason_code,a.message,a.provider,a.model,a.reserved_usd,a.cost_usd,a.credits,substr(a.result_json,1,2048) result_json FROM scheduler_attempts a LEFT JOIN scheduler_jobs j ON j.id=a.job_id ORDER BY a.started_at DESC LIMIT 50').all()).results||[]).map(schedulerAttemptDetails);
 const signal=await db.prepare("SELECT started_at FROM scheduler_runs WHERE generation=? AND trigger='automatic' ORDER BY started_at DESC LIMIT 1").bind(generation).first();
 const statistics=await db.prepare("SELECT count(CASE WHEN type='scan_source' AND status='completed' THEN 1 END) sources,coalesce(sum(CASE WHEN type='scan_source' AND status='completed' THEN json_extract(result_json,'$.found') ELSE 0 END),0) found,count(CASE WHEN type='classify_title' AND json_extract(result_json,'$.accepted')=1 THEN 1 END) accepted,count(CASE WHEN type='import_vacancy' AND status='completed' AND json_extract(result_json,'$.action')='create' THEN 1 END) imported FROM scheduler_attempts").first();
 const lastSuccessAt=(await db.prepare("SELECT max(finished_at) at FROM scheduler_runs WHERE generation=? AND status='completed' AND succeeded>0 AND trigger<>'probe'").bind(generation).first()).at||null;
 return {apifyPolicy:SCHEDULER_APIFY_POLICY,sourceOptions:SCHEDULER_SOURCE_OPTIONS,lastSuccessAt,settings:publicSchedulerSettings(s),counts:Object.fromEntries(counts.map(r=>[r.status,r.total])),runs,attempts,statistics,spending:await schedulerSpending(db),lastSignalAt:signal?.started_at||null,nextSignalAt:signal&&Date.now()-signal.started_at<75*60000?Math.ceil(Date.now()/3600000)*3600000:null,cloudStatus:signal?(Date.now()-signal.started_at>=75*60000?'signal_overdue':'signal_received'):s.cloudOwnerId?'ready_to_connect':'owner_binding_required'};
}
export async function schedulerSources(db,query={}){
 const page=Number(query.page||1);if(!Number.isSafeInteger(page)||page<1||page>100000)throw schedulerError('input','Некорректная страница.',400);
 const s=await schedulerSettings(db),all=await readVacancyParsingSources(db),stored=(await db.prepare('SELECT * FROM vacancy_source_parsing').all()).results||[],byKey=new Map(stored.map(r=>[parserOwnerKey(r)+':'+r.source_id,r]));
 const favorites=new Set(((await db.prepare('SELECT company_name FROM company_user_state WHERE user_name=? AND favorite=1').bind(s.ownerName).all()).results||[]).map(r=>r.company_name));
 const rows=all.filter(r=>!query.company||r.companyName.toLowerCase().includes(String(query.company).toLowerCase())).map(source=>{const row=byKey.get(parserOwnerKey(source)+':'+source.sourceId),fresh=row?.source_url===source.url;return {...source,autoEnabled:!!(source.supported&&s.sourceIds.includes(source.sourceId)),intervalDays:s.intervalDays,nextRunAt:row?.next_run_at||null,lastAttemptAt:row?.last_attempt_at||null,lastResult:row?.last_result||'',allowed:!s.favoritesOnly||favorites.has(source.companyName)};});
 return {sources:rows.slice((page-1)*30,page*30),total:rows.length,page,pageSize:30};
}
export async function schedulerProbe(env,requestId=crypto.randomUUID(),options={}){
 const db=await candidateDb(env.DB);await enqueueScheduledJob(db,{type:'diagnostic',key:'probe:'+requestId,payload:{nonce:requestId}});
 const run=await runDueTasks({...env,DB:db},{...options,trigger:'probe',requestId:'probe:'+requestId,diagnostic:true,limit:1,durationMs:10000});
 const job=await db.prepare("SELECT id,status,payload FROM scheduler_jobs WHERE generation=? AND type='diagnostic' AND json_extract(payload,'$.nonce')=?").bind(boundGeneration(db),requestId).first();return {run,job:job?{id:job.id,status:job.status}:null,externalRequests:0};
}
export async function internalSchedulerRoute(request,env){
 const token=env.SCHEDULER_TOKEN,submitted=request.headers.get('authorization')||'';
 if(!token||!submitted.startsWith('Bearer ')||!timingSafeEqual(submitted.slice(7),token))return jsonResponse({error:'Недействительный ключ диспетчера.'},401);
 if(request.method!=='POST')return jsonResponse({error:'Метод не поддерживается.'},405);
 try{const input=await readClassificationInput(request);checkKeys(input,['requestId','diagnostic']);if(input.requestId!==undefined&&(typeof input.requestId!=='string'||input.requestId.length>100)||input.diagnostic!==undefined&&typeof input.diagnostic!=='boolean')throw schedulerError('input','Некорректный сигнал.',400);return jsonResponse(input.diagnostic?await schedulerProbe(env,input.requestId):await runDueTasks(env,{trigger:'automatic',requestId:input.requestId}));}catch(e){return jsonResponse({error:e.status?e.message:'Не удалось выполнить цикл.',code:e.code||'storage'},e.status||500);}
}
export async function schedulerRoute(request,env,url,user,ctx){
 if(!user.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
 if(!env.DB)return jsonResponse({error:'Хранилище не подключено.'},503);
 if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  env={...env,DB:await candidateDb(env.DB)};const db=env.DB,path=url.pathname.slice('/api/scheduler'.length);
  if(path===''&&request.method==='GET')return jsonResponse(await schedulerDashboard(db));
  if(path==='/run'&&request.method==='GET'){
   checkKeys(Object.fromEntries(url.searchParams),['requestId']);const requestId=url.searchParams.get('requestId');
   if(!requestId||!uuid.test(requestId))throw schedulerError('input','Передайте UUID запуска.',400);
   const generation=boundGeneration(db),id=await candidateHash(generation+':'+requestId);
   const run=await db.prepare('SELECT * FROM scheduler_runs WHERE id=? AND generation=?').bind(id,generation).first();
   return run?jsonResponse(run):jsonResponse({error:'Сохранённый запуск не найден.',code:'not_found'},404);
  }
  if(path==='/issues'&&request.method==='GET'){checkKeys(Object.fromEntries(url.searchParams),[]);return jsonResponse(await parserIssues(db,await schedulerDashboard(db)));}
  if(path==='/funnel'&&request.method==='GET'){checkKeys(Object.fromEntries(url.searchParams),['sourceId','parser']);return jsonResponse(await parserFunnel(db,Object.fromEntries(url.searchParams)));}
  if(path==='/sources'&&request.method==='GET'){checkKeys(Object.fromEntries(url.searchParams),['page','company']);return jsonResponse(await schedulerSources(db,Object.fromEntries(url.searchParams)));}
  if(path==='/queue'&&request.method==='GET'){checkKeys(Object.fromEntries(url.searchParams),['page','status','type','companyId','agencyId','search']);return jsonResponse(await readScheduledQueue(db,Object.fromEntries(url.searchParams)));}
  const input=await readClassificationInput(request);
  if(path==='/settings'&&request.method==='PUT')return jsonResponse(await saveSchedulerSettings(db,input,user.name));
  if(path==='/run'&&request.method==='POST'){checkKeys(input,['requestId']);if(typeof input.requestId!=='string'||!uuid.test(input.requestId))throw schedulerError('input','Передайте UUID запуска.',400);if(request.headers.get('accept')?.includes('application/x-ndjson'))return streamSchedulerRun(request,env,ctx,input.requestId);const work=runDueTasks(env,{trigger:'manual',requestId:input.requestId,signal:request.signal});ctx?.waitUntil?.(work.catch(()=>{}));return jsonResponse(await work);}
  if(path==='/probe'&&request.method==='POST'){checkKeys(input,[]);return jsonResponse(await schedulerProbe(env));}
  if(path==='/cloud-owner'&&request.method==='POST'){
   checkKeys(input,['confirmed']);const id=request.headers.get('oai-authenticated-user-id');if(input.confirmed!==true||!id||id.length>300)throw schedulerError('cloud_identity','Сначала войдите через ChatGPT, затем привяжите облачный запуск в администраторском сеансе.',403);
   const s=await schedulerSettings(db),{favoritesOnly,...value}=s;await db.prepare("UPDATE firecrawl_settings SET scheduler_json=? WHERE id=1 AND json_extract(scheduler_json,'$.revision')=?").bind(JSON.stringify({...value,cloudOwnerId:id,ownerName:user.name,revision:s.revision+1}),s.revision).run();return jsonResponse({ok:true});
  }
  const source=path.match(/^\/sources\/(\d+)\/([A-Za-z]+)(\/check)?$/);
  if(source&&!source[3]&&request.method==='PUT')return jsonResponse({error:'Расписание теперь общее. Используйте настройки компаний и источников планировщика.',code:'global_schedule'},410);
  if(source&&source[3]&&request.method==='POST'){
   checkKeys(input,[]);const src=await readVacancyParsingSource(db,Number(source[1]),source[2]),s=await schedulerSettings(db);
   if(!src.enabled||!src.supported||!await schedulerCompanyAllowed(db,src.companyId,s.ownerName))throw schedulerError('scope','Включите допустимый источник избранной компании.',403);
   const job=await enqueueScheduledJob(db,{type:'scan_source',key:'manual-scan:'+src.companyId+':'+src.sourceId+':'+crypto.randomUUID(),companyId:src.companyId,sourceId:src.sourceId,payload:{manual:true,sourceUrl:src.url,sourceRevision:src.revision}});return jsonResponse({jobId:job.id});
  }
  const action=path.match(/^\/jobs\/([0-9a-f-]{36})\/(retry|cancel)$/);
  if(action&&request.method==='POST'){
   checkKeys(input,[]);const r=await db.prepare("UPDATE scheduler_jobs SET status=?,run_after=?,attempts=CASE WHEN ?='pending' THEN 0 ELSE attempts END,last_error='',reason_code='',updated_at=? WHERE id=? AND generation=? AND status IN ('pending','failed','cancelled')").bind(action[2]==='retry'?'pending':'cancelled',Date.now(),action[2]==='retry'?'pending':'cancelled',Date.now(),action[1],boundGeneration(db)).run();if(!r.meta?.changes)throw schedulerError('changed','Задание выполняется, завершено или не найдено.');return jsonResponse({ok:true});
  }
  return jsonResponse({error:'Метод не поддерживается.'},405);
 }catch(e){return jsonResponse({error:e.status?e.message:'Не удалось выполнить операцию планировщика.',code:e.code||'storage'},e.status||500);}
}
