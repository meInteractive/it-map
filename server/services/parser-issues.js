import {SCHEDULER_SOURCE_OPTIONS} from '../repositories/scheduler-schema.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {parserOwnerName,parserOwnerScopeSql} from '../repositories/parser-owner.js';
import {schedulerSettings,publicSchedulerSettings} from '../repositories/scheduler.js';
import {recordParserErrorSnapshot,readParserErrorHistory} from '../repositories/parser-error-history.js';

const actions={storage:'Откройте источник и сохранённую порцию. Проверьте ошибку сохранения; продолжайте с сохранённого прогресса.',extraction:'Откройте проверку источника и проверьте правило выделения заголовков и ссылок.',roles_pending:'Проверьте дочерние роли и последний результат назначения ролей перед повтором импорта.',roles_unresolved:'Проверьте справочник дочерних ролей и результат модели перед повтором импорта.',budget:'Проверьте остаток и резервы в «Сервисы и лимиты».',apify_running:'Проверьте состояние существующего Actor; не запускайте новый сбор вместо работающего.'};
const expected=new Set(['','not_due','source_scope','scope','agencies_disabled','disabled','apify_disabled','apify_running','apify_busy','apify_budget','apify_cycle_limit','time_slice','slice','scheduler_budget','rate_limit','provider_busy','concurrency','budget','budget_daily','budget_monthly','lease_busy']);
export const PARSER_EXPECTED_WAITS=[...expected];

