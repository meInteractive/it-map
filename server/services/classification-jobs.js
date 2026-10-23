import {enqueueDetails,drainDetailJobs} from './vacancy-detail-jobs.js';
import {jsonResponse} from '../http.js';
import {candidateDb,candidateHash,candidateError,candidateClassificationContext,selectClassificationCandidates,classificationCandidateStatistics,getVacancyCandidate} from '../repositories/vacancy-candidates.js';
import {classifyStoredCandidate} from './stored-vacancy-classification.js';
import {boundGeneration,generationBoundDb} from '../repositories/generation-bound-db.js';
import {CLASSIFICATION_LIMITS as limits} from './classification-config.js';
import {JEV_MODEL_ID} from '../integrations/jev.js';
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const transient=code=>/^provider_(429|500|502|503|504)$/.test(code)||code==='network';
const fatal=code=>['credits','missing_key','invalid_key','decrypt','jev_unavailable','provider_403','model_mismatch'].includes(code);
const messages={provider_429:'OpenRouter ограничил частоту запросов. Продолжите пакет позже.',credits:'Недостаточно средств OpenRouter. Пополните баланс и продолжите пакет.',missing_key:'Сохраните API-ключ OpenRouter и продолжите пакет.',invalid_output:'JEV не вернула корректное бинарное решение. Продолжите пакет.',storage:'Не удалось сохранить результат. Полученные решения не удалены.',timeout:'Окно фонового выполнения закончилось; продолжите пакет.',candidate_busy:'Первичная вакансия уже проверяется. Продолжите пакет позже.',classification_busy:'Два запроса JEV уже выполняются. Продолжите пакет позже.',no_active_roles:'Нет активных корневых ролей с промтом.',changed:'Настройки изменились. Продолжите пакет с актуальными промтами.'};
function safeError(code){return {code,message:messages[code]|| (fatal(code)?'OpenRouter недоступен или не настроен. Исправьте подключение и продолжите пакет.':transient(code)?'Временная ошибка OpenRouter.':'Не удалось завершить классификацию. Продолжите пакет.')};}
export function publicClassificationJob(row){
 const items=JSON.parse(row.items_json),done=items.filter(i=>i.outcome==='complete'),errors=items.filter(i=>i.outcome==='error'),logs=items.flatMap(i=>i.log||[]);
 return {id:row.id,state:row.state==='running'&&row.lease_until<=Date.now()?'paused':row.state,reason:row.state==='running'&&row.lease_until<=Date.now()?'Предыдущий обработчик прервался. Продолжите пакет.':row.reason,selected:items.length,completed:done.length,remaining:items.length-done.length,accepted:done.filter(i=>i.status==='accepted').length,rejected:done.filter(i=>i.status==='rejected').length,errors:errors.length,requests:logs.filter(l=>!l.reused).length,reused:logs.filter(l=>l.reused).length,durationMs:row.duration_ms,costUsd:logs.reduce((n,l)=>n+(l.costUsd??0),0),unknownCostRequests:logs.filter(l=>!l.reused&&l.costUsd==null).length,createdAt:new Date(row.created_at).toISOString(),updatedAt:new Date(row.updated_at).toISOString(),items:items.map(i=>({candidateId:i.id,outcome:i.outcome,status:i.status||null,attempts:i.attempts,error:i.error||null,log:i.log||[]}))};
}
export async function readClassificationJob(db,id){
 if(typeof id!=='string'||!uuid.test(id))throw candidateError('Некорректный ID пакета.');db=await candidateDb(db);
 const row=await db.prepare('SELECT * FROM vacancy_classification_jobs WHERE id=? AND generation=?').bind(id,boundGeneration(db)).first();if(!row)throw candidateError('Пакет не найден или отменён восстановлением базы.',404);return publicClassificationJob(row);
}
export async function classificationDashboard(db){
 db=await candidateDb(db);const statistics=await classificationCandidateStatistics(db),rows=(await db.prepare('SELECT * FROM vacancy_classification_jobs WHERE generation=? ORDER BY created_at DESC,id DESC LIMIT 20').bind(boundGeneration(db)).all()).results||[];
 return {statistics,limits,jobs:rows.map(publicClassificationJob)};
}
export async function enqueueClassification(env,input={},requestKey){
 const db=await candidateDb(env.DB),selection=await selectClassificationCandidates(db,input),generation=boundGeneration(db),roles=await candidateClassificationContext(db);
 if(!selection.candidates.length)return {jobIds:[],queued:0,reused:selection.reused,noActiveRoles:!selection.activeRoles,reason:selection.reason};
 const key=await candidateHash(JSON.stringify([requestKey??crypto.randomUUID(),input.force??false,selection.candidates.map(c=>[c.id,c.titleHash,c.titleRevision]),roles.map(r=>[r.roleId,r.promptVersion,r.modelKey])]));
 const id=crypto.randomUUID(),now=Date.now(),items=selection.candidates.map(c=>({id:c.id,forceRoles:input.force?roles.map(r=>r.roleId):[],outcome:'queued',attempts:0,nextAt:0,log:[]}));
 await db.prepare('INSERT OR IGNORE INTO vacancy_classification_jobs(id,generation,request_key,items_json,created_at,updated_at) VALUES(?,?,?,?,?,?)').bind(id,generation,key,JSON.stringify(items),now,now).run();
 const row=await db.prepare('SELECT id FROM vacancy_classification_jobs WHERE generation=? AND request_key=?').bind(generation,key).first();
 return {jobIds:[row.id],queued:items.length,reused:selection.reused,noActiveRoles:false,reason:null};
}
export async function enqueueSavedCandidates(env,ids,runId){
 const unique=[...new Set(ids)],all={jobIds:[],queued:0,reused:0,noActiveRoles:false,reason:null};
 // Partition saved IDs, not the full candidate table. Each job is <=50.
 for(let start=0;start<unique.length;start+=limits.batchSize){const group=unique.slice(start,start+limits.batchSize),next=await enqueueClassification(env,{ids:group,limit:50},'discovery:'+runId+':'+start);all.jobIds.push(...next.jobIds);all.queued+=next.queued;all.reused+=next.reused;all.noActiveRoles||=next.noActiveRoles;all.reason=next.reason||all.reason;}
 return all;
}
export async function runClassificationJob(env,id,{sliceMs=limits.sliceMs,resume=false}={}){
 const started=Date.now(),deadline=started+Math.min(limits.sliceMs,Math.max(0,sliceMs)),baseDb=await candidateDb(env.DB),generation=boundGeneration(baseDb),owner=crypto.randomUUID();env={...env,DB:baseDb};
 const current=await readClassificationJob(baseDb,id);if(current.state==='completed')return current;
 const claim=await baseDb.prepare("UPDATE vacancy_classification_jobs SET state='running',owner_token=?,lease_until=?,updated_at=? WHERE id=? AND generation=? AND (owner_token='' OR lease_until<=?)").bind(owner,started+limits.jobLeaseMs,started,id,generation,started).run();
 if(!claim.meta?.changes)throw Object.assign(candidateError('Пакет уже обрабатывается.',409),{code:'job_busy'});
 const db=generationBoundDb(baseDb,generation,{sql:'EXISTS(SELECT 1 FROM vacancy_classification_jobs WHERE id=? AND generation=? AND owner_token=? AND lease_until>CAST(julianday(\'now\')*86400000-210866760000000 AS INTEGER))',bindings:[id,generation,owner],failure:()=>Object.assign(candidateError('Владение пакетом изменилось.',409),{code:'job_changed'})});
 const row=await db.prepare('SELECT * FROM vacancy_classification_jobs WHERE id=?').bind(id).first(),items=JSON.parse(row.items_json);let stop='',closed=false;
 for(const item of items)if(item.outcome==='running')item.outcome='queued';
 if(resume)for(const item of items)if(item.outcome==='error'){item.outcome='queued';item.attempts=0;item.nextAt=0;item.error=null;}
 async function persist(){await db.prepare('UPDATE vacancy_classification_jobs SET items_json=?,updated_at=? WHERE id=? AND owner_token=?').bind(JSON.stringify(items),Date.now(),id,owner).run();}
 try{
  if(!(await candidateClassificationContext(db)).length)stop='no_active_roles';
  async function worker(){while(!stop&&Date.now()<deadline){
   const item=items.find(i=>i.outcome==='queued');if(!item)break;item.outcome='running';
   if(item.nextAt>Date.now()){const delay=item.nextAt-Date.now();if(Date.now()+delay>=deadline){item.outcome='queued';stop='timeout';break;}await new Promise(r=>setTimeout(r,delay));}
   if(Date.now()+50>=deadline){item.outcome='queued';stop='timeout';break;}
   const at=Date.now();let detail,result,logIndex;
   // Persist started ownership before a paid request; completed decisions are
   // independently durable and recovered by classifyStoredCandidate's cache.
   await persist();
   try{
    detail=await getVacancyCandidate(db,item.id);if(!detail)throw candidateError('Запись удалена.',404);
    if(detail.candidate.mainVacancyId!=null){item.outcome='complete';item.status='imported';await persist();continue;}
    const activeIds=detail.currentResults.map(r=>r.roleId);item.forceRoles=item.forceRoles.filter(id=>activeIds.includes(id));
    if(item.forceBaseline)for(const role of detail.currentResults){if(role.decision&&role.id!==item.forceBaseline[role.roleId])item.forceRoles=item.forceRoles.filter(id=>id!==role.roleId);}
    item.forceBaseline=Object.fromEntries(detail.currentResults.map(r=>[r.roleId,r.id??null]));await persist();
    if(!activeIds.length){item.outcome='queued';stop='no_active_roles';continue;}
    logIndex=item.log.length;item.log.push({candidateId:item.id,model:JEV_MODEL_ID,titleHash:detail.candidate.titleHash,roleCount:detail.currentResults.filter(r=>item.forceRoles.includes(r.roleId)||!r.decision).length,startedAt:new Date(at).toISOString(),durationMs:null,success:false,costUsd:null,reused:false,state:'started'});await persist();
    result=await classifyStoredCandidate({...env,DB:db},item.id,{forceRoleIds:item.forceRoles,recordTechnicalTimeout:true,signal:AbortSignal.timeout(Math.max(1,Math.min(limits.requestTimeoutMs,deadline-Date.now()-500)))});
    const attempt=result.attemptId?await db.prepare('SELECT * FROM vacancy_classification_attempts WHERE id=?').bind(result.attemptId).first():null;
    const errors=attempt?JSON.parse(attempt.errors_json):[];
    const log={model:attempt?.model_id||JEV_MODEL_ID,titleHash:detail.candidate.titleHash,candidateId:item.id,roleCount:result.requestedRoleIds.length,startedAt:new Date(at).toISOString(),durationMs:Date.now()-at,success:!errors.length,reused:result.reused,costUsd:result.reused?null:attempt?.cost_usd??null};
    item.log[logIndex]={...log,state:'finished'};if(!result.reused)item.attempts++;
    const failed=new Set(errors.map(e=>e.roleId));item.forceRoles=item.forceRoles.filter(id=>failed.has(id));
    item.status=result.candidate.processingStatus;
    if(item.status==='accepted')await enqueueDetails({...env,DB:db},{candidateId:item.id},'classification:'+String(result.attemptId||id+':'+item.id)).catch(()=>{});
    const code=errors[0]?.code;
    if(code==='timeout'){item.outcome='error';item.error=safeError(code);stop=code;}
    else if(code==='classification_busy'){item.outcome='queued';stop=code;}
    else if(!result.needsClassification&&!item.forceRoles.length&&!errors.length){item.outcome='complete';item.error=null;}
    else{item.error=safeError(code||'changed');if(fatal(code)){item.outcome='queued';stop=code;}else if(transient(code)&&item.attempts<=limits.maxRetries){item.outcome='queued';item.nextAt=Date.now()+limits.retryDelaysMs[Math.max(0,item.attempts-1)];}else item.outcome='error';}
   }catch(e){
    if(e.code==='restored'||e.code==='job_changed')throw e;
    const code=e.code==='candidate_changed'?'changed':e.code|| (e.name==='TimeoutError'||e.name==='AbortError'?'timeout':'storage');item.error=safeError(code);
    if(['candidate_busy','classification_busy'].includes(code)){if(logIndex!==undefined)item.log.splice(logIndex,1);item.outcome='queued';stop=code;}
    else{item.attempts++;const failedLog={candidateId:item.id,model:JEV_MODEL_ID,titleHash:detail?.candidate.titleHash||null,roleCount:detail?.currentResults.filter(r=>item.forceRoles.includes(r.roleId)||!r.decision).length??null,startedAt:new Date(at).toISOString(),durationMs:Date.now()-at,success:false,costUsd:null,reused:false,code,state:'failed'};if(logIndex!==undefined)item.log[logIndex]=failedLog;else if(detail)item.log.push(failedLog);item.outcome=code==='timeout'?'queued':'error';if(code==='timeout'||code==='storage')stop=code;}
   }
   await persist();
  }}
  await Promise.allSettled(Array.from({length:limits.concurrency},()=>worker())).then(results=>{for(const r of results)if(r.status==='rejected')throw r.reason;});
  for(const item of items)if(item.outcome==='running')item.outcome='queued';
  const complete=items.every(i=>i.outcome==='complete');if(!complete&&!stop)stop=items.some(i=>i.outcome==='error')?'error':'timeout';
  await db.prepare('UPDATE vacancy_classification_jobs SET items_json=?,state=?,reason=?,duration_ms=duration_ms+?,updated_at=?,owner_token=\'\',lease_until=0 WHERE id=? AND owner_token=?').bind(JSON.stringify(items),complete?'completed':'paused',complete?'':safeError(stop).message,Date.now()-started,Date.now(),id,owner).run();closed=true;
  return readClassificationJob(baseDb,id);
 }finally{
  if(!closed)await baseDb.prepare("UPDATE vacancy_classification_jobs SET state='paused',reason=?,owner_token='',lease_until=0,updated_at=? WHERE id=? AND generation=? AND owner_token=?").bind('Обработчик прервался. Завершённые решения сохранены; продолжите пакет.',Date.now(),id,generation,owner).run().catch(()=>{});
 }
}
export function startClassificationJobs(env,ctx,jobIds,{resume=false}={}){
 if(!jobIds.length)return 'empty';if(!ctx?.waitUntil)return 'manual_resume';
 // Browser cancellation does not cancel this promise. One combined time slice,
 // not one 30-second promise per job, respects Workers waitUntil's real limit.
 ctx.waitUntil((async()=>{const deadline=Date.now()+limits.sliceMs;for(const id of jobIds){const left=deadline-Date.now();if(left<=0)break;let result;try{result=await runClassificationJob(env,id,{sliceMs:left,resume});}catch{break;}if(result.state!=='completed')break;}const remaining=deadline-Date.now();if(remaining>100)await drainDetailJobs(env,{sliceMs:remaining});})().catch(()=>{}));return 'background';
}
export async function classificationJobsRoute(request,env,url,user,ctx){
 if(!user.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
 if(!env.DB)return jsonResponse({error:'Хранилище не подключено.'},503);
 if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  if(url.pathname==='/api/classification/jobs'&&request.method==='GET')return jsonResponse(await classificationDashboard(env.DB));
  const match=url.pathname.match(/^\/api\/classification\/jobs\/([0-9a-f-]{36})(?:\/(resume))?$/);
  if(match&&request.method==='GET'&&!match[2])return jsonResponse(await readClassificationJob(env.DB,match[1]));
  const {readClassificationInput}=await import('../repositories/classification-prompts.js');
  if(match&&request.method==='POST'&&match[2]){if(Object.keys(await readClassificationInput(request)).length)throw candidateError('Передайте только ID пакета.');const job=await readClassificationJob(env.DB,match[1]);if(job.state==='running')throw candidateError('Пакет уже выполняется.',409);const execution=startClassificationJobs(env,ctx,[job.id],{resume:true});return jsonResponse({job,execution},202);}
  if(url.pathname==='/api/classification/jobs'&&request.method==='POST'){
   const input=await readClassificationInput(request);if(Object.keys(input).some(k=>!['candidateId','companyId','agencyId','sourceId','limit','force','confirmed','requestId'].includes(k)))throw candidateError('Неизвестный параметр.');
   if(!uuid.test(input.requestId||''))throw candidateError('Нужен UUID намерения requestId.');if(input.force&&input.confirmed!==true)throw candidateError('Подтвердите платный принудительный пересчёт.');
   const {requestId,confirmed,...filters}=input,queued=await enqueueClassification(env,filters,'manual:'+requestId),execution=startClassificationJobs(env,ctx,queued.jobIds);return jsonResponse({...queued,execution},202);
  }
  return jsonResponse({error:'Метод не поддерживается.'},405);
 }catch(e){return jsonResponse({error:e.status?e.message:'Не удалось обработать пакет классификации.',code:e.code},e.status||500);}
}
