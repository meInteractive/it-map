import {parserOwner,parserOwnerKey,parserOwnerName} from './parser-owner.js';
import {ensureCrmSchema} from './schema.js';
import {ensureRuntimeGeneration} from './runtime-generation.js';
import {generationBoundDb,boundGeneration} from './generation-bound-db.js';
import {activeClassificationRoles} from './classification-prompts.js';
import {JEV_MODEL_ID} from '../integrations/jev.js';
import {VACANCY_SOURCE_META} from '../constants.js';

export const candidateError=(message,status=400)=>Object.assign(new Error(message),{status});
export async function candidateHash(value){return [...new Uint8Array(await crypto.subtle.digest('SHA-256',new TextEncoder().encode(value)))].map(n=>n.toString(16).padStart(2,'0')).join('');}
export function candidateId(value){const id=Number(value);if(!Number.isSafeInteger(id)||id<1)throw candidateError('Некорректный ID.');return id;}
export async function candidateDb(db){await ensureCrmSchema(db);return boundGeneration(db)?db:generationBoundDb(db,await ensureRuntimeGeneration(db));}
const text=(value,label,max,{optional=false,raw=false}={})=>{if(value==null&&optional)return null;if(typeof value!=='string'||value.includes('\0')||Array.from(value).length>max||(!optional&&!value.trim()))throw candidateError('Некорректное поле «'+label+'».');return raw?value:value.trim()||null;};
export function candidateUrl(value){if(value==null||value==='')return null;const original=text(value,'URL',2048);let url;try{url=new URL(original);}catch{throw candidateError('Укажите полный HTTP(S) URL.');}if(!['https:','http:'].includes(url.protocol)||url.username||url.password)throw candidateError('Укажите HTTP(S) URL без логина и пароля.');return {original,url};}
export function canonicalCandidateUrl(value){const parsed=candidateUrl(value);if(!parsed)return null;const {url}=parsed;url.hash='';for(const key of [...url.searchParams.keys()])if(/^utm_/i.test(key)||/^(gclid|fbclid|yclid|msclkid)$/i.test(key))url.searchParams.delete(key);url.searchParams.sort();return url.href;}

async function inputForCandidate(db,input){
 const allowed=['titleRaw','vacancyUrl','companyId','agencyId','companyNameRaw','sourceUrl','sourceId','sourceNamespace','externalJobId','importKey','acquisitionMethod'];
 if(!input||Array.isArray(input)||typeof input!=='object'||Object.keys(input).some(key=>!allowed.includes(key)))throw candidateError('Передайте только поля первичной вакансии.');
 const titleRaw=text(input.titleRaw,'Заголовок',500,{raw:true}),companyId=input.companyId==null?null:candidateId(input.companyId),agencyId=input.agencyId==null?null:candidateId(input.agencyId);
 if(companyId!==null&&agencyId!==null)throw candidateError('Вакансия не может принадлежать компании и агентству одновременно.');
 if(agencyId!==null&&!await db.prepare('SELECT id FROM agency_identity WHERE id=?').bind(agencyId).first())throw candidateError('Агентство с таким ID не найдено.');
 if(companyId!=null&&!await db.prepare('SELECT id FROM company_identity WHERE id=?').bind(companyId).first())throw candidateError('Компания с таким ID не найдена.');
 const companyNameRaw=text(input.companyNameRaw??'','Название компании',500,{optional:true,raw:true})||'';
 const vacancyUrl=candidateUrl(input.vacancyUrl)?.original||null,canonicalUrl=canonicalCandidateUrl(vacancyUrl),sourceUrl=candidateUrl(input.sourceUrl)?.original||null;
 const sourceId=text(input.sourceId,'Источник',120,{optional:true});if(sourceId&&!VACANCY_SOURCE_META.some(([id])=>id===sourceId))throw candidateError('Источник не найден в существующем справочнике.');
 const externalJobId=text(input.externalJobId,'Внешний ID',500,{optional:true}),importKey=text(input.importKey,'Ключ публикации',500,{optional:true});
 const explicitNamespace=text(input.sourceNamespace,'Пространство источника',500,{optional:true});
 // Discovery pages can link multiple publishers. Include the publication
 // origin and concrete source (local IDs can repeat in different channels).
 // Adapters may supply a stable explicit namespace across changing pages.
 const publisherOrigin=canonicalUrl?new URL(canonicalUrl).origin:sourceUrl?new URL(sourceUrl).origin:null;
 const sourceNamespace=explicitNamespace||(publisherOrigin?'scope:'+await candidateHash(JSON.stringify([publisherOrigin,canonicalCandidateUrl(sourceUrl)])):null);
 const method=input.acquisitionMethod||'manual';if(!['site','firecrawl','excel','manual'].includes(method))throw candidateError('Неизвестный способ получения.');
 if((externalJobId||importKey)&&!sourceNamespace)throw candidateError('Для внешнего ID или ключа импорта укажите пространство источника.');
 const keys=[];
 if(externalJobId)keys.push('external:'+await candidateHash(JSON.stringify([sourceNamespace,externalJobId])));
 if(canonicalUrl)keys.push('url:'+await candidateHash(canonicalUrl));
 if(importKey)keys.push('import:'+await candidateHash(JSON.stringify([sourceNamespace,importKey])));
 if(!keys.length)throw candidateError('Нужен URL, внешний ID или устойчивый ключ публикации. Заголовок не является идентификатором.');
 return {titleRaw,titleHash:await candidateHash(titleRaw.trim()),companyId,agencyId,companyNameRaw,vacancyUrl,canonicalUrl,sourceUrl,sourceId,sourceNamespace,externalJobId,importKey,method,keys};
}

