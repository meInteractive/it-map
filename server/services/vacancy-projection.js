import {matchesPublicationBounds,publicationBounds} from '../../shared/publication-period.ts';
import {vacancyStateSql, vacancyStateBindings, vacancyRevision} from './vacancy-revision.js';
// Server-only; extracted from the v218 Worker.
import {EUGENE_RELEVANCE_PROFILE_VERSION} from '../constants.js';
import {scoreEugeneResumeFit,vacancyRecord} from '../repositories/vacancies.js';
import {readCatalogProjectionSource,catalogVacancyFields,readEntityDirectory} from './catalog-projection.js';
import {attachVacancyRoles,readVacancyRoleCatalog} from '../repositories/vacancy-roles.js';
import {ensureCrmSchema} from '../repositories/schema.js';

export async function prepareVacancyProjection(db,userName,catalog){
 const result=await db.prepare("SELECT 'company' AS kind,company_name AS name FROM company_vacancy_state UNION ALL SELECT 'profile',profile_version FROM vacancy_relevance_profile_state WHERE user_name=?").bind(userName).all();
 const initialized=new Set((result.results||[]).filter(r=>r.kind==='company').map(r=>r.name));
 const pending=catalog.companies.filter(c=>!initialized.has(c.name)&&c.jobs?.length);
 if(pending.length){
  const jobs=[];
  for(const c of pending)for(const job of c.jobs){
   const title=String(job.title||'').trim();let url;try{url=new URL(job.url);if(!['http:','https:'].includes(url.protocol)||!title)continue;}catch{continue;}
   jobs.push({companyName:c.name,title,url:url.href,salaryFrom:Number.isSafeInteger(Number(job.salary?.[0]))&&Number(job.salary[0])>0?Number(job.salary[0]):null,salaryTo:Number.isSafeInteger(Number(job.salary?.[1]))&&Number(job.salary[1])>0?Number(job.salary[1]):null});
  }
  const names=JSON.stringify(pending.map(c=>c.name));
  await db.batch([
   db.prepare('INSERT OR IGNORE INTO company_crm(company_name) SELECT value FROM json_each(?)').bind(names),
   db.prepare(`INSERT OR IGNORE INTO company_vacancies(company_name,url,title,salary_from,salary_to,published_at,description_md) SELECT json_extract(value,'$.companyName'),json_extract(value,'$.url'),json_extract(value,'$.title'),json_extract(value,'$.salaryFrom'),json_extract(value,'$.salaryTo'),NULL,'' FROM json_each(?)`).bind(JSON.stringify(jobs)),
   db.prepare('INSERT OR IGNORE INTO company_vacancy_state(company_name,initialized_at) SELECT value,CURRENT_TIMESTAMP FROM json_each(?)').bind(names)
  ]);
 }
 // Preserve the existing one-time relevance backfill, using one bulk write instead of N writes.
 if(userName==='Eugene'&&!(result.results||[]).some(r=>r.kind==='profile'&&r.name===EUGENE_RELEVANCE_PROFILE_VERSION)){
  const rows=await db.prepare('SELECT id,title,role,level,description_md AS descriptionMd FROM company_vacancies ORDER BY id').all();
  const scores=(rows.results||[]).map(v=>({id:v.id,score:scoreEugeneResumeFit(v)}));
  await db.batch([
   db.prepare(`INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at) SELECT ?,json_extract(value,'$.id'),json_extract(value,'$.score'),CURRENT_TIMESTAMP FROM json_each(?) WHERE 1 ON CONFLICT(user_name,vacancy_id) DO UPDATE SET relevance=excluded.relevance,updated_at=excluded.updated_at WHERE company_vacancy_user_state.relevance IS NULL`).bind(userName,JSON.stringify(scores)),
   db.prepare('INSERT INTO vacancy_relevance_profile_state(user_name,profile_version,applied_at) VALUES(?,?,CURRENT_TIMESTAMP) ON CONFLICT(user_name) DO UPDATE SET profile_version=excluded.profile_version,applied_at=excluded.applied_at').bind(userName,EUGENE_RELEVANCE_PROFILE_VERSION)
  ]);
 }
}

