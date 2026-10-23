import {parserOwner} from '../repositories/parser-owner.js';
import {SCHEDULER_APIFY_POLICY} from '../repositories/scheduler-schema.js';
import {readDetailSettings} from '../repositories/vacancy-detail-settings.js';
import {operationErrorCode} from './operation-error.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {beginSchedulerRun,claimScheduledJob,finishScheduledJob,schedulerSettings,schedulerError,SCHEDULER_TYPES} from '../repositories/scheduler.js';
import {scheduleDueSources,enqueueNextStage,enqueueCandidateClassifications,executeScheduledStage,reconcileFailedCandidateDependencies} from './scheduler-pipeline.js';
import {schedulerBilling} from './scheduler-budget.js';
import {enqueueMissingRelevance} from './scheduler-relevance.js';
import {schedulerWaitDelay} from './scheduler-wait.js';
import {sampleParserErrorHistory} from './parser-issues.js';
const waiting=new Set(['scheduler_budget','budget_unknown','free_exhausted','hobby_exhausted','usage_unavailable','rate_limit','rate','busy','candidate_busy','classification_busy','llm_busy','detail_busy','no_active_roles','disabled','source_scope','apify_budget','apify_running','apify_busy','apify_disabled','apify_uncertain','apify_changed','apify_cycle_limit','relevance_model','relevance_resume','relevance_prompt','jev_unavailable','credits']);
const transient=code=>['network','timeout','apify_network','apify_timeout','storage','transaction','preview_changed','pipeline_changed','candidate_changed','changed','unavailable','provider_429','provider_500','provider_502','provider_503','provider_504','relevance_changed'].includes(code);
export async function runDueTasks(env,{trigger='manual',requestId,limit=50,durationMs=110000,signal,handlers,diagnostic=false,onProgress,jobId=null}={}){
 const started=Date.now();durationMs=Math.min(110000,Math.max(0,durationMs));limit=Math.min(50,Math.max(0,limit));const deadline=started+durationMs;
 const sample=()=>sampleParserErrorHistory(env.DB).catch(()=>{console.warn('parser_error_history_sample_failed');});
 if(!diagnostic)await sample();
 const initial=await beginSchedulerRun(env.DB,{trigger,requestId:requestId??(trigger==='automatic'?'hour:'+Math.floor(started/3600000):crypto.randomUUID()),durationMs,diagnostic}),{run,db}=initial;
 if(initial.reused)return {...run,reused:true};
 if(run.status==='paused'){await db.prepare("UPDATE scheduler_runs SET finished_at=?,reason='Диспетчер приостановлен' WHERE id=?").bind(Date.now(),run.id).run();return {...run,reason:'Диспетчер приостановлен'};}
 const base=initial.baseDb,owner=(await schedulerSettings(db)).ownerName;let processed=0,succeeded=0,errors=0,reason='empty',ready=0,cursor=0,apifyStages=0,apifyStarts=0,apifyStartBlocked=false,deferredApifyChecked=false;
 const assertApifyStart=()=>{if(apifyStartBlocked||apifyStarts>=SCHEDULER_APIFY_POLICY.maxStagesPerCycle)throw schedulerError('apify_cycle_limit','Лимит новых запусков Apify в этом тике достигнут. Готовые результаты продолжают обрабатываться.');};
 const apifyAdmission={assertCanStart:assertApifyStart,takeStart(){assertApifyStart();apifyStarts++;}};
 const report=()=>{try{onProgress?.({ready,processed,succeeded,errors});}catch{}};
 let roleWindow=90000;
 const refreshRoleWindow=async()=>{const settings=await readDetailSettings(db,'vacancy_role');roleWindow=settings.provider==='openrouter'&&settings.openrouter_model==='jev'?45000:90000;};
 const requiredWindow=type=>type==='assign_roles'?roleWindow:type==='extract_vacancy'?90000:['scan_source','classify_title','fetch_full_text','score_relevance'].includes(type)?30000:1000;
 async function executeClaim(claim){
   const {job}=claim,attemptId=crypto.randomUUID();const costly=['scan_source','classify_title','fetch_full_text','extract_vacancy','assign_roles','score_relevance'].includes(job.type);
   if(job.type==='assign_roles')await refreshRoleWindow();
   const minimumWindow=requiredWindow(job.type);
   if(costly&&Date.now()+Math.min(minimumWindow,durationMs)>deadline){await finishScheduledJob(db,claim,{status:'pending',code:'time_slice',message:'Недостаточно времени для начала внешнего запроса.',delayMs:1000,deferred:true});reason='time_slice';return;}
   if(job.source_id==='hh'&&['scan_source','fetch_full_text'].includes(job.type))apifyStages++;
   await db.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,company_id,source_id,started_at,status) VALUES(?,?,?,?,?,?,?,'running')").bind(attemptId,job.id,run.id,job.type,job.company_id,job.source_id,Date.now()).run();
   const billing=schedulerBilling(base,attemptId,()=>claim.db.prepare('SELECT 1').first());let output={},status='completed',code='',message='',deferred=false,delayMs=0;
   try{
    // Reserve time for guarded D1 writes and final receipts after provider abort.
    const cleanupMs=['extract_vacancy','assign_roles'].includes(job.type)?15000:job.type==='score_relevance'?10000:3000;
    const timeout=AbortSignal.timeout(Math.max(1,deadline-Date.now()-cleanupMs));
    const jobEnv={...env,DB:claim.db,SCHEDULER_BILLING:billing,SCHEDULER_APIFY_ADMISSION:apifyAdmission},jobSignal=signal?AbortSignal.any([signal,timeout]):timeout;
    output=await (handlers?.[job.type]??executeScheduledStage)(jobEnv,claim,{owner,signal:jobSignal});
    if(output.defer){status='pending';code=output.code;message=code==='apify_busy'?'Apify занят другим запуском; повторная проверка через минуту.':code==='apify_budget'?'Недостаточно подтверждённого доступного бюджета Apify; повторная проверка через 5 минут.':'Продолжение сохранено; ожидается следующее окно или доступный бюджет.';deferred=true;delayMs=output.delayMs||schedulerWaitDelay(code);if(code==='apify_busy')apifyStartBlocked=true;}else succeeded++;
   }catch(e){
    if(['restored','scheduler_changed','job_changed'].includes(e.code))throw e;
    code=operationErrorCode(e);message=code==='timeout'?'Модель не успела ответить в отведённое время. Полученные ранее данные сохранены.':typeof e.code==='string'&&e.status&&typeof e.message==='string'?e.message.slice(0,500):'Техническая ошибка этапа. Полученные результаты сохранены; повторите позже.';errors++;
    if(['scope','source_changed','not_found','relevance_scope'].includes(code))status='cancelled';
    else if(code==='agencies_disabled'){status='pending';deferred=true;delayMs=60000;}
    else if(waiting.has(code)){status='pending';deferred=true;delayMs=code.startsWith('apify_')?schedulerWaitDelay(code,{targeted:!!jobId}):code==='scheduler_budget'?3600000:300000;if(code==='apify_busy')apifyStartBlocked=true;}
    else if(code==='rabota_incomplete_body'&&job.type==='fetch_full_text'&&job.source_id==='rabota'&&job.attempts<Math.min(2,job.max_attempts)){status='pending';delayMs=60000;}
    else if((transient(code)||code==='invalid_output'&&['extract_vacancy','assign_roles','score_relevance'].includes(job.type))&&job.attempts<job.max_attempts){status='pending';delayMs=['network','timeout'].includes(code)?360000:Math.min(3600000,60000*2**(job.attempts-1));}
    else status='failed';
    if(job.type==='scan_source'&&(job.company_id!=null||job.agency_id!=null))await db.prepare(`UPDATE vacancy_source_parsing SET last_result=?,last_error=? WHERE ${parserOwner(job).column}=? AND source_id=?`).bind(status,message,parserOwner(job).id,job.source_id).run();
   }
   await base.prepare('UPDATE scheduler_attempts SET finished_at=?,status=?,reason_code=?,message=?,cost_usd=CASE WHEN ? THEN NULL ELSE reserved_usd END,credits=?,result_json=? WHERE id=?').bind(Date.now(),status,code,message,billing.unknown?1:0,output.credits??null,JSON.stringify(output),attemptId).run();
   await finishScheduledJob(db,claim,{status,code,message,delayMs,payload:output.payload,deferred});processed++;
   await db.prepare("UPDATE scheduler_runs SET ready=?,processed=MAX(processed,?),succeeded=MAX(succeeded,?),errors=MAX(errors,?),remaining=(SELECT count(*) FROM scheduler_jobs WHERE generation=? AND status IN ('pending','running')) WHERE id=? AND status='running'").bind(ready,processed,succeeded,errors,boundGeneration(db),run.id).run();
   report();
 }
 try{
  await refreshRoleWindow();
  // Ready work owns the time slice. Reconciliation used to spend ~100s
  // enumerating sources/candidates before executing a single queued stage.
  if(!handlers&&!diagnostic&&!jobId){
   // Apply the new cadence to legacy hour-long pauses as well. Keep checkedAt
   // unchanged: this is eligibility repair, not a fresh provider/budget check.
   await db.prepare("UPDATE scheduler_jobs SET run_after=updated_at+CASE reason_code WHEN 'apify_busy' THEN 60000 ELSE 300000 END WHERE generation=? AND status='pending' AND type='scan_source' AND source_id='hh' AND reason_code IN ('apify_busy','apify_budget') AND run_after>updated_at+CASE reason_code WHEN 'apify_busy' THEN 60000 ELSE 300000 END").bind(boundGeneration(db)).run();
   if(Date.now()+35000<deadline)await reconcileFailedCandidateDependencies({...env,DB:db},{limit:3,deadline:Math.min(deadline-35000,Date.now()+8000)});
   if(Date.now()+35000<deadline)await enqueueMissingRelevance({...env,DB:db},{limit:5,deadline:Math.min(deadline-35000,Date.now()+8000)});
   const previous=await db.prepare('SELECT cursor_id FROM scheduler_runs WHERE generation=? AND finished_at IS NOT NULL ORDER BY started_at DESC LIMIT 1').bind(boundGeneration(db)).first();cursor=previous?.cursor_id||0;
   // Ready work owns the slice, including preparation: do not inject extra
   // HH scans ahead of the existing queue while Apify is being investigated.
   const due=await db.prepare("SELECT 1 FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=? LIMIT 1").bind(boundGeneration(db),Date.now()).first();
   if(!due){
    const preparationDeadline=Math.min(deadline-35000,Date.now()+12000);
    if(Date.now()<preparationDeadline){
     let candidates=(await db.prepare('SELECT id FROM vacancy_candidates WHERE id>? ORDER BY id LIMIT 50').bind(cursor).all()).results||[];
     if(!candidates.length){cursor=0;candidates=(await db.prepare('SELECT id FROM vacancy_candidates ORDER BY id LIMIT 50').all()).results||[];}
     await enqueueCandidateClassifications({...env,DB:db},candidates.map(c=>c.id));
     // One detailed recovery probe repairs interrupted stage hand-offs. The
     // other candidates are queued in one batch, not read deeply in a loop.
     if(candidates.length&&Date.now()<preparationDeadline){await enqueueNextStage({...env,DB:db},candidates[0].id);cursor=candidates[0].id;}
     if(Date.now()<preparationDeadline)await scheduleDueSources(db,owner,Date.now(),{deadline:preparationDeadline,limit:5});
    }
   }
  }
  ready=(await db.prepare("SELECT count(*) n FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=?").bind(boundGeneration(db),Date.now()).first()).n;
  report();
  while(processed<limit&&Date.now()+1000<deadline){
   signal?.throwIfAborted();const settings=await schedulerSettings(db);if(!settings.enabled&&!diagnostic){reason='paused';break;}if(settings.ownerName!==owner){reason='owner_changed';break;}
   const types=SCHEDULER_TYPES.filter(type=>Date.now()+Math.min(requiredWindow(type),durationMs)<deadline);
   let claim=null;
   // A stream of title decisions must not retain an obsolete busy/budget
   // reason for hours. Recheck one overdue parked source per tick, after at
   // least one ordinary stage and only when no higher pipeline work is due.
   if(!jobId&&!diagnostic&&!deferredApifyChecked&&processed>0&&!apifyStartBlocked&&apifyStarts<SCHEDULER_APIFY_POLICY.maxStagesPerCycle&&apifyStages<SCHEDULER_APIFY_POLICY.maxHhStagesPerCycle&&types.includes('scan_source')){
    const higher=await db.prepare("SELECT 1 FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=? AND type NOT IN ('classify_title','scan_source','diagnostic') LIMIT 1").bind(boundGeneration(db),Date.now()).first();
    if(!higher){deferredApifyChecked=true;claim=await claimScheduledJob(db,run.id,{types:['scan_source'],deferredApifyOnly:true});}
   }
   claim??=await claimScheduledJob(db,run.id,{diagnostic,types,allowApify:apifyStages<SCHEDULER_APIFY_POLICY.maxHhStagesPerCycle,allowApifyStarts:!apifyStartBlocked&&apifyStarts<SCHEDULER_APIFY_POLICY.maxStagesPerCycle,jobId});if(!claim){const due=await db.prepare("SELECT 1 FROM scheduler_jobs WHERE generation=? AND status='pending' AND run_after<=? LIMIT 1").bind(boundGeneration(db),Date.now()).first();reason=due||reason==='time_slice'?'time_slice':'empty';break;}
   const claims=[claim];
   // Use the two existing LLM slots for distinct candidates, under the same
   // global cycle and atomic budget ledger. Firecrawl remains single-flight.
   const parallelTypes=['extract_vacancy','assign_roles'].filter(type=>Date.now()+Math.min(requiredWindow(type),durationMs)<deadline);
   if(!jobId&&['extract_vacancy','assign_roles'].includes(claim.job.type)&&processed+1<limit&&parallelTypes.length){
    const other=await claimScheduledJob(db,run.id,{diagnostic,types:parallelTypes});if(other)claims.push(other);
   }
   const results=await Promise.allSettled(claims.map(executeClaim));const rejected=results.find(r=>r.status==='rejected');if(rejected)throw rejected.reason;
   if(processed>=limit)reason='limit';else if(Date.now()+1000>=deadline)reason='time_slice';
  }
 }catch(e){reason=operationErrorCode(e,'interrupted');if(e.code==='restored')throw e;}
 finally{
  const remaining=await base.prepare("SELECT count(*) n FROM scheduler_jobs WHERE generation=? AND status IN ('pending','running')").bind(boundGeneration(base)).first().catch(()=>({n:0}));
  await base.prepare("UPDATE scheduler_runs SET finished_at=?,status=?,ready=?,processed=?,succeeded=?,errors=?,remaining=?,reason=?,cursor_id=?,lease_until=0 WHERE id=? AND status='running'").bind(Date.now(),errors?'partial':['empty','limit','time_slice','paused'].includes(reason)?'completed':'interrupted',ready,processed,succeeded,errors,remaining.n,reason,cursor,run.id).run().catch(()=>{});
  if(!diagnostic)await sampleParserErrorHistory(base).catch(()=>{console.warn('parser_error_history_sample_failed');});
 }
 return base.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(run.id).first();
}