// Local DB only. A maintained verification backlog is explicitly separate from
// observed runtime signals; old failed jobs are never declared current outages.
export async function parserIssues(database,dashboard,{includeHistory=true}={}){
 const db=await candidateDb(database),generation=boundGeneration(db),now=Date.now(),issues=[];
 const add=(id,priority,title,status,description,nextAction,extra={})=>issues.push({id,priority,title,status,description,nextAction,...extra});
 if(dashboard.settings.enabled&&dashboard.cloudStatus==='signal_overdue')add('cloud-overdue',1,'Не поступает облачный сигнал','observed','Сервер не получал автоматический сигнал более 75 минут.','Проверьте расписание и привязку аккаунта. Проверка сервера доступна ниже.',{target:'connection',observedAt:dashboard.lastSignalAt});
 const active=(await db.prepare("SELECT count(*) n FROM vacancy_role_classification c JOIN vacancy_roles r ON r.id=c.role_id WHERE r.parent_id IS NULL AND c.parsing_enabled=1 AND length(trim(c.markdown))>0").first()).n;
 if(dashboard.settings.enabled&&dashboard.settings.sourceIds.length&&!active)add('no-active-roles',1,'Нет активных направлений с промтом','observed','Отбор новых вакансий не сможет продолжиться.','Включите нужную корневую роль и сохраните её промт.',{target:'scope'});
 const scope=`FROM scheduler_jobs j WHERE j.generation=? AND j.status IN ('failed','pending')
 AND ${parserOwnerScopeSql('j')}
 AND EXISTS(SELECT 1 FROM vacancy_source_parsing s WHERE s.company_id IS j.company_id AND s.agency_id IS j.agency_id AND s.source_id=j.source_id AND s.enabled=1 AND (j.type<>'scan_source' OR s.last_success_at IS NULL OR CAST(strftime('%s',s.last_success_at) AS INTEGER)*1000<j.updated_at))
 AND NOT EXISTS(SELECT 1 FROM scheduler_jobs n WHERE n.generation=j.generation AND n.type=j.type AND n.company_id IS j.company_id AND n.agency_id IS j.agency_id AND n.source_id IS j.source_id AND n.candidate_id IS j.candidate_id AND n.status IN ('completed','running','pending') AND (n.updated_at>j.updated_at OR (n.updated_at=j.updated_at AND n.id>j.id)))
 AND j.source_id IN (SELECT value FROM json_each(?))
 AND (j.status='failed' OR COALESCE(j.reason_code,'') NOT IN (SELECT value FROM json_each(?)))`;
 const bindings=[generation,dashboard.settings.ownerName,JSON.stringify(dashboard.settings.sourceIds),JSON.stringify([...expected])];
 const rows=(await db.prepare(`SELECT j.id,j.type,j.source_id,j.candidate_id,j.company_id,j.agency_id,j.status,j.reason_code,j.last_error,j.updated_at,j.run_after,${parserOwnerName('j.')} companyName ${scope} ORDER BY j.updated_at DESC,j.id LIMIT 121`).bind(...bindings).all()).results||[];
 const p2=(await db.prepare(`SELECT count(*) n ${scope}`).bind(...bindings).first()).n;
 const limited=rows.length>120,groups=new Map();
 for(const row of rows.slice(0,120)){
  if(!dashboard.settings.sourceIds.includes(row.source_id)||row.status==='pending'&&expected.has(row.reason_code))continue;
  const key=[row.type,row.source_id,row.reason_code].join(':');
  if(!groups.has(key))groups.set(key,[]);groups.get(key).push(row);
 }
 for(const [key,jobs] of groups){const first=jobs[0],code=first.reason_code||'unknown';add('queue:'+key,2,`${SCHEDULER_SOURCE_OPTIONS.find(s=>s.id===first.source_id)?.name||first.source_id} · ${first.last_error||'Нужна проверка задания'}`,'observed',`В последнем доступном состоянии: ${jobs.length} ${jobs.length===1?'задание':'заданий'}. Это сигнал для проверки, а не подтверждение отказа всего источника.`,actions[code]||'Откройте очередь, проверьте последнюю попытку и повторите только необходимый этап.',{target:'queue',sourceId:first.source_id,stage:first.type,reasonCode:code,observedAt:first.updated_at,jobs:jobs.slice(0,8).map(j=>({id:j.id,companyName:j.companyName,agencyId:j.agency_id,candidateId:j.candidate_id,status:j.status,runAfter:j.run_after,message:j.last_error}))});}
 const missingRows=(await db.prepare("SELECT company_name,count(*) n FROM company_vacancies WHERE entity_type='company' AND substr(loaded_at,1,10)='2026-10-20' AND (level IS NULL OR trim(level)='') GROUP BY company_name").all()).results||[];
 const missing=missingRows.filter(r=>/сбер|sber/.test(r.company_name.toLocaleLowerCase('ru-RU'))).reduce((sum,r)=>sum+r.n,0);
 if(missing)add('sber-levels',2,'Уровни ранее загруженных вакансий Сбера','verification',`Без уровня: ${missing}. Проверяются карточки, загруженные 20.10.2026. Новые правила уровня уже заданы; прежние данные требуют отдельной проверки.`,'Сравнить исходный текст и историю обработки. Дозаполнять уровень с защитой ручных правок, без массовой перезаписи описаний.',{target:'fulltext',reviewedAt:'2026-10-21',affected:missing});
 add('yoomoney-cycle',2,'ЮMoney: проверить полный автоматический цикл','verification','Публичный каталог и адаптер проверены. Подтверждение обычного автоматического прохода от плановой проверки до импорта остаётся отдельной задачей.','Проверить плановый запуск, полноту страниц, сохранение и импорт. Наличие этой задачи не означает текущую ошибку источника.',{target:'source',reviewedAt:'2026-10-21'});
 add('hh-completeness',2,'HH: проверить полноту после тайм-аутов Apify','verification','Ранее встречались частичные наборы и ответы без описания. Требуется сверка восстановления на обычных запусках.','Сопоставить завершение Actor, число записей и продолжение обхода; проверить текст выбранных вакансий.',{target:'source',reviewedAt:'2026-10-21'});
 add('cost-attribution',3,'Уточнить стоимость этапов и неизвестные резервы','planned','Неизвестный расход сохраняется консервативно. Нужна более точная сверка подтверждений провайдера с резервами.','Проверить атрибуцию по этапам и завершённым запросам. Не обнулять неизвестные резервы без подтверждения.',{target:'services',reviewedAt:'2026-10-21'});
 add('history-reconciliation',3,'Сверить исторические ошибки с восстановлением','planned','Старый статус failed может остаться после успешного нового прохода. История сохранена и не равна списку актуальных сбоев.','Сверить старые ошибки с последующими успешными этапами и отдельно пометить восстановленные случаи.',{target:'queue',reviewedAt:'2026-10-21'});
 const activeCounts={p1:issues.filter(i=>i.priority===1&&i.status==='observed').length,p2};
 await recordParserErrorSnapshot(db,dashboard.settings.ownerName,activeCounts,now);
 return {checkedAt:now,issues:issues.sort((a,b)=>a.priority-b.priority),historicalFailed:dashboard.counts.failed||0,sampleLimited:limited,limit:120,activeCounts,
  ...(includeHistory?{history:await readParserErrorHistory(db,dashboard.settings.ownerName,now)}:{})};
}

// Local DB only: collect even when the admin UI is closed. Failure to sample
// must not prevent the existing parser from making progress.
export async function sampleParserErrorHistory(database){
 const db=await candidateDb(database),settings=publicSchedulerSettings(await schedulerSettings(db));
 const signal=await db.prepare("SELECT max(started_at) at FROM scheduler_runs WHERE generation=? AND trigger='automatic'").bind(boundGeneration(db)).first();
 await parserIssues(db,{settings,counts:{},lastSignalAt:signal?.at||null,cloudStatus:signal?.at&&Date.now()-signal.at>=75*60000?'signal_overdue':'signal_received'},{includeHistory:false});
}
