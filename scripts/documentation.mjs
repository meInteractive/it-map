import fs from 'node:fs';
import path from 'node:path';
import { buildBackupSchema } from './backup-schema.mjs';
import * as declarations from '../server/constants.js';
import * as exportsModule from '../server/import-export/xlsx.js';

const cell = value => String(value ?? '—').replaceAll('|','\\|').replaceAll('\n',' ');
const table = (headers, rows) => '| '+headers.join(' | ')+' |\n| '+headers.map(()=>'---').join(' | ')+' |\n'+rows.map(row=>'| '+row.map(cell).join(' | ')+' |').join('\n');
const descriptions = {
 parser_candidate_intakes:'Неизменяемое происхождение первичной публикации: creation_token, парсер сбора списка, источник и дата первого получения. Кандидат и receipt пишутся одной транзакцией. Без FK на переиспользуемые ID; переносится начиная с backup39.',
 parser_error_daily:'Дневные пики активных P1/P2 по владельцу парсинга и датам MSK. MAX-upsert, фактические дни наблюдений; без текстов/ключей; переносится начиная с backup39.',
 apify_settings:'Apify HH: AES-GCM токены Free/Starter, разрешения, CAS, безопасные billing snapshots. После restore сбор и Starter выключены.',
 apify_runs:'Несбрасываемый операционный журнал Apify: generation/operation key, run/dataset IDs, профиль/account/период, резервы и подтверждённые расходы; без токенов и без backup.',
 vacancy_role_settings:'Промт и модель конечных ролей, отдельные CAS редакции.',
 vacancy_role_results:'История выбора конечных ID, версии текста, дерева, промта/модели и стоимость.',
 vacancy_parser_fields:'Происхождение управляемых полей, последняя автоматическая величина и защита ручной правки.',
 vacancy_parser_roles:'Автоматические назначения по первичной записи; ручные назначения сохраняются.',
 vacancy_publication_sources:'Все проверенные публикации одной основной вакансии, включая дополнительные URL.',
 vacancy_publication_keys:'Уникальные устойчивые ключи публикаций; без объединения по заголовку.',
 vacancy_import_operations:'Минимальный технический аудит безопасного импорта.',
 vacancy_import_batches:'Операционный durable progress подтверждённого пакета импорта, без backup.',
 vacancy_import_write_context:'Только транзакционный контекст авторизованного parser UPDATE для триггеров ручных полей.',

 vacancy_detail_settings:'Общий промт структурирования до10000 Unicode-символов и независимый выбор модели через существующие OpenRouter/OpenAI; без ключей. CAS промта/модели раздельные.',
 vacancy_raw_snapshots:'Неизменённые Markdown версии конкретной первичной вакансии, hash/URL/проверка/profile/известные кредиты. Не содержат HTML/изображений.',
 vacancy_raw_snapshot_parts:'Части больших исходных Markdown без потери символов и без превышения размера D1 строки. Собираются строго по part_index.',
 vacancy_content_fetches:'История всех загрузок/ошибок Firecrawl, включая идентичное обновление без нового снимка; расходы только из API либо null.',
 vacancy_extractions:'Неизменяемые успешные и ошибочные обработки конкретного snapshot с prompt/model/schema keys и usage/cost. Основные вакансии не изменяют.',
 vacancy_detail_jobs:'Операционные bounded пакеты полных текстов с durable progress и generation/owner fencing. Без backup.',
 vacancy_detail_leases:'Операционные блокировки candidate/job и два слота генеративной LLM; expiry/generation/owner, без backup.',
 vacancy_classification_jobs:'Операционные пакеты JEV до50 ID: durable progress, owner/lease/generation, bounded retry, известная/неизвестная стоимость, pause/resume; без backup и без секретов.',
 vacancy_classification_slots:'Два общих слота классификации заголовков JEV. Atomic claim/expiry/owner/generation; не затрагивают Firecrawl или relevance, не экспортируются.',
 firecrawl_usage_events:'Операционный несбрасываемый журнал резервов и расходов Firecrawl: SHA ключа/профиль/реальный период, запросы/ошибки, верхняя оценка неизвестного списания и active_until. Без backup и без секретов.',
 firecrawl_usage_periods:'Операционные данные Credit Usage: реальный расчётный период, остаток, пакет и верхнее наблюдавшееся использование. Не переносятся назад при restore.',
 scheduler_jobs:'Постоянная операционная очередь этапов парсера: generation/idempotency, сроки/приоритеты, company_id/agency_id, ownership/lease и retry. Не экспортируется.',
 scheduler_runs:'Журнал циклов диспетчера и глобальная блокировка с ограничением времени; источник сигнала/счётчики/cursor.',
 scheduler_attempts:'Операционный журнал автоматических этапов и денежных резервов JEV/LLM. Содержимое вакансий и секреты не сохраняются; расходы не откатываются restore.',
 firecrawl_settings:'Общее подключение Firecrawl: favorites_only (default1, избранные текущего admin), AES-GCM ключ, маска, лимиты Scrape, явный basic/enhanced, CAS revision и проверка кредитов.',
 vacancy_source_parsing:'Настройки существующего источника: ровно одна company/agency identity + ключ источника, сохранённый URL snapshot, выключено по умолчанию, правила извлечения и пагинации, диагностика.',
 vacancy_discovery_runs:'Операционные результаты проверки на 1 час: UUID, пользователь, поколение, snapshot источника и найденные пары. Без HTML и без backup.',
 firecrawl_request_lease:'Глобальное владение последовательным ручным сбором/проверкой подключения. Restore generation и owner fencing, срок; без backup.',
 vacancy_candidates:'Первичные публикации отдельно от основной базы. Raw title/URLs, постоянные взаимоисключающие company_id/agency_id и main_vacancy_id, hash+revision заголовка, даты обнаружения. Статус вычисляется по текущим промтам.',
 vacancy_candidate_identities:'Уникальные идентификаторы публикаций: внешние ID в пространстве источника, канонические URL и ключи импорта. Несколько алиасов одной записи; конкуренция защищена транзакцией.',
 vacancy_classification_attempts:'История пакетных запросов JEV: снимок заголовка, проверенные версии промтов, технические ошибки и общая стоимость одного запроса. Без API-ключей.',
 vacancy_classifications:'Неизменяемая история match/reject по ID корневой роли и попытке; hash промта и версия заголовка. Только актуальные решения участвуют в статусе.',
 vacancy_candidate_classification_leases:'Операционное владение классификацией одной первичной вакансии: поколение restore, owner token, срок. Исключено из бэкапа.',
 vacancy_role_classification:'Общие промты корневых ролей и переключатели парсинга, привязка role_id; Markdown5000, CAS revision, дата. Инициализация по stable_key только для двух стандартных корней. При перемещении настройки сохраняются, дочерние не участвуют. Backup28.',
 openai_settings:'Общий административный ключ OpenAI: AES-GCM ciphertext, последние4 символа, редакция и дата. Ключ расшифровывается только на сервере через SESSION_SECRET с отдельным доменом openai:v1. Начиная с backup24 входит только зашифрованным; актуальный формат25.',
 prompt_model_settings:'Общий выбор провайдера для каждого промта: OpenRouter или OpenAI, ID модели OpenAI, уровень рассуждений, независимая редакция и дата. Только администратор; по умолчанию OpenRouter. Не содержит ключей. Начиная с backup23 входит в копию; актуальный формат25.',
 cover_letter_generation_leases:'Служебное владение генерацией: пользователь+вакансия, поколение БД, SHA-256 входного snapshot, UUID владельца и срок в Unix milliseconds (UTC). Не содержит исходных резюме/промтов, не имеет FK/cascade, не экспортируется и не восстанавливается из пользовательского бэкапа. HTTP генерация использует claim/renew/release, operation guard и owner fencing; один результат для параллельных запросов текущего кандидата.',
 application_creation_receipts:'Служебные результаты создания отклика: пользователь + ключ запроса, поколение БД, SHA-256 нормализованного тела и ID созданного отклика. Без исходных контактов/комментария и без FK/cascade. Строки переживают удаление и restore, не входят в пользовательский бэкап.',
 app_runtime_generation:'Служебное поколение живой базы: одна строка id=1 с UUID. Успешное восстановление меняет UUID в той же транзакции. Не экспортируется и не восстанавливается из бэкапа.',
 openrouter_settings:'Единая административная конфигурация OpenRouter. Ключ AES-GCM, только суффикс для интерфейса, JSON-кэш моделей, основная и последняя фактическая модели, время обновления, статус и редакция ключа.',
 openrouter_model_metrics:'Агрегированные замеры писем по модели; structured:task:generation:keyRevision:model — отдельные latency/error метрики парсера. Число успехов/ошибок, сумма и последнее время, даты. Без текста, резюме и ключей.',
 openrouter_ai_logs:'Последние 200 технических результатов AI-запросов: время, тип задачи, запрошенная/фактическая модель, переключение, успех и код ошибки. Без промтов, ответов и ключей.',
 system_prompts:'Два общих Markdown-промта: сопроводительное письмо и релевантность. Только администратор читает и меняет их через API, сопроводительное письмо до 15000, релевантность до 5000 Unicode-символов, каждый со своей редакцией.',
 vacancy_cover_letters:'Личные письма, созданные ИИ для пары пользователь + вакансия. Заголовок до 512, Markdown до 2000 символов. Общие письма, заполненные вручную, остаются в company_vacancies. Личный override_shared=1 имеет приоритет над общим; revision защищает повторную генерацию от гонки.',
 user_profiles:'Личное резюме пользователя в Markdown (до 20000 Unicode-символов), номер редакции для защиты от перезаписи и дата изменения. Доступ только по имени из сессии.',
 app_catalog:'Основной JSON-каталог компаний и агентств. Исходные seed-данные находятся в src/data.json; рабочая копия хранится здесь.',
 company_identity:'Постоянные числовые ID компаний для Excel. Не путать с местом в рейтинге.',agency_identity:'Постоянные числовые ID агентств для Excel.',
 company_crm:'Общие строковые ключи организаций и исторические CRM-поля. Личное состояние хранится отдельно.',
 company_user_state:'Личные статус, комментарий, избранное и скрытие организации; уникальна пара пользователь + организация.',
 company_status_history:'История изменений CRM-статуса организации с владельцем.',company_applications:'Отклики пользователя на организацию или вакансию; содержит контакты рекрутера и application_sources (JSON).',
 company_application_status_history:'История смены статуса конкретного отклика.',company_tasks:'Задачи пользователя, сроки и состояние синхронизации с календарём.',
 company_vacancies:'Общие вакансии. company_name обозначает владельца: компанию, агентство или служебную сущность канала; entity_type уточняет тип.',
 company_vacancy_state:'Отметка первоначального переноса встроенных вакансий компании в SQL.',company_vacancy_user_state:'Личные релевантность, избранное и скрытие вакансии.',vacancy_relevance_profile_state:'Какая версия профиля релевантности уже применена для пользователя.',
 vacancy_employer_guesses:'До пяти предположений о реальном работодателе вакансии агентства; связь с компанией, вакансией либо внешней ссылкой.',
 hiring_contacts:'До трёх общих контактов ответственных за найм на организацию.',telegram_channels:'Справочник Telegram-каналов с уникальным нормализованным username.',vacancy_telegram_sources:'Связь вакансий с каналами и исходными сообщениями.',
 google_calendar_connection:'Историческое общее подключение календаря; сохраняется ради совместимости.',google_calendar_user_connection:'Персональные подключения календаря; refresh-токен зашифрован.',vacancy_audit_runs:'Исторические результаты запусков проверки вакансий; количество удалений, обновлений дат и время завершения.'
};
export function buildDocumentation(root) {
 const manifest=JSON.parse(fs.readFileSync(path.join(root,'docs/manifest.json'),'utf8'));
 const source=fs.readFileSync(path.join(root,'frontend/features/settings/xlsx.ts'),'utf8');
 const exportSource=fs.readFileSync(path.join(root,'server/import-export/xlsx.js'),'utf8');
 const constants={...declarations,...exportsModule};
 const schema=buildBackupSchema(root);
 const latest=fs.readdirSync(path.join(root,'drizzle')).filter(name=>name.endsWith('.sql')).sort().at(-1).replace(/\.sql$/,'');
 if(manifest.versions.backup!==constants.BACKUP_VERSION||manifest.versions.rating!==constants.RATING_FORMULA_VERSION||manifest.versions.schema!==latest)throw new Error('Documentation version metadata is stale. Update docs/manifest.json and CHANGELOG.md.');
 const schemaMarkdown=[...schema.tables,...schema.operationalTables].map(t=>'## '+t.name+'\n\n'+(descriptions[t.name]||'Добавьте описание новой сущности в scripts/documentation.mjs.')+'\n\n'+table(['Поле SQL','Тип','NOT NULL','Ключ','По умолчанию'],t.columns.map(c=>[c.name,c.type,c.notNull?'Да':'Нет',c.primaryKey?'PRIMARY KEY':'—',c.defaultValue]))+'\n\n'+(t.foreignKeys.length?table(['Внешний ключ','Ссылается на','При удалении'],t.foreignKeys.map(k=>[k.from,k.table+'.'+k.to,k.on_delete]))+'\n\n':'')+'```sql\n'+schema.objects.filter(o=>o.tbl_name===t.name).map(o=>o.sql+';').join('\n')+'\n```').join('\n\n');
 const sheets=[['Компании','COMPANY_IMPORT_EXPORT_HEADERS'],['Офисы','COMPANY_OFFICE_IMPORT_EXPORT_HEADERS'],['Отзывы','COMPANY_REVIEW_IMPORT_EXPORT_HEADERS'],['Агентства','AGENCY_IMPORT_EXPORT_HEADERS'],['Контакты','HIRING_CONTACT_IMPORT_EXPORT_HEADERS']];
 const fieldMaps={};
 for(const [name,id] of [['Компании','companyFields'],['Офисы','officeFields'],['Отзывы','reviewFields'],['Агентства','agencyFields'],['Контакты','contactFields']]){
  const match=source.match(new RegExp('const\\s+'+id+'\\s*:\\s*FieldMap\\s*=\\s*(\\{[\\s\\S]*?\\});'));
  if(!match)throw new Error('Cannot extract import field map: '+id);
  fieldMaps[name]=new Function('return ('+match[1]+')')();
 }
 const formats=sheets.map(([sheet,key])=>'## Поля листа «'+sheet+'»\n\n'+table(['№','Заголовок Excel','Поле JSON импорта','Правило'],constants[key].map((header,index)=>{const entry=Object.entries(fieldMaps[sheet]).find(([,meta])=>meta.label===header);return [index+1,header,entry?.[0]||'Расчётное / см. обработчик',constants.companyImportInstruction(header,sheet)]}))).join('\n\n');
 const narrow=['companyVacancySourcesXlsx','companyReviewSourcesXlsx'].map((name,index)=>{
  const body=exportSource.slice(exportSource.indexOf('function '+name+'('));const headers=JSON.parse(body.match(/const headers = (\[[\s\S]*?\]);/)[1]);
  return '## '+(index?'Выгрузка источников отзывов':'Выгрузка источников вакансий')+'\n\n'+table(['№','Заголовок'],headers.map((h,i)=>[i+1,h]));
 }).join('\n\n');
 const vacancyKeys=['companyId','companyName','title','url','salaryFrom','salaryTo','level','role','relevance','publishedAt','loadedAt','descriptionMd','entityType','agencyId','agencyName','realCompanyId','realCompanyName','coverLetterSubject','coverLetterMd'];
 const vacancies=table(['№','Заголовок Excel','Поле JSON'],constants.VACANCY_EXPORT_HEADERS.map((header,index)=>[index+1,header,vacancyKeys[index]]));
 const replacements={'{{DATABASE_TABLES}}':schemaMarkdown,'{{COMPANY_FIELDS}}':formats,'{{NARROW_EXPORTS}}':narrow,'{{VACANCY_FIELDS}}':vacancies,'{{BACKUP_DETAILS}}':fs.readFileSync(path.join(root,'docs/BACKUP_FORMAT.md'),'utf8').replace(/^# /,'## ')};
 return {...manifest,sections:manifest.sections.map(section=>{
  let markdown=fs.readFileSync(path.join(root,'docs',section.file),'utf8');
  for(const [token,value] of Object.entries(replacements)){
   const name=token.slice(2,-2),start='<!-- AUTO:'+name+' -->',end='<!-- /AUTO:'+name+' -->';
   const block=start+'\n'+value+'\n'+end;
   if(markdown.includes(start)){
    const from=markdown.indexOf(start),to=markdown.indexOf(end,from);
    if(to<0)throw new Error('Unclosed automatic documentation block: '+name);
    markdown=markdown.slice(0,from)+block+markdown.slice(to+end.length);
   }else markdown=markdown.replace(token,()=>block);
  }
  const file=path.join(root,'docs',section.file);
  if(fs.readFileSync(file,'utf8')!==markdown)fs.writeFileSync(file,markdown);
  if(/\{\{[A-Z_]+\}\}/.test(markdown))throw new Error('Unresolved documentation marker in '+section.file);
  return {...section,markdown};
 })};
}
