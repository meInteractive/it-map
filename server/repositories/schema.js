import {repairLegacyVacancyRoles} from './legacy-vacancy-roles.js';
import {PROTECT_PARSER_URL_SQL,BACKFILL_PARSER_URL_SQL} from '../services/vacancy-source-priority.js';
import {APIFY_SCHEMA} from './apify-schema.js';
import {ensureSchedulerSchema} from './scheduler-schema.js';
import {ALIGN_RELEVANCE_PROMPT_SQL} from '../services/relevance-prompt.js';
import {VACANCY_IMPORT_SCHEMA} from './vacancy-import-schema.js';
import {VACANCY_DETAIL_SCHEMA} from './vacancy-detail-schema.js';
import {CLASSIFICATION_JOB_SCHEMA} from './classification-job-schema.js';
import {ensureFirecrawlPolicySchema} from './firecrawl-policy-schema.js';
import {FIRECRAWL_SCHEMA} from './firecrawl-schema.js';
import {VACANCY_CANDIDATE_SCHEMA} from './vacancy-candidate-schema.js';
import {CLASSIFICATION_TABLE_SQL} from './classification-schema.js';
// Server-only; extracted from the v218 Worker.
import {schemaState,CRM_TABLE_SQL,CRM_HISTORY_TABLE_SQL,CRM_HISTORY_INDEX_SQL,TASK_TABLE_SQL,TASK_DUE_INDEX_SQL,TASK_COMPANY_INDEX_SQL,GOOGLE_CALENDAR_CONNECTION_TABLE_SQL,GOOGLE_CALENDAR_USER_CONNECTION_TABLE_SQL,COMPANY_USER_STATE_TABLE_SQL,COMPANY_USER_STATE_FAVORITE_INDEX_SQL,COMPANY_USER_STATE_HIDDEN_INDEX_SQL,VACANCY_TABLE_SQL,VACANCY_COMPANY_DATE_INDEX_SQL,VACANCY_URL_UNIQUE_SQL,VACANCY_EMPLOYER_GUESS_TABLE_SQL,VACANCY_EMPLOYER_GUESS_COMPANY_INDEX_SQL,VACANCY_EMPLOYER_GUESS_MATCH_INDEX_SQL,VACANCY_EMPLOYER_GUESS_AGENCY_INSERT_TRIGGER_SQL,VACANCY_EMPLOYER_GUESS_AGENCY_UPDATE_TRIGGER_SQL,VACANCY_EMPLOYER_GUESS_MATCH_INSERT_TRIGGER_SQL,VACANCY_EMPLOYER_GUESS_MATCH_UPDATE_TRIGGER_SQL,VACANCY_STATE_TABLE_SQL,VACANCY_USER_STATE_TABLE_SQL,VACANCY_COVER_LETTERS_TABLE_SQL,VACANCY_COVER_LETTERS_INDEX_SQL,VACANCY_RELEVANCE_PROFILE_STATE_TABLE_SQL,APPLICATION_TABLE_SQL,APPLICATION_COMPANY_INDEX_SQL,APPLICATION_VACANCY_UNIQUE_SQL,APPLICATION_LIMIT_TRIGGER_SQL,APPLICATION_HISTORY_TABLE_SQL,APPLICATION_HISTORY_INDEX_SQL,APP_CATALOG_TABLE_SQL,COMPANY_IDENTITY_TABLE_SQL,AGENCY_IDENTITY_TABLE_SQL,TELEGRAM_CHANNEL_TABLE_SQL,VACANCY_TELEGRAM_SOURCE_TABLE_SQL,VACANCY_TELEGRAM_SOURCE_CHANNEL_INDEX_SQL,HIRING_CONTACT_TABLE_SQL,HIRING_CONTACT_COMPANY_INDEX_SQL,TASK_VACANCY_INDEX_SQL,VACANCY_ENTITY_INDEX_SQL,VACANCY_USER_STATE_FAVORITE_INDEX_SQL,VACANCY_USER_STATE_HIDDEN_INDEX_SQL,MAX_APPLICATIONS_PER_COMPANY} from '../constants.js';

export function quoteIdentifier(value) {
  return `"${String(value).replace(/"/g, '""')}"`;
}

