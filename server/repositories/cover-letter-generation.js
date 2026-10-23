// Operational ownership, shared across Worker instances; no prompt/resume text stored.
import {coverLetterInputFromSql,coverLetterInputSql} from '../services/cover-letter-input.js';
import {generationBoundDb} from './generation-bound-db.js';
import {runtimeGenerationSql} from './runtime-generation.js';

export const COVER_LETTER_GENERATION_TABLE_SQL = `CREATE TABLE IF NOT EXISTS cover_letter_generation_leases (
 user_name TEXT NOT NULL,
 vacancy_id INTEGER NOT NULL,
 generation TEXT NOT NULL,
 input_hash TEXT NOT NULL,
 owner_token TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 PRIMARY KEY(user_name,vacancy_id),
 CONSTRAINT letter_lease_user CHECK(length(user_name) BETWEEN 1 AND 160),
 CONSTRAINT letter_lease_vacancy CHECK(vacancy_id > 0),
 CONSTRAINT letter_lease_generation CHECK(length(generation) = 36),
 CONSTRAINT letter_lease_hash CHECK(length(input_hash) = 64 AND input_hash NOT GLOB '*[^0-9a-f]*'),
 CONSTRAINT letter_lease_owner CHECK(length(owner_token) = 36),
 CONSTRAINT letter_lease_expiry CHECK(expires_at > 0)
)`;
export const COVER_LETTER_LEASE_MS = 90000;
const nowSql = `(CAST(strftime('%s','now') AS INTEGER) * 1000)`;
function duration(ttlMs) {
 if(!Number.isSafeInteger(ttlMs)||ttlMs<1000||ttlMs>300000)throw new TypeError('Invalid generation lease duration');
 return ttlMs;
}
export async function coverLetterInputHash(snapshot) {
 const hash=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(snapshot));
 return Array.from(new Uint8Array(hash),b=>b.toString(16).padStart(2,'0')).join('');
}
const noLetterSql = `NOT EXISTS (
 SELECT 1 FROM vacancy_cover_letters letter WHERE letter.user_name = ? AND letter.vacancy_id = v.id)`;
export const coverLetterGenerationSnapshot=input=>input.regenerate?JSON.stringify([input.snapshot,'regenerate',input.personalSnapshot]):input.snapshot;
export function coverLetterSavedGuard(input,userName){
 return input.regenerate?{sql:`(SELECT json_array(subject,markdown,override_shared,revision) FROM vacancy_cover_letters
  WHERE user_name=? AND vacancy_id=v.id) IS ?`,bindings:[userName,input.personalSnapshot]}:{sql:noLetterSql,bindings:[userName]};
}

