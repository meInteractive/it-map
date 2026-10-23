// Server-only; extracted from the v218 Worker.
import {applicationStateSql, applicationRevision, applicationConflict} from '../services/application-revision.js';
import {ensureCrmSchema} from './schema.js';
import {readVacancies} from './vacancies.js';
import {MAX_APPLICATIONS_PER_COMPANY,RECRUITER_CONTACT_TYPES,APPLICATION_SOURCES} from '../constants.js';

export async function readCompanyCrm(db, userName, companyName) {
  await ensureCrmSchema(db);
  const [record, historyResult] = await Promise.all([
    db.prepare(
      "SELECT status, comment_md AS comment, favorite, hidden, updated_at AS updatedAt FROM company_user_state WHERE user_name = ? AND company_name = ?"
    ).bind(userName, companyName).first(),
    db.prepare(
      "SELECT from_status AS fromStatus, to_status AS toStatus, changed_at AS changedAt FROM company_status_history WHERE user_name = ? AND company_name = ? ORDER BY id DESC LIMIT 30"
    ).bind(userName, companyName).all()
  ]);
  return {
    status: record?.status || "",
    comment: record?.comment || "",
    favorite: Boolean(record?.favorite),
    hidden: Boolean(record?.hidden),
    updatedAt: record?.updatedAt || null,
    history: historyResult?.results || []
  };
}

export async function readFavoriteCompanyNames(db, userName) {
  await ensureCrmSchema(db);
  const result = await db.prepare(
    "SELECT company_name AS companyName FROM company_user_state WHERE user_name = ? AND favorite = 1 ORDER BY company_name ASC"
  ).bind(userName).all();
  return (result?.results || []).map((record) => record.companyName);
}

export async function saveCompanyFavorite(db, userName, companyName, favorite) {
  await ensureCrmSchema(db);
  await db.prepare("INSERT OR IGNORE INTO company_crm (company_name) VALUES (?)").bind(companyName).run();
  await db.prepare(
    `INSERT INTO company_user_state (user_name, company_name, favorite, updated_at)
     VALUES (?, ?, ?, CURRENT_TIMESTAMP)
     ON CONFLICT(user_name, company_name) DO UPDATE SET
       favorite = excluded.favorite,
       updated_at = CURRENT_TIMESTAMP`
  ).bind(userName, companyName, favorite ? 1 : 0).run();
  return readCompanyCrm(db, userName, companyName);
}

export async function readHiddenCompanyNames(db, userName) {
  await ensureCrmSchema(db);
  const result = await db.prepare(
    "SELECT company_name AS companyName FROM company_user_state WHERE user_name = ? AND hidden = 1 ORDER BY company_name ASC"
  ).bind(userName).all();
  return (result?.results || []).map((record) => record.companyName);
}

export async function saveCompanyHidden(db, userName, companyName, hidden) {
  await ensureCrmSchema(db);
  await db.prepare("INSERT OR IGNORE INTO company_crm (company_name) VALUES (?)").bind(companyName).run();
  await db.prepare(
    `INSERT INTO company_user_state (user_name, company_name, hidden, updated_at)
     VALUES (?, ?, ?, CURRENT_TIMESTAMP)
     ON CONFLICT(user_name, company_name) DO UPDATE SET
       hidden = excluded.hidden,
       updated_at = CURRENT_TIMESTAMP`
  ).bind(userName, companyName, hidden ? 1 : 0).run();
  return readCompanyCrm(db, userName, companyName);
}

export async function readApplicationsCrm(db, userName) {
  await ensureCrmSchema(db);
  return readApplicationRecords(db, userName);
}