export async function ensureCrmSchema(db) {
  if (schemaState.ready) return;
  await db.batch([
    db.prepare(CRM_TABLE_SQL),
    db.prepare(CRM_HISTORY_TABLE_SQL),
    db.prepare(CRM_HISTORY_INDEX_SQL),
    db.prepare(TASK_TABLE_SQL),
    db.prepare(TASK_DUE_INDEX_SQL),
    db.prepare(TASK_COMPANY_INDEX_SQL),
    db.prepare(GOOGLE_CALENDAR_CONNECTION_TABLE_SQL),
    db.prepare(GOOGLE_CALENDAR_USER_CONNECTION_TABLE_SQL),
    db.prepare(COMPANY_USER_STATE_TABLE_SQL),
    db.prepare(COMPANY_USER_STATE_FAVORITE_INDEX_SQL),
    db.prepare(COMPANY_USER_STATE_HIDDEN_INDEX_SQL),
    db.prepare(VACANCY_TABLE_SQL),
    db.prepare(VACANCY_COMPANY_DATE_INDEX_SQL),
    db.prepare(VACANCY_URL_UNIQUE_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_TABLE_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_COMPANY_INDEX_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_MATCH_INDEX_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_AGENCY_INSERT_TRIGGER_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_AGENCY_UPDATE_TRIGGER_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_MATCH_INSERT_TRIGGER_SQL),
    db.prepare(VACANCY_EMPLOYER_GUESS_MATCH_UPDATE_TRIGGER_SQL),
    db.prepare(VACANCY_STATE_TABLE_SQL),
    db.prepare(VACANCY_USER_STATE_TABLE_SQL),
    db.prepare(VACANCY_COVER_LETTERS_TABLE_SQL),
    db.prepare(VACANCY_COVER_LETTERS_INDEX_SQL),
    db.prepare(VACANCY_RELEVANCE_PROFILE_STATE_TABLE_SQL),
    db.prepare(APPLICATION_TABLE_SQL),
    db.prepare(APPLICATION_COMPANY_INDEX_SQL),
    db.prepare(APPLICATION_VACANCY_UNIQUE_SQL),
    db.prepare(APPLICATION_LIMIT_TRIGGER_SQL),
    db.prepare(APPLICATION_HISTORY_TABLE_SQL),
    db.prepare(APPLICATION_HISTORY_INDEX_SQL),
    db.prepare(APP_CATALOG_TABLE_SQL),
    db.prepare(COMPANY_IDENTITY_TABLE_SQL),
    db.prepare(AGENCY_IDENTITY_TABLE_SQL),
    db.prepare(TELEGRAM_CHANNEL_TABLE_SQL),
    db.prepare(VACANCY_TELEGRAM_SOURCE_TABLE_SQL),
    db.prepare(VACANCY_TELEGRAM_SOURCE_CHANNEL_INDEX_SQL),
    db.prepare(HIRING_CONTACT_TABLE_SQL),
    db.prepare(HIRING_CONTACT_COMPANY_INDEX_SQL)
  ]);
  await ensureVacancyColumns(db);
  await ensureVacancyRoleSchema(db);
  await removeLegacyVacancyLimit(db);
  await ensureVacancyUserStateColumns(db);
  await ensureTaskVacancyColumn(db);
  await ensureTaskCalendarColumns(db);
  await ensureApplicationContactColumns(db);
  await ensureApplicationSourceColumn(db);
  await ensureHiringContactColumns(db);
  await ensureUserColumns(db);
  await migrateLegacyUserData(db);
  await synchronizeVacancyApplicationStatuses(db);
  // Applications were migrated when the multi-application model was introduced.
  // An empty set now means the user deleted them, not that migration is pending.
  // Historical version-1 backups require a separate, explicit migration.
  await db.prepare(CLASSIFICATION_TABLE_SQL).run();
  await db.batch(VACANCY_CANDIDATE_SCHEMA.map(sql=>db.prepare(sql)));
  await db.batch(CLASSIFICATION_JOB_SCHEMA.map(sql=>db.prepare(sql)));
  await db.batch(FIRECRAWL_SCHEMA.map(sql=>db.prepare(sql)));
  await ensureFirecrawlPolicySchema(db);
  await db.batch(APIFY_SCHEMA.map(sql=>db.prepare(sql)));
  await db.batch(VACANCY_DETAIL_SCHEMA.map(sql=>db.prepare(sql)));
  await db.batch(VACANCY_IMPORT_SCHEMA.map(sql=>db.prepare(sql)));
  const roleColumns=(await db.prepare('PRAGMA table_info(vacancy_role_settings)').all()).results||[];
  if(!roleColumns.some(c=>c.name==='openrouter_model'))await db.prepare("ALTER TABLE vacancy_role_settings ADD COLUMN openrouter_model TEXT NOT NULL DEFAULT 'best_free' CHECK(openrouter_model IN ('best_free','jev'))").run();
  await db.batch([db.prepare(PROTECT_PARSER_URL_SQL),db.prepare(BACKFILL_PARSER_URL_SQL)]);
  await ensureSchedulerSchema(db);
  // Resume only imports stopped by the removed capacity rule. Other failures,
  // receipts, cancelled work and previous restore generations stay intact.
  if (await db.prepare("SELECT 1 FROM sqlite_master WHERE type='table' AND name='app_runtime_generation'").first()) await db.prepare("UPDATE scheduler_jobs SET status='pending',attempts=0,run_after=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER),reason_code='',last_error='',completed_at=NULL WHERE generation=(SELECT generation FROM app_runtime_generation WHERE id=1) AND type='import_vacancy' AND status='failed' AND reason_code='company_limit'").run();
  if (await db.prepare("SELECT 1 FROM sqlite_master WHERE type='table' AND name='app_runtime_generation'").first()) await db.prepare("UPDATE scheduler_jobs SET status='pending',attempts=0,run_after=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER),reason_code='',last_error='',completed_at=NULL WHERE generation=(SELECT generation FROM app_runtime_generation WHERE id=1) AND type IN ('extract_vacancy','assign_roles','classify_title') AND status='failed' AND reason_code IN ('23','23.0')").run();
  if (await db.prepare("SELECT 1 FROM sqlite_master WHERE type='table' AND name='app_runtime_generation'").first()) await db.prepare("UPDATE scheduler_jobs SET status='pending',attempts=0,run_after=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER),reason_code='',last_error='',completed_at=NULL WHERE generation=(SELECT generation FROM app_runtime_generation WHERE id=1) AND type='extract_vacancy' AND status='failed' AND reason_code='invalid_output' AND last_error IN ('Значение salary_period не подтверждается исходным текстом.','Значение salary_currency не подтверждается исходным текстом.','Значение salary_tax_type не подтверждается исходным текстом.','Значение work_format не подтверждается исходным текстом.','Валюта не подтверждена.','Дата публикации не подтверждается источником.')").run();
  if (await db.prepare("SELECT 1 FROM sqlite_master WHERE type='table' AND name='app_runtime_generation'").first()) await db.prepare("UPDATE scheduler_jobs SET status='pending',attempts=0,run_after=CAST((julianday('now')-2440587.5)*86400000 AS INTEGER),reason_code='',last_error='',completed_at=NULL WHERE generation=(SELECT generation FROM app_runtime_generation WHERE id=1) AND type='import_vacancy' AND status='failed' AND reason_code='mapping_invalid' AND last_error='Уровень вакансии указан некорректно: используйте Senior, Middle, Lead или Head.'").run();
  if (await db.prepare("SELECT 1 FROM sqlite_master WHERE type='table' AND name='system_prompts'").first()) await db.prepare(ALIGN_RELEVANCE_PROMPT_SQL).run();
  schemaState.ready = true;
}

