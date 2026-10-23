import {legacyVacancyRoleIds} from '../repositories/legacy-vacancy-roles.js';
import {SCHEDULER_DEFAULTS,SCHEDULER_SOURCE_COLUMNS,SCHEDULER_SOURCE_OPTIONS} from '../repositories/scheduler-schema.js';
import {IMPORT_TABLE_NAMES} from '../repositories/vacancy-import-schema.js';
import {DETAIL_TABLE_NAMES} from '../repositories/vacancy-detail-schema.js';
// Server-only; extracted from the v218 Worker.
import {BACKUP_MIGRATION_TABLES,BACKUP_FORMAT,SUPPORTED_BACKUP_VERSIONS,REVIEW_SOURCE_META,AGENCY_RATING_INPUT_META,MAX_BACKUP_TABLES,MAX_BACKUP_ROWS,BACKUP_V2_REQUIRED_COLUMNS,BACKUP_VERSION,MAX_BACKUP_BYTES,schemaState} from '../constants.js';
import {quoteIdentifier} from '../repositories/schema.js';
import {isValidCatalog} from '../repositories/catalog.js';
import {agencyRatingInputValue,normalizeAgencyCalculatedScore,normalizeCatalog} from '../services/catalog.js';
import {BACKUP_SCHEMA,DATA} from '../data.js';
const DIAGNOSTIC_TABLE_NAMES=['parser_error_daily','parser_candidate_intakes'];
import {rotateRuntimeGenerationStatement} from '../repositories/runtime-generation.js';

export async function listBackupTables(db) {
  const result = await db.prepare(
    `SELECT name
     FROM sqlite_schema
     WHERE type = 'table'
       AND name NOT LIKE 'sqlite_%'
       AND substr(name, 1, 4) <> '_cf_'
       AND name NOT IN (${BACKUP_MIGRATION_TABLES.map(() => '?').join(', ')})
     ORDER BY name ASC`
  ).bind(...BACKUP_MIGRATION_TABLES).all();
  const tables = [];
  for (const record of result?.results || []) {
    const name = String(record.name || "");
    if (!name) continue;
    const [columnResult, foreignKeyResult] = await Promise.all([
      db.prepare(`PRAGMA table_info(${quoteIdentifier(name)})`).all(),
      db.prepare(`PRAGMA foreign_key_list(${quoteIdentifier(name)})`).all()
    ]);
    tables.push({
      name,
      columns: (columnResult?.results || []).map((column) => ({
        name: String(column.name),
        type: String(column.type || ""),
        notNull: Boolean(column.notnull),
        defaultValue: column.dflt_value ?? null,
        primaryKey: Boolean(column.pk)
      })),
      parentTables: [...new Set((foreignKeyResult?.results || []).map((item) => String(item.table || "")).filter(Boolean))]
    });
  }
  return tables;
}

export function sortTablesParentFirst(tables) {
  const byName = new Map(tables.map((table) => [table.name, table]));
  const visiting = new Set();
  const visited = new Set();
  const ordered = [];
  function visit(table) {
    if (visited.has(table.name)) return;
    if (visiting.has(table.name)) return;
    visiting.add(table.name);
    for (const parentName of table.parentTables || []) {
      const parent = byName.get(parentName);
      if (parent) visit(parent);
    }
    visiting.delete(table.name);
    visited.add(table.name);
    ordered.push(table);
  }
  tables.forEach(visit);
  return ordered;
}

function sortVacancyRoleRows(rows) {
  const pending = [...rows], ordered = [], inserted = new Set();
  while (pending.length) {
    const ready = pending.filter(row => row.parent_id == null || inserted.has(row.parent_id));
    if (!ready.length) throw new Error('В справочнике ролей обнаружен цикл или отсутствующий родитель.');
    for (const row of ready) {
      ordered.push(row);
      inserted.add(row.id);
      pending.splice(pending.indexOf(row), 1);
    }
  }
  return ordered;
}

export function validateCompanyAiBackup(company, version) {
  const hasScore = Object.prototype.hasOwnProperty.call(company || {}, "aiScore");
  const hasEvidence = Object.prototype.hasOwnProperty.call(company || {}, "aiEvidence");
  if (version >= 15 && (!hasScore || !hasEvidence)) throw new Error("В бэкапе отсутствуют поля оценки ИИ компании.");
  if (hasScore && company.aiScore !== null && ![0, 5, 10].includes(company.aiScore)) throw new Error("Балл ИИ в бэкапе: допустимы только 0, 5, 10 или null.");
  if (hasEvidence && (typeof company.aiEvidence !== "string" || company.aiEvidence.length > 10000)) throw new Error("Обоснование ИИ в бэкапе должно быть текстом до 10000 символов.");
  if (company?.aiScore > 0 && (!hasEvidence || !company.aiEvidence.trim())) throw new Error("Для баллов за ИИ в бэкапе укажите обоснование и источники.");
}

