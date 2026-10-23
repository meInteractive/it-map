import {CRM_STATUSES, MAX_APPLICATIONS_PER_COMPANY} from '../constants.js';
import {normalizeRecruiterContact, normalizeApplicationSources, validateApplicationVacancy} from './applications.js';
import {readCompanyNames} from './catalog.js';
import {ensureCrmSchema} from './schema.js';
import {ensureApplicationReceiptSchema, readApplicationReceipt} from './application-receipts.js';
import {generationBoundDb, boundGeneration} from './generation-bound-db.js';
import {restoredDuringOperation} from './runtime-generation.js';
import {applicationRevision} from '../services/application-revision.js';

export const creationUuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const failure = (message, status, code) => Object.assign(new Error(message), {status, code});
const requiredChangeSql = `SELECT CASE WHEN changes() = 1 THEN 1
 ELSE abs(-9223372036854775808) END AS required_creation_write`;

export function normalizeApplicationCreation(payload) {
 const companyName = String(payload?.companyName || '').trim();
 const vacancyId = payload?.vacancyId == null || payload.vacancyId === '' ? null : Number(payload.vacancyId);
 const status = String(payload?.status || ''), comment = String(payload?.comment || '');
 if (!companyName) throw failure('Укажите компанию.',400);
 if (!CRM_STATUSES.includes(status)) throw failure('Неизвестный статус отклика.',400);
 if (comment.length > 1000) throw failure('Комментарий не должен превышать 1000 символов.',400);
 if (vacancyId != null && !Number.isSafeInteger(vacancyId)) throw failure('Некорректная вакансия.',400);
 try {
  return {companyName, vacancyId, status, comment, ...normalizeRecruiterContact(payload),
   applicationSources:normalizeApplicationSources(payload).sort()};
 } catch (error) { throw Object.assign(error,{status:400}); }
}
export async function applicationCreationHash(input) {
 // Fixed field order and canonical source set; no raw contact/comment in receipts.
 return applicationRevision(JSON.stringify([input.companyName, input.vacancyId,
  input.status, input.comment, input.recruiterName, input.recruiterPhone,
  input.recruiterEmail, input.recruiterContactType, input.applicationSources]));
}

async function replay(db, userName, key, generation, hash) {
 const receipt = await readApplicationReceipt(db,userName,key);
 if (!receipt) return null;
 if (receipt.generation !== generation) throw restoredDuringOperation();
 if (receipt.requestHash !== hash) throw failure('Этот запрос уже сохранён с другими данными. Черновик сохранён; не повторяйте создание с новым ключом автоматически.',409,'intent_conflict');
 if (!Number.isSafeInteger(receipt.applicationId) || receipt.applicationId <= 0)
  throw failure('Результат создания неполон. Обратитесь к администратору.',503,'incomplete_receipt');
 const row = await db.prepare('SELECT id FROM company_applications WHERE id = ? AND user_name = ?').bind(receipt.applicationId,userName).first();
 if (!row) throw failure('Созданный отклик уже удалён. Повтор не создаст его заново.',410,'application_deleted');
 return {applicationId:receipt.applicationId,replayed:true};
}

// Shortcut-only: an already existing personal vacancy application is a valid
// result. Claim its identity atomically without overwriting fields or history.
async function rememberExistingVacancy(db, userName, input, key, generation, hash) {
 if (input.vacancyId == null) return null;
 const row = await db.prepare(`SELECT id FROM company_applications
  WHERE user_name = ? AND vacancy_id = ?`).bind(userName,input.vacancyId).first();
 if (!row) return null;
 await db.batch([
  db.prepare(`INSERT INTO application_creation_receipts
   (user_name,idempotency_key,generation,request_hash,created_application_id)
   SELECT ?,?,?,?,a.id FROM company_applications a
   JOIN company_vacancies v ON v.id = a.vacancy_id
   WHERE a.id = ? AND a.user_name = ? AND a.vacancy_id = ? AND v.company_name = ?
   ON CONFLICT(user_name,idempotency_key) DO NOTHING`)
   .bind(userName,key,generation,hash,row.id,userName,input.vacancyId,input.companyName),
  db.prepare(requiredChangeSql),
  db.prepare(`SELECT CASE WHEN EXISTS (SELECT 1 FROM application_creation_receipts r
   JOIN company_applications a ON a.id = r.created_application_id AND a.user_name = r.user_name
   WHERE r.user_name = ? AND r.idempotency_key = ? AND r.generation = ? AND r.request_hash = ?)
   THEN 1 ELSE abs(-9223372036854775808) END AS complete_receipt_guard`).bind(userName,key,generation,hash),
 ]);
 const result = await replay(db,userName,key,generation,hash);
 if (!result) throw failure('Не удалось прочитать результат отклика.',503);
 return {...result,replayed:false};
}

