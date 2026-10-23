import {operationErrorCode} from './operation-error.js';
import {nextCandidateStage,candidateTerminalDecision} from './scheduler-pipeline.js';
import {candidateDb,candidateHash} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {schedulerSettings,schedulerError,enqueueScheduledJob,beginSchedulerRun} from '../repositories/scheduler.js';
import {runDueTasks} from './scheduler-dispatcher.js';
import {reconcileApifyRuns} from './apify-runs.js';
import {auditApifyBudget} from './apify-budget-audit.js';
import {inspectApifyListingReplays,replayApifyListing} from './apify-listing-replay.js';
import {checkListingSource} from './listing-source-check.js';
import {checkRabotaSource} from './rabota-source-check.js';
import {readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {readHHTitleFilter} from './hh-title-filter.js';
import {parserCompanyDb} from '../repositories/parser-company-scope.js';
export const SCHEDULER_SERIES_LIMIT=5;
const pattern=/^series:(\d{5,9}):([1-5])$/;
const cloudPattern=/^cloud:([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}):([1-5])$/;
const terminal=run=>['completed','partial'].includes(run?.status)||run?.status==='interrupted'&&run.reason==='Обработчик прервался';
// One cloud occurrence follows server-issued continuations. Durable run receipts
// make replay harmless; a predecessor in the current restore generation is required.
export async function runScheduledSeriesTick(env,requestId,options={}){
 const db=await candidateDb(env.DB),generation=boundGeneration(db),hour=Math.floor(Date.now()/3600000);
 // Preparation has no processing side effects: losing its response is safe.
 // The caller knows the stable work ID before any provider request can begin.
 if(requestId===undefined){const settings=await schedulerSettings(db);return {status:settings.enabled?'prepared':'paused',receivedAt:Date.now(),series:{index:0,maxTicks:SCHEDULER_SERIES_LIMIT},continuationRequestId:settings.enabled?`cloud:${crypto.randomUUID()}:1`:null,continuationStopReason:settings.enabled?null:'paused'};}
 const key=requestId,legacy=key.match(pattern),cloud=key.match(cloudPattern),match=legacy||cloud;
 if(key.startsWith('candidate-step:')){
  const check=key.match(/^candidate-step:([1-9]\d{0,8}):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  if(!check)throw schedulerError('input','Некорректный ID проверки этапа кандидата.');
  const receipt=await db.prepare('SELECT id FROM scheduler_runs WHERE id=? AND generation=?').bind(await candidateHash(generation+':'+key),generation).first();
  if(receipt)return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,limit:1}),continuationRequestId:null};
  const id=Number(check[1]),settings=await schedulerSettings(db),candidate=await db.prepare('SELECT company_id,agency_id,source_id FROM vacancy_candidates WHERE id=?').bind(id).first();
  if(!candidate||!settings.enabled||!settings.sourceIds.includes(candidate.source_id))throw schedulerError('source_scope','Кандидат отсутствует или источник не выбран.');
  const scoped=await parserCompanyDb(db,candidate,settings.ownerName),scopedEnv={...env,DB:scoped},stage=await nextCandidateStage(scopedEnv,id);
  if(!stage)return {candidateId:id,status:'completed',decision:await candidateTerminalDecision(scoped,id),processed:0,continuationRequestId:null};
  const job=await enqueueScheduledJob(scoped,stage);
  if(job.status!=='pending')return {candidateId:id,status:'blocked',stage:job.type,reason:job.reason_code||job.status,processed:0,continuationRequestId:null};
  return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,jobId:job.id,limit:1}),candidateId:id,stage:job.type,continuationRequestId:null};
 }
 if(key.startsWith('hh-filter-check:')){
  const check=key.match(/^hh-filter-check:([1-9]\d{0,8}):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  if(!check)throw schedulerError('input','Некорректный ID проверки фильтра HH.');
  const receipt=await db.prepare('SELECT id FROM scheduler_runs WHERE id=? AND generation=?').bind(await candidateHash(generation+':'+key),generation).first();
  if(receipt)return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,limit:1}),continuationRequestId:null};
  const companyId=Number(check[1]),settings=await schedulerSettings(db),scoped=await parserCompanyDb(db,companyId,settings.ownerName),source=await readVacancyParsingSource(scoped,companyId,'hh');
  if(!settings.enabled||!settings.sourceIds.includes('hh')||!source.enabled||!source.supported)throw schedulerError('source_scope','Нужен включённый планировщик и источник HH.');
  const filter=await readHHTitleFilter(scoped);if(!filter.query)throw schedulerError('source_stage','Для активных ролей нет безопасного фильтра HH.');
  const job=await enqueueScheduledJob(scoped,{type:'scan_source',key:'hh-title-filter-v1:'+companyId+':'+source.revision+':'+source.url+':'+await candidateHash(filter.query),companyId,sourceId:'hh',payload:{manual:true,sourceUrl:source.url,sourceRevision:source.revision}});
  return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,jobId:job.id,limit:1}),continuationRequestId:null};
 }
 if((key.startsWith('series:')||key.startsWith('cloud:'))&&!match)throw schedulerError('series_order','Некорректное продолжение серверной серии.');
 if(key.startsWith('source-page-check:')){
  const check=key.match(/^source-page-check:([1-9]\d{0,8}):(companySite|getmatch):(inspect|page|next-page):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  if(!check)throw schedulerError('input','Некорректный ID проверки источника.');
  const initial=await beginSchedulerRun(db,{trigger:'probe',requestId:key,durationMs:100000,diagnostic:true});
  if(initial.reused){const attempt=await db.prepare("SELECT result_json FROM scheduler_attempts WHERE run_id=? AND type='diagnostic' ORDER BY started_at DESC LIMIT 1").bind(initial.run.id).first();return {...initial.run,reused:true,...(attempt?JSON.parse(attempt.result_json):{}),continuationRequestId:null};}
  const attemptId=crypto.randomUUID();await initial.db.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,company_id,source_id,started_at,status) VALUES(?,?,?,'diagnostic',?,?,?,'running')").bind(attemptId,initial.run.id,initial.run.id,Number(check[1]),check[2],Date.now()).run();
  let result={},errors=0;
  try{const timeout=AbortSignal.timeout(85000),signal=options.signal?AbortSignal.any([options.signal,timeout]):timeout;result={report:await checkListingSource({...env,DB:initial.db},Number(check[1]),check[2],check[3],{signal})};if(result.report.page&&!result.report.page.configured.ok){errors=1;result.code=result.report.page.configured.errorCode||'extraction';}}
  catch(e){errors=1;result={code:operationErrorCode(e),message:e.status?e.message:'Не удалось проверить страницу источника.',providerStatus:e.providerStatus??null,targetStatus:e.targetStatus??null};if(e.code==='restored')throw e;}
  finally{await initial.baseDb.prepare("UPDATE scheduler_attempts SET finished_at=?,status=?,reason_code=?,result_json=? WHERE id=? AND status='running'").bind(Date.now(),errors?'failed':'completed',result.code??'',JSON.stringify(result),attemptId).run();await initial.baseDb.prepare("UPDATE scheduler_runs SET finished_at=?,status=?,processed=1,succeeded=?,errors=?,reason='source_page_check',lease_until=0 WHERE id=? AND status='running'").bind(Date.now(),errors?'partial':'completed',errors?0:1,errors,initial.run.id).run();}
  return {...await initial.baseDb.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(initial.run.id).first(),...result,continuationRequestId:null};
 }
 if(key.startsWith('rabota-check:')){
  const check=key.match(/^rabota-check:([1-9]\d{0,8}):(inspect|list|queue|scan|advance-[1-9]\d{0,8}|candidate-[1-9]\d{0,8}|page-[1-9]\d{0,2}|list-[a-z0-9][a-z0-9_-]{0,39}|[1-9]\d{1,10}):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  if(!check)throw schedulerError('input','Некорректный ID проверки rabota.ru.');
  if(check[2].startsWith('advance-')){
   const receipt=await db.prepare('SELECT id FROM scheduler_runs WHERE id=? AND generation=?').bind(await candidateHash(generation+':'+key),generation).first();
   if(receipt)return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,limit:1}),continuationRequestId:null};
   const id=Number(check[2].slice(8)),candidate=await db.prepare("SELECT company_id FROM vacancy_candidates WHERE id=? AND source_id='rabota'").bind(id).first();
   if(!candidate||candidate.company_id!==Number(check[1]))throw schedulerError('source_scope','Первичная вакансия не относится к rabota.ru этой компании.');
   const settings=await schedulerSettings(db),source=await readVacancyParsingSource(db,Number(check[1]),'rabota');
   if(!settings.enabled||!settings.sourceIds.includes('rabota')||!source.enabled||!source.supported)throw schedulerError('source_scope','Нужен включённый планировщик и выбранный источник rabota.ru.');
   const stage=await nextCandidateStage({...env,DB:db},id);
   if(!stage)throw schedulerError('source_stage','У вакансии нет следующего разрешённого этапа.');
   const job=await enqueueScheduledJob(db,stage);
   if(job.status!=='pending')throw schedulerError('source_stage','Текущий этап уже завершён либо требует отдельного разбора ошибки.');
   return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,jobId:job.id,limit:1}),continuationRequestId:null};
  }
  if(check[2]==='scan'){
   const receipt=await db.prepare('SELECT id FROM scheduler_runs WHERE id=? AND generation=?').bind(await candidateHash(generation+':'+key),generation).first();
   if(receipt)return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,limit:1}),continuationRequestId:null};
   const settings=await schedulerSettings(db),source=await readVacancyParsingSource(db,Number(check[1]),'rabota');
   if(!settings.enabled||!settings.sourceIds.includes('rabota')||!source.enabled||!source.supported)throw schedulerError('source_scope','Нужен включённый планировщик и выбранный источник rabota.ru.');
   const job=await db.prepare("SELECT id FROM scheduler_jobs WHERE generation=? AND type='scan_source' AND company_id=? AND source_id='rabota' AND status='pending' AND json_extract(payload,'$.sourceUrl')=? AND json_extract(payload,'$.sourceRevision')=? ORDER BY created_at LIMIT 1").bind(generation,Number(check[1]),source.url,source.revision).first();
   if(!job)throw schedulerError('source_stage','Нет ожидающего задания текущего источника rabota.ru.');
   // A normal dispatcher claim owns all scope, provider, billing and save gates.
   return {...await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,jobId:job.id,limit:1}),continuationRequestId:null};
  }
  const initial=await beginSchedulerRun(db,{trigger:'probe',requestId:key,durationMs:100000,diagnostic:true});
  if(initial.reused){const attempt=await db.prepare("SELECT result_json FROM scheduler_attempts WHERE run_id=? AND type='diagnostic' ORDER BY started_at DESC LIMIT 1").bind(initial.run.id).first();return {...initial.run,reused:true,...(attempt?JSON.parse(attempt.result_json):{}),continuationRequestId:null};}
  const attemptId=crypto.randomUUID();await initial.db.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,company_id,source_id,started_at,status) VALUES(?,?,?,'diagnostic',?,'rabota',?,'running')").bind(attemptId,initial.run.id,initial.run.id,Number(check[1]),Date.now()).run();
  let result={},errors=0;
  try{const timeout=AbortSignal.timeout(85000),signal=options.signal?AbortSignal.any([options.signal,timeout]):timeout;result={report:await checkRabotaSource({...env,DB:initial.db},Number(check[1]),check[2],{signal})};if(result.report.listing?.status==='error'||result.report.detail?.validation.valid===false){errors=1;result.code=result.report.listing?.errors[0]?.code??'invalid_content';}}
  catch(e){errors=1;result={code:operationErrorCode(e),message:e.status?e.message:'Не удалось проверить rabota.ru.',providerStatus:e.providerStatus??null,targetStatus:e.targetStatus??null};if(e.code==='restored')throw e;}
  finally{await initial.baseDb.prepare("UPDATE scheduler_attempts SET finished_at=?,status=?,reason_code=?,result_json=? WHERE id=? AND status='running'").bind(Date.now(),errors?'failed':'completed',result.code??'',JSON.stringify(result),attemptId).run();await initial.baseDb.prepare("UPDATE scheduler_runs SET finished_at=?,status=?,processed=1,succeeded=?,errors=?,reason='rabota_check',lease_until=0 WHERE id=? AND status='running'").bind(Date.now(),errors?'partial':'completed',errors?0:1,errors,initial.run.id).run();}
  return {...await initial.baseDb.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(initial.run.id).first(),...result,continuationRequestId:null};
 }
 if(key.startsWith('apify-listing-replay:')){
  const uuid='[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}',m=key.match(new RegExp('^apify-listing-replay:(inspect|'+uuid+'):'+uuid+'$'));
  if(!m)throw schedulerError('input','Некорректный ID восстановления списка Apify.');
  const initial=await beginSchedulerRun(db,{trigger:'probe',requestId:key,durationMs:100000,diagnostic:true});
  if(initial.reused)return {...initial.run,reused:true,actorStarts:0,continuationRequestId:null};
  let report,code='',errors=0;
  try{const timeout=AbortSignal.timeout(90000),signal=options.signal?AbortSignal.any([options.signal,timeout]):timeout;report=m[1]==='inspect'?await inspectApifyListingReplays({...env,DB:initial.db}):await replayApifyListing({...env,DB:initial.db},m[1],{signal});}
  catch(e){errors=1;code=operationErrorCode(e,'replay_failed');if(e.code==='restored')throw e;}
  finally{await initial.baseDb.prepare("UPDATE scheduler_runs SET finished_at=?,status=?,processed=?,succeeded=?,errors=?,reason='apify_listing_replay',lease_until=0 WHERE id=? AND status='running'").bind(Date.now(),errors?'partial':'completed',report?.processed??report?.candidates?.length??0,errors?0:report?.processed??report?.candidates?.length??0,errors,initial.run.id).run();}
  return {...await initial.baseDb.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(initial.run.id).first(),report,...(code?{code}:{}),actorStarts:0,continuationRequestId:null};
 }
 if(key.startsWith('apify-reconcile:')||key.startsWith('apify-budget-check:')){
  if(!/^apify-(?:reconcile|budget-check):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(key))throw schedulerError('input','Некорректный ID сверки Apify.');
  const initial=await beginSchedulerRun(db,{trigger:'probe',requestId:key,durationMs:90000,diagnostic:true});
  if(initial.reused)return {...initial.run,reused:true,actorStarts:0,continuationRequestId:null};
  let reconciliation,budgetAudit,errors=0;const audit=key.startsWith('apify-budget-check:');
  try{const timeout=AbortSignal.timeout(80000),settings={signal:options.signal?AbortSignal.any([options.signal,timeout]):timeout};if(audit){budgetAudit=await auditApifyBudget({...env,DB:initial.db},settings);errors=budgetAudit.errors;}else{reconciliation=await reconcileApifyRuns({...env,DB:initial.db},{...settings,limit:4});errors=reconciliation.results.filter(r=>r.code).length;}}
  catch(e){errors=1;if(e.code==='restored')throw e;}
  finally{await initial.baseDb.prepare("UPDATE scheduler_runs SET finished_at=?,status=?,processed=?,succeeded=?,errors=?,reason=?,lease_until=0 WHERE id=? AND status='running'").bind(Date.now(),errors?'partial':'completed',audit?Object.keys(budgetAudit?.profiles||{}).length:reconciliation?.checked||0,Math.max(0,(audit?Object.keys(budgetAudit?.profiles||{}).length:reconciliation?.checked||0)-errors),errors,audit?'apify_budget_audit':'apify_reconciliation',initial.run.id).run();}
  return {...await initial.baseDb.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(initial.run.id).first(),...(audit?{budgetAudit}:{reconciliation}),actorStarts:0,continuationRequestId:null};
 }
 if(key.startsWith('apify-proxy-check:')){
  const check=key.match(/^apify-proxy-check:([1-9]\d{0,8}):([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/);
  if(!check)throw schedulerError('input','Некорректный ID проверки Apify.');
  const id=await candidateHash(generation+':'+key),receipt=await db.prepare('SELECT * FROM scheduler_runs WHERE id=? AND generation=?').bind(id,generation).first();
  if(receipt){const run=await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,limit:1});return {...run,continuationRequestId:null};}
  const stage=await nextCandidateStage({...env,DB:db},Number(check[1]));
  if(stage?.type!=='fetch_full_text'||stage.sourceId!=='hh')throw schedulerError('apify_check_stage','Для проверки нужна отобранная HH вакансия без полного текста.');
  const job=await enqueueScheduledJob(db,stage);
  if(job.status!=='pending')throw schedulerError('apify_check_stage','Этап получения текста не ожидает выполнения.');
  const run=await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key,jobId:job.id,limit:1});
  return {...run,continuationRequestId:null};
 }
 if(!match){const run=await runDueTasks({...env,DB:db},{...options,trigger:'manual',requestId:key});return {...run,continuationRequestId:null,continuationStopReason:'single_tick'};}
 const prefix=cloud?`cloud:${cloud[1]}`:`series:${legacy[1]}`,index=Number(match[2]),originHour=legacy?Number(legacy[1]):null;
 if(legacy&&(originHour>hour||originHour<hour-1))throw schedulerError('series_expired','Серия запусков устарела. Ожидается следующий облачный сигнал.');
 if(index>1){const previousId=await candidateHash(generation+':'+`${prefix}:${index-1}`),previous=await db.prepare('SELECT status,finished_at,reason FROM scheduler_runs WHERE id=? AND generation=?').bind(previousId,generation).first();
  if(!previous?.finished_at||!terminal(previous))throw schedulerError('series_order','Сначала должен завершиться предыдущий серверный тик.');
  const rootId=await candidateHash(generation+':'+`${prefix}:1`),root=await db.prepare('SELECT started_at FROM scheduler_runs WHERE id=? AND generation=?').bind(rootId,generation).first();
  const existingId=await candidateHash(generation+':'+key),existing=await db.prepare('SELECT id FROM scheduler_runs WHERE id=? AND generation=?').bind(existingId,generation).first();
  if(!existing&&(!root||Date.now()-root.started_at>=720000))throw schedulerError('series_expired','Время серии истекло. Ожидается следующий облачный сигнал.');
 }
 const run=await runDueTasks({...env,DB:db},{...options,trigger:'automatic',requestId:key});
 const rootId=await candidateHash(generation+':'+`${prefix}:1`),root=await db.prepare('SELECT started_at FROM scheduler_runs WHERE id=? AND generation=?').bind(rootId,generation).first();
 const settings=await schedulerSettings(db),due=await db.prepare("SELECT 1 FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=? LIMIT 1").bind(generation,Date.now()).first();
 const stopReason=!settings.enabled?'paused':!run.finished_at?'run_active':!terminal(run)?'run_interrupted':index>=SCHEDULER_SERIES_LIMIT?'tick_limit':!root||Date.now()-root.started_at>=720000?'series_expired':!due?'no_ready_work':null;
 console.info(JSON.stringify({event:'scheduler_series',seriesId:prefix,index,runId:run.id,reused:!!run.reused,stopReason}));
 return {...run,series:{id:prefix,index,maxTicks:SCHEDULER_SERIES_LIMIT},continuationRequestId:stopReason?null:`${prefix}:${index+1}`,continuationStopReason:stopReason};
}
