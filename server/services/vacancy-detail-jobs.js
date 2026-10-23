import {operationErrorCode} from './operation-error.js';
import {candidateDb,candidateId,candidateHash,candidateError,detailCandidateProjection} from '../repositories/vacancy-candidates.js';
import {boundGeneration,generationBoundDb} from '../repositories/generation-bound-db.js';
import {withDetailLease} from './vacancy-detail-leases.js';
import {fetchCandidateMarkdown,eligibleDetailCandidate} from './vacancy-fulltext.js';
import {extractionContext,extractCandidateMarkdown,vacancyDetailState} from './vacancy-extraction.js';
import {jsonResponse} from '../http.js';
import {readClassificationInput} from '../repositories/classification-prompts.js';
export const DETAIL_LIMITS={batchSize:20,llmConcurrency:2,maxRetries:3,sliceMs:24000};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const transient=code=>/^provider_(429|500|502|503|504)$/.test(code)||['rate','rate_limit','provider'].includes(code);
const busy=code=>['detail_busy','llm_busy','busy'].includes(code);
const pause=code=>busy(code)||['missing_key','disabled','free_exhausted','hobby_exhausted','usage_unavailable','rate_limit','no_structured_models','no_models','decrypt','network','timeout','auth','invalid_key','credits','provider_401','provider_402','provider_403','apify_running','apify_budget','apify_busy','apify_disabled','apify_uncertain','apify_changed','apify_network','apify_timeout'].includes(code);
async function projection(db){
 const p=await detailCandidateProjection(db),x=await extractionContext(db);
 const sql=p.sql+`, fulltext AS (SELECT p.*,(SELECT s.id FROM vacancy_raw_snapshots s WHERE s.candidate_id=p.id AND s.source_url=p.vacancyUrl ORDER BY s.created_at DESC,s.rowid DESC LIMIT 1) snapshotId,(SELECT f.id FROM vacancy_content_fetches f WHERE f.candidate_id=p.id AND f.source_url=p.vacancyUrl ORDER BY f.started_at DESC,f.rowid DESC LIMIT 1) fetchId FROM projected p WHERE processingStatus='accepted' AND mainVacancyId IS NULL), details AS (SELECT f.*,s.content_status rawState,(SELECT e.id FROM vacancy_extractions e WHERE e.candidate_id=f.id AND e.snapshot_id=s.id AND e.config_hash=? AND e.prompt_version=? AND e.schema_version=? AND e.status='success' ORDER BY e.processed_at DESC,e.rowid DESC LIMIT 1) extractionId,(SELECT e.id FROM vacancy_extractions e WHERE e.candidate_id=f.id AND e.snapshot_id=s.id AND e.config_hash=? AND e.prompt_version=? AND e.schema_version=? ORDER BY e.processed_at DESC,e.rowid DESC LIMIT 1) llmId FROM fulltext f LEFT JOIN vacancy_raw_snapshots s ON s.id=f.snapshotId)`;
 return {sql,bindings:[...p.bindings,x.configHash,x.promptVersion,x.schemaVersion,x.configHash,x.promptVersion,x.schemaVersion]};
}
const publicJob=row=>{const items=JSON.parse(row.items_json);return {id:row.id,state:row.state,reason:row.reason,createdAt:new Date(row.created_at).toISOString(),durationMs:row.duration_ms,selected:items.length,completed:items.filter(i=>i.outcome==='complete').length,remaining:items.filter(i=>i.outcome!=='complete').length,items};};
export async function readDetailJob(db,id){db=await candidateDb(db);const row=await db.prepare('SELECT * FROM vacancy_detail_jobs WHERE id=? AND generation=?').bind(id,boundGeneration(db)).first();if(!row)throw candidateError('Пакет не найден в текущем поколении базы.',404);return publicJob(row);}
export async function detailDashboard(db){
 db=await candidateDb(db);const p=await projection(db),results=await db.batch([
 db.prepare(p.sql+` SELECT count(*) accepted,coalesce(sum(rawState IS NULL OR rawState<>'valid'),0) pendingFetch,coalesce(sum(rawState='valid'),0) textLoaded,coalesce(sum(rawState='valid' AND extractionId IS NULL),0) pendingLLM,coalesce(sum(rawState='valid' AND extractionId IS NOT NULL),0) extracted,coalesce(sum(rawState='valid' AND extractionId IS NOT NULL),0) ready,coalesce(sum(EXISTS(SELECT 1 FROM vacancy_content_fetches f WHERE f.id=d.fetchId AND f.status='error')),0) fetchErrors,coalesce(sum(EXISTS(SELECT 1 FROM vacancy_extractions e WHERE e.id=d.llmId AND e.status='error')),0) llmErrors FROM details d`).bind(...p.bindings),
 db.prepare(p.sql+' SELECT id,titleRaw,companyName,companyNameRaw,vacancyUrl,rawState,extractionId FROM details ORDER BY id DESC LIMIT 50').bind(...p.bindings),
 db.prepare('SELECT * FROM vacancy_detail_jobs WHERE generation=? ORDER BY created_at DESC,rowid DESC LIMIT 20').bind(boundGeneration(db))
 ]);return {statistics:results[0].results[0],candidates:results[1].results||[],jobs:(results[2].results||[]).map(publicJob),limits:DETAIL_LIMITS};
}
export async function enqueueDetails(env,input={},requestKey=crypto.randomUUID()){
 const db=await candidateDb(env.DB),generation=boundGeneration(db),action=input.action||'pipeline';
 if(!input||Array.isArray(input)||Object.keys(input).some(k=>!['candidateId','ids','limit','action','confirmed'].includes(k))||!['pipeline','fetch','llm','refresh'].includes(action))throw candidateError('Некорректные параметры обработки.');
 const limit=input.limit??20;if(!Number.isSafeInteger(limit)||limit<1||limit>20)throw candidateError('Размер пакета — от 1 до 20.');
 if(action==='refresh'&&(input.confirmed!==true||input.candidateId==null))throw candidateError('Подтвердите повторную загрузку одной вакансии с расходом провайдера.');
 const key=await candidateHash(JSON.stringify([requestKey,input])),old=await db.prepare('SELECT id FROM vacancy_detail_jobs WHERE generation=? AND request_key=?').bind(generation,key).first();if(old)return {jobIds:[old.id],queued:(await readDetailJob(db,old.id)).selected};
 let ids;if(input.candidateId!=null)ids=[candidateId(input.candidateId)];else if(input.ids!==undefined){if(!Array.isArray(input.ids)||input.ids.length>20)throw candidateError('Не более 20 ID.');ids=[...new Set(input.ids.map(candidateId))];}else{const p=await projection(db);ids=((await db.prepare(p.sql+` SELECT id FROM details WHERE ${action==='fetch'?"rawState IS NULL OR rawState<>'valid'":action==='llm'?"rawState='valid' AND extractionId IS NULL":"rawState IS NULL OR rawState<>'valid' OR extractionId IS NULL"} ORDER BY id LIMIT ?`).bind(...p.bindings,limit).all()).results||[]).map(r=>r.id);}
 const items=[];for(const id of ids.slice(0,limit)){const s=await vacancyDetailState(db,id);if(!s.eligible){if(input.candidateId)await eligibleDetailCandidate(db,id);continue;}if(action!=='refresh'&&((action==='pipeline'&&s.readyForImport)||(action==='fetch'&&s.rawStatus==='loaded')||(action==='llm'&&s.llmStatus==='extracted')))continue;items.push({id,action,outcome:'queued',attempts:0,nextAt:0,refreshDone:false,refreshBaseline:null,error:null});}
 if(!items.length)return {jobIds:[],queued:0};const id=crypto.randomUUID(),now=Date.now();await db.prepare('INSERT OR IGNORE INTO vacancy_detail_jobs(id,generation,request_key,items_json,created_at,updated_at) VALUES(?,?,?,?,?,?)').bind(id,generation,key,JSON.stringify(items),now,now).run();const row=await db.prepare('SELECT id FROM vacancy_detail_jobs WHERE generation=? AND request_key=?').bind(generation,key).first();return {jobIds:[row.id],queued:items.length};
}
export async function runDetailJob(env,id,{sliceMs=24000,resume=false}={}){
 const base=await candidateDb(env.DB),before=await readDetailJob(base,id);if(before.state==='completed')return before;
 return withDetailLease(base,'job:'+id,async leaseDb=>{
  const started=Date.now(),deadline=started+Math.max(0,Math.min(120000,sliceMs)),owner=crypto.randomUUID(),generation=boundGeneration(base);
  await leaseDb.prepare("UPDATE vacancy_detail_jobs SET state='running',owner_token=?,lease_until=?,updated_at=? WHERE id=?").bind(owner,Date.now()+150000,Date.now(),id).run();
  const db=generationBoundDb(leaseDb,generation,{sql:'EXISTS(SELECT 1 FROM vacancy_detail_jobs WHERE id=? AND owner_token=?)',bindings:[id,owner],failure:()=>Object.assign(candidateError('Обработчик сменился.',409),{code:'detail_changed'})});
  const row=await db.prepare('SELECT * FROM vacancy_detail_jobs WHERE id=?').bind(id).first(),items=JSON.parse(row.items_json);let reason='';
  for(const item of items)if(item.outcome==='running'||resume&&item.outcome==='error'){item.outcome='queued';item.attempts=0;item.nextAt=0;}
  const persist=()=>db.prepare('UPDATE vacancy_detail_jobs SET items_json=?,updated_at=? WHERE id=? AND owner_token=?').bind(JSON.stringify(items),Date.now(),id,owner).run();
  try{
   for(let index=0;index<items.length;index++){const item=items[index];if(item.outcome!=='queued')continue;if(Date.now()+100>=deadline){reason='Окно выполнения завершено. Продолжите пакет.';break;}if(item.nextAt>Date.now()){if(item.nextAt+100>=deadline){reason='Ожидание повторной попытки.';break;}await new Promise(r=>setTimeout(r,item.nextAt-Date.now()));}
    item.outcome='running';await persist();
    try{
     await eligibleDetailCandidate(db,item.id);const signal=AbortSignal.timeout(Math.max(1,deadline-Date.now()-100));
     if(item.action!=='llm'){
      if(item.action==='refresh'&&!item.refreshDone){const latest=await db.prepare('SELECT id,status FROM vacancy_content_fetches WHERE candidate_id=? ORDER BY started_at DESC,rowid DESC LIMIT 1').bind(item.id).first();if(item.refreshBaseline&&latest?.id!==item.refreshBaseline&&latest?.status==='success')item.refreshDone=true;else{item.refreshBaseline=latest?.id||'none';await persist();await fetchCandidateMarkdown({...env,DB:db},item.id,{refresh:true,confirmed:true,signal});item.refreshDone=true;await persist();}}
      else if(item.action!=='refresh')await fetchCandidateMarkdown({...env,DB:db},item.id,{signal});
     }
     if(item.action!=='fetch')await extractCandidateMarkdown({...env,DB:db},item.id,{signal});
     item.outcome='complete';item.error=null;
    }catch(e){if(['restored','detail_changed'].includes(e.code))throw e;const code=operationErrorCode(e);item.error={code,message:e.status?e.message:'Выполнение прервано. Сохранённые этапы доступны для продолжения.'};if(!busy(code))item.attempts++;
     if(pause(code)){item.outcome='queued';reason=item.error.message;}else if(transient(code)&&item.attempts<=3){item.outcome='queued';item.nextAt=Date.now()+[250,1000,4000][item.attempts-1];}else item.outcome='error';
    }
    await persist();if(reason)break;
    // Retry only the same incomplete stage, with durable backoff and <=3 retries.
    if(item.outcome==='queued'&&item.nextAt){const remaining=deadline-Date.now();if(remaining<item.nextAt-Date.now()+100){reason='Ожидание повторной попытки.';break;} // handled in another slice; no unbounded loop
     index--;continue;}
   }
   const complete=items.every(i=>i.outcome==='complete');await db.prepare("UPDATE vacancy_detail_jobs SET items_json=?,state=?,reason=?,duration_ms=duration_ms+?,owner_token='',lease_until=0,updated_at=? WHERE id=? AND owner_token=?").bind(JSON.stringify(items),complete?'completed':'paused',complete?'':reason||'Есть ошибки. Исправьте причину и продолжите пакет.',Date.now()-started,Date.now(),id,owner).run();return readDetailJob(base,id);
  }catch(e){await base.prepare("UPDATE vacancy_detail_jobs SET state='paused',reason=?,owner_token='',lease_until=0 WHERE id=? AND generation=? AND owner_token=?").bind('Обработчик прерван; сохранённые этапы не потеряны.',id,generation,owner).run().catch(()=>{});throw e;}
 },{ttl:150000});
}
export async function drainDetailJobs(env,{sliceMs=24000}={}){const db=await candidateDb(env.DB),deadline=Date.now()+sliceMs,rows=(await db.prepare("SELECT id FROM vacancy_detail_jobs WHERE generation=? AND state='queued' ORDER BY created_at LIMIT 20").bind(boundGeneration(db)).all()).results||[];for(const row of rows){if(Date.now()+100>=deadline)break;try{const job=await runDetailJob(env,row.id,{sliceMs:deadline-Date.now()});if(job.state!=='completed')break;}catch{break;}}}
export function startDetailJobs(env,ctx,ids,{resume=false}={}){if(!ids.length)return 'empty';if(!ctx?.waitUntil)return 'manual_resume';ctx.waitUntil((async()=>{const deadline=Date.now()+24000;for(const id of ids){if(Date.now()+100>=deadline)break;const job=await runDetailJob(env,id,{sliceMs:deadline-Date.now(),resume});if(job.state!=='completed')break;}})().catch(()=>{}));return 'background';}
export async function detailJobsRoute(request,env,url,user,ctx){
 if(!user.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);if(!env.DB)return jsonResponse({error:'Хранилище не подключено.'},503);if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  if(url.pathname==='/api/vacancy-details'&&request.method==='GET')return jsonResponse(await detailDashboard(env.DB));
  const detail=url.pathname.match(/^\/api\/vacancy-details\/(\d+)$/);if(detail&&request.method==='GET')return jsonResponse(await vacancyDetailState(env.DB,Number(detail[1]),{includeMarkdown:true}));
  const job=url.pathname.match(/^\/api\/vacancy-details\/jobs\/([0-9a-f-]{36})(?:\/(resume|run))?$/);
  if(job&&request.method==='GET'&&!job[2])return jsonResponse(await readDetailJob(env.DB,job[1]));
  if(job&&request.method==='POST'&&job[2]){if(Object.keys(await readClassificationInput(request)).length)throw candidateError('Передайте пустой объект.');if(job[2]==='run')return jsonResponse(await runDetailJob(env,job[1],{resume:true,sliceMs:120000}));const found=await readDetailJob(env.DB,job[1]);return jsonResponse({job:found,execution:startDetailJobs(env,ctx,[found.id],{resume:true})},202);}
  if(url.pathname==='/api/vacancy-details/jobs'&&request.method==='POST'){const {requestId,...input}=await readClassificationInput(request);if(!uuid.test(requestId||''))throw candidateError('Нужен UUID requestId.');const queued=await enqueueDetails(env,input,'manual:'+requestId);return jsonResponse({...queued,execution:startDetailJobs(env,ctx,queued.jobIds)},202);}
  return jsonResponse({error:'Метод не поддерживается.'},405);
 }catch(e){return jsonResponse({error:e.status?e.message:'Не удалось обработать полные тексты.',code:e.code},e.status||500);}
}
