// Server-only; extracted from the v218 Worker.
import {bytesToBase64Url,sign,readCookie,timingSafeEqual,base64UrlToBytes} from '../auth/session.js';
import {GOOGLE_OAUTH_STATE_TTL_MS,encoder,GOOGLE_OAUTH_STATE_COOKIE,USERS} from '../constants.js';
import {ensureCrmSchema} from '../repositories/schema.js';
import {taskRecord} from '../repositories/tasks.js';

export async function createGoogleOAuthState(secret, userName) {
  const nonce = bytesToBase64Url(crypto.getRandomValues(new Uint8Array(18)));
  const expiresAt = Date.now() + GOOGLE_OAUTH_STATE_TTL_MS;
  const encodedUser = bytesToBase64Url(encoder.encode(userName));
  const payload = `v2.${expiresAt}.${nonce}.${encodedUser}`;
  return `${payload}.${await sign(secret, payload)}`;
}

export async function validGoogleOAuthState(request, secret, state) {
  const cookieState = readCookie(request, GOOGLE_OAUTH_STATE_COOKIE);
  if (!cookieState || !state || !timingSafeEqual(cookieState, state)) return null;
  const parts = state.split(".");
  if (parts.length !== 5 || parts[0] !== "v2") return null;
  const expiresAt = Number(parts[1]);
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) return null;
  const payload = parts.slice(0, 4).join(".");
  if (!timingSafeEqual(parts[4], await sign(secret, payload))) return null;
  try {
    const userName = new TextDecoder().decode(base64UrlToBytes(parts[3]));
    return USERS[userName.toLowerCase()]?.name || null;
  } catch { return null; }
}

export async function googleTokenEncryptionKey(secret) {
  const digest = await crypto.subtle.digest("SHA-256", encoder.encode(`google-calendar:${secret}`));
  return crypto.subtle.importKey("raw", digest, { name: "AES-GCM" }, false, ["encrypt", "decrypt"]);
}

export async function encryptGoogleToken(secret, value) {
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await googleTokenEncryptionKey(secret);
  const encrypted = await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, encoder.encode(value));
  return `${bytesToBase64Url(iv)}.${bytesToBase64Url(new Uint8Array(encrypted))}`;
}

export async function decryptGoogleToken(secret, value) {
  const [ivValue, encryptedValue] = String(value || "").split(".");
  if (!ivValue || !encryptedValue) throw new Error("Некорректные данные подключения Google Calendar.");
  const key = await googleTokenEncryptionKey(secret);
  const decrypted = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: base64UrlToBytes(ivValue) },
    key,
    base64UrlToBytes(encryptedValue)
  );
  return new TextDecoder().decode(decrypted);
}

export function googleCalendarConfigured(env) {
  return Boolean(env?.GOOGLE_CALENDAR_CLIENT_ID && env?.GOOGLE_CALENDAR_CLIENT_SECRET && env?.SESSION_SECRET);
}

export function googleCalendarCallbackUrl(url) {
  return `${url.origin}/api/google-calendar/callback`;
}

