import {parserOwner,parserOwnerColumns,agencyCandidateDb} from '../repositories/parser-owner.js';
import {isHhVacancyUrl,sameHhPublication} from './vacancy-source-priority.js';
import {vacancyPublicationIdentity,sameVacancyPublication} from './vacancy-duplicate-match.js';
import {candidateDb,candidateId,candidateHash,canonicalCandidateUrl} from '../repositories/vacancy-candidates.js';
import {leafRoleContext,currentLeafRoles,importError} from './vacancy-leaf-roles.js';
import {readAppCatalog} from '../repositories/catalog.js';
import {vacancyInput} from '../repositories/vacancies.js';
import {readVacancyRoleCatalog,vacancyRoleSelection} from '../repositories/vacancy-roles.js';
import {withDetailLease} from './vacancy-detail-leases.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {MAX_VACANCY_DESCRIPTION_LENGTH,VACANCY_LEVELS} from '../constants.js';
const fields=['url','title','description_md','salary_from','salary_to','level','published_at'];
const quote=name=>'"'+name+'"';
const optionsFor=input=>{if(!input||Array.isArray(input)||Object.keys(input).some(k=>!['existingVacancyId','createNew'].includes(k))||input.createNew!==undefined&&typeof input.createNew!=='boolean'||input.existingVacancyId!=null&&input.createNew)throw importError('input','Некорректное разрешение совпадений.',400);return {...(input.existingVacancyId!=null?{existingVacancyId:candidateId(input.existingVacancyId)}:{}),...(input.createNew?{createNew:true}:{})};};
// A bounded snapshot of every input read by the plan. It is compared again in
// the write transaction; a SELECT followed by an unguarded write is insufficient.
async function captureImportFence(db,id){
 const specs=[
 ['vacancy_candidates',['id','company_id','agency_id','title_hash','title_revision','source_id','vacancy_url','source_namespace','external_job_id','main_vacancy_id'],'id=?',[id]],
 ['vacancy_classifications',['id','candidate_id','root_role_id','title_hash','title_revision','prompt_version','decision'],'candidate_id=?',[id]],
 ['vacancy_role_classification',['role_id','markdown','revision','parsing_enabled'],'1',[]],
 ['vacancy_roles',['id','stable_key','name','parent_id','sort_order'],'1',[]],
 ['vacancy_detail_settings',['id','markdown','prompt_revision','provider','openai_model','reasoning_effort','model_revision'],'1',[]],
 ['vacancy_role_settings',['id','markdown','prompt_revision','provider','openai_model','openrouter_model','reasoning_effort','model_revision'],'1',[]],
 ['vacancy_raw_snapshots',['id','candidate_id','source_url','content_hash','content_status'],'candidate_id=?',[id]],
 ['vacancy_extractions',['id','candidate_id','snapshot_id','config_hash','prompt_version','schema_version','status'],'candidate_id=?',[id]],
 ['vacancy_role_results',['id','candidate_id','input_hash','role_ids','status'],'candidate_id=?',[id]],
 ['company_identity',['id','company_name'],'1',[]],
 ['agency_identity',['id','agency_name'],'1',[]],
 ['firecrawl_settings',['id','scheduler_json','favorites_only'],'id=1',[]],
 ['app_catalog',['id','data_json'],'id=1',[]],
 ['company_vacancies',['id','company_name','entity_type','url','title'],'1',[]],
 ['company_vacancies',['id','company_name','entity_type','url',...fields.filter(f=>f!=='title'&&f!=='url'),'title','archived','role'],'company_name=(SELECT CASE WHEN c.agency_id IS NOT NULL THEN (SELECT agency_name FROM agency_identity WHERE id=c.agency_id) ELSE (SELECT company_name FROM company_identity WHERE id=c.company_id) END FROM vacancy_candidates c WHERE c.id=?)',[id]],
 ['vacancy_role_assignments',['vacancy_id','role_id'],'1',[]],
 ['vacancy_parser_fields',['vacancy_id','field','value_json','manual','candidate_id'],'vacancy_id IN (SELECT id FROM company_vacancies WHERE company_name=(SELECT CASE WHEN c.agency_id IS NOT NULL THEN (SELECT agency_name FROM agency_identity WHERE id=c.agency_id) ELSE (SELECT company_name FROM company_identity WHERE id=c.company_id) END FROM vacancy_candidates c WHERE c.id=?))',[id]],
 ['vacancy_parser_roles',['vacancy_id','role_id','candidate_id'],'1',[]],
 ['vacancy_publication_sources',['candidate_id','vacancy_id','canonical_url','extraction_id'],'1',[]],
 ['vacancy_publication_keys',['identity_key','vacancy_id'],'1',[]],
 ['company_vacancy_state',['company_name'],'1',[]]
 ];
 const queries=specs.map(([table,columns,where,args])=>({sql:`SELECT coalesce(json_group_array(json_array(${columns.map(quote).join(',')})),'[]') AS snapshot FROM (SELECT ${columns.map(quote).join(',')} FROM ${table} WHERE ${where} ORDER BY ${columns.map(quote).join(',')})`,args}));
 const result=await db.batch(queries.map(q=>db.prepare(q.sql).bind(...q.args)));
 return queries.map((q,i)=>({...q,snapshot:result[i].results?.[0]?.snapshot||'[]'}));
}
const snapshotsEqual=(a,b)=>a.every((q,i)=>q.snapshot===b[i]?.snapshot);
const normalizedMainUrl=url=>{try{return canonicalCandidateUrl(url)}catch{return null}};
const mainSelect='SELECT id,company_name,entity_type,url,title,description_md FROM company_vacancies';
export async function prepareVacancyImport(env,id,options={}){
 const db=await candidateDb(env.DB),resolution=optionsFor(options),before=await captureImportFence(db,candidateId(id));let plan;
 try{plan=await buildPlan(db,id,resolution);}catch(e){if(!e.status)throw e;plan={candidateId:candidateId(id),status:'blocked',code:e.code||'invalid_data',reason:e.message,changes:[],roleIds:[],rolePaths:[],possibleDuplicates:[]};}
 const after=await captureImportFence(db,candidateId(id));if(!snapshotsEqual(before,after))throw importError('preview_changed','Данные изменились во время проверки. Повторите предпросмотр.');
 const token=await candidateHash(JSON.stringify([boundGeneration(db),id,resolution,before.map(q=>q.snapshot),plan.status,plan.targetId,plan.changes,plan.roleIds]));
 return {...plan,resolution,token,_fence:before};
}
async function buildPlan(db,id,resolution){
 const x=await leafRoleContext(db,id),c=x.candidate,roleResult=await currentLeafRoles(db,x);if(!roleResult)throw importError('roles_pending','Сначала определите конечные роли по сохранённому тексту.');
 const autoRoles=JSON.parse(roleResult.role_ids);if(!autoRoles.length)throw importError('roles_unresolved','LLM не смогла выбрать конечную роль. Нужны уточнение справочника или промта.');if(autoRoles.some(id=>!x.allowed.some(r=>r.id===id)))throw importError('roles_stale','Результат роли устарел.');
 const owner=parserOwner(c),entityType=owner.type;
 if(owner.type==='agency')await agencyCandidateDb(db,id);
 const company=await db.prepare(`SELECT ${owner.nameColumn} AS company_name FROM ${owner.identityTable} WHERE id=?`).bind(owner.id).first();
 const catalog=await readAppCatalog(db,{persistRatings:false});if(!company||!(owner.type==='agency'?catalog.agencies:catalog.companies).some(co=>co.name===company.company_name))throw importError('company_missing','Привяжите первичную вакансию к существующей компании или агентству. Организация из текста LLM не создаётся.');
 const companyName=company.company_name,all=(await db.prepare(mainSelect).all()).results||[];
 const canonical=canonicalCandidateUrl(c.vacancyUrl),keys=[...x.detail.identityKeys];if(!canonical)throw importError('source_url','Нет допустимого URL.');
 const urlKey='url:'+await candidateHash(canonical);if(!keys.includes(urlKey))keys.push(urlKey);
 const known=(await db.prepare('SELECT identity_key,vacancy_id FROM vacancy_publication_keys WHERE identity_key IN (SELECT value FROM json_each(?))').bind(JSON.stringify(keys)).all()).results||[];
 const publicationId=vacancyPublicationIdentity(c.vacancyUrl);
 const reliable=new Set(known.map(k=>k.vacancy_id));for(const main of all)if(normalizedMainUrl(main.url)===canonical||publicationId&&vacancyPublicationIdentity(main.url)===publicationId)reliable.add(main.id);
 if(publicationId){const rows=(await db.prepare('SELECT vacancy_id,canonical_url FROM vacancy_publication_sources').all()).results||[];for(const row of rows)if(vacancyPublicationIdentity(row.canonical_url)===publicationId)reliable.add(row.vacancy_id);}
 if(c.mainVacancyId!=null)reliable.add(c.mainVacancyId);
 if(reliable.size>1)throw importError('identity_conflict','Устойчивые ключи указывают на разные основные вакансии. Автоматическое объединение запрещено.');
 let targetId=[...reliable][0]||null,matchMethod=targetId?'identity':null;if(resolution.existingVacancyId){if(targetId&&targetId!==resolution.existingVacancyId)throw importError('identity_conflict','Выбранная карточка противоречит существующей точной связи.');targetId=resolution.existingVacancyId;matchMethod='explicit';}
 if(!targetId&&!resolution.createNew&&!resolution.existingVacancyId){
  const prior=(await db.prepare(`SELECT s.vacancy_id,s.canonical_url,e.extracted_data FROM vacancy_publication_sources s JOIN vacancy_extractions e ON e.id=s.extraction_id JOIN company_vacancies v ON v.id=s.vacancy_id WHERE v.company_name=? AND v.entity_type=?`).bind(companyName,entityType).all()).results||[];
  const conflicts=url=>{try{const old=vacancyPublicationIdentity(url);if(publicationId&&old&&old.split(':')[0]===publicationId.split(':')[0])return old!==publicationId;return new URL(url).hostname.replace(/^www\./,'')===new URL(c.vacancyUrl).hostname.replace(/^www\./,'')&&normalizedMainUrl(url)!==canonical;}catch{return false}};
  const distinctPublications=new Set([...all.filter(m=>conflicts(m.url)).map(m=>m.id),...prior.filter(p=>conflicts(p.canonical_url)).map(p=>p.vacancy_id)]);
  const matches=main=>sameHhPublication(main,c,x.data,companyName,entityType)||sameVacancyPublication(main,c,x.data,companyName,entityType);
  const confirmed=new Set(all.filter(m=>!distinctPublications.has(m.id)&&matches(m)).map(m=>m.id));
  for(const p of prior){if(distinctPublications.has(p.vacancy_id))continue;let data;try{data=JSON.parse(p.extracted_data);}catch{continue;}if(matches({company_name:companyName,entity_type:entityType,url:p.canonical_url,title:data.title,description_md:data.description_md}))confirmed.add(p.vacancy_id);}
  if(confirmed.size>1)throw importError('duplicate_ambiguous','Найдены несколько карточек с совпадающими обязанностями и требованиями. Новая карточка не создаётся.');
  if(confirmed.size===1){targetId=[...confirmed][0];matchMethod='content';}
 }
 const current=targetId?await db.prepare('SELECT * FROM company_vacancies WHERE id=?').bind(targetId).first():null;if(targetId&&!current)throw importError('target_missing','Связанная основная вакансия удалена.');if(current&&(current.company_name!==companyName||current.entity_type!==entityType))throw importError('company_conflict','Совпавшая карточка относится к другой организации.');
 const similar=all.filter(m=>m.company_name===companyName&&m.entity_type===entityType&&m.id!==targetId&&m.title.trim().toLocaleLowerCase('ru')===x.data.title.trim().toLocaleLowerCase('ru')).map(m=>({id:m.id,title:m.title,url:m.url}));
 if(!targetId&&similar.length&&!resolution.createNew)return {candidateId:c.id,status:'possible_duplicate',code:'possible_duplicate',reason:'Одинаковый заголовок не доказывает тождество. Выберите карточку или явно разрешите новую.',companyName,changes:[],roleIds:autoRoles,rolePaths:x.allowed.filter(r=>autoRoles.includes(r.id)).map(r=>r.path),possibleDuplicates:similar};
 const initialized=await db.prepare('SELECT company_name FROM company_vacancy_state WHERE company_name=?').bind(companyName).first();
 if(!initialized&&(entityType==='agency'?catalog.agencies:catalog.companies).find(co=>co.name===companyName)?.jobs?.some(j=>j.title&&j.url))throw importError('company_initialization','Откройте карточку компании для завершения прежней инициализации вакансий, затем повторите проверку.');
 const rawLevel=x.data.level,mainLevel=VACANCY_LEVELS.find(level=>level.toLowerCase()===String(rawLevel||'').trim().toLowerCase())||'';
 const dollar=x.data.salary_currency!=='RUB'||x.data.salary_period!=='month';
 let input;try{input=vacancyInput({companyName,entityType,url:c.vacancyUrl,title:x.data.title,descriptionMd:x.data.description_md,salaryFrom:dollar?null:x.data.salary_from,salaryTo:dollar?null:x.data.salary_to,level:mainLevel,publishedAt:x.data.published_at,loadedAt:x.snapshot.fetched_at.slice(0,10),roleIds:autoRoles},{allowEmptyFields:true});}catch(e){throw importError('mapping_invalid',e.message);}
 if(Array.from(input.descriptionMd).length>MAX_VACANCY_DESCRIPTION_LENGTH)throw importError('description_limit','Полное описание превышает лимит основной карточки. Усечение не выполняется.');
 const incoming={url:input.url,title:input.title,description_md:input.descriptionMd,salary_from:input.salaryFrom,salary_to:input.salaryTo,level:input.level||null,published_at:input.publishedAt||null};
 const sources=targetId?(await db.prepare('SELECT * FROM vacancy_publication_sources WHERE vacancy_id=?').bind(targetId).all()).results||[]:[],source=sources.find(s=>s.candidate_id===c.id),incomingHh=isHhVacancyUrl(c.vacancyUrl),lowerPriority=!!(current&&!incomingHh&&(isHhVacancyUrl(current.url)||sources.some(s=>isHhVacancyUrl(s.url))));
 const owned=targetId?(await db.prepare('SELECT * FROM vacancy_parser_fields WHERE vacancy_id=?').bind(targetId).all()).results||[]:[];
 const changes=fields.map(field=>{const value=incoming[field],old=current?.[field]??null,owner=owned.find(o=>o.field===field),empty=old===null||old==='';let action='preserved',reason='Ручное значение или неизвестное происхождение.';
 if(field==='url'&&current&&!incomingHh){reason='Основная ссылка сохраняется; заменить её автоматически может только hh.ru.';}else if(lowerPriority){reason='Сохранён приоритетный вариант hh.ru; другая публикация добавляется как источник.';}else if(value===null||value===''){reason='Источник не содержит значения.';}else if(!current||!owner?.manual&&(empty||owner&&owner.value_json===JSON.stringify(old))){action=old===value?'unchanged':'update';reason=old===value?'Значение актуально.':'Автоматически управляемое или пустое поле.';}else if(old===value){action='unchanged';reason='Значение совпадает, происхождение сохраняется.';}
 return {field,before:old,after:action==='update'?value:old,action,reason,incoming:value,managed:!(field==='url'&&current&&!incomingHh)&&!lowerPriority&&(!current||(!owner?.manual&&(empty||owner?.value_json===JSON.stringify(old))))};});
 // Do not produce an invalid mixed salary range when only one boundary is owned.
 const a=changes.find(f=>f.field==='salary_from'),b=changes.find(f=>f.field==='salary_to');if(a.after!==null&&b.after!==null&&a.after>b.after)for(const f of [a,b]){f.after=f.before;f.action='preserved';f.reason='Обновление создало бы противоречивую вилку.';f.managed=false;}
 const oldRoles=targetId?((await db.prepare('SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id=?').bind(targetId).all()).results||[]).map(r=>r.role_id):[],ledger=targetId?(await db.prepare('SELECT * FROM vacancy_parser_roles WHERE vacancy_id=?').bind(targetId).all()).results||[]:[];
 const acceptedAutoRoles=lowerPriority?[]:autoRoles;
 const stale=lowerPriority?[]:incomingHh?oldRoles.filter(role=>ledger.some(l=>l.role_id===role)&&!autoRoles.includes(role)):oldRoles.filter(role=>ledger.some(l=>l.role_id===role&&l.candidate_id===c.id)&&!ledger.some(l=>l.role_id===role&&l.candidate_id!==c.id)&&!autoRoles.includes(role));
 const roleIds=[...new Set([...oldRoles.filter(id=>!stale.includes(id)),...acceptedAutoRoles])].sort((a,b)=>a-b),roleCatalog=await readVacancyRoleCatalog(db),selection=(()=>{try{return vacancyRoleSelection(roleCatalog,{roleIds})}catch(e){throw importError('roles_invalid',e.message)}})();
 const changed=!current||changes.some(f=>f.action==='update')||JSON.stringify([...oldRoles].sort((a,b)=>a-b))!==JSON.stringify(roleIds)||!source||source.extraction_id!==x.extraction.id||keys.some(key=>!known.some(k=>k.identity_key===key));
 const metadata=Object.fromEntries(Object.entries(x.data).filter(([key])=>!['title','description_md','salary_from','salary_to','level','published_at'].includes(key)));
 if(rawLevel&&!mainLevel)metadata.level=rawLevel;
 if(dollar&&(x.data.salary_from!==null||x.data.salary_to!==null))Object.assign(metadata,{salary_from:x.data.salary_from,salary_to:x.data.salary_to});
 return {candidateId:c.id,status:current?(changed?'update':'unchanged'):'create',targetId,companyName,entityType,...parserOwnerColumns(c),changes,roleIds,rolePaths:selection.rolePaths,oldRoleIds:oldRoles,autoRoleIds:acceptedAutoRoles,staleRoleIds:stale,deduplication:{method:matchMethod||'new',vacancyId:targetId},preferredSource:incomingHh||lowerPriority?'hh':c.sourceId,lowerPriority,possibleDuplicates:similar,sourceUrls:[...new Set([...(current?[current.url]:[]),...sources.map(s=>s.url),c.vacancyUrl])],unmappedMetadata:metadata,notes:[...(dollar&&(x.data.salary_from!==null||x.data.salary_to!==null)?['В основную числовую вилку переносится только RUB/month. Иная валюта/период остаётся в исходном описании и метаданных.']:[]),...(incomingHh||lowerPriority?['При подтверждённом совпадении основной вариант — hh.ru. Ручные правки сохраняются; прочие публикации остаются источниками.']:[]),'Письма, отклики, персональная релевантность, избранное, скрытие, архив и заметки сохраняются.'],_data:{x,keys,incoming,oldRoles,ledger,input,selection,canonical,incomingHh,lowerPriority}};
}
export const publicImportPlan=plan=>{const {_data,_fence,_reviewHash,...value}=plan;return value;};
export async function importVacancyCandidate(env,id,{token,resolution={},operationId=crypto.randomUUID(),batchId=null}={}){
 const base=await candidateDb(env.DB);return withDetailLease(base,'import:global',async leased=>{
  const db=await agencyCandidateDb(leased,id);
  const receipt=await db.prepare('SELECT * FROM vacancy_import_operations WHERE id=?').bind(operationId).first();if(receipt?.status==='success'){if(receipt.candidate_id!==candidateId(id))throw importError('receipt_conflict','Идентификатор операции уже использован.');return {action:receipt.action,candidateId:receipt.candidate_id,vacancyId:receipt.vacancy_id,reused:true};}
  const plan=await prepareVacancyImport({...env,DB:db},id,resolution);if(plan.token!==token)throw importError('preview_changed','Данные изменились после предпросмотра. Проверьте импорт ещё раз.');
  if(!['create','update','unchanged'].includes(plan.status))throw importError(plan.code||'blocked',plan.reason||'Импорт заблокирован.');
  const {x,keys,input,selection,canonical,ledger,oldRoles}=plan._data,target='(SELECT main_vacancy_id FROM vacancy_candidates WHERE id=?)',at=new Date().toISOString(),owner=crypto.randomUUID();
  const auditValues=[];for(const f of plan.changes.filter(f=>f.action==='update')){const describe=async value=>typeof value==='string'&&value.length>2048?{sha256:await candidateHash(value),characters:Array.from(value).length}:value;auditValues.push({field:f.field,before:await describe(f.before),after:await describe(f.after)});}
  const statements=plan._fence.map(q=>db.prepare('SELECT CASE WHEN ('+q.sql+')=? THEN 1 ELSE abs(-9223372036854775808) END AS import_fence').bind(...q.args,q.snapshot));
  statements.push(db.prepare('INSERT INTO company_crm(company_name) VALUES(?) ON CONFLICT(company_name) DO NOTHING').bind(plan.companyName));
  if(plan.status==='create'){
   statements.push(db.prepare("INSERT INTO company_vacancies(company_name,entity_type,url,title,description_md,salary_from,salary_to,level,published_at,loaded_at,role) VALUES(?,?,?,?,?,?,?,?,?,?,?)").bind(plan.companyName,plan.entityType,input.url,input.title,input.descriptionMd,input.salaryFrom,input.salaryTo,input.level,input.publishedAt||null,input.loadedAt,selection.role));
   statements.push(db.prepare('UPDATE vacancy_candidates SET main_vacancy_id=(SELECT id FROM company_vacancies WHERE url=?),updated_at=? WHERE id=?').bind(input.url,at,x.candidate.id));
  }else statements.push(db.prepare('UPDATE vacancy_candidates SET main_vacancy_id=?,updated_at=CASE WHEN main_vacancy_id IS ? THEN updated_at ELSE ? END WHERE id=?').bind(plan.targetId,plan.targetId,at,x.candidate.id));
  if(plan.status!=='unchanged'){
   statements.push(db.prepare('INSERT INTO vacancy_import_write_context(vacancy_id,generation,owner_token) VALUES('+target+',?,?)').bind(x.candidate.id,boundGeneration(db),owner));
   const changed=plan.changes.filter(f=>f.action==='update');if(plan.status!=='create'&&changed.length)statements.push(db.prepare('UPDATE company_vacancies SET '+changed.map(f=>quote(f.field)+'=?').join(',')+',updated_at=CURRENT_TIMESTAMP WHERE id='+target).bind(...changed.map(f=>f.after),x.candidate.id));
   for(const f of plan.changes)if(f.managed&&f.incoming!==null&&f.incoming!=='')statements.push(db.prepare('INSERT INTO vacancy_parser_fields(vacancy_id,field,value_json,candidate_id) VALUES('+target+',?,?,?) ON CONFLICT(vacancy_id,field) DO UPDATE SET value_json=excluded.value_json,candidate_id=excluded.candidate_id WHERE vacancy_parser_fields.manual=0').bind(x.candidate.id,f.field,JSON.stringify(f.after),x.candidate.id));
   if(!plan.lowerPriority)statements.push(db.prepare('INSERT INTO vacancy_parser_fields(vacancy_id,field,value_json,candidate_id) VALUES('+target+",'source_metadata',?,?) ON CONFLICT(vacancy_id,field) DO UPDATE SET value_json=excluded.value_json,candidate_id=excluded.candidate_id").bind(x.candidate.id,JSON.stringify(plan.unmappedMetadata),x.candidate.id));
   statements.push(db.prepare('DELETE FROM vacancy_parser_roles WHERE vacancy_id='+target+' AND candidate_id=?').bind(x.candidate.id,x.candidate.id));
   if(plan._data.incomingHh)for(const role of plan.staleRoleIds)statements.push(db.prepare('DELETE FROM vacancy_parser_roles WHERE vacancy_id='+target+' AND role_id=?').bind(x.candidate.id,role));
   for(const role of plan.staleRoleIds)statements.push(db.prepare('DELETE FROM vacancy_role_assignments WHERE vacancy_id='+target+' AND role_id=? AND NOT EXISTS(SELECT 1 FROM vacancy_parser_roles WHERE vacancy_id='+target+' AND role_id=?)').bind(x.candidate.id,role,x.candidate.id,role));
   for(const role of plan.autoRoleIds){statements.push(db.prepare('INSERT OR IGNORE INTO vacancy_role_assignments(vacancy_id,role_id) VALUES('+target+',?)').bind(x.candidate.id,role));if(!oldRoles.includes(role)||ledger.some(l=>l.role_id===role))statements.push(db.prepare('INSERT OR IGNORE INTO vacancy_parser_roles(vacancy_id,role_id,candidate_id) VALUES('+target+',?,?)').bind(x.candidate.id,role,x.candidate.id));}
   if(JSON.stringify([...oldRoles].sort((a,b)=>a-b))!==JSON.stringify(plan.roleIds))statements.push(db.prepare('UPDATE company_vacancies SET role=?,updated_at=CURRENT_TIMESTAMP WHERE id='+target).bind(selection.role,x.candidate.id));
   statements.push(db.prepare('INSERT INTO vacancy_publication_sources(candidate_id,vacancy_id,url,canonical_url,source_namespace,external_job_id,extraction_id,updated_at) VALUES(?,'+target+',?,?,?,?,?,?) ON CONFLICT(candidate_id) DO UPDATE SET url=excluded.url,canonical_url=excluded.canonical_url,extraction_id=excluded.extraction_id,updated_at=excluded.updated_at WHERE vacancy_publication_sources.vacancy_id=excluded.vacancy_id').bind(x.candidate.id,x.candidate.id,x.candidate.vacancyUrl,canonical,x.candidate.sourceNamespace,x.candidate.externalJobId,x.extraction.id,at));
   for(const key of keys)statements.push(db.prepare('INSERT INTO vacancy_publication_keys(identity_key,vacancy_id) VALUES(?,'+target+') ON CONFLICT(identity_key) DO NOTHING').bind(key,x.candidate.id));
   statements.push(db.prepare('DELETE FROM vacancy_import_write_context WHERE vacancy_id='+target+' AND owner_token=?').bind(x.candidate.id,owner));
  }
  statements.push(db.prepare('INSERT INTO company_vacancy_state(company_name) VALUES(?) ON CONFLICT(company_name) DO NOTHING').bind(plan.companyName));
  statements.push(db.prepare('INSERT INTO vacancy_import_operations(id,candidate_id,vacancy_id,batch_id,action,status,processed_at,input_hash,changes_json) VALUES(?,?,'+target+",?,?,'success',?,?,?)").bind(operationId,x.candidate.id,x.candidate.id,batchId,plan.status,at,x.inputHash,JSON.stringify({resolution:plan.resolution,deduplication:plan.deduplication,snapshotId:x.snapshot.id,extractionId:x.extraction.id,roleInputHash:x.inputHash,values:auditValues,fields:plan.changes.filter(f=>f.action==='update').map(f=>f.field),roleIds:plan.roleIds,protectedFields:plan.changes.filter(f=>f.action==='preserved').map(f=>f.field)})));
  try{await db.batch(statements);}catch(e){if(e.code==='restored'||e.code==='detail_changed')throw e;await db.prepare("INSERT OR IGNORE INTO vacancy_import_operations(id,candidate_id,batch_id,action,status,processed_at,changes_json,error_code,message) VALUES(?,?,?,'skip','error',?,'{}','transaction','Транзакция отменена; повторите проверку и импорт.')").bind(crypto.randomUUID(),x.candidate.id,batchId,at).run().catch(()=>{});throw importError('transaction','Импорт не сохранён. Транзакция отменена, повторите предпросмотр.');}
  const row=await db.prepare('SELECT main_vacancy_id FROM vacancy_candidates WHERE id=?').bind(x.candidate.id).first();return {action:plan.status,deduplication:plan.deduplication,candidateId:x.candidate.id,vacancyId:row.main_vacancy_id,reused:false};
 },{ttl:150000});
}
export async function getVacancyImportHistory(db,id){db=await candidateDb(db);return (await db.prepare('SELECT * FROM vacancy_import_operations WHERE candidate_id=? ORDER BY processed_at DESC,rowid DESC LIMIT 100').bind(candidateId(id)).all()).results||[];}