export async function upsertVacancyCandidate(db,input,{onlyNew=false,creationToken,preserveCompany=false,intakeParser}={}){
 db=await candidateDb(db);const value=await inputForCandidate(db,input),keys=JSON.stringify(value.keys),token=creationToken??crypto.randomUUID(),now=new Date().toISOString();
 const parser=intakeParser??(value.method==='site'?'unknown':value.method);
 if(!['apify','firecrawl','http','manual','excel','unknown'].includes(parser))throw candidateError('Неизвестный парсер получения.');
 if(creationToken!==undefined&&(!onlyNew||!/^replay:[a-f0-9]{64}:[a-f0-9]{64}$/.test(creationToken)))throw candidateError('Некорректный внутренний идентификатор восстановления.');
 const matched=`SELECT candidate_id FROM vacancy_candidate_identities WHERE identity_key IN (SELECT value FROM json_each(?))`;
 const target=`SELECT candidate_id FROM vacancy_candidate_identities WHERE identity_key IN (SELECT value FROM json_each(?)) ORDER BY CASE WHEN identity_key=? THEN 0 ELSE 1 END LIMIT 1`;
 try{
  const result=await db.batch([
   // Fail within the same transaction if aliases point at different records.
   db.prepare(`SELECT CASE WHEN (SELECT count(DISTINCT candidate_id) FROM (${matched}))<=1 THEN 1 ELSE abs(-9223372036854775808) END AS identity_guard`).bind(keys),
   db.prepare(`SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM vacancy_candidates WHERE id IN (${matched}) AND ? IS NOT NULL AND source_namespace=? AND external_job_id IS NOT NULL AND external_job_id<>?) AND NOT EXISTS(SELECT 1 FROM vacancy_candidates WHERE id IN (${matched}) AND (? IS NOT NULL OR agency_id IS NOT NULL) AND (? IS NOT NULL OR ? IS NOT NULL) AND (company_id IS NOT ? OR agency_id IS NOT ?) AND (company_id IS NOT NULL OR agency_id IS NOT NULL)) THEN 1 ELSE abs(-9223372036854775808) END AS external_guard`).bind(keys,value.externalJobId,value.sourceNamespace,value.externalJobId,keys,value.agencyId,value.companyId,value.agencyId,value.companyId,value.agencyId),
   db.prepare(`INSERT INTO vacancy_candidates(creation_token,company_id,agency_id,company_name_raw,title_raw,title_hash,vacancy_url,canonical_url,source_url,source_id,source_namespace,external_job_id,import_key,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)
    SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE NOT EXISTS(${matched})`).bind(token,value.companyId,value.agencyId,value.companyNameRaw,value.titleRaw,value.titleHash,value.vacancyUrl,value.canonicalUrl,value.sourceUrl,value.sourceId,value.sourceNamespace,value.externalJobId,value.importKey,value.method,now,now,now,now,keys),
   // changes() is the preceding candidate INSERT: stable retries cannot add
   // aliases or UPDATE a row created earlier, even by this same replay.
   db.prepare(`INSERT INTO vacancy_candidate_identities(identity_key,candidate_id)
    SELECT value,coalesce((${target}),(SELECT id FROM vacancy_candidates WHERE creation_token=?)) FROM json_each(?) WHERE (?=0 OR changes()>0) ON CONFLICT(identity_key) DO NOTHING`).bind(keys,value.keys[0],token,keys,onlyNew?1:0),
   db.prepare(`UPDATE vacancy_candidates SET
    title_updated_at=CASE WHEN title_raw<>? THEN ? ELSE title_updated_at END,
    title_revision=title_revision+CASE WHEN title_hash<>? THEN 1 ELSE 0 END,title_raw=?,title_hash=?,
    company_id=CASE WHEN ? IS NOT NULL THEN NULL ELSE coalesce(?,company_id) END,agency_id=CASE WHEN ? IS NOT NULL THEN NULL ELSE coalesce(?,agency_id) END,company_name_raw=CASE WHEN ?<>'' THEN ? ELSE company_name_raw END,
    vacancy_url=coalesce(?,vacancy_url),canonical_url=coalesce(?,canonical_url),source_url=coalesce(?,source_url),source_id=coalesce(?,source_id),
    source_namespace=coalesce(?,source_namespace),external_job_id=coalesce(?,external_job_id),import_key=coalesce(?,import_key),acquisition_method=?,last_seen_at=max(last_seen_at,?),updated_at=?
    WHERE id=(${target}) AND ?=0 AND (?=0 OR (company_id IS NULL AND agency_id IS NULL) OR (company_id IS ? AND agency_id IS ?))`).bind(value.titleRaw,now,value.titleHash,value.titleRaw,value.titleHash,value.agencyId,value.companyId,value.companyId,value.agencyId,value.companyNameRaw,value.companyNameRaw,value.vacancyUrl,value.canonicalUrl,value.sourceUrl,value.sourceId,value.sourceNamespace,value.externalJobId,value.importKey,value.method,now,now,keys,value.keys[0],onlyNew?1:0,preserveCompany?1:0,value.companyId,value.agencyId),
   db.prepare('INSERT OR IGNORE INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) SELECT creation_token,?,source_id,first_seen_at FROM vacancy_candidates WHERE creation_token=?').bind(parser,token),
   db.prepare(`SELECT id,creation_token=? AS created FROM vacancy_candidates WHERE id=(${target})`).bind(token,keys,value.keys[0])
  ]);
  const row=result.at(-1).results?.[0];if(!row)throw candidateError('Не удалось установить идентичность публикации.',409);
  const candidate=await candidateSummary(db,row.id);return {id:row.id,created:!!row.created,...(onlyNew?{inserted:!!result[2].meta?.changes}:{}),candidate};
 }catch(error){if(error.code==='restored')throw error;if(/overflow|identity_guard|external_guard/i.test(error.message))throw Object.assign(candidateError('Идентификаторы публикации конфликтуют. Автоматическое объединение отменено.',409),{code:'candidate_identity'});throw error;}
}

