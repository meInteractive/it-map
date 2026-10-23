import {vacancyStateSql, vacancyStateBindings, vacancyRevision, readVacancyState, vacancyConflict} from '../services/vacancy-revision.js';
import {batchWithRequiredUpdate} from './conditional-batch.js';
// Server-only; extracted from the v218 Worker.
import {TELEGRAM_ONLY_OWNER_PREFIX,VACANCY_LEVELS,MAX_VACANCY_DESCRIPTION_LENGTH,MAX_COVER_LETTER_SUBJECT_LENGTH,MAX_COVER_LETTER_LENGTH,EUGENE_RELEVANCE_PROFILE_VERSION,MAX_VACANCY_IMPORT_ROWS} from '../constants.js';
import {telegramMessageUrlInput} from './channels.js';
import {ensureCrmSchema} from './schema.js';
import {readAppCatalog,recalculateAgencySalaryRanges,ensureCompanyIdentities,ensureAgencyIdentities} from './catalog.js';
import {createCompanyApplication} from './applications.js';
import {agencySalaryRangeStatements} from './agency-salaries.js';
import {attachVacancyRoles,readVacancyRoleCatalog,vacancyRoleAssignmentStatements,vacancyRoleSelection} from './vacancy-roles.js';

export function vacancyRecord(row) {
  let telegramSources = [];
  try {
    const parsed = JSON.parse(row.telegramSourcesJson || "[]");
    telegramSources = Array.isArray(parsed) ? parsed.map((source) => ({
      channelId: Number(source.channelId),
      channelName: String(source.channelName || ""),
      channelUrl: String(source.channelUrl || ""),
      channelUsername: String(source.channelUsername || ""),
      messageUrl: String(source.messageUrl || ""),
      telegramContact: String(source.telegramContact || "")
    })).filter((source) => source.channelId && source.messageUrl) : [];
  } catch {}
  return {
    id: Number(row.id),
    companyName: row.companyName,
    entityType: ["agency", "channel"].includes(row.entityType) ? row.entityType : "company",
    realCompanyName: row.realCompanyName || "",
    url: row.url,
    salaryFrom: row.salaryFrom == null ? null : Number(row.salaryFrom),
    salaryTo: row.salaryTo == null ? null : Number(row.salaryTo),
    title: row.title,
    role: row.role || "",
    level: row.level || "",
    relevance: row.relevance == null ? null : Number(row.relevance),
    favorite: Boolean(row.favorite),
    hidden: Boolean(row.hidden),
    publishedAt: row.publishedAt || null,
    loadedAt: row.loadedAt || null,
    lastCheckedAt: row.lastCheckedAt || null,
    archived: Boolean(row.archived),
    descriptionMd: row.descriptionMd || "",
    coverLetterSubject: row.coverLetterSubject || "",
    coverLetterMd: row.coverLetterMd || "",
    coverLetterScope: row.coverLetterScope === "personal" ? "personal" : "shared",
    telegramSources,
    applicationId: row.applicationId == null ? null : Number(row.applicationId),
    applicationStatus: row.applicationStatus || "",
    createdAt: row.createdAt,
    updatedAt: row.updatedAt
  };
}

export function validIsoDate(value) {
  if (value == null || value === "") return true;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const [year, month, day] = value.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day));
  return date.getUTCFullYear() === year && date.getUTCMonth() === month - 1 && date.getUTCDate() === day;
}