// HTTP service also reads the compatible response through the same generation guard.
// generation must come from a VERIFIED creation context, never refreshed on retry.
export async function createApplicationWithReceipt(db, userName, payload, intent, {reuseVacancyApplication = false} = {}) {
 const {idempotencyKey:key,generation} = intent || {};
 if (!key || !generation) throw failure('Для создания нужен ключ и контекст запроса.',428,'creation_context_required');
 if (typeof key !== 'string' || typeof generation !== 'string' || !creationUuidPattern.test(key) || !creationUuidPattern.test(generation)) throw failure('Некорректный ключ или поколение запроса.',400);
 if (typeof userName !== 'string' || !userName || userName.length > 160) throw failure('Некорректный пользователь.',400);
 const input = normalizeApplicationCreation(payload),hash = await applicationCreationHash(input);
 const currentBound = boundGeneration(db);
 if (currentBound && currentBound !== generation) throw restoredDuringOperation();
 const guarded = currentBound ? db : generationBoundDb(db,generation);
 await ensureCrmSchema(guarded);await ensureApplicationReceiptSchema(guarded);
 try {
  const existing = await replay(guarded,userName,key,generation,hash);
  if (existing) return existing;
  if (reuseVacancyApplication) {
   // Shortcut targets the vacancy owner, not its guessed real employer.
   const vacancy = await guarded.prepare('SELECT company_name AS companyName FROM company_vacancies WHERE id = ?').bind(input.vacancyId).first();
   if (!vacancy) throw failure('Вакансия не найдена.',404);
   if (vacancy.companyName !== input.companyName) throw failure('Владелец вакансии изменился. Обновите список.',409,'vacancy_owner_changed');
   const linked = await rememberExistingVacancy(guarded,userName,input,key,generation,hash);
   if (linked) return linked;
  }
  // Shortcut owner was verified in SQL above, including channel-only owners.
  if (!reuseVacancyApplication && !(await readCompanyNames(guarded)).has(input.companyName)) throw failure('Компания не найдена.',404);
  const count = await guarded.prepare('SELECT COUNT(*) AS count FROM company_applications WHERE user_name = ? AND company_name = ?').bind(userName,input.companyName).first();
  if (Number(count?.count || 0) >= MAX_APPLICATIONS_PER_COMPANY) throw failure('У компании уже сохранено 10 откликов.',400);
  await validateApplicationVacancy(guarded,userName,input.companyName,input.vacancyId);
  const receiptWhere = 'user_name = ? AND idempotency_key = ? AND generation = ? AND request_hash = ?';
  const receiptArgs = [userName,key,generation,hash];
  const statements = [
   guarded.prepare(`INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash)
    VALUES(?,?,?,?) ON CONFLICT(user_name,idempotency_key) DO NOTHING`).bind(...receiptArgs),
   guarded.prepare(requiredChangeSql),
   guarded.prepare('INSERT INTO company_crm(company_name) VALUES(?) ON CONFLICT(company_name) DO NOTHING').bind(input.companyName),
   // Recheck vacancy ownership at the actual INSERT, not only in JS preflight.
   guarded.prepare(`INSERT INTO company_applications(user_name,company_name,vacancy_id,status,comment_md,
    recruiter_name,recruiter_phone,recruiter_email,recruiter_contact_type,application_sources)
    SELECT ?,?,?,?,?,?,?,?,?,? WHERE ? IS NULL OR EXISTS
    (SELECT 1 FROM company_vacancies WHERE id = ? AND (company_name = ? OR (? = 0 AND real_company_name = ?)))`)
    .bind(userName,input.companyName,input.vacancyId,input.status,input.comment,
     input.recruiterName,input.recruiterPhone,input.recruiterEmail,input.recruiterContactType,
     JSON.stringify(input.applicationSources),input.vacancyId,input.vacancyId,input.companyName,reuseVacancyApplication?1:0,input.companyName),
   guarded.prepare(requiredChangeSql),
   // No other INSERT between application INSERT and this capture of last_insert_rowid.
   guarded.prepare(`UPDATE application_creation_receipts SET created_application_id = last_insert_rowid()
    WHERE ${receiptWhere} AND created_application_id IS NULL`).bind(...receiptArgs),
   guarded.prepare(requiredChangeSql),
  ];
  if (input.status) statements.push(guarded.prepare(`INSERT INTO company_application_status_history(application_id,from_status,to_status)
   SELECT created_application_id,'',? FROM application_creation_receipts WHERE ${receiptWhere}`).bind(input.status,...receiptArgs), guarded.prepare(requiredChangeSql));
  // Also fails closed if a trigger drops/replaces a supposedly successful write.
  statements.push(guarded.prepare(`SELECT CASE WHEN EXISTS (SELECT 1 FROM application_creation_receipts r
   JOIN company_applications a ON a.id = r.created_application_id AND a.user_name = r.user_name
   WHERE r.user_name = ? AND r.idempotency_key = ? AND r.generation = ? AND r.request_hash = ?)
   THEN 1 ELSE abs(-9223372036854775808) END AS complete_receipt_guard`).bind(...receiptArgs));
  await guarded.batch(statements);
  const result = await replay(guarded,userName,key,generation,hash);
  if (!result) throw failure('Не удалось прочитать результат создания.',503);
  return {...result,replayed:false};
 } catch (error) {
  // A concurrent winner may commit after our first receipt read, even before count.
  // Read after rollback / committed write; preserve real errors if no winner exists.
  const existing = await replay(guarded,userName,key,generation,hash);
  if (existing) return existing;
  // A different intent may have won the unique user/vacancy race. Remember that
  // result too, so deleting it cannot turn this retry into a new creation.
  if (reuseVacancyApplication && error.status !== 409 && error.status !== 404) {
   const linked = await rememberExistingVacancy(guarded,userName,input,key,generation,hash);
   if (linked) return linked;
  }
  throw error;
 }
}