export async function readApplicationRecords(db, userName, companyName = "", applicationId = null) {
  await ensureCrmSchema(db);
  const where = ['application.user_name = ?'];
  const values = [userName];
  if (companyName) { where.push('application.company_name = ?'); values.push(companyName); }
  if (applicationId != null) { where.push('application.id = ?'); values.push(applicationId); }
  const filter = where.join(' AND ');
  const recordsQuery = db.prepare(
    `SELECT application.id,
            application.company_name AS companyName,
            application.vacancy_id AS vacancyId,
            vacancy.title AS vacancyTitle,
            application.status,
            application.comment_md AS comment,
            application.recruiter_name AS recruiterName,
            application.recruiter_phone AS recruiterPhone,
            application.recruiter_email AS recruiterEmail,
            application.recruiter_contact_type AS recruiterContactType,
            application.application_sources AS applicationSources,
            application.created_at AS createdAt,
            application.updated_at AS updatedAt,
            ${applicationStateSql('application.')} AS stateSnapshot
     FROM company_applications AS application
     LEFT JOIN company_vacancies AS vacancy ON vacancy.id = application.vacancy_id
     WHERE ${filter}
     ORDER BY application.updated_at DESC, application.id DESC`
  ).bind(...values);
  const historyQuery = db.prepare(
    `SELECT history.application_id AS applicationId,
            history.from_status AS fromStatus,
            history.to_status AS toStatus,
            history.changed_at AS changedAt
     FROM company_application_status_history AS history
     INNER JOIN company_applications AS application ON application.id = history.application_id
     WHERE ${filter}
     ORDER BY history.id DESC`
  ).bind(...values);
  const [recordsResult, historyResult] = await Promise.all([recordsQuery.all(), historyQuery.all()]);
  const historyByApplication = new Map();
  for (const item of historyResult?.results || []) {
    const applicationId = Number(item.applicationId);
    if (!historyByApplication.has(applicationId)) historyByApplication.set(applicationId, []);
    historyByApplication.get(applicationId).push({
      fromStatus: item.fromStatus || "",
      toStatus: item.toStatus || "",
      changedAt: item.changedAt
    });
  }
  return Promise.all((recordsResult?.results || []).map(async (record) => ({
    id: Number(record.id),
    revision: await applicationRevision(record.stateSnapshot),
    companyName: record.companyName,
    vacancyId: record.vacancyId == null ? null : Number(record.vacancyId),
    vacancyTitle: record.vacancyTitle || "",
    status: record.status || "",
    comment: record.comment || "",
    recruiterName: record.recruiterName || "",
    recruiterPhone: record.recruiterPhone || "",
    recruiterEmail: record.recruiterEmail || "",
    recruiterContactType: record.recruiterContactType || "",
    applicationSources: parseApplicationSources(record.applicationSources),
    createdAt: record.createdAt,
    updatedAt: record.updatedAt,
    history: historyByApplication.get(Number(record.id)) || []
  })));
}

export async function readCompanyApplications(db, userName, companyName) {
  const [records, vacancies, crm] = await Promise.all([
    readApplicationRecords(db, userName, companyName),
    readVacancies(db, userName, companyName),
    readCompanyCrm(db, userName, companyName)
  ]);
  return {
    companyName,
    favorite: crm.favorite,
    hidden: crm.hidden,
    limit: MAX_APPLICATIONS_PER_COMPANY,
    records,
    vacancies
  };
}

export async function validateApplicationVacancy(db, userName, companyName, vacancyId, applicationId = 0) {
  if (vacancyId == null) return null;
  const vacancy = await db.prepare(
    "SELECT id FROM company_vacancies WHERE id = ? AND (company_name = ? OR real_company_name = ?)"
  ).bind(vacancyId, companyName, companyName).first();
  if (!vacancy) throw Object.assign(new Error("Выбранная вакансия не относится к этой компании."),{status:400});
  const linked = await db.prepare(
    "SELECT id FROM company_applications WHERE user_name = ? AND vacancy_id = ? AND id <> ?"
  ).bind(userName, vacancyId, applicationId).first();
  if (linked) throw Object.assign(new Error("Для этой вакансии отклик уже создан."),{status:400});
  return Number(vacancy.id);
}

