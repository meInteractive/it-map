// Server-only; extracted from the v218 Worker.
import {MAX_HIRING_CONTACTS_PER_COMPANY} from '../constants.js';
import {ensureCrmSchema} from './schema.js';

export function hiringContactInput(payload, companyName = "") {
  const position = Number(payload?.position);
  const fullName = String(payload?.fullName || "").trim();
  const linkedinUrl = String(payload?.linkedinUrl || "").trim();
  const facebookUrl = String(payload?.facebookUrl || "").trim();
  const setkaUrl = String(payload?.setkaUrl || "").trim();
  const telegram = String(payload?.telegram || "").trim();
  const phone = String(payload?.phone || "").trim();
  const email = String(payload?.email || "").trim();
  const specialization = String(payload?.specialization || "").trim();
  if (!Number.isSafeInteger(position) || position < 1 || position > MAX_HIRING_CONTACTS_PER_COMPANY) throw new Error("позиция контакта должна быть целым числом от 1 до 3");
  if (!fullName) throw new Error("ФИО контакта обязательно");
  if (fullName.length > 256) throw new Error("ФИО контакта не должно превышать 256 символов");
  for (const [value, label] of [[linkedinUrl, "LinkedIn"], [facebookUrl, "Facebook"], [setkaUrl, "Сетка"]]) {
    if (!value) continue;
    let parsed;
    try { parsed = new URL(value); } catch { throw new Error(`${label}: укажите корректную ссылку`); }
    if (!["http:", "https:"].includes(parsed.protocol) || value.length > 2048) throw new Error(`${label}: укажите корректную ссылку`);
  }
  if (telegram.length > 128) throw new Error("Telegram не должен превышать 128 символов");
  if (phone.length > 64) throw new Error("Номер телефона не должен превышать 64 символа");
  if (email && (email.length > 254 || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email))) throw new Error("Email: укажите корректный адрес");
  if (specialization.length > 512) throw new Error("Специализация контакта не должна превышать 512 символов");
  return { companyName: String(companyName || payload?.companyName || "").trim(), position, fullName, linkedinUrl, facebookUrl, setkaUrl, telegram, phone, email, specialization };
}

export async function readHiringContacts(db, companyName) {
  await ensureCrmSchema(db);
  const result = await db.prepare(
    `SELECT id, company_name AS companyName, position, full_name AS fullName,
            linkedin_url AS linkedinUrl, facebook_url AS facebookUrl, setka_url AS setkaUrl, telegram, phone, email, specialization,
            created_at AS createdAt, updated_at AS updatedAt
     FROM hiring_contacts
     WHERE company_name = ?
     ORDER BY position ASC`
  ).bind(companyName).all();
  return result?.results || [];
}

export async function createHiringContact(db, companyName, payload) {
  await ensureCrmSchema(db);
  const input = hiringContactInput(payload, companyName);
  const count = await db.prepare("SELECT COUNT(*) AS count FROM hiring_contacts WHERE company_name = ?").bind(companyName).first();
  if (Number(count?.count || 0) >= MAX_HIRING_CONTACTS_PER_COMPANY) throw new Error("У организации уже добавлены три контакта ответственных за найм.");
  await db.prepare("INSERT OR IGNORE INTO company_crm (company_name) VALUES (?)").bind(companyName).run();
  try {
    await db.prepare(
      `INSERT INTO hiring_contacts (company_name, position, full_name, linkedin_url, facebook_url, setka_url, telegram, phone, email, specialization, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
    ).bind(companyName, input.position, input.fullName, input.linkedinUrl, input.facebookUrl, input.setkaUrl, input.telegram, input.phone, input.email, input.specialization).run();
  } catch (error) {
    if (String(error?.message || "").toLowerCase().includes("unique")) throw new Error("Эта позиция контакта уже занята.");
    throw error;
  }
  return { ok: true, contacts: await readHiringContacts(db, companyName) };
}

export async function updateHiringContact(db, contactId, payload) {
  await ensureCrmSchema(db);
  const current = await db.prepare("SELECT id, company_name AS companyName, position FROM hiring_contacts WHERE id = ?").bind(contactId).first();
  if (!current) return null;
  const input = hiringContactInput({ ...payload, position: payload?.position ?? current.position }, current.companyName);
  try {
    await db.prepare(
      `UPDATE hiring_contacts
       SET position = ?, full_name = ?, linkedin_url = ?, facebook_url = ?, setka_url = ?, telegram = ?, phone = ?, email = ?, specialization = ?, updated_at = CURRENT_TIMESTAMP
       WHERE id = ?`
    ).bind(input.position, input.fullName, input.linkedinUrl, input.facebookUrl, input.setkaUrl, input.telegram, input.phone, input.email, input.specialization, contactId).run();
  } catch (error) {
    if (String(error?.message || "").toLowerCase().includes("unique")) throw new Error("Эта позиция контакта уже занята.");
    throw error;
  }
  return { ok: true, contacts: await readHiringContacts(db, current.companyName) };
}

export async function deleteHiringContact(db, contactId) {
  await ensureCrmSchema(db);
  const current = await db.prepare("SELECT company_name AS companyName FROM hiring_contacts WHERE id = ?").bind(contactId).first();
  if (!current) return null;
  await db.prepare("DELETE FROM hiring_contacts WHERE id = ?").bind(contactId).run();
  return { ok: true, contacts: await readHiringContacts(db, current.companyName) };
}