export function vacancyInput(payload, options = {}) {
  const allowEmptyFields = options.allowEmptyFields === true;
  const requestedEntityType = String(payload?.entityType || "company").trim().toLowerCase();
  const entityType = requestedEntityType === "agency" ? "agency" : requestedEntityType === "channel" ? "channel" : "company";
  const telegramChannelId = payload?.telegramChannelId == null || payload.telegramChannelId === "" ? null : Number(payload.telegramChannelId);
  let companyName = String(payload?.companyName || "").trim();
  if (entityType === "channel" && !companyName && Number.isSafeInteger(telegramChannelId) && telegramChannelId > 0) companyName = `${TELEGRAM_ONLY_OWNER_PREFIX}${telegramChannelId}`;
  const realCompanyName = String(payload?.realCompanyName || "").trim();
  const url = String(payload?.url || "").trim();
  const title = String(payload?.title || "").trim();
  const role = String(payload?.role || "").trim();
  const roleIds = Array.isArray(payload?.roleIds) ? payload.roleIds : undefined;
  const levelValue = String(payload?.level || "").trim();
  const level = VACANCY_LEVELS.find((item) => item.toLowerCase() === levelValue.toLowerCase()) || "";
  const relevance = payload?.relevance == null || payload.relevance === "" ? null : Number(payload.relevance);
  const publishedAt = payload?.publishedAt == null ? "" : String(payload.publishedAt).trim();
  const loadedAt = payload?.loadedAt == null ? "" : String(payload.loadedAt).trim();
  const descriptionMd = String(payload?.descriptionMd || "");
  const coverLetterSubject = String(payload?.coverLetterSubject || "");
  const coverLetterMd = String(payload?.coverLetterMd || "");
  const telegramMessageUrlValue = String(payload?.telegramMessageUrl || "").trim();
  const telegramContact = String(payload?.telegramContact || "").trim();
  const salaryFrom = payload?.salaryFrom == null || payload.salaryFrom === "" ? null : Number(payload.salaryFrom);
  const salaryTo = payload?.salaryTo == null || payload.salaryTo === "" ? null : Number(payload.salaryTo);
  let parsedUrl;
  try {
    parsedUrl = new URL(url);
  } catch {
    throw new Error("Укажите корректную ссылку на вакансию.");
  }
  if (!["http:", "https:"].includes(parsedUrl.protocol)) throw new Error("Ссылка должна начинаться с http:// или https://.");
  if (!title && !allowEmptyFields) throw new Error("Укажите заголовок вакансии.");
  if (entityType === "channel" && (!Number.isSafeInteger(telegramChannelId) || telegramChannelId <= 0)) throw new Error("Укажите Telegram-канал для вакансии.");
  if (telegramChannelId != null && (!Number.isSafeInteger(telegramChannelId) || telegramChannelId <= 0)) throw new Error("Telegram-канал указан некорректно.");
  if (telegramChannelId != null && !telegramMessageUrlValue) throw new Error("Укажите прямую ссылку на Telegram-сообщение.");
  if (telegramContact.length > 256) throw new Error("Telegram-контакт не должен превышать 256 символов.");
  if (title.length > 300) throw new Error("Заголовок не должен превышать 300 символов.");
  if (realCompanyName.length > 300) throw new Error("Название работодателя не должно превышать 300 символов.");
  if (levelValue && !level) throw new Error("Уровень вакансии указан некорректно: используйте Senior, Middle, Lead или Head.");
  if (!level && !allowEmptyFields) throw new Error("Укажите уровень вакансии: Senior, Middle, Lead или Head.");
  if (relevance != null && (!Number.isSafeInteger(relevance) || relevance < 0 || relevance > 100)) {
    throw new Error("Релевантность должна быть целым числом от 0 до 100 без знака %.");
  }
  if (url.length > 2048) throw new Error("Ссылка на вакансию слишком длинная.");
  if (publishedAt && !validIsoDate(publishedAt)) throw new Error("Дата публикации должна быть в формате YYYY-MM-DD.");
  if (loadedAt && !validIsoDate(loadedAt)) throw new Error("Дата загрузки должна быть в формате YYYY-MM-DD.");
  for (const value of [salaryFrom, salaryTo]) {
    if (value != null && (!Number.isSafeInteger(value) || value < 0)) throw new Error("Зарплата должна быть указана целым неотрицательным числом.");
  }
  if (salaryFrom != null && salaryTo != null && salaryFrom > salaryTo) throw new Error("Нижняя граница зарплаты не может быть выше верхней.");
  if (!descriptionMd.trim() && !allowEmptyFields) throw new Error("Добавьте полный текст вакансии в Markdown.");
  if (descriptionMd.length > MAX_VACANCY_DESCRIPTION_LENGTH) throw new Error("Полный текст вакансии слишком длинный.");
  if (coverLetterSubject.length > MAX_COVER_LETTER_SUBJECT_LENGTH) throw new Error("Заголовок сопроводительного письма не должен превышать 512 символов.");
  if (coverLetterMd.length > MAX_COVER_LETTER_LENGTH) throw new Error("Текст сопроводительного письма не должен превышать 2000 символов.");
  return {
    companyName,
    entityType,
    realCompanyName: entityType === "company" ? "" : realCompanyName,
    url: parsedUrl.href,
    salaryFrom,
    salaryTo,
    title,
    role,
    roleIds,
    level,
    relevance,
    publishedAt: publishedAt || null,
    loadedAt: loadedAt || null,
    descriptionMd,
    coverLetterSubject,
    coverLetterMd,
    telegramChannelId,
    telegramMessageUrl: telegramMessageUrlValue,
    telegramContact
  };
}

export function scoreEugeneResumeFit(vacancy) {
  const title = `${vacancy?.title || ""} ${vacancy?.role || ""}`.toLowerCase().replaceAll("ё", "е");
  const description = String(vacancy?.descriptionMd || "").toLowerCase().replaceAll("ё", "е");
  const text = `${title}\n${description}`;
  const has = (...phrases) => phrases.some((phrase) => text.includes(phrase));
  const hasTitle = (...phrases) => phrases.some((phrase) => title.includes(phrase));
  const hasPattern = (pattern) => pattern.test(text);
  let score = 14;

  if (hasTitle("директор по разработке", "технический директор", "cto", "head of engineering", "head of development")) {
    score += 46;
  } else if (hasTitle("руководитель разработки", "engineering manager", "development manager", "тимлид", "team lead", "руководитель команды разработки")) {
    score += 42;
  } else if (hasTitle("delivery", "program manager", "руководитель проектов", "менеджер проектов", "technical project manager", "технический менеджер проекта")) {
    score += 36;
  } else if (hasTitle("product manager", "product owner", "менеджер продукта", "продакт")) {
    score += 26;
  } else if (hasTitle("разработчик", "developer", "инженер", "engineer")) {
    score += 8;
  }

  if (has("руководить команд", "управлять команд", "развивать инженеров", "линейное управление", "people management", "нанимать", "ментор", "подбор команды")) score += 10;
  if (has("roadmap", "дорожн", "бэклог", "backlog", "требован", "user stor", "планирован", "приоритизац")) score += 7;
  if (has("scrum", "agile", "канбан", "kanban", "спринт")) score += 5;
  if (has("бюджет", "p&l", "юнит-эконом", "финансов", "смет")) score += 6;
  if (has("стейкхолдер", "stakeholder", "заказчик", "клиент", "партнер", "партнёр")) score += 5;
  if (has("архитектур", "api", "высоконагруж", "highload", "ci/cd", "инфраструктур", "надежност", "надёжност", "производительност")) score += 7;
  if (has("искусственн", "нейросет", "machine learning", "ml", "ai ", " ai", "генеративн")) score += 9;
  if (has("мобильн", "mobile", "marketplace", "маркетплейс", "e-commerce", "ecommerce", "b2c")) score += 5;
  if (has("a/b", "эксперимент", "product analytics", "продуктов", "growth", "retention", "cjm", "jtbd")) score += 3;
  if (hasTitle("senior", "lead", "head", "ведущий", "старший")) score += 5;

  const managementRole = hasTitle("директор", "руководител", "manager", "lead", "head", "delivery", "program", "project");
  const explicitHandsOnCoding = hasPattern(/(?:обязател(?:ьно|ен)|не менее|минимум|от)\s+.{0,80}(?:лет|года).{0,100}(?:java|python|c\+\+|typescript|react|backend|frontend|разработк)/i)
    || hasPattern(/(?:писать|разрабатывать)\s+(?:production\s+)?код|hands[- ]on\s+(?:software\s+)?development/i);
  const individualDeveloperRole = hasTitle("разработчик", "developer", "software engineer", "backend engineer", "frontend engineer") && !managementRole;
  if (explicitHandsOnCoding) score -= managementRole ? 14 : 28;
  if (individualDeveloperRole) score -= 28;
  if (hasTitle("рекрутер", "hr", "продажи", "sales", "account manager", "маркетолог", "support") && !managementRole) score -= 45;
  if (has("регулярные переработки", "работа по выходным", "работа в выходные", "ночные смены", "дежурства 24/7")) score -= 22;

  return Math.max(5, Math.min(98, Math.round(score)));
}