export async function candidateClassificationContext(db){
 const roles=await activeClassificationRoles(db);
 return Promise.all(roles.map(async role=>({...role,promptVersion:await candidateHash(role.markdown),modelKey:JEV_MODEL_ID})));
}
const ACTIVE_CTE=`active AS (SELECT CAST(json_extract(value,'$.roleId') AS INTEGER) role_id,json_extract(value,'$.promptVersion') prompt_version,json_extract(value,'$.modelKey') model_key FROM json_each(?))`;
const currentDecision=`SELECT x.id FROM vacancy_classifications x WHERE x.candidate_id=c.id AND x.root_role_id=a.role_id AND x.title_hash=c.title_hash AND x.title_revision=c.title_revision AND x.prompt_version=a.prompt_version AND x.model_key=a.model_key ORDER BY x.id DESC LIMIT 1`;
const STATUS_SQL=`CASE
 WHEN c.main_vacancy_id IS NOT NULL THEN 'imported'
 WHEN NOT EXISTS(SELECT 1 FROM active) THEN 'no_active_roles'
 WHEN EXISTS(SELECT 1 FROM active a JOIN vacancy_classifications d ON d.id=(${currentDecision}) WHERE d.decision='match') THEN 'accepted'
 WHEN NOT EXISTS(SELECT 1 FROM active a WHERE NOT EXISTS(${currentDecision})) THEN 'rejected'
 WHEN EXISTS(SELECT 1 FROM active a JOIN vacancy_classification_attempts t ON t.candidate_id=c.id AND t.title_revision=c.title_revision AND t.title_hash=c.title_hash AND t.model_key=a.model_key JOIN json_each(t.errors_json) e ON json_extract(e.value,'$.roleId')=a.role_id AND json_extract(e.value,'$.promptVersion')=a.prompt_version WHERE NOT EXISTS(${currentDecision})) THEN 'classification_error'
 ELSE 'pending_classification' END`;