export async function googleTokenRequest(env, parameters) {
  const response = await fetch("https://oauth2.googleapis.com/token", {
    method: "POST",
    headers: { "content-type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(parameters)
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok || !data.access_token) throw new Error(data.error_description || "Google не выдал доступ к календарю.");
  return data;
}

export async function readGoogleCalendarConnection(db, userName) {
  await ensureCrmSchema(db);
  return db.prepare(
    `SELECT account_email AS accountEmail,
            refresh_token_encrypted AS refreshTokenEncrypted,
            calendar_id AS calendarId,
            connected_at AS connectedAt,
            updated_at AS updatedAt
     FROM google_calendar_user_connection
     WHERE user_name = ?`
  ).bind(userName).first();
}

export async function googleAccessToken(env, connection) {
  const refreshToken = await decryptGoogleToken(env.SESSION_SECRET, connection.refreshTokenEncrypted);
  const token = await googleTokenRequest(env, {
    client_id: env.GOOGLE_CALENDAR_CLIENT_ID,
    client_secret: env.GOOGLE_CALENDAR_CLIENT_SECRET,
    refresh_token: refreshToken,
    grant_type: "refresh_token"
  });
  return token.access_token;
}

export async function googleAccountEmail(accessToken) {
  const response = await fetch("https://www.googleapis.com/oauth2/v2/userinfo", {
    headers: { authorization: `Bearer ${accessToken}` }
  });
  const data = await response.json().catch(() => ({}));
  return response.ok ? String(data.email || "") : "";
}

export async function readTaskById(db, userName, taskId) {
  const row = await db.prepare(
    `SELECT task.id,
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
     LEFT JOIN company_vacancies AS vacancy ON vacancy.id = task.vacancy_id
     WHERE task.user_name = ? AND task.id = ?`
  ).bind(userName, taskId).first();
  return row ? taskRecord(row) : null;
}

export async function createGoogleCalendarEvent(accessToken, calendarId, task, origin, updateExisting = false) {
  const labels = { call: "Звонок", email: "Письмо", messenger: "Мессенджер", meeting: "Встреча" };
  const start = new Date(task.dueAt);
  const end = new Date(start.getTime() + 30 * 60 * 1000);
  const description = [
    task.note || "",
    task.vacancyTitle ? `Вакансия: ${task.vacancyTitle}` : "",
    `Компания: ${task.companyName}`,
    `Источник: ${origin}`
  ].filter(Boolean).join("\n\n").slice(0, 8000);
  const response = await fetch(
    `https://www.googleapis.com/calendar/v3/calendars/${encodeURIComponent(calendarId || "primary")}/events${updateExisting ? "/" + encodeURIComponent(task.googleEventId) : ""}?sendUpdates=none`,
    {
      method: updateExisting ? "PATCH" : "POST",
      headers: { authorization: `Bearer ${accessToken}`, "content-type": "application/json" },
      body: JSON.stringify(updateExisting ? {
        start: { dateTime: start.toISOString(), timeZone: "Europe/Moscow" },
        end: { dateTime: end.toISOString(), timeZone: "Europe/Moscow" }
      } : {
        summary: `${labels[task.type] || "Задача"}: ${task.companyName}`,
        description,
        start: { dateTime: start.toISOString(), timeZone: "Europe/Moscow" },
        end: { dateTime: end.toISOString(), timeZone: "Europe/Moscow" },
        extendedProperties: { private: { source: "moscow-it-employers-map", taskId: String(task.id) } }
      })
    }
  );
  const data = await response.json().catch(() => ({}));
  if (!response.ok || !data.id) throw new Error(data?.error?.message || "Не удалось добавить событие в Google Calendar.");
  return { id: String(data.id), htmlLink: String(data.htmlLink || "") };
}

export async function syncTaskWithGoogle(db, task, accessToken, connection, origin, updateExisting = false) {
  if (!task || (task.completed && !task.googleEventId) || (task.googleEventId && !updateExisting)) return { status: task?.googleEventId ? "synced" : "skipped" };
  try {
    const event = await createGoogleCalendarEvent(accessToken, connection.calendarId, task, origin, Boolean(updateExisting && task.googleEventId));
    await db.prepare(
      `UPDATE company_tasks
       SET google_event_id = ?, google_event_html_link = ?, google_sync_status = 'synced', google_sync_error = ''
       WHERE id = ?`
    ).bind(event.id, event.htmlLink, task.id).run();
    return { status: "synced", eventId: event.id };
  } catch (error) {
    const message = String(error?.message || "Ошибка синхронизации").slice(0, 500);
    await db.prepare(
      "UPDATE company_tasks SET google_sync_status = 'error', google_sync_error = ? WHERE id = ?"
    ).bind(message, task.id).run();
    return { status: "error", error: message };
  }
}

export async function syncUnsyncedTasks(env, userName, origin) {
  const connection = await readGoogleCalendarConnection(env.DB, userName);
  if (!connection) return { synced: 0, errors: 0 };
  const accessToken = await googleAccessToken(env, connection);
  const result = await env.DB.prepare(
    `SELECT id FROM company_tasks
     WHERE user_name = ? AND ((completed = 0 AND google_event_id IS NULL) OR (google_event_id IS NOT NULL AND google_sync_status = 'error'))
     ORDER BY due_at ASC, id ASC
     LIMIT 250`
  ).bind(userName).all();
  let synced = 0;
  let errors = 0;
  for (const row of result?.results || []) {
    const outcome = await syncTaskWithGoogle(env.DB, await readTaskById(env.DB, userName, Number(row.id)), accessToken, connection, origin, true);
    if (outcome.status === "synced") synced += 1;
    if (outcome.status === "error") errors += 1;
  }
  return { synced, errors };
}

export async function googleCalendarStatus(env, userName) {
  const configured = googleCalendarConfigured(env);
  if (!env?.DB) return { configured, connected: false, accountEmail: "", syncedTasks: 0, syncErrors: 0 };
  const connection = await readGoogleCalendarConnection(env.DB, userName);
  const counts = await env.DB.prepare(
    `SELECT SUM(CASE WHEN google_event_id IS NOT NULL THEN 1 ELSE 0 END) AS syncedTasks,
            SUM(CASE WHEN google_sync_status = 'error' THEN 1 ELSE 0 END) AS syncErrors
     FROM company_tasks WHERE user_name = ?`
  ).bind(userName).first();
  return {
    configured,
    connected: Boolean(connection),
    accountEmail: connection?.accountEmail || "",
    connectedAt: connection?.connectedAt || null,
    syncedTasks: Number(counts?.syncedTasks || 0),
    syncErrors: Number(counts?.syncErrors || 0)
  };
}