export function validateBackupPayload(payload) {
  if (!payload || payload.format !== BACKUP_FORMAT || !SUPPORTED_BACKUP_VERSIONS.has(payload.version)) {
    throw new Error("Это не поддерживаемый файл бэкапа.");
  }
  if (!isValidCatalog(payload.catalog)) {
    throw new Error("В бэкапе отсутствует корректный список компаний.");
  }
  for (const company of payload.catalog.companies) {
    validateCompanyAiBackup(company, payload.version);
    if (payload.version >= 17) {
      const vacancySources = company?.vacancySources && typeof company.vacancySources === "object" ? company.vacancySources : {};
      for (const [key, label] of [["dreamJob", "DreamJob"], ["hCareers", "Эйч"], ["rabota", "rabota.ru"]]) {
        if (vacancySources[key] != null && typeof vacancySources[key] !== "string") {
          throw new Error(`Источник вакансий «${label}» в бэкапе должен быть строкой.`);
        }
      }
    }
    const reviewSources = company?.reviewSources && typeof company.reviewSources === "object" ? company.reviewSources : null;
    const reviews = company?.reviews && typeof company.reviews === "object" ? company.reviews : {};
    if (payload.version >= 5) {
      if (!reviewSources || REVIEW_SOURCE_META.some(([key]) => typeof reviewSources[key] !== "string")) {
        throw new Error("В бэкапе отсутствуют источники отзывов компаний.");
      }
      if (!company?.reviews || typeof company.reviews !== "object") {
        throw new Error("В бэкапе отсутствуют агрегированные отзывы компаний.");
      }
    }
    for (const [key, label, maxLength] of [["plusShort", "Plus Short", 512], ["minusShort", "Minus Short", 512], ["plus", "Plus", 2000], ["minus", "Minus", 2000]]) {
      if (reviews[key] != null && typeof reviews[key] !== "string") throw new Error(`${label}: в бэкапе должно быть текстовое значение.`);
      if (typeof reviews[key] === "string" && reviews[key].length > maxLength) throw new Error(`${label}: значение в бэкапе длиннее ${maxLength} символов.`);
    }
  }
  if (payload.version >= 13) {
    for (const agency of Array.isArray(payload.catalog.agencies) ? payload.catalog.agencies : []) {
      for (const [key, label] of AGENCY_RATING_INPUT_META) {
        const value = agency?.[key];
        if (value != null && agencyRatingInputValue(value) == null) throw new Error(`${label}: в бэкапе должна быть оценка от 0 до 10 с шагом 0,5 или NULL.`);
      }
      for (const [key, label, max, decimal] of [["reviewsScore", "Балл отзывов", 80, true], ["relevanceScore", "Балл релевантности", 10, false], ["revenueScore", "Балл выручки", 10, false]]) {
        const value = agency?.[key];
        if (value != null && (normalizeAgencyCalculatedScore(value, max, decimal) == null || (!decimal && !Number.isInteger(Number(value))))) throw new Error(`${label}: в бэкапе содержится некорректное значение.`);
      }
      const reviewSources = agency?.reviewSources && typeof agency.reviewSources === "object" ? agency.reviewSources : {};
      for (const key of ["twoGis", "otzovik", "personAgency", "yell"]) if (typeof reviewSources[key] !== "string") throw new Error("В бэкапе отсутствуют новые источники отзывов агентств.");
    }
  }
  const tables = payload?.storage?.tables;
  if (!Array.isArray(tables) || tables.length > MAX_BACKUP_TABLES) {
    throw new Error("Некорректный состав данных бэкапа.");
  }
  let rowCount = 0;
  const names = new Set();
  for (const table of tables) {
    if (!table || typeof table.name !== "string" || !Array.isArray(table.rows) || !Array.isArray(table.columns)) {
      throw new Error("Некорректный раздел данных в бэкапе.");
    }
    if (names.has(table.name)) throw new Error("В бэкапе повторяется раздел данных.");
    names.add(table.name);
    rowCount += table.rows.length;
    if (rowCount > MAX_BACKUP_ROWS) throw new Error("В бэкапе слишком много записей.");
  }
  if (payload.version >= 2) {
    const byName = new Map(tables.map((table) => [table.name, table]));
    for (const [tableName, requiredColumns] of Object.entries(BACKUP_V2_REQUIRED_COLUMNS)) {
      const table = byName.get(tableName);
      const columns = new Set((table?.columns || []).map((column) => String(column?.name || "")));
      if (!table || requiredColumns.some((column) => !columns.has(column))) {
        throw new Error(`В бэкапе версии 2 отсутствует актуальная структура раздела "${tableName}".`);
      }
    }
    const applicationColumns = new Set((byName.get("company_applications")?.columns || []).map((column) => String(column?.name || "")));
    if (payload.version >= 4 && !applicationColumns.has("application_sources")) {
      throw new Error("В бэкапе отсутствуют источники откликов.");
    }
    if (payload.version >= 6) {
      for (const [tableName, columns] of [["company_tasks", ["user_name"]], ["company_applications", ["user_name"]], ["company_status_history", ["user_name"]], ["company_user_state", ["user_name", "company_name"]], ["company_vacancy_user_state", ["user_name", "vacancy_id"]]]) {
        const available = new Set((byName.get(tableName)?.columns || []).map((column) => String(column?.name || "")));
        if (!byName.has(tableName) || columns.some((column) => !available.has(column))) {
          throw new Error(`В бэкапе отсутствуют пользовательские данные раздела "${tableName}".`);
        }
      }
    }
    if (payload.version >= 7) {
      const userStateColumns = new Set((byName.get("company_user_state")?.columns || []).map((column) => String(column?.name || "")));
      if (!userStateColumns.has("hidden")) throw new Error("В бэкапе отсутствует признак скрытых компаний.");
    }
    if (payload.version >= 9) {
      const guessColumns = new Set((byName.get("vacancy_employer_guesses")?.columns || []).map((column) => String(column?.name || "")));
      const requiredGuessColumns = ["agency_vacancy_id", "position", "employer_name", "linked_company_name", "matched_vacancy_id", "external_vacancy_url"];
      if (!byName.has("vacancy_employer_guesses") || requiredGuessColumns.some((column) => !guessColumns.has(column))) {
        throw new Error("В бэкапе отсутствуют предположения о реальных работодателях.");
      }
    }
    if (payload.version >= 10) {
      const vacancyColumns = new Set((byName.get("company_vacancies")?.columns || []).map((column) => String(column?.name || "")));
      if (["cover_letter_subject", "cover_letter_md"].some((column) => !vacancyColumns.has(column))) {
        throw new Error("В бэкапе отсутствуют сопроводительные письма вакансий.");
      }
      const contactColumns = new Set((byName.get("hiring_contacts")?.columns || []).map((column) => String(column?.name || "")));
      const requiredContactColumns = ["company_name", "position", "full_name", "linkedin_url", "facebook_url", "telegram", "phone"];
      if (!byName.has("hiring_contacts") || requiredContactColumns.some((column) => !contactColumns.has(column))) {
        throw new Error("В бэкапе отсутствуют контакты ответственных за найм.");
      }
    }
    if (payload.version >= 11) {
      const contactColumns = new Set((byName.get("hiring_contacts")?.columns || []).map((column) => String(column?.name || "")));
      if (["email", "specialization"].some((column) => !contactColumns.has(column))) {
        throw new Error("В бэкапе отсутствуют email и специализация контактов.");
      }
    }
    if (payload.version >= 12) {
      const contactColumns = new Set((byName.get("hiring_contacts")?.columns || []).map((column) => String(column?.name || "")));
      if (!contactColumns.has("setka_url")) {
        throw new Error("В бэкапе отсутствуют ссылки на профили «Сетка».");
      }
    }
    if (payload.version >= 14) {
      const channelColumns = new Set((byName.get("telegram_channels")?.columns || []).map((column) => String(column?.name || "")));
      const sourceColumns = new Set((byName.get("vacancy_telegram_sources")?.columns || []).map((column) => String(column?.name || "")));
      if (!byName.has("telegram_channels") || ["name", "url", "normalized_username", "username", "last_successful_check_at", "comment"].some((column) => !channelColumns.has(column))) {
        throw new Error("В бэкапе отсутствуют независимые Telegram-каналы.");
      }
      if (!byName.has("vacancy_telegram_sources") || ["vacancy_id", "channel_id", "message_url", "telegram_contact"].some((column) => !sourceColumns.has(column))) {
        throw new Error("В бэкапе отсутствуют связи Telegram-каналов с вакансиями.");
      }
    }
  }
  return tables;
}