const COMPLETENESS_SQL=`EXISTS(SELECT 1 FROM active) AND NOT EXISTS(SELECT 1 FROM active a WHERE NOT EXISTS(${currentDecision}))`;
const NEEDS_SQL=`c.main_vacancy_id IS NULL AND EXISTS(SELECT 1 FROM active a WHERE NOT EXISTS(${currentDecision}))`;
const FIELDS=`c.id,c.company_id AS companyId,c.agency_id AS agencyId,CASE WHEN c.agency_id IS NULL THEN 'company' ELSE 'agency' END AS entityType,${parserOwnerName('c.')} AS companyName,c.company_name_raw AS companyNameRaw,c.title_raw AS titleRaw,c.title_hash AS titleHash,c.title_revision AS titleRevision,c.vacancy_url AS vacancyUrl,c.canonical_url AS canonicalUrl,c.source_url AS sourceUrl,c.source_id AS sourceId,c.source_namespace AS sourceNamespace,c.external_job_id AS externalJobId,c.import_key AS importKey,c.acquisition_method AS acquisitionMethod,c.first_seen_at AS firstSeenAt,c.last_seen_at AS lastSeenAt,c.title_updated_at AS titleUpdatedAt,c.main_vacancy_id AS mainVacancyId,c.created_at AS createdAt,c.updated_at AS updatedAt`;
export const CANDIDATE_STATUSES=['pending_classification','accepted','rejected','classification_error','imported','no_active_roles'];
const activeJson=roles=>JSON.stringify(roles.map(({roleId,promptVersion,modelKey})=>({roleId,promptVersion,modelKey})));
export const candidateStatusProjection=roles=>({cte:ACTIVE_CTE,sql:STATUS_SQL,bindings:[activeJson(roles)]});
export async function candidateSummary(db,id,roles){roles=roles||await candidateClassificationContext(db);return db.prepare(`WITH ${ACTIVE_CTE} SELECT ${FIELDS},${STATUS_SQL} AS processingStatus,${COMPLETENESS_SQL} AS classificationComplete,${NEEDS_SQL} AS needsClassification FROM vacancy_candidates c WHERE c.id=?`).bind(activeJson(roles),candidateId(id)).first();}
function pageNumber(value,fallback,max){const number=value==null?fallback:Number(value);if(!Number.isSafeInteger(number)||number<1||number>max)throw candidateError('Некорректная страница или размер выборки.');return number;}

export async function getVacancyCandidates(db,filters={}){
 db=await candidateDb(db);const roles=await candidateClassificationContext(db),page=pageNumber(filters.page,1,100000),pageSize=pageNumber(filters.pageSize,50,100),order=filters.order||'desc';
 if(!['asc','desc'].includes(order))throw candidateError('Некорректная сортировка.');
 const conditions=[],args=[];
 if(filters.agencyId!=null){conditions.push('agencyId=?');args.push(candidateId(filters.agencyId));}
 if(filters.companyId!=null){conditions.push('companyId=?');args.push(candidateId(filters.companyId));}
 if(filters.sourceId!=null){conditions.push('sourceId=?');args.push(text(filters.sourceId,'Источник',120));}
 if(filters.sourceNamespace!=null){conditions.push('sourceNamespace=?');args.push(text(filters.sourceNamespace,'Пространство источника',500));}
 if(filters.status!=null){if(!CANDIDATE_STATUSES.includes(filters.status))throw candidateError('Неизвестное состояние.');conditions.push('processingStatus=?');args.push(filters.status);}
 const cte=`WITH ${ACTIVE_CTE}, projected AS (SELECT ${FIELDS},${STATUS_SQL} AS processingStatus,${COMPLETENESS_SQL} AS classificationComplete,${NEEDS_SQL} AS needsClassification FROM vacancy_candidates c)`,where=conditions.length?' WHERE '+conditions.join(' AND '):'';
 const result=await db.batch([
  db.prepare(cte+' SELECT count(*) AS total FROM projected'+where).bind(activeJson(roles),...args),
  db.prepare(cte+' SELECT * FROM projected'+where+` ORDER BY lastSeenAt ${order},id ${order} LIMIT ? OFFSET ?`).bind(activeJson(roles),...args,pageSize,(page-1)*pageSize)
 ]);
 return {candidates:result[1].results||[],total:result[0].results?.[0]?.total||0,page,pageSize};
}


