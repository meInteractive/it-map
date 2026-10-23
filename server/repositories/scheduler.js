import {parserOwner,parserOwnerName} from './parser-owner.js';
import {candidateDb,candidateHash} from './vacancy-candidates.js';
import {firecrawlSettings} from './firecrawl-settings.js';
import {SCHEDULER_DEFAULTS,SCHEDULER_SOURCE_OPTIONS,SCHEDULER_APIFY_POLICY} from './scheduler-schema.js';
import {boundGeneration,generationBoundDb} from './generation-bound-db.js';
export const SCHEDULER_TYPES=Object.keys(SCHEDULER_DEFAULTS.priorities);
export const schedulerError=(code,message,status=409)=>Object.assign(new Error(message),{code,status});
export async function schedulerSettings(db){
 const s=await firecrawlSettings(db),saved=JSON.parse(s.scheduler_json);
 // Older backup35 settings used individual source switches. Preserve their
 // selected source types, then apply the user's new global scope on every run.
 if(!Object.hasOwn(saved,'sourceIds')){
  const legacy=(await db.prepare('SELECT DISTINCT source_id FROM vacancy_source_parsing WHERE auto_enabled=1').all()).results||[];
  saved.sourceIds=legacy.map(r=>r.source_id).filter(id=>SCHEDULER_SOURCE_OPTIONS.some(s=>s.id===id&&s.supported));
 }
 return {...SCHEDULER_DEFAULTS,...saved,priorities:{...SCHEDULER_DEFAULTS.priorities,...saved.priorities},favoritesOnly:s.favorites_only!==0};
}
export function publicSchedulerSettings(s){const {cloudOwnerId,...value}=s;return {...value,cloudConnected:!!cloudOwnerId};}
export async function saveSchedulerSettings(db,input,ownerName){
 const old=await schedulerSettings(db),keys=['enabled','dayBudgetUsd','monthBudgetUsd','revision','priorities','favoritesOnly','sourceIds','intervalDays','agenciesEnabled'];
 if(!input||Object.keys(input).some(k=>!keys.includes(k))||typeof input.enabled!=='boolean'||!Number.isSafeInteger(input.revision)||!['dayBudgetUsd','monthBudgetUsd'].every(k=>typeof input[k]==='number'&&Number.isFinite(input[k])&&input[k]>=0&&input[k]<=10000))throw schedulerError('input','Передайте enabled, дневной/месячный бюджет USD (0–10000) и revision.',400);
 if(input.agenciesEnabled!==undefined&&typeof input.agenciesEnabled!=='boolean'||input.favoritesOnly!==undefined&&typeof input.favoritesOnly!=='boolean'||input.intervalDays!==undefined&&![1,3,7].includes(input.intervalDays)||input.sourceIds!==undefined&&(!Array.isArray(input.sourceIds)||input.sourceIds.length>SCHEDULER_SOURCE_OPTIONS.length||new Set(input.sourceIds).size!==input.sourceIds.length||input.sourceIds.some(id=>!SCHEDULER_SOURCE_OPTIONS.some(s=>s.id===id&&s.supported))))throw schedulerError('input','Выберите поддерживаемые источники без повторов, период 1/3/7 дней и область компаний.',400);
 if(input.priorities!=null&&(typeof input.priorities!=='object'||Array.isArray(input.priorities)))throw schedulerError('input','Приоритеты должны быть объектом.',400);
 const priorities=input.priorities?{...old.priorities,...input.priorities}:old.priorities;if(!priorities||SCHEDULER_TYPES.some(k=>!Number.isSafeInteger(priorities[k])||priorities[k]<0||priorities[k]>100)||Object.keys(priorities).some(k=>!SCHEDULER_TYPES.includes(k)))throw schedulerError('input','Приоритеты: целые0–100 для всех типов.',400);
 const {favoritesOnly,...settings}=old,next={...settings,enabled:input.enabled,agenciesEnabled:input.agenciesEnabled??old.agenciesEnabled,ownerName,dayBudgetUsd:input.dayBudgetUsd,monthBudgetUsd:input.monthBudgetUsd,priorities,sourceIds:input.sourceIds??old.sourceIds,intervalDays:input.intervalDays??old.intervalDays,revision:old.revision+1};
 const r=await db.prepare("UPDATE firecrawl_settings SET scheduler_json=?,revision=revision+CASE WHEN ? IS NOT NULL AND favorites_only<>? THEN 1 ELSE 0 END,favorites_only=coalesce(?,favorites_only) WHERE id=1 AND json_extract(scheduler_json,'$.revision')=?").bind(JSON.stringify(next),input.favoritesOnly===undefined?null:Number(input.favoritesOnly),input.favoritesOnly===undefined?null:Number(input.favoritesOnly),input.favoritesOnly===undefined?null:Number(input.favoritesOnly),input.revision).run();if(!r.meta?.changes)throw schedulerError('changed','Настройки планировщика изменились. Обновите страницу.');return publicSchedulerSettings(await schedulerSettings(db));
}
export async function enqueueScheduledJob(db,{type,key,companyId=null,agencyId=null,candidateId=null,sourceId=null,payload={},runAfter=Date.now(),priority,maxAttempts=4}){
 db=await candidateDb(db);if(agencyId!=null)parserOwner({companyId,agencyId});if(!SCHEDULER_TYPES.includes(type)||typeof key!=='string'||key.length>500||!Number.isSafeInteger(runAfter))throw schedulerError('input','Некорректное задание.',400);
 const settings=await schedulerSettings(db),id=crypto.randomUUID(),generation=boundGeneration(db),now=Date.now(),dedupe=await candidateHash(key);
 await db.prepare("INSERT OR IGNORE INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)").bind(id,generation,dedupe,type,companyId,agencyId,candidateId,sourceId,JSON.stringify(payload),priority??settings.priorities[type],runAfter,maxAttempts,now,now).run();
 return db.prepare("SELECT * FROM scheduler_jobs WHERE generation=? AND (dedupe_key=? OR (type='scan_source' AND ?='scan_source' AND (company_id IS NOT NULL OR agency_id IS NOT NULL) AND company_id IS ? AND agency_id IS ? AND source_id=? AND status IN ('pending','running'))) ORDER BY created_at LIMIT 1").bind(generation,dedupe,type,companyId,agencyId,sourceId).first();
}
export async function enqueueScheduledJobs(db,jobs){
 db=await candidateDb(db);if(!Array.isArray(jobs)||jobs.length>200)throw schedulerError('input','Некорректный пакет заданий.',400);
 const settings=await schedulerSettings(db),generation=boundGeneration(db),now=Date.now();
 const statements=await Promise.all(jobs.map(async j=>{
  if(j.agencyId!=null)parserOwner(j);
  if(!SCHEDULER_TYPES.includes(j.type)||typeof j.key!=='string'||j.key.length>500)throw schedulerError('input','Некорректное задание.',400);
  return db.prepare('INSERT OR IGNORE INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,priority,run_after,max_attempts,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)').bind(crypto.randomUUID(),generation,await candidateHash(j.key),j.type,j.companyId??null,j.agencyId??null,j.candidateId??null,j.sourceId??null,JSON.stringify(j.payload||{}),settings.priorities[j.type],now,4,now,now);
 }));
 let added=0;for(let i=0;i<statements.length;i+=50){const results=await db.batch(statements.slice(i,i+50));added+=results.reduce((n,r)=>n+(r.meta?.changes||0),0);}return added;
}
export async function recoverExpiredSchedulerRuns(db,now=Date.now(),excludeId=''){
 // Never revive the expired owner: fence it off and retain its terminal receipt.
 // Provider reservations and unexpired job/LLM leases remain conservative.
 await db.batch([
  db.prepare("UPDATE scheduler_attempts SET finished_at=?,status='interrupted',reason_code='interrupted',message='Обработчик прервался; сохранённый этап продолжит очередь.' WHERE status='running' AND run_id IN (SELECT id FROM scheduler_runs WHERE status='running' AND lease_until<=? AND id<>?)").bind(now,now,excludeId),
  db.prepare("UPDATE scheduler_runs SET status='interrupted',finished_at=?,lease_until=0,reason='Обработчик прервался' WHERE status='running' AND lease_until<=? AND id<>?").bind(now,now,excludeId),
  db.prepare("UPDATE scheduler_jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed' ELSE 'pending' END,locked_by='',locked_until=0,run_after=MAX(run_after,?),reason_code='interrupted',last_error='Предыдущая попытка прервана; результаты и резерв расходов сохранены.',updated_at=? WHERE generation=? AND status='running' AND locked_until<=?").bind(now,now,boundGeneration(db),now)
 ]);
}
export async function beginSchedulerRun(db,{trigger='manual',requestId=crypto.randomUUID(),durationMs=110000,diagnostic=false}={}){
 db=await candidateDb(db);const generation=boundGeneration(db),now=Date.now(),id=await candidateHash(generation+':'+requestId),settings=await schedulerSettings(db);
 const old=await db.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(id).first();if(old){
  if(old.status==='running'&&old.lease_until<=now){await recoverExpiredSchedulerRuns(db,now);return {db,run:await db.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(id).first(),reused:true};}
  return {db,run:old,reused:true};
 }
 // One owner across generations: restore must not let a second paid worker start.
 const acquired=await db.prepare("INSERT OR IGNORE INTO scheduler_runs(id,generation,trigger,started_at,status,lease_until) SELECT ?,?,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM scheduler_runs WHERE status='running' AND lease_until>?)").bind(id,generation,trigger,now,settings.enabled||diagnostic?'running':'paused',settings.enabled||diagnostic?now+durationMs+30000:0,now).run();
 if(!acquired.meta?.changes)throw schedulerError('scheduler_busy','Другой цикл диспетчера ещё выполняется.');
 const run=await db.prepare('SELECT * FROM scheduler_runs WHERE id=?').bind(id).first();
 if(!settings.enabled&&!diagnostic)return {db,run,reused:false};
 // Expired owners cannot write through this facade; their provider reservations
 // stay conservative in the separate ledger, including after backup restore.
 const owned=generationBoundDb(db,generation,{sql:"EXISTS(SELECT 1 FROM scheduler_runs WHERE id=? AND generation=? AND status='running' AND lease_until>CAST((julianday('now')-2440587.5)*86400000 AS INTEGER))",bindings:[id,generation],failure:()=>schedulerError('scheduler_changed','Обработчик потерял владение циклом.')});
 await recoverExpiredSchedulerRuns(owned,now,id);
 return {db:owned,baseDb:db,run,reused:false};
}
export async function claimScheduledJob(db,runId,{now=Date.now(),leaseMs=300000,diagnostic=false,types=SCHEDULER_TYPES,allowApify=true,allowApifyStarts=true,jobId=null,deferredApifyOnly=false}={}){
 const settings=await schedulerSettings(db);if(!settings.enabled&&!diagnostic)return null;const generation=boundGeneration(db),owner=crypto.randomUUID();
 if(!Array.isArray(types)||types.some(t=>!SCHEDULER_TYPES.includes(t)))throw schedulerError('input','Неизвестный тип задания.',400);if(!types.length)return null;
 // Match the exact cursor page: an old page's terminal ledger must never
 // authorize another paid page after this tick has exhausted its POST quota.
 const existingPage=`EXISTS(SELECT 1 FROM apify_runs a WHERE a.generation=scheduler_jobs.generation AND a.operation_key='listing:'||coalesce(json_extract(scheduler_jobs.payload,'$.runId'),scheduler_jobs.id)||':'||coalesce((SELECT json_extract(d.result_json,'$.paused.apify.page') FROM vacancy_discovery_runs d WHERE d.generation=scheduler_jobs.generation AND d.id=coalesce(json_extract(scheduler_jobs.payload,'$.runId'),scheduler_jobs.id)),0))`;
 const r=await db.prepare(`UPDATE scheduler_jobs SET status='running',locked_by=?,locked_until=?,attempts=attempts+1,updated_at=? WHERE id=(SELECT id FROM scheduler_jobs WHERE generation=? AND status='pending' AND (agency_id IS NULL OR coalesce(json_extract((SELECT scheduler_json FROM firecrawl_settings WHERE id=1),'$.agenciesEnabled'),0)=1) AND run_after<=? AND (? IS NULL OR id=?) AND (?=0 OR type='diagnostic') AND type IN (SELECT value FROM json_each(?)) AND (?=0 OR source_id='hh' AND type='scan_source' AND reason_code IN ('apify_busy','apify_budget') AND run_after<=?-300000) AND (?=1 OR source_id IS NULL OR source_id<>'hh' OR type NOT IN ('scan_source','fetch_full_text')) AND (?=1 OR source_id IS NULL OR source_id<>'hh' OR type<>'scan_source' OR ${existingPage}) AND (candidate_id IS NULL OR candidate_id NOT IN (SELECT candidate_id FROM scheduler_jobs WHERE generation=? AND status='running' AND candidate_id IS NOT NULL)) ORDER BY priority+CAST((?-created_at)/3600000 AS INTEGER)-CASE WHEN source_id='hh' AND type='scan_source' THEN ? ELSE 0 END DESC,CASE WHEN source_id='hh' AND type='scan_source' AND ${existingPage} THEN 1 ELSE 0 END DESC,run_after,created_at,id LIMIT 1) AND status='pending'`).bind(owner,now+leaseMs,now,generation,now,jobId,jobId,diagnostic?1:0,JSON.stringify(types),deferredApifyOnly?1:0,now,allowApify?1:0,allowApifyStarts?1:0,generation,now,SCHEDULER_APIFY_POLICY.priorityPenalty).run();
 if(!r.meta?.changes)return null;const job=await db.prepare('SELECT * FROM scheduler_jobs WHERE locked_by=?').bind(owner).first();
 const jobDb=generationBoundDb(db,generation,{sql:"EXISTS(SELECT 1 FROM scheduler_jobs WHERE id=? AND generation=? AND status='running' AND locked_by=? AND locked_until>CAST((julianday('now')-2440587.5)*86400000 AS INTEGER))",bindings:[job.id,generation,owner],failure:()=>schedulerError('job_changed','Задание уже принадлежит другому обработчику.')});
 return {job,db:jobDb,runId,owner};
}
export async function finishScheduledJob(db,claim,{status='completed',code='',message='',delayMs=0,payload,result={},deferred=false}={}){
 const j=claim.job,now=Date.now();await db.prepare("UPDATE scheduler_jobs SET status=?,attempts=MAX(0,attempts-?),reason_code=?,last_error=?,run_after=?,payload=?,locked_by='',locked_until=0,completed_at=?,updated_at=? WHERE id=? AND generation=? AND locked_by=? AND status='running' AND locked_until>?").bind(status,deferred?1:0,code,message,now+delayMs,JSON.stringify(payload??JSON.parse(j.payload)),['completed','failed','cancelled'].includes(status)?now:null,now,j.id,boundGeneration(db),claim.owner,now).run();
 return result;
}
export async function readScheduledQueue(db,{status='',type='',companyId='',agencyId='',search='',page=1}={}){
 db=await candidateDb(db);page=Number(page);if(!Number.isSafeInteger(page)||page<1||page>100000||status&&!['pending','running','completed','failed','cancelled'].includes(status)||type&&!SCHEDULER_TYPES.includes(type)||[companyId,agencyId].some(id=>id&&(!Number.isSafeInteger(Number(id))||Number(id)<1))||companyId&&agencyId||typeof search!=='string'||search.length>200)throw schedulerError('input','Некорректные фильтры очереди.',400);
 const values=[boundGeneration(db),status,status,type,type,companyId||null,companyId||null,agencyId||null,agencyId||null];let where="generation=? AND (?='' OR status=?) AND (?='' OR type=?) AND (? IS NULL OR company_id=?) AND (? IS NULL OR agency_id=?)";
 if(search.trim()){
  const needle=search.trim().toLocaleLowerCase('ru-RU'),owners=(await db.prepare("SELECT DISTINCT 'company' kind,i.id,i.company_name name FROM scheduler_jobs j JOIN company_identity i ON i.id=j.company_id WHERE j.generation=? UNION ALL SELECT DISTINCT 'agency' kind,i.id,i.agency_name name FROM scheduler_jobs j JOIN agency_identity i ON i.id=j.agency_id WHERE j.generation=?").bind(boundGeneration(db),boundGeneration(db)).all()).results||[],matched=owners.filter(o=>o.name.toLocaleLowerCase('ru-RU').includes(needle));
  where+=" AND (company_id IN (SELECT value FROM json_each(?)) OR agency_id IN (SELECT value FROM json_each(?)))";values.push(JSON.stringify(matched.filter(o=>o.kind==='company').map(o=>o.id)),JSON.stringify(matched.filter(o=>o.kind==='agency').map(o=>o.id)));
 }
 const total=(await db.prepare('SELECT count(*) n FROM scheduler_jobs WHERE '+where).bind(...values).first()).n;
 const rows=(await db.prepare(`SELECT scheduler_jobs.*,${parserOwnerName('scheduler_jobs.')} AS companyName,(SELECT title_raw FROM vacancy_candidates WHERE id=scheduler_jobs.candidate_id) AS candidateTitle FROM scheduler_jobs WHERE `+where+' ORDER BY created_at DESC,id LIMIT 30 OFFSET ?').bind(...values,(page-1)*30).all()).results||[];return {jobs:rows.map(({payload,...row})=>row),total,page,pageSize:30};
}