export async function prepareVacancyTelegramSource(db, vacancyId, input) {
  if (input.telegramChannelId == null) return null;
  const channel = await db.prepare(
    "SELECT id, normalized_username AS normalizedUsername FROM telegram_channels WHERE id = ?"
  ).bind(input.telegramChannelId).first();
  if (!channel) throw new Error("Telegram-канал не найден.");
  const messageUrl = telegramMessageUrlInput(input.telegramMessageUrl, channel.normalizedUsername);
  const duplicate = await db.prepare(
    "SELECT vacancy_id AS vacancyId FROM vacancy_telegram_sources WHERE message_url = ? AND vacancy_id <> ?"
  ).bind(messageUrl, vacancyId ?? 0).first();
  if (duplicate) throw new Error("Это Telegram-сообщение уже связано с другой вакансией.");
  // A new row is resolved by its globally unique URL inside the same batch.
  const idExpression = vacancyId == null ? "(SELECT id FROM company_vacancies WHERE url = ?)" : "?";
  return db.prepare(
    `INSERT INTO vacancy_telegram_sources
       (vacancy_id, channel_id, message_url, telegram_contact, created_at, updated_at)
     VALUES (${idExpression}, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
     ON CONFLICT(vacancy_id, channel_id) DO UPDATE SET
       message_url = excluded.message_url,
       telegram_contact = excluded.telegram_contact,
       updated_at = CURRENT_TIMESTAMP`
  ).bind(vacancyId ?? input.url, input.telegramChannelId, messageUrl, input.telegramContact || "");
}

export async function saveVacancyTelegramSource(db, vacancyId, input) {
  const statement = await prepareVacancyTelegramSource(db, vacancyId, input);
  if (statement) await statement.run();
}

export async function applyEugeneResumeRelevanceScores(db) {
  const current = await db.prepare(
    "SELECT profile_version AS profileVersion FROM vacancy_relevance_profile_state WHERE user_name = ?"
  ).bind("Eugene").first();
  if (current?.profileVersion === EUGENE_RELEVANCE_PROFILE_VERSION) return;

  const result = await db.prepare(
    `SELECT id, title, role, level, description_md AS descriptionMd
     FROM company_vacancies
     ORDER BY id ASC`
  ).all();
  const vacancies = result?.results || [];
  for (let index = 0; index < vacancies.length; index += 40) {
    await db.batch(vacancies.slice(index, index + 40).map((vacancy) => db.prepare(
      `INSERT INTO company_vacancy_user_state (user_name, vacancy_id, relevance, updated_at)
       VALUES (?, ?, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(user_name, vacancy_id) DO UPDATE SET relevance = excluded.relevance, updated_at = CURRENT_TIMESTAMP WHERE company_vacancy_user_state.relevance IS NULL`
    ).bind("Eugene", Number(vacancy.id), scoreEugeneResumeFit(vacancy))));
  }
  await db.prepare(
    `INSERT INTO vacancy_relevance_profile_state (user_name, profile_version, applied_at)
     VALUES (?, ?, CURRENT_TIMESTAMP)
     ON CONFLICT(user_name) DO UPDATE SET profile_version = excluded.profile_version, applied_at = CURRENT_TIMESTAMP`
  ).bind("Eugene", EUGENE_RELEVANCE_PROFILE_VERSION).run();
}

export async function initializeCompanyVacancies(db, companyName) {
  await ensureCrmSchema(db);
  const initialized = await db.prepare(
    "SELECT company_name FROM company_vacancy_state WHERE company_name = ?"
  ).bind(companyName).first();
  if (initialized) return;
  const catalog = await readAppCatalog(db);
  const company = catalog.companies.find((item) => item.name === companyName);
  const legacyJobs = Array.isArray(company?.jobs) ? company.jobs : [];
  const statements = [
    db.prepare(
      `INSERT INTO company_crm (company_name)
       VALUES (?)
       ON CONFLICT(company_name) DO NOTHING`
    ).bind(companyName)
  ];
  for (const job of legacyJobs) {
    const title = String(job?.title || "").trim();
    const url = String(job?.url || "").trim();
    if (!title || !url) continue;
    let normalizedUrl;
    try {
      const parsed = new URL(url);
      if (!["http:", "https:"].includes(parsed.protocol)) continue;
      normalizedUrl = parsed.href;
    } catch {
      continue;
    }
    const salaryFrom = Number.isSafeInteger(Number(job?.salary?.[0])) && Number(job.salary[0]) > 0 ? Number(job.salary[0]) : null;
    const salaryTo = Number.isSafeInteger(Number(job?.salary?.[1])) && Number(job.salary[1]) > 0 ? Number(job.salary[1]) : null;
    statements.push(
      db.prepare(
        `INSERT OR IGNORE INTO company_vacancies
           (company_name, url, salary_from, salary_to, title, published_at, description_md)
         VALUES (?, ?, ?, ?, ?, NULL, '')`
      ).bind(companyName, normalizedUrl, salaryFrom, salaryTo, title)
    );
  }
  statements.push(
    db.prepare(
      `INSERT INTO company_vacancy_state (company_name, initialized_at)
       VALUES (?, CURRENT_TIMESTAMP)
       ON CONFLICT(company_name) DO NOTHING`
    ).bind(companyName)
  );
  await db.batch(statements);
}