export async function ensureVacancyRoleSchema(db) {
  await db.batch([
    db.prepare(`CREATE TABLE IF NOT EXISTS vacancy_roles (
      id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
      stable_key TEXT NOT NULL UNIQUE,
      name TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK(length(trim(name)) BETWEEN 1 AND 120),
      parent_id INTEGER REFERENCES vacancy_roles(id) ON DELETE RESTRICT,
      sort_order INTEGER NOT NULL DEFAULT 0 CHECK(sort_order >= 0),
      created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
      CHECK(parent_id IS NULL OR parent_id <> id))`),
    db.prepare('CREATE INDEX IF NOT EXISTS vacancy_roles_parent_order_idx ON vacancy_roles(parent_id,sort_order,id)'),
    db.prepare(`CREATE TABLE IF NOT EXISTS vacancy_role_assignments (
      vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE,
      role_id INTEGER NOT NULL REFERENCES vacancy_roles(id) ON DELETE RESTRICT,
      PRIMARY KEY(vacancy_id,role_id))`),
    db.prepare('CREATE INDEX IF NOT EXISTS vacancy_role_assignments_role_idx ON vacancy_role_assignments(role_id,vacancy_id)'),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) VALUES
      ('root-it-manager','IT Manager',NULL,10),('root-journalist','Журналист',NULL,20)`)
  ]);
  await db.batch([
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) SELECT 'it-project-manager','Менеджер проектов',id,10 FROM vacancy_roles WHERE stable_key='root-it-manager'`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) SELECT 'it-product-manager','Менеджер продуктов',id,20 FROM vacancy_roles WHERE stable_key='root-it-manager'`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) SELECT 'it-development-manager','Руководитель разработки',id,30 FROM vacancy_roles WHERE stable_key='root-it-manager'`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) SELECT 'it-development-director','Директор по разработке',id,40 FROM vacancy_roles WHERE stable_key='root-it-manager'`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) SELECT 'journalist-editor','Редактор',id,10 FROM vacancy_roles WHERE stable_key='root-journalist'`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order) SELECT 'journalist-data','Дата-журналист',id,20 FROM vacancy_roles WHERE stable_key='root-journalist'`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order)
      SELECT 'it-unspecified','Без уточнения (IT Manager)',id,90 FROM vacancy_roles
      WHERE stable_key='root-it-manager' AND EXISTS(SELECT 1 FROM company_vacancies WHERE lower(trim(role))=lower('IT Manager'))`),
    db.prepare(`INSERT OR IGNORE INTO vacancy_roles(stable_key,name,parent_id,sort_order)
      SELECT 'journalist-unspecified','Без уточнения (Журналист)',id,90 FROM vacancy_roles
      WHERE stable_key='root-journalist' AND EXISTS(SELECT 1 FROM company_vacancies WHERE lower(trim(role))=lower('Журналист'))`)
  ]);
  await repairLegacyVacancyRoles(db);
}

