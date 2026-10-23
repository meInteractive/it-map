// Server-only; extracted from the v218 Worker.
import {MAX_TELEGRAM_CHANNEL_COMMENT_LENGTH} from '../constants.js';
import {readAppCatalog} from './catalog.js';
import {ensureCrmSchema} from './schema.js';

export function normalizeTelegramUsername(value) {
  let source = String(value || "").trim();
  if (!source) return "";
  if (source.startsWith("@")) source = source.slice(1);
  else {
    if (!/^https?:\/\//i.test(source)) source = `https://${source}`;
    let parsed;
    try { parsed = new URL(source); } catch { return ""; }
    const host = parsed.hostname.toLowerCase().replace(/^www\./, "");
    if (!["t.me", "telegram.me"].includes(host)) return "";
    const parts = parsed.pathname.split("/").filter(Boolean);
    if (parts[0]?.toLowerCase() === "s") parts.shift();
    source = parts[0] || "";
  }
  const username = source.replace(/^@/, "").trim().toLowerCase();
  return /^[a-z0-9_]{5,64}$/.test(username) ? username : "";
}

export function telegramMessageUrlInput(value, expectedUsername = "") {
  let source = String(value || "").trim();
  if (!source) throw new Error("Укажите прямую ссылку на сообщение в Telegram-канале.");
  if (!/^https?:\/\//i.test(source)) source = `https://${source}`;
  let parsed;
  try { parsed = new URL(source); } catch { throw new Error("Укажите корректную ссылку на Telegram-сообщение."); }
  const host = parsed.hostname.toLowerCase().replace(/^www\./, "");
  if (!["t.me", "telegram.me"].includes(host)) throw new Error("Ссылка на сообщение должна вести на t.me.");
  const parts = parsed.pathname.split("/").filter(Boolean);
  if (parts[0]?.toLowerCase() === "s") parts.shift();
  const username = normalizeTelegramUsername(parts[0] ? `@${parts[0]}` : "");
  const messageId = parts[1] || "";
  if (!username || !/^\d+$/.test(messageId)) throw new Error("Нужна прямая ссылка на конкретное Telegram-сообщение.");
  if (expectedUsername && username !== expectedUsername) throw new Error("Ссылка на сообщение относится к другому Telegram-каналу.");
  return `https://t.me/${username}/${messageId}`;
}

export function telegramChannelInput(payload, current = null) {
  const name = String(payload?.name ?? current?.name ?? "").trim();
  const rawUrl = String(payload?.url ?? payload?.username ?? current?.url ?? current?.username ?? "").trim();
  const normalizedUsername = normalizeTelegramUsername(rawUrl);
  const comment = String(payload?.comment ?? current?.comment ?? "");
  const lastSuccessfulCheckAtValue = payload?.lastSuccessfulCheckAt ?? current?.lastSuccessfulCheckAt ?? null;
  const lastSuccessfulCheckAt = lastSuccessfulCheckAtValue == null || lastSuccessfulCheckAtValue === "" ? null : String(lastSuccessfulCheckAtValue).trim();
  if (!name) throw new Error("Укажите название Telegram-канала.");
  if (name.length > 160) throw new Error("Название канала не должно превышать 160 символов.");
  if (!normalizedUsername) throw new Error("Укажите корректную ссылку на публичный Telegram-канал или @username.");
  if (comment.length > MAX_TELEGRAM_CHANNEL_COMMENT_LENGTH) throw new Error("Комментарий не должен превышать 512 символов.");
  if (lastSuccessfulCheckAt && Number.isNaN(Date.parse(lastSuccessfulCheckAt))) throw new Error("Укажите корректные дату и время последней проверки.");
  return {
    name,
    url: `https://t.me/${normalizedUsername}`,
    normalizedUsername,
    username: `@${normalizedUsername}`,
    lastSuccessfulCheckAt: lastSuccessfulCheckAt ? new Date(lastSuccessfulCheckAt).toISOString() : null,
    comment
  };
}

export async function assertIndependentTelegramChannel(db, input, excludedChannelId = null) {
  const catalog = await readAppCatalog(db);
  const owned = [...catalog.companies, ...catalog.agencies].flatMap((entity) => Object.values(entity?.vacancySources || {}));
  if (owned.some((value) => normalizeTelegramUsername(value) === input.normalizedUsername)) {
    throw new Error("Этот Telegram-канал уже указан как источник компании или агентства.");
  }
  const duplicate = await db.prepare(
    "SELECT id FROM telegram_channels WHERE (normalized_username = ? OR LOWER(name) = LOWER(?)) AND (? IS NULL OR id <> ?)"
  ).bind(input.normalizedUsername, input.name, excludedChannelId, excludedChannelId).first();
  if (duplicate) throw new Error("Такой независимый Telegram-канал уже добавлен.");
}

export function telegramChannelRecord(row) {
  return {
    id: Number(row.id),
    name: row.name,
    url: row.url,
    username: row.username || `@${row.normalizedUsername}`,
    normalizedUsername: row.normalizedUsername,
    lastSuccessfulCheckAt: row.lastSuccessfulCheckAt || null,
    comment: row.comment || "",
    vacancyCount: Number(row.vacancyCount || 0),
    createdAt: row.createdAt,
    updatedAt: row.updatedAt
  };
}

export async function readTelegramChannels(db) {
  await ensureCrmSchema(db);
  const result = await db.prepare(
    `SELECT channel.id, channel.name, channel.url, channel.username,
            channel.normalized_username AS normalizedUsername,
            channel.last_successful_check_at AS lastSuccessfulCheckAt,
            channel.comment,
            channel.created_at AS createdAt,
            channel.updated_at AS updatedAt,
            COUNT(source.vacancy_id) AS vacancyCount
     FROM telegram_channels AS channel
     LEFT JOIN vacancy_telegram_sources AS source ON source.channel_id = channel.id
     GROUP BY channel.id
     ORDER BY channel.name COLLATE NOCASE ASC`
  ).all();
  return (result?.results || []).map(telegramChannelRecord);
}

export async function createTelegramChannel(db, payload) {
  await ensureCrmSchema(db);
  const input = telegramChannelInput(payload);
  await assertIndependentTelegramChannel(db, input);
  const inserted = await db.prepare(
    `INSERT INTO telegram_channels
       (name, url, normalized_username, username, last_successful_check_at, comment, created_at, updated_at)
     VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
  ).bind(input.name, input.url, input.normalizedUsername, input.username, input.lastSuccessfulCheckAt, input.comment).run();
  return { ok: true, channelId: Number(inserted?.meta?.last_row_id || 0), channels: await readTelegramChannels(db) };
}

export async function updateTelegramChannel(db, channelId, payload) {
  await ensureCrmSchema(db);
  const current = await db.prepare(
    `SELECT id, name, url, username, normalized_username AS normalizedUsername,
            last_successful_check_at AS lastSuccessfulCheckAt, comment
     FROM telegram_channels WHERE id = ?`
  ).bind(channelId).first();
  if (!current) return null;
  const input = telegramChannelInput(payload, current);
  await assertIndependentTelegramChannel(db, input, channelId);
  await db.prepare(
    `UPDATE telegram_channels
     SET name = ?, url = ?, normalized_username = ?, username = ?,
         last_successful_check_at = ?, comment = ?, updated_at = CURRENT_TIMESTAMP
     WHERE id = ?`
  ).bind(input.name, input.url, input.normalizedUsername, input.username, input.lastSuccessfulCheckAt, input.comment, channelId).run();
  return { ok: true, channels: await readTelegramChannels(db) };
}

export async function deleteTelegramChannel(db, channelId) {
  await ensureCrmSchema(db);
  const current = await db.prepare("SELECT id, name FROM telegram_channels WHERE id = ?").bind(channelId).first();
  if (!current) return null;
  const linked = await db.prepare("SELECT COUNT(*) AS count FROM vacancy_telegram_sources WHERE channel_id = ?").bind(channelId).first();
  const deleted = await db.prepare(`DELETE FROM telegram_channels
    WHERE id = ? AND NOT EXISTS (
      SELECT 1 FROM vacancy_telegram_sources source
      JOIN company_vacancies vacancy ON vacancy.id = source.vacancy_id
      WHERE source.channel_id = telegram_channels.id AND vacancy.entity_type = 'channel'
        AND NOT EXISTS (
          SELECT 1 FROM vacancy_telegram_sources other
          WHERE other.vacancy_id = vacancy.id AND other.channel_id <> telegram_channels.id
        )
    )`).bind(channelId).run();
  if (!Number(deleted?.meta?.changes || 0)) {
    const remains = await db.prepare("SELECT id FROM telegram_channels WHERE id = ?").bind(channelId).first();
    if (!remains) return null;
    const error = new Error("Нельзя удалить канал: он является единственным источником одной или нескольких вакансий. Добавьте другой источник или перенесите вакансии в компанию/агентство.");
    error.status = 409;
    throw error;
  }
  return { ok: true, channelId, name: current.name, detachedVacancies: Number(linked?.count || 0) };
}