export async function readVacancies(db, userName, companyName) {
  await initializeCompanyVacancies(db, companyName);
  const result = await db.prepare(
    `SELECT vacancy.id,
            vacancy.company_name AS companyName,
            vacancy.entity_type AS entityType,
            vacancy.real_company_name AS realCompanyName,
            vacancy.url,
            vacancy.salary_from AS salaryFrom,
            vacancy.salary_to AS salaryTo,
            vacancy.title,
            vacancy.role,
            vacancy.level,
            user_state.relevance,
            COALESCE(user_state.favorite, 0) AS favorite,
            COALESCE(user_state.hidden, 0) AS hidden,
            vacancy.published_at AS publishedAt,
            vacancy.loaded_at AS loadedAt,
            vacancy.last_checked_at AS lastCheckedAt,
            vacancy.archived,
            vacancy.description_md AS descriptionMd,
            vacancy.cover_letter_subject AS coverLetterSubject,
            vacancy.cover_letter_md AS coverLetterMd,
            COALESCE((SELECT json_group_array(json_object(
              'channelId', source.channel_id,
              'channelName', channel.name,
              'channelUrl', channel.url,
              'channelUsername', channel.username,
              'messageUrl', source.message_url,
              'telegramContact', source.telegram_contact
            )) FROM vacancy_telegram_sources AS source
               JOIN telegram_channels AS channel ON channel.id = source.channel_id
              WHERE source.vacancy_id = vacancy.id), '[]') AS telegramSourcesJson,
            (SELECT application.id
             FROM company_applications AS application
             WHERE application.user_name = ? AND application.vacancy_id = vacancy.id
             ORDER BY application.updated_at DESC, application.id DESC
             LIMIT 1) AS applicationId,
            COALESCE((
              SELECT application.status
              FROM company_applications AS application
              WHERE application.user_name = ? AND application.vacancy_id = vacancy.id
              ORDER BY application.updated_at DESC, application.id DESC
              LIMIT 1
            ), '') AS applicationStatus,
            vacancy.created_at AS createdAt,
            vacancy.updated_at AS updatedAt
     FROM company_vacancies AS vacancy
     LEFT JOIN company_vacancy_user_state AS user_state ON user_state.vacancy_id = vacancy.id AND user_state.user_name = ?
     WHERE vacancy.company_name = ? OR vacancy.real_company_name = ?
     ORDER BY vacancy.published_at IS NULL ASC, vacancy.published_at DESC, vacancy.id DESC
     LIMIT 100`
  ).bind(userName, userName, userName, companyName, companyName).all();
  return attachVacancyRoles(db, (result?.results || []).map(vacancyRecord));
}

export async function initializeAllCatalogVacancies(db) {
  await ensureCrmSchema(db);
  const catalog = await readAppCatalog(db);
  const stateResult = await db.prepare("SELECT company_name AS companyName FROM company_vacancy_state").all();
  const initialized = new Set((stateResult?.results || []).map((item) => item.companyName));
  const statements = [];
  for (const company of catalog.companies) {
    if (initialized.has(company.name) || !Array.isArray(company.jobs) || !company.jobs.length) continue;
    statements.push(
      db.prepare(
        `INSERT INTO company_crm (company_name)
         VALUES (?)
         ON CONFLICT(company_name) DO NOTHING`
      ).bind(company.name)
    );
    for (const job of company.jobs) {
      const title = String(job?.title || "").trim();
      const rawUrl = String(job?.url || "").trim();
      if (!title || !rawUrl) continue;
      let normalizedUrl;
      try {
        const parsed = new URL(rawUrl);
        if (!["http:", "https:"].includes(parsed.protocol)) continue;
        normalizedUrl = parsed.href;
      } catch {
        continue;
      }
      const salaryFrom = Number.isSafeInteger(Number(job?.salary?.[0])) && Number(job.salary[0]) > 0 ? Number(job.salary[0]) : null;
      const salaryTo = Number.isSafeInteger(Number(job?.salary?.[1])) && Number(job.salary[1]) > 0 ? Number(job.salary[1]) : null;
      statements.push(
        db.prepare(
          `INSERT OR IGNORE INTO company_vacancies
             (company_name, url, salary_from, salary_to, title, published_at, description_md)
           VALUES (?, ?, ?, ?, ?, NULL, '')`
        ).bind(company.name, normalizedUrl, salaryFrom, salaryTo, title)
      );
    }
    statements.push(
      db.prepare(
        `INSERT INTO company_vacancy_state (company_name, initialized_at)
         VALUES (?, CURRENT_TIMESTAMP)
         ON CONFLICT(company_name) DO NOTHING`
      ).bind(company.name)
    );
  }
  for (let index = 0; index < statements.length; index += 60) {
    await db.batch(statements.slice(index, index + 60));
  }
}

export async function readAllVacancies(db, userName) {
  await initializeAllCatalogVacancies(db);
  if (userName === "Eugene") await applyEugeneResumeRelevanceScores(db);
  const result = await db.prepare(
    `SELECT vacancy.id,
            vacancy.company_name AS companyName,
            vacancy.entity_type AS entityType,
            vacancy.real_company_name AS realCompanyName,
            vacancy.url,
            vacancy.salary_from AS salaryFrom,
            vacancy.salary_to AS salaryTo,
            vacancy.title,
            vacancy.role,
            vacancy.level,
            user_state.relevance,
            COALESCE(user_state.favorite, 0) AS favorite,
            COALESCE(user_state.hidden, 0) AS hidden,
            vacancy.published_at AS publishedAt,
            vacancy.loaded_at AS loadedAt,
            vacancy.last_checked_at AS lastCheckedAt,
            vacancy.archived,
            vacancy.description_md AS descriptionMd,
            vacancy.cover_letter_subject AS coverLetterSubject,
            vacancy.cover_letter_md AS coverLetterMd,
            COALESCE((SELECT json_group_array(json_object(
              'channelId', source.channel_id,
              'channelName', channel.name,
              'channelUrl', channel.url,
              'channelUsername', channel.username,
              'messageUrl', source.message_url,
              'telegramContact', source.telegram_contact
            )) FROM vacancy_telegram_sources AS source
               JOIN telegram_channels AS channel ON channel.id = source.channel_id
              WHERE source.vacancy_id = vacancy.id), '[]') AS telegramSourcesJson,
            (SELECT application.id
             FROM company_applications AS application
             WHERE application.user_name = ? AND application.vacancy_id = vacancy.id
             ORDER BY application.updated_at DESC, application.id DESC
             LIMIT 1) AS applicationId,
            COALESCE((
              SELECT application.status
              FROM company_applications AS application
              WHERE application.user_name = ? AND application.vacancy_id = vacancy.id
              ORDER BY application.updated_at DESC, application.id DESC
              LIMIT 1
            ), '') AS applicationStatus,
            vacancy.created_at AS createdAt,
            vacancy.updated_at AS updatedAt
     FROM company_vacancies AS vacancy
     LEFT JOIN company_vacancy_user_state AS user_state ON user_state.vacancy_id = vacancy.id AND user_state.user_name = ?
     ORDER BY vacancy.company_name COLLATE NOCASE ASC,
              vacancy.published_at IS NULL ASC,
              vacancy.published_at DESC,
              vacancy.id DESC`
  ).bind(userName, userName, userName).all();
  return attachVacancyRoles(db, (result?.results || []).map(vacancyRecord));
}