export function sameNames(actual, expected) {
  return actual.length === expected.length && new Set(actual).size === actual.length && expected.every(name => actual.includes(name));
}

export function assertBackupLayout(table, definition) {
  const columns = (table.columns || []).map(column => column.name);
  if (!sameNames(columns, definition.columns.map(column => column.name))) {
    throw new Error(`Неполная или несовместимая структура раздела "${table.name}". Обновите приложение и создайте полный бэкап.`);
  }
  for (const row of table.rows || []) {
    if (!row || Array.isArray(row) || !sameNames(Object.keys(row), columns) || Object.values(row).some(value => value !== null && typeof value !== 'string' && (typeof value !== 'number' || !Number.isFinite(value)))) {
      throw new Error(`Неполная или некорректная запись в разделе "${table.name}".`);
    }
  }
}

export async function backupDigest(payload) {
  const { integrity, ...contents } = payload;
  const bytes = new TextEncoder().encode(JSON.stringify(contents));
  return [...new Uint8Array(await crypto.subtle.digest('SHA-256', bytes))].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

export async function backupInventory(db) {
  const tables = await listBackupTables(db);
  for (const table of tables) {
    const expected = [...BACKUP_SCHEMA.tables, ...(BACKUP_SCHEMA.operationalTables || [])].find(item => item.name === table.name);
    if (!expected) throw new Error(`Обнаружена неизвестная таблица "${table.name}". Экспорт остановлен, чтобы не потерять данные.`);
    assertBackupLayout(table, expected);
  }
  return tables.filter(table => !BACKUP_SCHEMA.operationalTables?.some(item => item.name === table.name));
}

export async function createBackup(db) {
  // Only SELECT/PRAGMA: downloading a backup must never migrate or normalize data.
  const schemaSql = "SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' AND substr(name, 1, 4) <> '_cf_' ORDER BY type, name";
  const schemaBefore = (await db.prepare(schemaSql).all()).results || [];
  const actual = await backupInventory(db);
  const existing = new Set(actual.map(table => table.name));
  const metadataNames = BACKUP_MIGRATION_TABLES.filter(name => schemaBefore.some(item => item.name === name && item.type === 'table'));
  const hasSequence = !!await db.prepare("SELECT name FROM sqlite_schema WHERE name = 'sqlite_sequence'").first();
  const ordered = BACKUP_SCHEMA.tables;
  // D1 batch gives one transactional snapshot across every application table.
  const statements = ordered.map(table => db.prepare(existing.has(table.name) ? `SELECT * FROM ${quoteIdentifier(table.name)}` : 'SELECT 1 WHERE 0'));
  statements.push(db.prepare(schemaSql));
  statements.push(db.prepare(hasSequence ? 'SELECT name, seq FROM sqlite_sequence ORDER BY name' : 'SELECT 1 WHERE 0'));
  for (const name of metadataNames) statements.push(db.prepare(`SELECT * FROM ${quoteIdentifier(name)}`));
  const results = await db.batch(statements);
  const schema = results[ordered.length].results || [];
  if (JSON.stringify(schema) !== JSON.stringify(schemaBefore)) throw new Error('Структура базы изменилась во время экспорта. Повторите скачивание.');
  const tables = ordered.map((table, index) => ({name:table.name, columns:table.columns, rows:results[index].results || []}));
  for (const table of tables) assertBackupLayout(table, ordered.find(item => item.name === table.name));
  const rawCatalog = tables.find(table => table.name === 'app_catalog').rows.find(row => row.id === 1);
  const catalog = rawCatalog ? JSON.parse(rawCatalog.data_json) : normalizeCatalog(DATA);
  if (!isValidCatalog(catalog)) throw new Error('Сохранённый каталог повреждён. Полный бэкап не создан.');
  const payload = {
    format:BACKUP_FORMAT, version:BACKUP_VERSION, createdAt:new Date().toISOString(), catalog,
    storage:{tables},
    manifest:tables.map(table => ({name:table.name, columns:table.columns.map(column => column.name), rows:table.rows.length})),
    metadata:{
      schema,
      sequences:(results[ordered.length + 1].results || []).filter(row => existing.has(row.name)),
      migrations:metadataNames.map((name, index) => ({name, rows:results[ordered.length + 2 + index].results || []})),
      missingTables:ordered.filter(table => !existing.has(table.name)).map(table => table.name),
    },
  };
  payload.integrity = {algorithm:'SHA-256', digest:await backupDigest(payload)};
  // Use the very same checks as import. Never offer a file we cannot read back.
  await validateFullBackup(payload);
  if (new TextEncoder().encode(JSON.stringify(payload, null, 2)).byteLength > MAX_BACKUP_BYTES) throw new Error('Полный бэкап превышает 25 МБ. Используйте SQL-экспорт D1; усечённый файл не создаётся.');
  return payload;
}

export async function validateFullBackup(payload) {
  if (!payload || payload.format !== BACKUP_FORMAT || !SUPPORTED_BACKUP_VERSIONS.has(payload.version)) throw new Error('Поддерживаются полные бэкапы версий 14–39. Более старые файлы требуют отдельного переноса.');
  let tables;
  if (payload.version >= 16) {
    if (payload.integrity?.algorithm !== 'SHA-256' || payload.integrity.digest !== await backupDigest(payload)) throw new Error('Контрольная сумма не совпадает. Файл изменён или повреждён.');
    tables = payload.storage?.tables;
    if (!Array.isArray(tables) || !sameNames(tables.map(table => table?.name), BACKUP_SCHEMA.tables.filter(table => (payload.version>=39||!DIAGNOSTIC_TABLE_NAMES.includes(table.name)) && (payload.version>=36||table.name!=='apify_settings') && (payload.version>=33||!IMPORT_TABLE_NAMES.includes(table.name)) && (payload.version>=32||!DETAIL_TABLE_NAMES.includes(table.name)) && (payload.version >= 18 || table.name !== 'user_profiles') && (payload.version >= 19 || !['openrouter_settings','openrouter_ai_logs'].includes(table.name)) && (payload.version >= 20 || table.name !== 'system_prompts') && (payload.version >= 21 || table.name !== 'vacancy_cover_letters') && (payload.version >= 22 || table.name !== 'openrouter_model_metrics') && (payload.version >= 23 || table.name !== 'prompt_model_settings') && (payload.version >= 24 || table.name !== 'openai_settings') && (payload.version >= 27 || !['vacancy_roles','vacancy_role_assignments'].includes(table.name)) && (payload.version >= 28 || table.name !== 'vacancy_role_classification') && (payload.version >= 30 || !['firecrawl_settings','vacancy_source_parsing'].includes(table.name)) && (payload.version >= 29 || !['vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications'].includes(table.name))).map(table => table.name))) throw new Error('В бэкапе отсутствуют таблицы или присутствуют неизвестные разделы.');
    const manifest = tables.map(table => ({name:table.name, columns:(table.columns || []).map(column => column.name), rows:table.rows?.length}));
    if (JSON.stringify(payload.manifest) !== JSON.stringify(manifest)) throw new Error('Манифест не соответствует содержимому бэкапа.');
    if (!Array.isArray(payload.metadata?.sequences) || !Array.isArray(payload.metadata?.migrations) || !Array.isArray(payload.metadata?.schema)) throw new Error('Отсутствуют метаданные бэкапа.');
    const sequenceNames = new Set();
    for (const sequence of payload.metadata.sequences) {
      const definition = BACKUP_SCHEMA.objects.find(item => item.type === 'table' && item.name === sequence.name && /AUTOINCREMENT/i.test(item.sql));
      if (!definition || sequenceNames.has(sequence.name) || !Number.isSafeInteger(sequence.seq) || sequence.seq < 0) throw new Error('Некорректные счётчики идентификаторов.');
      sequenceNames.add(sequence.name);
    }
  } else {
    tables = validateBackupPayload(payload);
    const required = BACKUP_SCHEMA.tables.filter(table => ![...DIAGNOSTIC_TABLE_NAMES,...IMPORT_TABLE_NAMES,...DETAIL_TABLE_NAMES,'apify_settings','app_catalog', 'google_calendar_connection', 'vacancy_audit_runs', 'user_profiles', 'openrouter_settings', 'openrouter_ai_logs', 'openrouter_model_metrics', 'system_prompts', 'vacancy_cover_letters', 'prompt_model_settings', 'openai_settings', 'vacancy_roles', 'vacancy_role_assignments', 'vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(table.name));
    const names = tables.map(table => table.name);
    if (!required.every(table => names.includes(table.name)) || names.some(name => !required.some(table => table.name === name) && name !== 'vacancy_audit_runs')) throw new Error('Старый бэкап неполон: отсутствуют обязательные таблицы. Восстановление отменено.');
  }
  let count = 0;
  for (const table of tables) {
    if (!Array.isArray(table.rows) || !Array.isArray(table.columns)) throw new Error('Некорректные таблицы бэкапа.');
    let definition=BACKUP_SCHEMA.tables.find(item=>item.name===table.name);
    if(payload.version<25&&table.name==='vacancy_cover_letters'){
      // Old files have no explicit personal override or CAS revision. Validate
      // their exact old layout before adding safe defaults during restore.
      const names=new Set(table.columns.map(c=>c.name));
      if(!names.has('override_shared')&&!names.has('revision'))definition={...definition,columns:definition.columns.filter(c=>!['override_shared','revision'].includes(c.name))};
    }
    if(payload.version<38&&['vacancy_candidates','vacancy_source_parsing'].includes(table.name))definition={...definition,columns:definition.columns.filter(c=>c.name!=='agency_id')};
    if(payload.version<37&&table.name==='vacancy_role_settings')definition={...definition,columns:definition.columns.filter(c=>c.name!=='openrouter_model')};
    if(payload.version<26&&table.name==='prompt_model_settings'&&!table.columns.some(c=>c.name==='openrouter_model'))definition={...definition,columns:definition.columns.filter(c=>c.name!=='openrouter_model')};
    if(payload.version<31&&table.name==='firecrawl_settings')definition={...definition,columns:definition.columns.filter(c=>!['mode','hobby_key_encrypted','hobby_key_suffix','hobby_connection_status','hobby_last_error','hobby_checked_at','free_key_hash','hobby_key_hash','free_usage_json','hobby_usage_json'].includes(c.name))};
    if(payload.version<34&&table.name==='firecrawl_settings')definition={...definition,columns:definition.columns.filter(c=>c.name!=='favorites_only')};
    if(payload.version<35&&table.name==='firecrawl_settings')definition={...definition,columns:definition.columns.filter(c=>c.name!=='scheduler_json')};
    if(payload.version<35&&table.name==='vacancy_source_parsing')definition={...definition,columns:definition.columns.filter(c=>!Object.hasOwn(SCHEDULER_SOURCE_COLUMNS,c.name))};
    assertBackupLayout(table,definition);
    count += table.rows.length;
    if (count > MAX_BACKUP_ROWS) throw new Error('В бэкапе слишком много записей.');
  }
  // Validate declared foreign keys before enabling restore in the UI.
  const byName = new Map(tables.map(table => [table.name, table]));
  for (const definition of BACKUP_SCHEMA.tables) {
    const table = byName.get(definition.name);
    if (!table) continue;
    for (const key of definition.foreignKeys) {
      if(payload.version<38&&key.from==='agency_id'&&['vacancy_candidates','vacancy_source_parsing'].includes(definition.name))continue;
      const parents = new Set((byName.get(key.table)?.rows || []).map(row => row[key.to]));
      if (table.rows.some(row => row[key.from] !== null && !parents.has(row[key.from]))) throw new Error(`Нарушена связь раздела "${definition.name}" с "${key.table}".`);
    }
  }
  if (payload.version >= 16) {
    const catalogRows = tables.find(table => table.name === 'app_catalog').rows;
    const catalog = catalogRows.find(row => row.id === 1);
    if (!isValidCatalog(payload.catalog) || (catalog && JSON.stringify(JSON.parse(catalog.data_json)) !== JSON.stringify(payload.catalog))) throw new Error('Каталог не соответствует исходным данным.');
  }
  return tables;
}

export async function prepareBackupRestore(db, payload) {
  const incoming = await validateFullBackup(payload);
  const current = await backupInventory(db);
  const tables = incoming.map(table => payload.version<25&&table.name==='vacancy_cover_letters'
    ? {...BACKUP_SCHEMA.tables.find(t=>t.name===table.name),rows:table.rows.map(row=>({...row,override_shared:row.override_shared??0,revision:row.revision??0}))}
    : payload.version<37&&table.name==='vacancy_role_settings'
      ? {...BACKUP_SCHEMA.tables.find(t=>t.name===table.name),rows:table.rows.map(row=>({...row,openrouter_model:'best_free'}))}
    : payload.version<26&&table.name==='prompt_model_settings'
      ? {...BACKUP_SCHEMA.tables.find(t=>t.name===table.name),rows:table.rows.map(row=>({...row,openrouter_model:row.openrouter_model??'best_free'}))}
      : {...table,rows:table.rows});
  if (payload.version < 16) {
    // Legacy formats did not export these sections. Preserve existing contents,
    // explicitly report the limitation, and never silently erase audit records.
    for (const name of ['google_calendar_connection', 'vacancy_audit_runs']) {
      if (!tables.some(table => table.name === name)) {
        const definition = BACKUP_SCHEMA.tables.find(table => table.name === name);
        const rows = current.some(table => table.name === name) ? (await db.prepare(`SELECT * FROM ${quoteIdentifier(name)}`).all()).results || [] : [];
        tables.push({...definition, rows});
      }
    }
    tables.push({...BACKUP_SCHEMA.tables.find(table => table.name === 'app_catalog'), rows:[{id:1, data_json:JSON.stringify(normalizeCatalog(payload.catalog)), updated_at:payload.createdAt || new Date().toISOString()}]});
  }
  if (payload.version < 18) {
    const definition = BACKUP_SCHEMA.tables.find(table => table.name === 'user_profiles');
    const rows = current.some(table => table.name === 'user_profiles') ? (await db.prepare('SELECT * FROM user_profiles').all()).results || [] : [];
    tables.push({...definition, rows});
  }
  if (payload.version < 19) {
    for (const name of ['openrouter_settings','openrouter_ai_logs']) {
      const definition=BACKUP_SCHEMA.tables.find(table=>table.name===name);
      const rows=current.some(table=>table.name===name)?(await db.prepare('SELECT * FROM '+name).all()).results||[]:[];
      tables.push({...definition,rows});
    }
  }
  if (payload.version < 20) {
    const definition=BACKUP_SCHEMA.tables.find(table=>table.name==='system_prompts');
    const rows=current.some(table=>table.name==='system_prompts')?(await db.prepare('SELECT * FROM system_prompts').all()).results||[]:[];
    tables.push({...definition,rows});
  }
  if (payload.version < 21) {
    const definition=BACKUP_SCHEMA.tables.find(table=>table.name==='vacancy_cover_letters');
    const rows=current.some(table=>table.name==='vacancy_cover_letters')?(await db.prepare('SELECT * FROM vacancy_cover_letters').all()).results||[]:[];
    tables.push({...definition,rows});
  }
  if (payload.version < 22) {
    const definition=BACKUP_SCHEMA.tables.find(table=>table.name==='openrouter_model_metrics');
    const rows=current.some(table=>table.name==='openrouter_model_metrics')?(await db.prepare('SELECT * FROM openrouter_model_metrics').all()).results||[]:[];
    tables.push({...definition,rows});
  }
  if(payload.version < 23){
    const definition=BACKUP_SCHEMA.tables.find(table=>table.name==='prompt_model_settings');
    const rows=current.some(table=>table.name==='prompt_model_settings')?(await db.prepare('SELECT * FROM prompt_model_settings').all()).results||[]:[];
    tables.push({...definition,rows});
  }
  if(payload.version < 24){
    const definition=BACKUP_SCHEMA.tables.find(table=>table.name==='openai_settings');
    const rows=current.some(table=>table.name==='openai_settings')?(await db.prepare('SELECT * FROM openai_settings').all()).results||[]:[];
    tables.push({...definition,rows});
  }
  if(payload.version < 27){
    const vacancyTable=tables.find(table=>table.name==='company_vacancies');
    const now=payload.createdAt||new Date().toISOString();
    const roles=[
      {id:1,stable_key:'root-it-manager',name:'IT Manager',parent_id:null,sort_order:10,created_at:now,updated_at:now},
      {id:2,stable_key:'root-journalist',name:'Журналист',parent_id:null,sort_order:20,created_at:now,updated_at:now},
      {id:3,stable_key:'it-project-manager',name:'Менеджер проектов',parent_id:1,sort_order:10,created_at:now,updated_at:now},
      {id:4,stable_key:'it-product-manager',name:'Менеджер продуктов',parent_id:1,sort_order:20,created_at:now,updated_at:now},
      {id:5,stable_key:'it-development-manager',name:'Руководитель разработки',parent_id:1,sort_order:30,created_at:now,updated_at:now},
      {id:6,stable_key:'journalist-editor',name:'Редактор',parent_id:2,sort_order:10,created_at:now,updated_at:now},
      {id:7,stable_key:'journalist-data',name:'Дата-журналист',parent_id:2,sort_order:20,created_at:now,updated_at:now},
      {id:8,stable_key:'it-development-director',name:'Директор по разработке',parent_id:1,sort_order:40,created_at:now,updated_at:now}
    ];
    const standard=new Map([['менеджер проектов',3],['менеджер продукта',4],['менеджер продуктов',4],['руководитель разработки',5],['редактор',6],['дата-журналист',7],['директор по разработке',8]]);
    const values=[...new Set((vacancyTable?.rows||[]).map(row=>String(row.role||'').trim()).filter(Boolean))];
    let nextId=9;
    if(values.some(value=>value.toLocaleLowerCase('ru')==='it manager')){roles.push({id:nextId,stable_key:'it-unspecified',name:'Без уточнения (IT Manager)',parent_id:1,sort_order:90,created_at:now,updated_at:now});standard.set('it manager',nextId++);}
    if(values.some(value=>value.toLocaleLowerCase('ru')==='журналист')){roles.push({id:nextId,stable_key:'journalist-unspecified',name:'Без уточнения (Журналист)',parent_id:2,sort_order:90,created_at:now,updated_at:now});standard.set('журналист',nextId++);}
    const unknown=values.filter(value=>!legacyVacancyRoleIds(value,roles));
    let preservedId=null;
    if(unknown.length){preservedId=nextId++;roles.push({id:preservedId,stable_key:'root-preserved',name:'Сохранённые роли',parent_id:null,sort_order:90,created_at:now,updated_at:now});}
    for(const value of unknown){const id=nextId++,key=value.toLocaleLowerCase('ru');roles.push({id,stable_key:'legacy-'+[...new TextEncoder().encode(value)].map(byte=>byte.toString(16).padStart(2,'0')).join(''),name:value,parent_id:preservedId,sort_order:100+id,created_at:now,updated_at:now});standard.set(key,id);}
    const assignments=(vacancyTable?.rows||[]).flatMap(row=>(legacyVacancyRoleIds(row.role,roles)||[]).map(id=>({vacancy_id:row.id,role_id:id})));
    tables.push({...BACKUP_SCHEMA.tables.find(table=>table.name==='vacancy_roles'),rows:roles});
    tables.push({...BACKUP_SCHEMA.tables.find(table=>table.name==='vacancy_role_assignments'),rows:assignments});
  }
  // Old backups contain no role-bound prompts. Preserve only settings whose
  // stable role identity matches the restored tree; numeric IDs can be reused.
  if(payload.version < 28){
    const restored=tables.find(t=>t.name==='vacancy_roles')?.rows||[];
    const previous=current.some(t=>t.name==='vacancy_roles')?(await db.prepare('SELECT id,stable_key FROM vacancy_roles').all()).results||[]:[];
    const byKey=new Map(restored.map(r=>[r.stable_key,r.id]));
    const priorIds=new Map(previous.map(r=>[r.id,r.stable_key]));
    const saved=current.some(t=>t.name==='vacancy_role_classification')?(await db.prepare('SELECT * FROM vacancy_role_classification').all()).results||[]:[];
    const rows=saved.filter(r=>byKey.has(priorIds.get(r.role_id))).map(r=>({...r,role_id:byKey.get(priorIds.get(r.role_id))}));
    tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name==='vacancy_role_classification'),rows});
  }
  if(payload.version<29){
    // Old backups replace companies/vacancies/roles, so staging FK IDs cannot
    // safely be carried across implicitly. Warn and initialize empty storage.
    for(const name of ['vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications'])tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name===name),rows:[]});
  }
  if(payload.version<30){
    tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name==='firecrawl_settings'),rows:current.some(t=>t.name==='firecrawl_settings')?(await db.prepare('SELECT * FROM firecrawl_settings').all()).results||[]:[]});
    tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name==='vacancy_source_parsing'),rows:[]});
  }
  if(payload.version<38)for(const name of ['vacancy_candidates','vacancy_source_parsing']){const table=tables.find(t=>t.name===name);table.columns=BACKUP_SCHEMA.tables.find(t=>t.name===name).columns;table.rows=table.rows.map(row=>({...row,agency_id:null}));}
  if(payload.version<31){const table=tables.find(t=>t.name==='firecrawl_settings'),definition=BACKUP_SCHEMA.tables.find(t=>t.name==='firecrawl_settings');table.columns=definition.columns;table.rows=table.rows.map(row=>({...row,mode:'free',hobby_key_encrypted:row.hobby_key_encrypted||'',hobby_key_suffix:row.hobby_key_suffix||'',hobby_connection_status:'unchecked',hobby_last_error:'',hobby_checked_at:null,free_key_hash:'',hobby_key_hash:'',free_usage_json:'',hobby_usage_json:'',proxy:'basic',enhanced_allowed:0,revision:(row.revision||0)+1}));}
  if(payload.version<34){const table=tables.find(t=>t.name==='firecrawl_settings');table.columns=BACKUP_SCHEMA.tables.find(t=>t.name==='firecrawl_settings').columns;table.rows=table.rows.map(row=>({...row,favorites_only:1}));}
  {const table=tables.find(t=>t.name==='firecrawl_settings');table.columns=BACKUP_SCHEMA.tables.find(t=>t.name===table.name).columns;table.rows=table.rows.map(row=>{const saved=payload.version>=35?JSON.parse(row.scheduler_json):{};
    const legacy=payload.version>=35?(tables.find(t=>t.name==='vacancy_source_parsing')?.rows||[]).filter(r=>r.auto_enabled===1).map(r=>r.source_id):[];
    return {...row,scheduler_json:JSON.stringify({...SCHEDULER_DEFAULTS,...saved,sourceIds:saved.sourceIds??[...new Set(legacy)].filter(id=>SCHEDULER_SOURCE_OPTIONS.some(s=>s.id===id&&s.supported)),enabled:false,cloudOwnerId:''})};});}
  if(payload.version<35){const table=tables.find(t=>t.name==='vacancy_source_parsing');table.columns=BACKUP_SCHEMA.tables.find(t=>t.name===table.name).columns;table.rows=table.rows.map(row=>({...row,auto_enabled:0,interval_days:7,next_run_at:null,last_attempt_at:null,last_result:''}));}
  if(payload.version<36)tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name==='apify_settings'),rows:current.some(t=>t.name==='apify_settings')?(await db.prepare('SELECT * FROM apify_settings').all()).results||[]:[]});
  {const table=tables.find(t=>t.name==='apify_settings');table.rows=table.rows.map(row=>({...row,enabled:0,paid_allowed:0,free_account_json:'',starter_account_json:'',free_error:'',starter_error:''}));}
  if(payload.version<33)for(const name of IMPORT_TABLE_NAMES)tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name===name),rows:[]});
  if(payload.version<32)for(const name of DETAIL_TABLE_NAMES)tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name===name),rows:[]});
  if(payload.version<39)for(const name of DIAGNOSTIC_TABLE_NAMES){
    // Old copies have no intake provenance. Preserve daily observations, but
    // retain intake receipts only for the exact immutable restored token.
    const tokens=new Set(tables.find(t=>t.name==='vacancy_candidates')?.rows.map(r=>r.creation_token)||[]);
    const rows=current.some(t=>t.name===name)?(await db.prepare(`SELECT * FROM ${quoteIdentifier(name)}`).all()).results||[]:[];
    tables.push({...BACKUP_SCHEMA.tables.find(t=>t.name===name),rows:name==='parser_candidate_intakes'?rows.filter(r=>tokens.has(r.candidate_token)):rows});
  }
  const resumeWarnings = payload.version < 18 ? ['В этом бэкапе нет резюме: существующие резюме пользователей сохраняются; на новой базе они будут пустыми.'] : [];
  if(payload.version<39)resumeWarnings.push('В этом бэкапе нет истории графика ошибок и происхождения вакансий по парсерам. Существующие дневные наблюдения сохраняются; происхождение сохраняется только для совпавших постоянных токенов вакансий. На новой базе эти данные будут отсутствовать.');
  resumeWarnings.push('Планировщик и Apify после восстановления выключены. Временная очередь и блокировки не переносятся; для возобновления обработки включите планировщик и заново подключите облачный триггер. Исходники сайта, пароли, переменные среды и сами облачные расписания восстанавливаются отдельно; для зашифрованных подключений нужен прежний SESSION_SECRET либо повторное подключение.');
  if(payload.version < 19) resumeWarnings.push('В этом бэкапе нет OpenRouter: текущие настройки, кэш и журнал сохраняются.');
  if(payload.version < 20) resumeWarnings.push('В этом бэкапе нет системных промтов: текущие промты сохраняются; на новой базе они будут пустыми.');
  if(payload.version < 21) resumeWarnings.push('В этом бэкапе нет личных сопроводительных писем: текущие письма сохраняются; на новой базе они будут пустыми.');
  if(payload.version < 22) resumeWarnings.push('В этом бэкапе нет замеров времени моделей: текущая статистика сохраняется; на новой базе она будет пустой.');
  if(payload.version < 23) resumeWarnings.push('В этом бэкапе нет выбора провайдера промтов: текущие настройки сохраняются; на новой базе используется OpenRouter.');
  if(payload.version < 24) resumeWarnings.push('В этом бэкапе нет подключения OpenAI: текущий зашифрованный ключ сохраняется.');
  if(payload.version<37&&incoming.some(t=>t.name==='vacancy_role_settings'))resumeWarnings.push('В старом бэкапе модель конечных ролей OpenRouter не указана: восстановлена прежняя «Лучшая бесплатная». При необходимости выберите JEV в настройках промта.');
  if(payload.version<26&&incoming.some(t=>t.name==='prompt_model_settings'))resumeWarnings.push('В старом бэкапе выбор JEV отсутствует: для OpenRouter используется «Лучшая бесплатная».');
  if(payload.version<27)resumeWarnings.push('Роли вакансий перенесены из старого одиночного поля в новый иерархический справочник.');
  if(payload.version<28)resumeWarnings.push('В старом бэкапе нет классификации: текущие промты и переключатели сохраняются только для совпадающих постоянных ключей ролей.');
  if(payload.version<29)resumeWarnings.push('В этом бэкапе нет промежуточных вакансий и истории решений. При восстановлении промежуточное хранилище будет пустым.');
  if(payload.version<30)resumeWarnings.push('В старом бэкапе нет Firecrawl: глобальный зашифрованный ключ сохраняется, настройки источников сбрасываются в выключенное состояние.');
  if(payload.version<33)resumeWarnings.push('В этом бэкапе нет безопасного импорта/конечных ролей; метаданные пустые, существующие поля считаются неизвестного происхождения и защищены.');
  if(payload.version<32)resumeWarnings.push('В этом бэкапе нет полных текстов/структурирования; новое хранилище пустое, настройки структурирования по умолчанию.');
  if(payload.version<31)resumeWarnings.push('В старом бэкапе нет режимов Firecrawl: восстановлен Free, Hobby выключен. История бюджета в текущей базе не сбрасывается.');
  return {tables, current, warnings:payload.version < 16 ? ['Старый формат: исходная дата каталога, счётчики ID и служебные метаданные не сохранены. Разделы, отсутствовавшие в старом экспорте, сохраняются в текущей базе. Полноту строк старого файла нельзя подтвердить контрольной суммой.', ...resumeWarnings] : resumeWarnings};
}

