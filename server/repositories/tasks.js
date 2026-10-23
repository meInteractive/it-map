// Server-only; extracted from the v218 Worker.
import {ensureCrmSchema} from './schema.js';
import {googleCalendarConfigured,readGoogleCalendarConnection,googleAccessToken,syncTaskWithGoogle,readTaskById} from '../integrations/google-calendar.js';

export function taskRecord(row) {
  return {
    id: Number(row.id),
    companyName: row.companyName,
    vacancyId: row.vacancyId == null ? null : Number(row.vacancyId),
    vacancyTitle: row.vacancyTitle || "",
    type: row.type,
    note: row.note || "",
    dueAt: row.dueAt,
    completed: Boolean(row.completed),
    googleEventId: row.googleEventId || "",
    googleEventHtmlLink: row.googleEventHtmlLink || "",
    googleSyncStatus: row.googleSyncStatus || "",
    googleSyncError: row.googleSyncError || "",
    createdAt: row.createdAt,
    completedAt: row.completedAt || null
  };
}

export async function readTasks(db, userName, companyName = "", includeCompleted = false, vacancyId = null) {
  await ensureCrmSchema(db);
  const clauses = ["task.user_name = ?"];
  const values = [userName];
  if (companyName) {
    clauses.push("task.company_name = ?");
    values.push(companyName);
  }
  if (vacancyId != null) {
    clauses.push("task.vacancy_id = ?");
    values.push(vacancyId);
  }
  if (!includeCompleted) clauses.push("task.completed = 0");
  let query = `SELECT task.id,
                      task.company_name AS companyName,
                      task.vacancy_id AS vacancyId,
                      vacancy.title AS vacancyTitle,
                      task.type,
                      task.note,
                      task.due_at AS dueAt,
                      task.completed,
                      task.google_event_id AS googleEventId,
                      task.google_event_html_link AS googleEventHtmlLink,
                      task.google_sync_status AS googleSyncStatus,
                      task.google_sync_error AS googleSyncError,
                      task.created_at AS createdAt,
                      task.completed_at AS completedAt
               FROM company_tasks AS task
               LEFT JOIN company_vacancies AS vacancy ON vacancy.id = task.vacancy_id`;
  if (clauses.length) query += ` WHERE ${clauses.join(" AND ")}`;
  query += " ORDER BY task.completed ASC, task.due_at ASC, task.id ASC";
  let statement = db.prepare(query);
  if (values.length) statement = statement.bind(...values);
  const result = await statement.all();
  return (result?.results || []).map(taskRecord);
}

export async function createTask(env, userName, companyName, vacancyId, type, note, dueAt, origin) {
  const db = env.DB;
  await ensureCrmSchema(db);
  let normalizedVacancyId = null;
  if (vacancyId != null) {
    const vacancy = await db.prepare(
      "SELECT id FROM company_vacancies WHERE id = ? AND (company_name = ? OR real_company_name = ?)"
    ).bind(vacancyId, companyName, companyName).first();
    if (!vacancy) throw new Error("Выбранная вакансия не относится к этой компании.");
    normalizedVacancyId = Number(vacancy.id);
  }
  await db.prepare(
    `INSERT INTO company_crm (company_name)
     VALUES (?)
     ON CONFLICT(company_name) DO NOTHING`
  ).bind(companyName).run();
  const inserted = await db.prepare(
    `INSERT INTO company_tasks (user_name, company_name, vacancy_id, type, note, due_at)
     VALUES (?, ?, ?, ?, ?, ?)`
  ).bind(userName, companyName, normalizedVacancyId, type, note, dueAt).run();
  const taskId = Number(inserted?.meta?.last_row_id);
  let calendarSync = { status: "not_connected" };
  if (Number.isSafeInteger(taskId) && googleCalendarConfigured(env)) {
    const connection = await readGoogleCalendarConnection(db, userName);
    if (connection) {
      try {
        const accessToken = await googleAccessToken(env, connection);
        calendarSync = await syncTaskWithGoogle(db, await readTaskById(db, userName, taskId), accessToken, connection, origin);
      } catch (error) {
        const message = String(error?.message || "Ошибка синхронизации").slice(0, 500);
        await db.prepare("UPDATE company_tasks SET google_sync_status = 'error', google_sync_error = ? WHERE user_name = ? AND id = ?").bind(message, userName, taskId).run();
        calendarSync = { status: "error", error: message };
      }
    }
  }
  return { ok: true, taskId, calendarSync };
}

export async function rescheduleTask(env, userName, taskId, dueAt, origin) {
  const db=env.DB;await ensureCrmSchema(db);
  const task=await readTaskById(db,userName,taskId);if(!task)return null;
  await db.prepare('UPDATE company_tasks SET due_at = ? WHERE user_name = ? AND id = ?').bind(dueAt,userName,taskId).run();
  task.dueAt=dueAt;let calendarSync={status:'not_connected'};
  try{
    const connection=googleCalendarConfigured(env)?await readGoogleCalendarConnection(db,userName):null;
    if(connection){calendarSync=await syncTaskWithGoogle(db,task,await googleAccessToken(env,connection),connection,origin,true);}
    else if(task.googleEventId)throw new Error('Подключите Google Calendar заново и повторите синхронизацию.');
  }catch(error){
    const message=String(error?.message||'Ошибка синхронизации').slice(0,500);
    await db.prepare("UPDATE company_tasks SET google_sync_status='error',google_sync_error=? WHERE user_name=? AND id=?").bind(message,userName,taskId).run();
    calendarSync={status:'error',error:message};
  }
  return {ok:true,dueAt,calendarSync};
}

export async function setTaskCompletion(db, userName, taskId, completed) {
  await ensureCrmSchema(db);
  const result = await db.prepare(
    `UPDATE company_tasks
     SET completed = ?,
         completed_at = CASE WHEN ? = 1 THEN CURRENT_TIMESTAMP ELSE NULL END
     WHERE user_name = ? AND id = ?`
  ).bind(completed ? 1 : 0, completed ? 1 : 0, userName, taskId).run();
  if (!result?.meta?.changes) return null;
  return { ok: true };
}