export async function readVacancyCompanyNames(db) {
  await initializeAllCatalogVacancies(db);
  const result = await db.prepare(
    `SELECT company_name AS companyName FROM company_vacancies
     UNION
     SELECT real_company_name AS companyName FROM company_vacancies WHERE real_company_name IS NOT NULL AND real_company_name <> ''`
  ).all();
  return (result?.results || []).map((row) => String(row.companyName || "").trim()).filter(Boolean);
}

export async function createVacancy(db, userName, input) {
  await initializeCompanyVacancies(db, input.companyName);
  input = {...input, ...vacancyRoleSelection(await readVacancyRoleCatalog(db), input)};
  const duplicate = await db.prepare(
    "SELECT id FROM company_vacancies WHERE url = ?"
  ).bind(input.url).first();
  if (duplicate) throw new Error("Вакансия с такой ссылкой уже сохранена.");
  const sourceStatement = await prepareVacancyTelegramSource(db, null, input);
  const statements = [db.prepare(
    `INSERT INTO company_vacancies
       (company_name, entity_type, real_company_name, url, salary_from, salary_to, title, role, level, published_at, loaded_at, description_md, cover_letter_subject, cover_letter_md, created_at, updated_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
  ).bind(input.companyName, input.entityType, input.realCompanyName || null, input.url, input.salaryFrom, input.salaryTo, input.title, input.role, input.level, input.publishedAt, input.loadedAt, input.descriptionMd, input.coverLetterSubject, input.coverLetterMd)];
  statements.push(...vacancyRoleAssignmentStatements(db, '(SELECT id FROM company_vacancies WHERE url = ?)', input.url, input.roleIds));
  if (sourceStatement) statements.push(sourceStatement);
  if (input.relevance != null) {
    statements.push(db.prepare(`INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at)
      SELECT ?,id,?,CURRENT_TIMESTAMP FROM company_vacancies WHERE url = ?
      ON CONFLICT(user_name,vacancy_id) DO UPDATE SET relevance = excluded.relevance, updated_at = CURRENT_TIMESTAMP`)
      .bind(userName, input.relevance, input.url));
  }
  if (input.entityType === "agency") {
    const catalog = await readAppCatalog(db);
    statements.push(...agencySalaryRangeStatements(db, catalog));
  }
  await db.batch(statements);
  return { ok: true, vacancies: await readVacancies(db, userName, input.companyName) };
}

export async function importVacancies(db, userName, rows, mode = "create") {
  await initializeAllCatalogVacancies(db);
  if (!Array.isArray(rows) || !rows.length) throw new Error("В Excel-файле нет вакансий для импорта.");
  if (rows.length > MAX_VACANCY_IMPORT_ROWS) throw new Error(`За один раз можно импортировать не более ${MAX_VACANCY_IMPORT_ROWS} вакансий.`);
  if (!["create", "update"].includes(mode)) throw new Error("Указан неизвестный режим импорта.");

  const catalog = await readAppCatalog(db);
  const roleCatalog = await readVacancyRoleCatalog(db);
  const [identities, agencyIdentities] = await Promise.all([ensureCompanyIdentities(db, catalog.companies), ensureAgencyIdentities(db, catalog.agencies)]);
  const companiesById = new Map([...identities.entries()].map(([companyName, id]) => [Number(id), companyName]));
  const agenciesById = new Map([...agencyIdentities.entries()].map(([agencyName, id]) => [Number(id), agencyName]));
  const agencyNames = new Set(catalog.agencies.map((agency) => agency.name));
  const existingUrlResult = await db.prepare("SELECT id, company_name AS companyName, entity_type AS entityType, real_company_name AS realCompanyName, cover_letter_subject AS coverLetterSubject, cover_letter_md AS coverLetterMd, url FROM company_vacancies").all();
  const vacanciesByUrl = new Map((existingUrlResult?.results || []).map((item) => {
    let normalizedUrl;
    try { normalizedUrl = new URL(String(item.url || "")).href; } catch { normalizedUrl = String(item.url || ""); }
    return [normalizedUrl, { id: Number(item.id), companyName: item.companyName, entityType: item.entityType || "company", realCompanyName: item.realCompanyName || "", coverLetterSubject: item.coverLetterSubject || "", coverLetterMd: item.coverLetterMd || "" }];
  }));
  const accepted = [];
  const errors = [];
  const importUrls = new Set();
  let imported = 0;
  let updated = 0;
  let duplicates = 0;
  let missingSkipped = 0;
  const limitSkipped = 0; // Compatibility field: no per-organization vacancy cap.
  let invalid = 0;

  rows.forEach((row, index) => {
    const rowNumber = index + 2;
    try {
      if (row?.coverLetterScope === "personal") throw new Error("Личные письма нельзя импортировать в общие поля вакансии.");
      const ownerValue = String(row?.entityType || "").trim().toLowerCase();
      const entityType = ownerValue === "agency" || ownerValue.includes("агент") || row?.agencyId || String(row?.agencyName || "").trim() ? "agency" : "company";
      let companyName = "", realCompanyName = "";
      if (entityType === "agency") {
        companyName = agenciesById.get(Number(row?.agencyId)) || String(row?.agencyName || "").trim();
        if (!companyName || !agencyNames.has(companyName)) throw new Error("агентство с таким ID или названием не найдено");
        realCompanyName = companiesById.get(Number(row?.realCompanyId)) || String(row?.realCompanyName || "").trim();
      } else {
        const companyId = Number(row?.companyId);
        if (!Number.isSafeInteger(companyId) || companyId <= 0) throw new Error("некорректный ID компании");
        companyName = companiesById.get(companyId);
        if (!companyName) throw new Error("компания с таким ID не найдена");
      }
      const input = vacancyInput({ ...row, companyName, entityType, realCompanyName }, { allowEmptyFields: true });
      Object.assign(input, vacancyRoleSelection(roleCatalog, input, {allowEmpty: true}));
      if (importUrls.has(input.url)) throw new Error("ссылка повторяется в импортируемом файле");
      importUrls.add(input.url);
      const existing = vacanciesByUrl.get(input.url);
      if (existing) {
        if (existing.companyName !== companyName || existing.entityType !== entityType) throw new Error("ссылка уже принадлежит другой организации");
        if (mode === "create") {
          duplicates += 1;
          return;
        }
        accepted.push({
          ...input,
          coverLetterSubject: Object.prototype.hasOwnProperty.call(row, "coverLetterSubject") ? input.coverLetterSubject : existing.coverLetterSubject,
          coverLetterMd: Object.prototype.hasOwnProperty.call(row, "coverLetterMd") ? input.coverLetterMd : existing.coverLetterMd,
          existingId: existing.id
        });
        updated += 1;
        return;
      }
      if (mode === "update") {
        missingSkipped += 1;
        return;
      }
      vacanciesByUrl.set(input.url, { id: null, companyName, entityType, realCompanyName });
      accepted.push({ ...input, existingId: null });
      imported += 1;
    } catch (error) {
      invalid += 1;
      if (errors.length < 50) errors.push({ row: rowNumber, message: error?.message || "некорректные данные" });
    }
  });

  const importedCompanies = [...new Set(accepted.map((item) => item.companyName))];
  for (let index = 0; index < importedCompanies.length; index += 50) {
    await db.batch(importedCompanies.slice(index, index + 50).map((companyName) => db.prepare(
      `INSERT INTO company_crm (company_name)
       VALUES (?)
       ON CONFLICT(company_name) DO NOTHING`
    ).bind(companyName)));
  }
  for (let index = 0; index < accepted.length; index += 40) {
    const batch = accepted.slice(index, index + 40);
    await db.batch(batch.map((input) => input.existingId
      ? db.prepare(
        `UPDATE company_vacancies
         SET entity_type = ?,
             real_company_name = ?,
             url = ?,
             salary_from = ?,
             salary_to = ?,
             title = ?,
             role = ?,
             level = ?,
             published_at = ?,
             loaded_at = ?,
             description_md = ?,
             cover_letter_subject = ?,
             cover_letter_md = ?,
             updated_at = CURRENT_TIMESTAMP
         WHERE id = ?`
      ).bind(input.entityType, input.realCompanyName || null, input.url, input.salaryFrom, input.salaryTo, input.title, input.role, input.level, input.publishedAt, input.loadedAt, input.descriptionMd, input.coverLetterSubject, input.coverLetterMd, input.existingId)
      : db.prepare(
        `INSERT INTO company_vacancies
           (company_name, entity_type, real_company_name, url, salary_from, salary_to, title, role, level, published_at, loaded_at, description_md, cover_letter_subject, cover_letter_md, created_at, updated_at)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
      ).bind(input.companyName, input.entityType, input.realCompanyName || null, input.url, input.salaryFrom, input.salaryTo, input.title, input.role, input.level, input.publishedAt, input.loadedAt, input.descriptionMd, input.coverLetterSubject, input.coverLetterMd)
    ));
    for (const input of batch) {
      const vacancyId = input.existingId || Number((await db.prepare("SELECT id FROM company_vacancies WHERE url = ?").bind(input.url).first())?.id || 0);
      if (vacancyId) {
        await db.batch(vacancyRoleAssignmentStatements(db, '?', vacancyId, input.roleIds));
        await saveVacancyRelevance(db, userName, vacancyId, input.relevance);
      }
    }
  }

  if (accepted.some((item) => item.entityType === "agency")) await recalculateAgencySalaryRanges(db);

  return {
    ok: true,
    imported,
    updated,
    duplicates,
    missingSkipped,
    invalid,
    limitSkipped,
    total: rows.length,
    errors
  };
}