export function normalizeRecruiterContact(payload) {
  const recruiterName = String(payload?.recruiterName || "").trim();
  const recruiterPhone = String(payload?.recruiterPhone || "").trim();
  const recruiterEmail = String(payload?.recruiterEmail || "").trim();
  const recruiterContactType = String(payload?.recruiterContactType || "");
  if (recruiterName.length > 160) throw new Error("ФИО рекрутера не должно превышать 160 символов.");
  if (recruiterPhone.length > 64) throw new Error("Телефон рекрутера не должен превышать 64 символа.");
  if (recruiterEmail.length > 254) throw new Error("Почта рекрутера не должна превышать 254 символа.");
  if (recruiterEmail && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(recruiterEmail)) throw new Error("Укажите корректную почту рекрутера.");
  if (!RECRUITER_CONTACT_TYPES.includes(recruiterContactType)) throw new Error("Неизвестный тип контакта рекрутера.");
  return { recruiterName, recruiterPhone, recruiterEmail, recruiterContactType };
}

export function parseApplicationSources(value) {
  let sources = [];
  try { sources = Array.isArray(value) ? value : JSON.parse(String(value || "")); } catch { sources = []; }
  const normalized = [...new Set((Array.isArray(sources) ? sources : []).map((item) => String(item || "").trim()).filter((item) => APPLICATION_SOURCES.includes(item)))];
  return normalized.length ? normalized : ["hh"];
}

export function normalizeApplicationSources(payload) {
  const value = Array.isArray(payload?.applicationSources) ? payload.applicationSources : undefined;
  if (value === undefined) return ["hh"];
  const sources = [...new Set(value.map((item) => String(item || "").trim()))];
  if (!sources.length) throw new Error("Выберите хотя бы одно место, где вы откликнулись.");
  if (sources.some((item) => !APPLICATION_SOURCES.includes(item))) throw new Error("Указан неизвестный источник отклика.");
  return sources;
}