export async function removeLegacyVacancyLimit(db) {
  await db.prepare("DROP TRIGGER IF EXISTS company_vacancies_max_10").run();
}

export async function ensureUserColumns(db) {
  for (const tableName of ["company_status_history", "company_tasks", "company_applications"]) {
    const result = await db.prepare(`PRAGMA table_info(${tableName})`).all();
    const columns = new Set((result?.results || []).map((item) => String(item.name)));
    if (!columns.has("user_name")) {
      await db.prepare(`ALTER TABLE ${tableName} ADD COLUMN user_name TEXT NOT NULL DEFAULT 'Eugene'`).run();
    }
  }
}

export async function migrateLegacyUserData(db) {
  await db.batch([
    db.prepare(`INSERT OR IGNORE INTO company_user_state (user_name, company_name, status, comment_md, favorite, updated_at)
                SELECT 'Eugene', company_name, status, comment_md, favorite, updated_at FROM company_crm`),
    db.prepare(`INSERT OR IGNORE INTO company_vacancy_user_state (user_name, vacancy_id, relevance, updated_at)
                SELECT 'Eugene', id, relevance, CURRENT_TIMESTAMP FROM company_vacancies WHERE relevance IS NOT NULL`),
    db.prepare(`INSERT OR IGNORE INTO google_calendar_user_connection
                  (user_name, account_email, refresh_token_encrypted, calendar_id, connected_at, updated_at)
                SELECT 'Eugene', account_email, refresh_token_encrypted, calendar_id, connected_at, updated_at
                FROM google_calendar_connection WHERE id = 1`)
  ]);
}

export async function ensureTaskVacancyColumn(db) {
  const result = await db.prepare("PRAGMA table_info(company_tasks)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  if (!columns.has("vacancy_id")) {
    await db.prepare(
      "ALTER TABLE company_tasks ADD COLUMN vacancy_id INTEGER REFERENCES company_vacancies(id) ON DELETE SET NULL"
    ).run();
  }
  await db.prepare(TASK_VACANCY_INDEX_SQL).run();
}