export async function updateVacancy(db, userName, vacancyId, payload) {
  await ensureCrmSchema(db);
  const current = await db.prepare(
    `SELECT ${vacancyStateSql("vacancy")} AS stateSnapshot, vacancy.company_name AS companyName,
            vacancy.entity_type AS entityType,
            vacancy.real_company_name AS realCompanyName,
            vacancy.cover_letter_subject AS coverLetterSubject,
            vacancy.cover_letter_md AS coverLetterMd,
            source.channel_id AS telegramChannelId,
            source.message_url AS telegramMessageUrl,
            source.telegram_contact AS telegramContact
     FROM company_vacancies AS vacancy
     LEFT JOIN vacancy_telegram_sources AS source ON source.vacancy_id = vacancy.id
     WHERE vacancy.id = ?
     ORDER BY source.id ASC
     LIMIT 1`
  ).bind(...vacancyStateBindings(userName), vacancyId).first();
  if (!current) return null;
  if (typeof payload?.revision !== 'string' || !/^[a-f0-9]{64}$/.test(payload.revision)) {
    throw Object.assign(new Error('Загрузите актуальную карточку: для сохранения нужна версия вакансии.'), {status: 428});
  }
  if (payload.revision !== await vacancyRevision(current.stateSnapshot)) throw vacancyConflict();
  const writesLetter = Object.hasOwn(payload || {}, "coverLetterSubject") || Object.hasOwn(payload || {}, "coverLetterMd");
  const letterScope = payload?.coverLetterScope;
  if (writesLetter && (!["shared", "personal"].includes(letterScope)
      || typeof payload.coverLetterSubject !== "string" || typeof payload.coverLetterMd !== "string")) {
    throw new Error("Для сохранения письма укажите coverLetterScope (personal/shared) и оба поля письма. Обновите страницу.");
  }
  const writesSharedLetter = writesLetter && letterScope === "shared";
  const writesPersonalLetter = writesLetter && letterScope === "personal";
  if (writesPersonalLetter && Boolean(payload.coverLetterSubject.trim()) !== Boolean(payload.coverLetterMd.trim())) {
    throw new Error("Заполните и тему, и текст личного письма или очистите оба поля.");
  }
  const hasCompanyName = Object.prototype.hasOwnProperty.call(payload || {}, "companyName");
  const hasEntityType = Object.prototype.hasOwnProperty.call(payload || {}, "entityType");
  let input = vacancyInput({
    ...payload,
    companyName: hasCompanyName ? payload.companyName : current.companyName,
    entityType: hasEntityType ? payload.entityType : current.entityType,
    realCompanyName: payload?.realCompanyName ?? current.realCompanyName,
    coverLetterSubject: payload?.coverLetterSubject ?? current.coverLetterSubject,
    coverLetterMd: payload?.coverLetterMd ?? current.coverLetterMd,
    telegramChannelId: Object.prototype.hasOwnProperty.call(payload || {}, "telegramChannelId") ? payload.telegramChannelId : current.telegramChannelId,
    telegramMessageUrl: Object.prototype.hasOwnProperty.call(payload || {}, "telegramMessageUrl") ? payload.telegramMessageUrl : current.telegramMessageUrl,
    telegramContact: Object.prototype.hasOwnProperty.call(payload || {}, "telegramContact") ? payload.telegramContact : current.telegramContact
  });
  input = {...input, ...vacancyRoleSelection(await readVacancyRoleCatalog(db), input)};
  if (Object.prototype.hasOwnProperty.call(payload || {}, "telegramChannelId") && input.telegramChannelId == null && input.entityType === "channel") {
    throw new Error("Нельзя отвязать единственный источник у вакансии без работодателя.");
  }
  const catalog = await readAppCatalog(db);
  if (input.entityType === "channel") {
    const channel = await db.prepare("SELECT id FROM telegram_channels WHERE id = ?").bind(input.telegramChannelId).first();
    if (!channel) throw new Error("Telegram-канал не найден.");
  } else {
    const owners = input.entityType === "agency" ? new Set(catalog.agencies.map((agency) => agency.name)) : new Set(catalog.companies.map((company) => company.name));
    if (!owners.has(input.companyName)) throw new Error(input.entityType === "agency" ? "Агентство не найдено." : "Компания не найдена.");
    await initializeCompanyVacancies(db, input.companyName);

  }
  const sourceStatement = await prepareVacancyTelegramSource(db, vacancyId, input);
  const duplicate = await db.prepare(
    "SELECT id FROM company_vacancies WHERE url = ? AND id <> ?"
  ).bind(input.url, vacancyId).first();
  if (duplicate) throw new Error("Вакансия с такой ссылкой уже сохранена.");
  const statements = [db.prepare(
    `UPDATE company_vacancies AS v
     SET company_name = ?,
         entity_type = ?,
         url = ?,
         salary_from = ?,
         salary_to = ?,
         title = ?,
         role = ?,
         level = ?,
         published_at = ?,
         loaded_at = ?,
         description_md = ?,
         cover_letter_subject = CASE WHEN ? THEN ? ELSE cover_letter_subject END,
         cover_letter_md = CASE WHEN ? THEN ? ELSE cover_letter_md END,
         real_company_name = ?,
         updated_at = CURRENT_TIMESTAMP
     WHERE v.id = ? AND ${vacancyStateSql()} = ?`
  ).bind(input.companyName, input.entityType, input.url, input.salaryFrom, input.salaryTo, input.title, input.role, input.level, input.publishedAt, input.loadedAt, input.descriptionMd, writesSharedLetter ? 1 : 0, input.coverLetterSubject, writesSharedLetter ? 1 : 0, input.coverLetterMd, input.realCompanyName || null, vacancyId, ...vacancyStateBindings(userName), current.stateSnapshot)];
  statements.push(...vacancyRoleAssignmentStatements(db, '?', vacancyId, input.roleIds));
  if (writesPersonalLetter) {
    if (!input.coverLetterMd.trim()) {
      statements.push(db.prepare("DELETE FROM vacancy_cover_letters WHERE user_name = ? AND vacancy_id = ?").bind(userName, vacancyId));
    } else {
      statements.push(db.prepare(`INSERT INTO vacancy_cover_letters(user_name, vacancy_id, subject, markdown)
        VALUES (?, ?, ?, ?) ON CONFLICT(user_name, vacancy_id)
        DO UPDATE SET subject = excluded.subject, markdown = excluded.markdown, revision = vacancy_cover_letters.revision + 1`)
        .bind(userName, vacancyId, input.coverLetterSubject, input.coverLetterMd));
    }
  }
  if (input.companyName !== current.companyName) {
    statements.push(
      db.prepare("UPDATE company_applications SET company_name = ?, updated_at = CURRENT_TIMESTAMP WHERE vacancy_id = ?").bind(input.companyName, vacancyId),
      db.prepare("UPDATE company_tasks SET company_name = ? WHERE vacancy_id = ?").bind(input.companyName, vacancyId)
    );
  }
  if (Object.prototype.hasOwnProperty.call(payload || {}, "telegramChannelId") && input.telegramChannelId == null) {
    statements.push(db.prepare("DELETE FROM vacancy_telegram_sources WHERE vacancy_id = ?").bind(vacancyId));
  } else {
    if (sourceStatement) statements.push(sourceStatement);
  }
  statements.push(db.prepare(`INSERT INTO company_vacancy_user_state (user_name, vacancy_id, relevance, updated_at)
    VALUES (?, ?, ?, CURRENT_TIMESTAMP)
    ON CONFLICT(user_name, vacancy_id) DO UPDATE SET relevance = excluded.relevance, updated_at = CURRENT_TIMESTAMP`)
    .bind(userName, vacancyId, input.relevance));
  if (input.entityType === "agency" || current.entityType === "agency") statements.push(...agencySalaryRangeStatements(db, catalog));
  try {
    await batchWithRequiredUpdate(db, statements[0], statements.slice(1));
  } catch (error) {
    // Read AFTER rollback. A guard failure has no dependent writes. Do not call
    // every integer overflow a conflict: later SQL can throw the same error.
    const latest = await readVacancyState(db, userName, vacancyId);
    if (!latest || latest.snapshot !== current.stateSnapshot) throw vacancyConflict();
    throw error;
  }
  return { ok: true, vacancies: await readVacancies(db, userName, input.companyName) };
}