export async function restoreBackup(db, payload) {
  const plan = await prepareBackupRestore(db, payload);
  const byName = new Map(plan.tables.map(table => [table.name, table]));
  const ordered = sortTablesParentFirst(BACKUP_SCHEMA.tables);
  const statements = [db.prepare('DROP TRIGGER IF EXISTS company_vacancies_max_10')], verification = [];
  // Schema bootstrap and every replacement belong to the SAME D1 transaction.
  for (const object of BACKUP_SCHEMA.objects) {
    statements.push(db.prepare(object.sql.replace(/^CREATE (TABLE|UNIQUE INDEX|INDEX|TRIGGER|VIEW) /i, 'CREATE $1 IF NOT EXISTS ')));
  }
  for (const table of [...ordered].reverse()) {
    // SQLite RESTRICT checks a self-reference before the DELETE statement ends.
    if (table.name === 'vacancy_roles') statements.push(db.prepare('UPDATE vacancy_roles SET parent_id=NULL'));
    statements.push(db.prepare(`DELETE FROM ${quoteIdentifier(table.name)}`));
  }
  for (const table of ordered) {
    const incoming = byName.get(table.name);
    const columns = table.columns.map(column => column.name);
    // JSON chunks avoid D1's bound-parameter limit, without changing value types.
    let chunk = [], size = 0;
    const flush = () => {
      if (!chunk.length) return;
      statements.push(db.prepare(`INSERT INTO ${quoteIdentifier(table.name)} (${columns.map(quoteIdentifier).join(',')}) SELECT ${columns.map(name => `json_extract(value, '$.${name}')`).join(',')} FROM json_each(?)`).bind(JSON.stringify(chunk)));
      verification.push(db.prepare(`INSERT INTO _backup_restore_guard (ok) SELECT CASE WHEN NOT EXISTS (SELECT ${columns.map(name => `json_extract(value, '$.${name}')`).join(',')} FROM json_each(?) EXCEPT SELECT ${columns.map(quoteIdentifier).join(',')} FROM ${quoteIdentifier(table.name)}) THEN 1 ELSE 0 END`).bind(JSON.stringify(chunk)));
      chunk = []; size = 0;
    };
    const rows = table.name === 'vacancy_roles' ? sortVacancyRoleRows(incoming.rows) : incoming.rows;
    for (const row of rows) {
      const length = JSON.stringify(row).length;
      if (size + length > 64000) flush();
      chunk.push(row); size += length;
    }
    flush();
  }
  if (payload.version >= 16) {
    for (const table of ordered) statements.push(db.prepare('DELETE FROM sqlite_sequence WHERE name = ?').bind(table.name));
    for (const sequence of payload.metadata.sequences) statements.push(db.prepare('INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)').bind(sequence.name, sequence.seq));
  }
  // Abort the transaction if constraints/triggers silently drop rows or FKs fail.
  statements.push(db.prepare('CREATE TABLE _backup_restore_guard (ok INTEGER NOT NULL CHECK (ok = 1))'));
  for (const table of ordered) {
    statements.push(db.prepare(`INSERT INTO _backup_restore_guard (ok) SELECT CASE WHEN (SELECT COUNT(*) FROM ${quoteIdentifier(table.name)}) = ? THEN 1 ELSE 0 END`).bind(byName.get(table.name).rows.length));
  }
  statements.push(...verification);
  statements.push(db.prepare('INSERT INTO _backup_restore_guard (ok) SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM pragma_foreign_key_check) THEN 1 ELSE 0 END'));
  statements.push(db.prepare('DROP TABLE _backup_restore_guard'));
  statements.push(rotateRuntimeGenerationStatement(db));
  await db.batch(statements);
  // No migration, normalization or other fallible write after COMMIT.
  schemaState.ready = true;
  return {ok:true, sourceVersion:payload.version, version:BACKUP_VERSION, companies:payload.catalog.companies.length, agencies:payload.catalog.agencies?.length || 0, tables:plan.tables.length, rows:plan.tables.reduce((sum, table) => sum + table.rows.length, 0), warnings:plan.warnings};
}

export async function readBackupRequest(request) {
  if (Number(request.headers.get('content-length') || 0) > MAX_BACKUP_BYTES) throw Object.assign(new Error('Файл бэкапа больше допустимых 25 МБ.'), {status:413});
  if (!request.body) throw new Error('Пустой файл бэкапа.');
  const reader = request.body.getReader(), decoder = new TextDecoder();
  let size = 0, text = '';
  try {
    for (;;) {
      const {done, value} = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_BACKUP_BYTES) { await reader.cancel(); throw Object.assign(new Error('Файл бэкапа больше допустимых 25 МБ.'), {status:413}); }
      text += decoder.decode(value, {stream:true});
    }
    return JSON.parse(text + decoder.decode());
  } finally { reader.releaseLock(); }
}