export async function ensureTaskCalendarColumns(db) {
  const result = await db.prepare("PRAGMA table_info(company_tasks)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  const additions = [
    ["google_event_id", "ALTER TABLE company_tasks ADD COLUMN google_event_id TEXT"],
    ["google_event_html_link", "ALTER TABLE company_tasks ADD COLUMN google_event_html_link TEXT"],
    ["google_sync_status", "ALTER TABLE company_tasks ADD COLUMN google_sync_status TEXT NOT NULL DEFAULT ''"],
    ["google_sync_error", "ALTER TABLE company_tasks ADD COLUMN google_sync_error TEXT NOT NULL DEFAULT ''"]
  ].filter(([name]) => !columns.has(name)).map(([, statement]) => db.prepare(statement));
  if (additions.length) await db.batch(additions);
}

export async function ensureVacancyColumns(db) {
  const result = await db.prepare("PRAGMA table_info(company_vacancies)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  if (!columns.has("relevance")) {
    await db.prepare(
      "ALTER TABLE company_vacancies ADD COLUMN relevance INTEGER CHECK (relevance IS NULL OR (relevance >= 0 AND relevance <= 100))"
    ).run();
  }
  const additions = [
    ["entity_type", "ALTER TABLE company_vacancies ADD COLUMN entity_type TEXT NOT NULL DEFAULT 'company'"],
    ["real_company_name", "ALTER TABLE company_vacancies ADD COLUMN real_company_name TEXT"],
    ["cover_letter_subject", "ALTER TABLE company_vacancies ADD COLUMN cover_letter_subject TEXT NOT NULL DEFAULT ''"],
    ["cover_letter_md", "ALTER TABLE company_vacancies ADD COLUMN cover_letter_md TEXT NOT NULL DEFAULT ''"],
    ["last_checked_at", "ALTER TABLE company_vacancies ADD COLUMN last_checked_at TEXT"],
    ["archived", "ALTER TABLE company_vacancies ADD COLUMN archived INTEGER NOT NULL DEFAULT 0"]
  ].filter(([name]) => !columns.has(name)).map(([, statement]) => db.prepare(statement));
  if (additions.length) await db.batch(additions);
  await db.prepare(VACANCY_ENTITY_INDEX_SQL).run();
}

export async function ensureVacancyUserStateColumns(db) {
  const result = await db.prepare("PRAGMA table_info(company_vacancy_user_state)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  const additions = [
    ["favorite", "ALTER TABLE company_vacancy_user_state ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0"],
    ["hidden", "ALTER TABLE company_vacancy_user_state ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0"]
  ].filter(([name]) => !columns.has(name)).map(([, statement]) => db.prepare(statement));
  if (additions.length) await db.batch(additions);
  await db.batch([
    db.prepare(VACANCY_USER_STATE_FAVORITE_INDEX_SQL),
    db.prepare(VACANCY_USER_STATE_HIDDEN_INDEX_SQL)
  ]);
}

export async function ensureApplicationContactColumns(db) {
  const result = await db.prepare("PRAGMA table_info(company_applications)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  const additions = [
    ["recruiter_name", "ALTER TABLE company_applications ADD COLUMN recruiter_name TEXT NOT NULL DEFAULT ''"],
    ["recruiter_phone", "ALTER TABLE company_applications ADD COLUMN recruiter_phone TEXT NOT NULL DEFAULT ''"],
    ["recruiter_email", "ALTER TABLE company_applications ADD COLUMN recruiter_email TEXT NOT NULL DEFAULT ''"],
    ["recruiter_contact_type", "ALTER TABLE company_applications ADD COLUMN recruiter_contact_type TEXT NOT NULL DEFAULT ''"]
  ].filter(([name]) => !columns.has(name)).map(([, statement]) => db.prepare(statement));
  if (additions.length) await db.batch(additions);
}