export async function saveVacancyRelevance(db, userName, vacancyId, relevance) {
  await db.prepare(
    `INSERT INTO company_vacancy_user_state (user_name, vacancy_id, relevance, updated_at)
     VALUES (?, ?, ?, CURRENT_TIMESTAMP)
     ON CONFLICT(user_name, vacancy_id) DO UPDATE SET relevance = excluded.relevance, updated_at = CURRENT_TIMESTAMP`
  ).bind(userName, vacancyId, relevance).run();
}

export async function readVacancyUserState(db, userName, vacancyId) {
  await ensureCrmSchema(db);
  const record = await db.prepare(
    `SELECT vacancy.id,
            COALESCE(user_state.favorite, 0) AS favorite,
            COALESCE(user_state.hidden, 0) AS hidden
     FROM company_vacancies AS vacancy
     LEFT JOIN company_vacancy_user_state AS user_state
       ON user_state.vacancy_id = vacancy.id AND user_state.user_name = ?
     WHERE vacancy.id = ?`
  ).bind(userName, vacancyId).first();
  return record ? { vacancyId: Number(record.id), favorite: Boolean(record.favorite), hidden: Boolean(record.hidden) } : null;
}

export async function saveVacancyUserFlag(db, userName, vacancyId, key, value) {
  await ensureCrmSchema(db);
  const exists = await db.prepare("SELECT id FROM company_vacancies WHERE id = ?").bind(vacancyId).first();
  if (!exists) return null;
  if (key === "favorite") {
    await db.prepare(
      `INSERT INTO company_vacancy_user_state (user_name, vacancy_id, favorite, updated_at)
       VALUES (?, ?, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(user_name, vacancy_id) DO UPDATE SET favorite = excluded.favorite, updated_at = CURRENT_TIMESTAMP`
    ).bind(userName, vacancyId, value ? 1 : 0).run();
  } else if (key === "hidden") {
    await db.prepare(
      `INSERT INTO company_vacancy_user_state (user_name, vacancy_id, hidden, updated_at)
       VALUES (?, ?, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(user_name, vacancy_id) DO UPDATE SET hidden = excluded.hidden, updated_at = CURRENT_TIMESTAMP`
    ).bind(userName, vacancyId, value ? 1 : 0).run();
  } else {
    throw new Error("Неизвестный признак вакансии.");
  }
  return readVacancyUserState(db, userName, vacancyId);
}