export async function getVacancyCandidate(db,id,options={}){
 db=await candidateDb(db);id=candidateId(id);const roles=await candidateClassificationContext(db);
 const historyPage=pageNumber(options.historyPage,1,100000),historyPageSize=pageNumber(options.historyPageSize,50,100);
 // Candidate state, current decisions and history are one transactional read,
 // so a completing classifier cannot produce a mixed before/after response.
 const reads=await db.batch([
  db.prepare(`WITH ${ACTIVE_CTE} SELECT ${FIELDS},${STATUS_SQL} AS processingStatus,${COMPLETENESS_SQL} AS classificationComplete,${NEEDS_SQL} AS needsClassification FROM vacancy_candidates c WHERE c.id=?`).bind(activeJson(roles),id),
  db.prepare(`WITH ${ACTIVE_CTE} SELECT a.role_id AS roleId,d.id,d.decision,d.prompt_version AS promptVersion,d.prompt_revision AS promptRevision,d.model_id AS modelId,d.attempt_id AS attemptId,d.classified_at AS classifiedAt
   FROM vacancy_candidates c CROSS JOIN active a LEFT JOIN vacancy_classifications d ON d.id=(${currentDecision}) WHERE c.id=?`).bind(activeJson(roles),id),
  db.prepare('SELECT count(*) AS total FROM vacancy_classifications WHERE candidate_id=?').bind(id),
  db.prepare(`SELECT d.*,t.duration_ms AS batch_duration_ms,t.cost_usd AS batch_cost_usd,t.title_raw AS classified_title FROM vacancy_classifications d JOIN vacancy_classification_attempts t ON t.id=d.attempt_id WHERE d.candidate_id=? ORDER BY d.id DESC LIMIT ? OFFSET ?`).bind(id,historyPageSize,(historyPage-1)*historyPageSize),
  db.prepare('SELECT count(*) AS total FROM vacancy_classification_attempts WHERE candidate_id=?').bind(id),
  db.prepare('SELECT * FROM vacancy_classification_attempts WHERE candidate_id=? ORDER BY classified_at DESC,id DESC LIMIT ? OFFSET ?').bind(id,historyPageSize,(historyPage-1)*historyPageSize),
  db.prepare('SELECT identity_key FROM vacancy_candidate_identities WHERE candidate_id=? ORDER BY identity_key LIMIT 100').bind(id)
 ]);
 const candidate=reads[0].results?.[0];if(!candidate)return null;
 const byId=new Map((reads[1].results||[]).map(row=>[row.roleId,row]));
 const currentResults=roles.map(role=>({roleId:role.roleId,name:role.name,expectedPromptVersion:role.promptVersion,currentPromptRevision:role.revision,...byId.get(role.roleId),decision:byId.get(role.roleId)?.decision||null}));
 return {candidate,currentResults,needsClassification:candidate.mainVacancyId==null&&roles.some(role=>!byId.get(role.roleId)?.decision),history:{items:reads[3].results||[],total:reads[2].results?.[0]?.total||0,page:historyPage,pageSize:historyPageSize},attempts:{items:reads[5].results||[],total:reads[4].results?.[0]?.total||0,page:historyPage,pageSize:historyPageSize},identityKeys:(reads[6].results||[]).map(row=>row.identity_key)};
}

export async function linkCandidateToMainVacancy(db,id,mainId){
 db=await candidateDb(db);id=candidateId(id);mainId=candidateId(mainId);
 const result=await db.prepare(`UPDATE vacancy_candidates SET main_vacancy_id=?,updated_at=? WHERE id=? AND (main_vacancy_id IS NULL OR main_vacancy_id=?)
  AND EXISTS(SELECT 1 FROM company_vacancies v WHERE v.id=? AND (company_id IS NULL OR EXISTS(SELECT 1 FROM company_identity i WHERE i.id=vacancy_candidates.company_id AND i.company_name=v.company_name AND v.entity_type='company')))`)
 .bind(mainId,new Date().toISOString(),id,mainId,mainId).run();
 if(!result.meta?.changes)throw candidateError('Первичная или основная вакансия не найдена, компания не совпадает либо связь уже задана.',409);
 return getVacancyCandidate(db,id);
}