export async function ensureApplicationSourceColumn(db) {
  const result = await db.prepare("PRAGMA table_info(company_applications)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  if (!columns.has("application_sources")) {
    await db.prepare("ALTER TABLE company_applications ADD COLUMN application_sources TEXT NOT NULL DEFAULT '[\"hh\"]'").run();
  }
}

export async function ensureHiringContactColumns(db) {
  const result = await db.prepare("PRAGMA table_info(hiring_contacts)").all();
  const columns = new Set((result?.results || []).map((item) => String(item.name)));
  const additions = [
    ["email", "ALTER TABLE hiring_contacts ADD COLUMN email TEXT NOT NULL DEFAULT ''"],
    ["specialization", "ALTER TABLE hiring_contacts ADD COLUMN specialization TEXT NOT NULL DEFAULT ''"]
  ].filter(([name]) => !columns.has(name)).map(([, statement]) => db.prepare(statement));
  if (additions.length) await db.batch(additions);
}

export async function migrateLegacyApplications(db) {
  const legacyResult = await db.prepare(
    `SELECT crm.company_name AS companyName,
            crm.status,
            crm.comment_md AS comment
     FROM company_crm AS crm
     WHERE (crm.status <> '' OR crm.comment_md <> '')
       AND NOT EXISTS (
         SELECT 1 FROM company_applications AS application
         WHERE application.company_name = crm.company_name
       )`
  ).all();
  for (const legacy of legacyResult?.results || []) {
    const vacancyResult = await db.prepare(
      `SELECT id
       FROM company_vacancies
       WHERE company_name = ? AND application_status <> ''
       ORDER BY updated_at DESC, id DESC
       LIMIT ?`
    ).bind(legacy.companyName, MAX_APPLICATIONS_PER_COMPANY).all();
    const vacancyIds = (vacancyResult?.results || []).map((item) => Number(item.id));
    const historyResult = await db.prepare(
      `SELECT from_status AS fromStatus,
              to_status AS toStatus,
              changed_at AS changedAt
       FROM company_status_history
       WHERE company_name = ?
       ORDER BY id ASC`
    ).bind(legacy.companyName).all();
    const legacyHistory = historyResult?.results || [];
    const targets = vacancyIds.length ? vacancyIds : [null];
    for (let index = 0; index < targets.length; index += 1) {
      const inserted = await db.prepare(
        `INSERT INTO company_applications
           (company_name, vacancy_id, status, comment_md, created_at, updated_at)
         VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
      ).bind(
        legacy.companyName,
        targets[index],
        legacy.status || "Откликнулся",
        index === 0 ? legacy.comment || "" : ""
      ).run();
      const applicationId = Number(inserted?.meta?.last_row_id || 0);
      if (applicationId && legacyHistory.length) {
        await db.batch(legacyHistory.map((item) => db.prepare(
          `INSERT INTO company_application_status_history
             (application_id, from_status, to_status, changed_at)
           VALUES (?, ?, ?, ?)`
        ).bind(applicationId, item.fromStatus || "", item.toStatus || "", item.changedAt)));
      } else if (applicationId && legacy.status) {
        await db.prepare(
          `INSERT INTO company_application_status_history
             (application_id, from_status, to_status, changed_at)
           VALUES (?, '', ?, CURRENT_TIMESTAMP)`
        ).bind(applicationId, legacy.status).run();
      }
    }
  }
}

export async function synchronizeVacancyApplicationStatuses(db) {
  await db.batch([
    db.prepare(
      `UPDATE company_vacancies
       SET application_status = '',
           updated_at = CURRENT_TIMESTAMP
       WHERE application_status <> ''
         AND COALESCE(
           (SELECT status
            FROM company_crm
            WHERE company_name = company_vacancies.company_name),
           ''
         ) = ''`
    ),
    db.prepare(
      `UPDATE company_vacancies
       SET application_status = (
             SELECT status
             FROM company_crm
             WHERE company_name = company_vacancies.company_name
           ),
           updated_at = CURRENT_TIMESTAMP
       WHERE application_status <> ''
         AND COALESCE(
           (SELECT status
            FROM company_crm
            WHERE company_name = company_vacancies.company_name),
           ''
         ) <> ''
         AND application_status <> (
           SELECT status
           FROM company_crm
           WHERE company_name = company_vacancies.company_name
         )`
    )
  ]);
}