export async function deleteVacancyRecord(db, vacancyId) {
  await ensureCrmSchema(db);
  const current = await db.prepare(
    `SELECT vacancy.company_name AS companyName,
            vacancy.entity_type AS entityType,
            vacancy.archived,
            EXISTS(SELECT 1 FROM company_applications WHERE vacancy_id = vacancy.id) AS hasApplication,
            EXISTS(SELECT 1 FROM company_tasks WHERE vacancy_id = vacancy.id) AS hasTask
     FROM company_vacancies AS vacancy
     WHERE vacancy.id = ?`
  ).bind(vacancyId).first();
  if (!current) return null;
  if (!current.archived && (current.hasApplication || current.hasTask)) {
    await db.prepare(
      "UPDATE company_vacancies SET archived = 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?"
    ).bind(vacancyId).run();
    if (current.entityType === "agency") await recalculateAgencySalaryRanges(db);
    return { ok: true, archived: true, companyName: current.companyName };
  }
  const results = await db.batch([
    db.prepare("UPDATE company_applications SET vacancy_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE vacancy_id = ?").bind(vacancyId),
    db.prepare("UPDATE company_tasks SET vacancy_id = NULL WHERE vacancy_id = ?").bind(vacancyId),
    db.prepare("DELETE FROM company_vacancies WHERE id = ?").bind(vacancyId)
  ]);
  if (!results?.[2]?.meta?.changes) return null;
  if (current.entityType === "agency") await recalculateAgencySalaryRanges(db);
  return {
    ok: true,
    archived: false,
    companyName: current.companyName,
    preservedApplication: Boolean(current.hasApplication),
    preservedTask: Boolean(current.hasTask)
  };
}

export async function applyToVacancyRecord(db, userName, vacancyId) {
  await ensureCrmSchema(db);
  const vacancy = await db.prepare(
    `SELECT id, company_name AS companyName
     FROM company_vacancies
     WHERE id = ?`
  ).bind(vacancyId).first();
  if (!vacancy) return null;
  const existing = await db.prepare(
    "SELECT id FROM company_applications WHERE user_name = ? AND vacancy_id = ?"
  ).bind(userName, vacancyId).first();
  if (!existing) {
    await createCompanyApplication(db, userName, vacancy.companyName, Number(vacancyId), "Откликнулся", "", { recruiterName: "", recruiterPhone: "", recruiterEmail: "", recruiterContactType: "" });
  }
  const vacancies = await readVacancies(db, userName, vacancy.companyName);
  return { ok: true, vacancy: vacancies.find((item) => Number(item.id) === Number(vacancyId)) };
}