// One projection for statistics and selection; accepted can still lack roles.
export async function selectClassificationCandidates(db,input={}){
 db=await candidateDb(db);const allowed=['candidateId','companyId','agencyId','sourceId','ids','limit','force'];
 if(!input||Array.isArray(input)||Object.keys(input).some(k=>!allowed.includes(k)))throw candidateError('Неизвестный фильтр классификации.');
 if(input.force!==undefined&&typeof input.force!=='boolean')throw candidateError('Некорректный force.');
 const limit=pageNumber(input.limit,50,50),roles=await candidateClassificationContext(db),conditions=['mainVacancyId IS NULL',"(agencyId IS NULL OR coalesce(json_extract((SELECT scheduler_json FROM firecrawl_settings WHERE id=1),'$.agenciesEnabled'),0)=1)"],args=[];
 if(input.candidateId!=null){conditions.push('id=?');args.push(candidateId(input.candidateId));}
 if(input.agencyId!=null){conditions.push('agencyId=?');args.push(candidateId(input.agencyId));}
 if(input.companyId!=null){conditions.push('companyId=?');args.push(candidateId(input.companyId));}
 if(input.sourceId!=null){conditions.push('sourceId=?');args.push(text(input.sourceId,'Источник',120));}
 if(input.ids!==undefined){if(!Array.isArray(input.ids)||input.ids.length>200)throw candidateError('Не более200 ID.');conditions.push('id IN (SELECT value FROM json_each(?))');args.push(JSON.stringify([...new Set(input.ids.map(candidateId))]));}
 const projected=`WITH ${ACTIVE_CTE}, projected AS (SELECT ${FIELDS},${STATUS_SQL} AS processingStatus,${COMPLETENESS_SQL} AS classificationComplete,${NEEDS_SQL} AS needsClassification FROM vacancy_candidates c)`;
 const allWhere=conditions.join(' AND ');
 const counts=(await db.prepare(projected+` SELECT count(*) total,coalesce(sum(needsClassification),0) needed FROM projected WHERE ${allWhere}`).bind(activeJson(roles),...args).first());
 const candidates=roles.length?(await db.prepare(projected+` SELECT * FROM projected WHERE ${allWhere}${input.force?'':' AND needsClassification'} ORDER BY id LIMIT ?`).bind(activeJson(roles),...args,limit).all()).results||[]:[];
 return {candidates,total:counts.total,needed:counts.needed,reused:counts.total-counts.needed,activeRoles:roles.length,reason:roles.length?null:'Нет активных корневых ролей с промтом.'};
}
export async function classificationCandidateStatistics(db){
 db=await candidateDb(db);const roles=await candidateClassificationContext(db);
 const rows=(await db.prepare(`WITH ${ACTIVE_CTE}, projected AS (SELECT ${STATUS_SQL} processingStatus,${NEEDS_SQL} needsClassification FROM vacancy_candidates c) SELECT processingStatus,count(*) count,sum(needsClassification) missing FROM projected GROUP BY processingStatus`).bind(activeJson(roles)).all()).results||[];
 const counts=Object.fromEntries(CANDIDATE_STATUSES.map(status=>[status,0]));for(const row of rows)counts[row.processingStatus]=row.count;
 return {total:rows.reduce((n,r)=>n+r.count,0),pending:rows.reduce((n,r)=>n+(r.missing||0),0),accepted:counts.accepted,rejected:counts.rejected,errors:counts.classification_error,imported:counts.imported,noActiveRoles:counts.no_active_roles,activeRoles:roles.length,counts};
}
// Reuse exactly the current JEV projection for the full-text pipeline.
export async function detailCandidateProjection(db){const roles=await candidateClassificationContext(db);return {sql:`WITH ${ACTIVE_CTE}, projected AS (SELECT ${FIELDS},${STATUS_SQL} AS processingStatus FROM vacancy_candidates c)`,bindings:[activeJson(roles)]};}