export async function readVacancyProjectionRows(db,userName,id=null,roleCatalog=null){
 const detail=id!==null;
 const sources=detail?",'channelUrl',channel.url,'channelUsername',channel.username,'messageUrl',source.message_url,'telegramContact',source.telegram_contact":'';
 const result=await db.prepare(`WITH applications AS (
  SELECT id,vacancy_id,status,ROW_NUMBER() OVER(PARTITION BY vacancy_id ORDER BY updated_at DESC,id DESC) AS position
  FROM company_applications WHERE user_name=? AND vacancy_id IS NOT NULL ${detail?'AND vacancy_id=?':''}
 ), sources AS (
  SELECT source.vacancy_id,json_group_array(json_object('channelId',source.channel_id,'channelName',channel.name ${sources})) AS telegramSourcesJson
  FROM vacancy_telegram_sources source JOIN telegram_channels channel ON channel.id=source.channel_id
  ${detail?'WHERE source.vacancy_id=?':''} GROUP BY source.vacancy_id
 ) SELECT v.id,v.company_name AS companyName,v.entity_type AS entityType,v.real_company_name AS realCompanyName,
 v.title,v.salary_from AS salaryFrom,v.salary_to AS salaryTo,v.role,v.level,s.relevance,
 COALESCE(s.favorite,0) AS favorite,COALESCE(s.hidden,0) AS hidden,v.published_at AS publishedAt,v.loaded_at AS loadedAt,
 v.archived,v.created_at AS createdAt,a.id AS applicationId,COALESCE(a.status,'') AS applicationStatus,
 COALESCE(sources.telegramSourcesJson,'[]') AS telegramSourcesJson
 ${detail?`,${vacancyStateSql()} AS stateSnapshot,v.url,v.description_md AS descriptionMd,v.last_checked_at AS lastCheckedAt,v.updated_at AS updatedAt,
 CASE WHEN letter.override_shared=1 THEN letter.subject WHEN trim(v.cover_letter_md)<>'' THEN v.cover_letter_subject ELSE COALESCE(letter.subject,v.cover_letter_subject) END AS coverLetterSubject,
 CASE WHEN letter.override_shared=1 THEN letter.markdown WHEN trim(v.cover_letter_md)<>'' THEN v.cover_letter_md ELSE COALESCE(letter.markdown,v.cover_letter_md) END AS coverLetterMd,
 CASE WHEN (letter.override_shared=1 OR trim(v.cover_letter_md)='') AND letter.vacancy_id IS NOT NULL THEN 'personal' ELSE 'shared' END AS coverLetterScope`:''}
 FROM company_vacancies v LEFT JOIN company_vacancy_user_state s ON s.vacancy_id=v.id AND s.user_name=?
 LEFT JOIN applications a ON a.vacancy_id=v.id AND a.position=1 LEFT JOIN sources ON sources.vacancy_id=v.id
 ${detail?'LEFT JOIN vacancy_cover_letters letter ON letter.vacancy_id=v.id AND letter.user_name=? WHERE v.id=?':''}
 ORDER BY v.company_name COLLATE NOCASE ASC,v.published_at IS NULL ASC,v.published_at DESC,v.id DESC`).bind(...(detail?[userName,id,id,...vacancyStateBindings(userName),userName,userName,id]:[userName,userName])).all();
 const vacancies=await Promise.all((result.results||[]).map(async row=>{
  if(detail)return {...vacancyRecord(row),revision:await vacancyRevision(row.stateSnapshot)};
  return {id:Number(row.id),companyName:row.companyName,entityType:['agency','channel'].includes(row.entityType)?row.entityType:'company',realCompanyName:row.realCompanyName||'',title:row.title,
   salaryFrom:row.salaryFrom,salaryTo:row.salaryTo,role:row.role||'',level:row.level||'',relevance:row.relevance??null,
   favorite:!!row.favorite,hidden:!!row.hidden,publishedAt:row.publishedAt||null,loadedAt:row.loadedAt||null,archived:!!row.archived,
   createdAt:row.createdAt,applicationId:row.applicationId??null,applicationStatus:row.applicationStatus||'',telegramSources:JSON.parse(row.telegramSourcesJson)};
 }));
 return attachVacancyRoles(db,vacancies,roleCatalog);
}

export async function readCompactVacancyList(db,userName,{period,now=Date.now()}={}){
 const catalog=await readCatalogProjectionSource(db);
 const roleCatalog=await readVacancyRoleCatalog(db);
 await prepareVacancyProjection(db,userName,catalog);
 const vacancies=await readVacancyProjectionRows(db,userName,null,roleCatalog);
 const names=new Set(vacancies.flatMap(v=>[v.companyName,v.realCompanyName]).filter(Boolean));
 const states=await db.prepare(`WITH application_flags AS (
  SELECT company_name,COUNT(*) AS total,MAX(vacancy_id IS NULL) AS general FROM company_applications WHERE user_name=? GROUP BY company_name
 ) SELECT n.value AS name,COALESCE(s.favorite,0) AS favorite,COALESCE(s.hidden,0) AS hidden,COALESCE(a.total,0) AS applications,COALESCE(a.general,0) AS general
 FROM json_each(?) n LEFT JOIN company_user_state s ON s.company_name=n.value AND s.user_name=? LEFT JOIN application_flags a ON a.company_name=n.value`).bind(userName,JSON.stringify([...names]),userName).all();
 const stateByName=new Map((states.results||[]).map(r=>[r.name,r]));
 const entities=[...catalog.companies,...catalog.agencies].filter(c=>names.has(c.name)).map(c=>{
  const s=stateByName.get(c.name)||{};
  return {name:c.name,rank:c.rank,entityType:c.entityType,favorite:!!s.favorite,hidden:!!s.hidden,hasVacancies:true,hasApplications:!!s.applications,...catalogVacancyFields(c)};
 });
 const bounds=period?publicationBounds(period,now):null;
 return {vacancies:bounds?vacancies.filter(v=>matchesPublicationBounds(v,bounds)):vacancies,entities,roleTree:roleCatalog.roles,applicationSummary:(states.results||[]).filter(s=>s.general).map(s=>({companyName:s.name,vacancyId:null}))};
}

export async function readVacancyDetail(db,userName,id){
 await ensureCrmSchema(db);
 const vacancy=(await readVacancyProjectionRows(db,userName,id))[0];
 if(!vacancy)return null;
 const publicationSources=(await db.prepare('SELECT candidate_id AS candidateId,url,updated_at AS updatedAt FROM vacancy_publication_sources WHERE vacancy_id=? ORDER BY candidate_id').bind(id).all()).results||[];
 return {vacancy:{...vacancy,publicationSources},entities:await readEntityDirectory(db,[vacancy.companyName,vacancy.realCompanyName],true)};
}