// Null means this request did not obtain ownership: caller must reread current
// input/saved text before deciding whether to wait, return a saved letter or fail.
export async function claimCoverLetterGeneration(db,userName,vacancyId,input,{ttlMs=COVER_LETTER_LEASE_MS}={}) {
 duration(ttlMs);
 // JS trim matches existing manual-letter semantics (including Unicode spaces).
 // The coherent SQL snapshot fences a shared letter changed after this check.
 if(!input.regenerate&&(input.markdown.trim() || input.personalMarkdown != null))return null;
 await db.prepare(COVER_LETTER_GENERATION_TABLE_SQL).run();
 const ownerToken=crypto.randomUUID(),hash=await coverLetterInputHash(coverLetterGenerationSnapshot(input)),guard=coverLetterSavedGuard(input,userName);
 await db.prepare(`INSERT INTO cover_letter_generation_leases(user_name,vacancy_id,generation,input_hash,owner_token,expires_at)
 SELECT ?,v.id,?,?,?,${nowSql}+? FROM ${coverLetterInputFromSql}
 WHERE v.id = ? AND ${coverLetterInputSql} = ? AND ${guard.sql}
 ON CONFLICT(user_name,vacancy_id) DO UPDATE SET
 generation=excluded.generation,input_hash=excluded.input_hash,
 owner_token=excluded.owner_token,expires_at=excluded.expires_at
 WHERE cover_letter_generation_leases.generation <> excluded.generation
 OR cover_letter_generation_leases.expires_at <= ${nowSql}`)
 .bind(userName,input.generation,hash,ownerToken,ttlMs,userName,vacancyId,input.snapshot,...guard.bindings).run();
 const row=await db.prepare(`SELECT expires_at AS expiresAt FROM cover_letter_generation_leases
 WHERE user_name=? AND vacancy_id=? AND generation=? AND input_hash=? AND owner_token=?
 AND generation=${runtimeGenerationSql} AND expires_at>${nowSql}`)
 .bind(userName,vacancyId,input.generation,hash,ownerToken).first();
 return row?{userName,vacancyId,generation:input.generation,inputHash:hash,ownerToken,expiresAt:row.expiresAt}:null;
}
export async function renewCoverLetterGeneration(db,lease,input,{ttlMs=COVER_LETTER_LEASE_MS}={}) {
 duration(ttlMs);
 if(input.generation!==lease.generation||await coverLetterInputHash(coverLetterGenerationSnapshot(input))!==lease.inputHash)return false;
 const guard=coverLetterSavedGuard(input,lease.userName);
 const result=await db.prepare(`UPDATE cover_letter_generation_leases SET expires_at=${nowSql}+?
 WHERE user_name=? AND vacancy_id=? AND generation=? AND input_hash=? AND owner_token=?
 AND generation=${runtimeGenerationSql} AND expires_at>${nowSql}
 AND EXISTS (SELECT 1 FROM ${coverLetterInputFromSql} WHERE v.id=?
 AND ${coverLetterInputSql}=? AND ${guard.sql})`)
 .bind(ttlMs,...leaseBindings(lease),lease.userName,lease.vacancyId,input.snapshot,...guard.bindings).run();
 return result.meta?.changes===1;
}
export async function releaseCoverLetterGeneration(db,lease) {
 // Old/expired owners cannot release a replacement lease, even for identical input.
 await db.prepare(`DELETE FROM cover_letter_generation_leases
 WHERE user_name=? AND vacancy_id=? AND generation=? AND input_hash=? AND owner_token=?`)
 .bind(...leaseBindings(lease)).run();
}
export const coverLetterLeaseSql = `EXISTS (SELECT 1 FROM cover_letter_generation_leases
 WHERE user_name=? AND vacancy_id=? AND generation=? AND input_hash=? AND owner_token=?
 AND generation=${runtimeGenerationSql} AND expires_at>${nowSql})`;
export const leaseBindings=lease=>[lease.userName,lease.vacancyId,lease.generation,lease.inputHash,lease.ownerToken];
export function lostCoverLetterGeneration() {
 return Object.assign(new Error('Право на генерацию письма истекло или данные изменились. Обновите карточку и повторите запрос.'),{status:409,code:'generation_lease_lost'});
}

export async function readCoverLetterGeneration(db,userName,vacancyId) {
 return db.prepare(`SELECT generation,input_hash AS inputHash,owner_token AS ownerToken,
 expires_at AS expiresAt,expires_at>${nowSql} AS active FROM cover_letter_generation_leases
 WHERE user_name=? AND vacancy_id=?`).bind(userName,vacancyId).first();
}
export function coverLetterGenerationDb(db,lease,input) {
 const guard=coverLetterSavedGuard(input,lease.userName);
 return generationBoundDb(db,lease.generation,{
  sql:`${coverLetterLeaseSql} AND EXISTS (SELECT 1 FROM ${coverLetterInputFromSql}
   WHERE v.id=? AND ${coverLetterInputSql}=? AND ${guard.sql})`,
  bindings:[...leaseBindings(lease),lease.userName,lease.vacancyId,input.snapshot,...guard.bindings],
  failure:lostCoverLetterGeneration,
 });
}