export async function createCompanyApplication(db, userName, companyName, vacancyId, status, comment, recruiterContact, applicationSources = ["hh"]) {
  await ensureCrmSchema(db);
  const count = await db.prepare(
    "SELECT COUNT(*) AS count FROM company_applications WHERE user_name = ? AND company_name = ?"
  ).bind(userName, companyName).first();
  if (Number(count?.count || 0) >= MAX_APPLICATIONS_PER_COMPANY) {
    throw new Error("У компании уже сохранено 10 откликов.");
  }
  const normalizedVacancyId = await validateApplicationVacancy(db, userName, companyName, vacancyId);
  const statements = [
    db.prepare(`INSERT INTO company_crm (company_name) VALUES (?)
      ON CONFLICT(company_name) DO NOTHING`).bind(companyName),
    db.prepare(`INSERT INTO company_applications
       (user_name, company_name, vacancy_id, status, comment_md, recruiter_name, recruiter_phone, recruiter_email, recruiter_contact_type, application_sources, created_at, updated_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
      .bind(userName, companyName, normalizedVacancyId, status, comment, recruiterContact.recruiterName, recruiterContact.recruiterPhone, recruiterContact.recruiterEmail, recruiterContact.recruiterContactType, JSON.stringify(applicationSources))
  ];
  if (status) {
    // Keep directly after the application INSERT, on the same D1 batch connection.
    // The existing unique index and max-10 trigger also roll back this entire batch.
    statements.push(db.prepare(`INSERT INTO company_application_status_history
      (application_id, from_status, to_status, changed_at)
      VALUES (last_insert_rowid(), '', ?, CURRENT_TIMESTAMP)`).bind(status));
  }
  await db.batch(statements);
  return readCompanyApplications(db, userName, companyName);
}

export async function updateCompanyApplication(db, userName, applicationId, vacancyId, status, comment, recruiterContact, applicationSources = ["hh"], revision) {
  await ensureCrmSchema(db);
  const current = await db.prepare(
    `SELECT id, company_name AS companyName, status, ${applicationStateSql()} AS stateSnapshot
     FROM company_applications
     WHERE user_name = ? AND id = ?`
  ).bind(userName, applicationId).first();
  if (!current) return null;
  if (typeof revision !== 'string' || !/^[a-f0-9]{64}$/.test(revision)) {
    throw Object.assign(new Error('Загрузите актуальный отклик: для сохранения нужна версия записи.'), {status: 428});
  }
  if (revision !== await applicationRevision(current.stateSnapshot)) throw applicationConflict();
  const normalizedVacancyId = await validateApplicationVacancy(db, userName, current.companyName, vacancyId, applicationId);
  const statements = [
    db.prepare(
      `UPDATE company_applications
       SET vacancy_id = ?, status = ?, comment_md = ?, recruiter_name = ?, recruiter_phone = ?, recruiter_email = ?, recruiter_contact_type = ?, application_sources = ?, updated_at = CURRENT_TIMESTAMP
       WHERE user_name = ? AND id = ? AND ${applicationStateSql()} = ?`
    ).bind(normalizedVacancyId, status, comment, recruiterContact.recruiterName, recruiterContact.recruiterPhone, recruiterContact.recruiterEmail, recruiterContact.recruiterContactType, JSON.stringify(applicationSources), userName, applicationId, current.stateSnapshot)
  ];
  if ((current.status || "") !== status) {
    statements.push(
      db.prepare(
        `INSERT INTO company_application_status_history
           (application_id, from_status, to_status, changed_at)
         SELECT ?, ?, ?, CURRENT_TIMESTAMP WHERE changes() = 1`
      ).bind(applicationId, current.status || "", status)
    );
  }
  const results = await db.batch(statements);
  // UPDATE and its history are one transaction. A race after preflight cannot
  // change the record or append a false history entry.
  if (!results[0]?.meta?.changes) throw applicationConflict();
  return readCompanyApplications(db, userName, current.companyName);
}

export async function deleteCompanyApplication(db, userName, applicationId) {
  await ensureCrmSchema(db);
  const current = await db.prepare(
    "SELECT company_name AS companyName FROM company_applications WHERE user_name = ? AND id = ?"
  ).bind(userName, applicationId).first();
  if (!current) return null;
  await db.batch([
    db.prepare("DELETE FROM company_application_status_history WHERE application_id = ?").bind(applicationId),
    db.prepare("DELETE FROM company_applications WHERE user_name = ? AND id = ?").bind(userName, applicationId)
  ]);
  return readCompanyApplications(db, userName, current.companyName);
}

export async function saveCompanyCrm(db, userName, companyName, status, comment) {
  await ensureCrmSchema(db);
  await db.prepare("INSERT OR IGNORE INTO company_crm (company_name) VALUES (?)").bind(companyName).run();
  const current = await db.prepare(
    "SELECT status FROM company_user_state WHERE user_name = ? AND company_name = ?"
  ).bind(userName, companyName).first();
  const previousStatus = current?.status || "";
  const statements = [
    db.prepare(
      `INSERT INTO company_user_state (user_name, company_name, status, comment_md, updated_at)
       VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(user_name, company_name) DO UPDATE SET
         status = excluded.status,
         comment_md = excluded.comment_md,
         updated_at = CURRENT_TIMESTAMP`
    ).bind(userName, companyName, status, comment)
  ];
  if (previousStatus !== status) {
    statements.push(
      db.prepare(
        "INSERT INTO company_status_history (user_name, company_name, from_status, to_status, changed_at) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)"
      ).bind(userName, companyName, previousStatus, status)
    );
  }
  await db.batch(statements);
  const [crm, vacancies] = await Promise.all([
    readCompanyCrm(db, userName, companyName),
    readVacancies(db, userName, companyName)
  ]);
  return { ...crm, vacancies };
}
