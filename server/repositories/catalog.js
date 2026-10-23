// Server-only; extracted from the v218 Worker.
import {ensureCrmSchema} from './schema.js';
import {normalizeCatalog,normalizeCompanySalaryRange,calculateAgencyRating} from '../services/catalog.js';
import {DATA} from '../data.js';
import {RATING_FORMULA_VERSION} from '../constants.js';

export function isValidCatalog(value) {
  return Boolean(
    value &&
    typeof value === "object" &&
    Array.isArray(value.companies) &&
    value.companies.every((company) => company && typeof company.name === "string" && company.name.trim())
  );
}

export async function readAppCatalog(db,{persistRatings=true}={}) {
  await ensureCrmSchema(db);
  const row = await db.prepare("SELECT data_json AS dataJson FROM app_catalog WHERE id = 1").first();
  if (!row?.dataJson) return normalizeCatalog(DATA);
  let source = DATA;
  let catalogPersistable = false;
  try {
    const parsed = JSON.parse(row.dataJson);
    if (isValidCatalog(parsed)) {
      source = parsed;
      catalogPersistable = true;
    }
  } catch {
    source = DATA;
  }
  const ratingRefreshRequired = catalogPersistable && source.companies.some(
    (company) => Number(company?.ratingFormulaVersion) !== RATING_FORMULA_VERSION
  );
  const catalog = normalizeCatalog(ratingRefreshRequired
    ? { ...source, updated: new Date().toISOString().slice(0, 10) }
    : source);
  if (ratingRefreshRequired&&persistRatings) {
    await db.prepare(
      `INSERT INTO app_catalog (id, data_json, updated_at)
       VALUES (1, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`
    ).bind(JSON.stringify(catalog)).run();
  }
  return catalog;
}

export async function recalculateAgencySalaryRanges(db) {
  const catalog = await readAppCatalog(db);
  if (!catalog.agencies.length) return { updated: 0 };
  const result = await db.prepare(
    `SELECT company_name AS agencyName, LOWER(level) AS level, salary_from AS salaryFrom, salary_to AS salaryTo
     FROM company_vacancies
     WHERE entity_type = 'agency' AND level <> '' AND (salary_from IS NOT NULL OR salary_to IS NOT NULL)`
  ).all();
  const aggregates = new Map();
  for (const row of result?.results || []) {
    const level = String(row.level || "").toLowerCase();
    if (!["middle", "senior", "lead", "head"].includes(level)) continue;
    const key = `${row.agencyName}\u0000${level}`, current = aggregates.get(key) || { from: [], to: [] };
    if (row.salaryFrom != null) current.from.push(Number(row.salaryFrom));
    if (row.salaryTo != null) current.to.push(Number(row.salaryTo));
    aggregates.set(key, current);
  }
  let updated = 0;
  const agencies = catalog.agencies.map((agency) => {
    const next = { ...agency, salaryRangeSource: { ...agency.salaryRangeSource } };
    for (const level of ["middle", "senior", "lead", "head"]) {
      const currentRange = normalizeCompanySalaryRange(agency[level]);
      const canReplace = agency.salaryRangeSource?.[level] === "vacancies" || (!currentRange[0] && !currentRange[1]);
      if (!canReplace) continue;
      const values = aggregates.get(`${agency.name}\u0000${level}`);
      const derived = values ? [values.from.length ? Math.min(...values.from) : null, values.to.length ? Math.max(...values.to) : null] : [null, null];
      if (currentRange[0] !== derived[0] || currentRange[1] !== derived[1] || (derived[0] || derived[1]) && agency.salaryRangeSource?.[level] !== "vacancies") updated += 1;
      next[level] = derived;
      next.salaryRangeSource[level] = derived[0] || derived[1] ? "vacancies" : "manual";
    }
    return next;
  });
  if (updated) {
    const nextCatalog = normalizeCatalog({ ...catalog, agencies, updated: new Date().toISOString().slice(0, 10) });
    await db.prepare(`INSERT INTO app_catalog (id, data_json, updated_at) VALUES (1, ?, CURRENT_TIMESTAMP) ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`).bind(JSON.stringify(nextCatalog)).run();
  }
  return { updated };
}

export async function recalculateCompanyRating(db) {
  const catalog = await readAppCatalog(db);
  let agenciesRecalculated = 0;
  let agencies = catalog.agencies.map((agency) => {
    const rating = calculateAgencyRating(agency);
    if (!rating) return agency;
    agenciesRecalculated += 1;
    return { ...agency, ...rating };
  });
  if (agenciesRecalculated) {
    const descending = (left, right, field) => {
      const leftRaw = left?.[field], rightRaw = right?.[field];
      const leftValue = leftRaw == null || String(leftRaw).trim() === "" ? NaN : Number(leftRaw);
      const rightValue = rightRaw == null || String(rightRaw).trim() === "" ? NaN : Number(rightRaw);
      const leftNumber = Number.isFinite(leftValue) ? leftValue : Number.NEGATIVE_INFINITY;
      const rightNumber = Number.isFinite(rightValue) ? rightValue : Number.NEGATIVE_INFINITY;
      return rightNumber - leftNumber;
    };
    agencies = agencies
      .slice()
      .sort((left, right) => descending(left, right, "score") || descending(left, right, "reviewsScore") || descending(left, right, "relevanceScore") || descending(left, right, "revenueScore") || left.name.localeCompare(right.name, "ru"))
      .map((agency, index) => ({ ...agency, rank: index + 1 }));
  }
  const nextCatalog = normalizeCatalog({ ...catalog, agencies, updated: new Date().toISOString().slice(0, 10) });
  await db.prepare(
    `INSERT INTO app_catalog (id, data_json, updated_at)
     VALUES (1, ?, CURRENT_TIMESTAMP)
     ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`
  ).bind(JSON.stringify(nextCatalog)).run();
  return {
    ok: true,
    companies: nextCatalog.companies.length,
    agencies: agenciesRecalculated,
    agenciesSkipped: nextCatalog.agencies.length - agenciesRecalculated
  };
}

export async function readCompanyNames(db) {
  const catalog = await readAppCatalog(db);
  return new Set([
    ...catalog.companies.map((company) => company.name),
    ...catalog.agencies.map((agency) => agency.name)
  ]);
}

export async function ensureCompanyIdentities(db, companies) {
  // Most calls only need stable IDs. Avoid hundreds of no-op INSERTs on every
  // candidate/stage lookup; the guarded insert remains safe across requests.
  const read=async()=>new Map(((await db.prepare('SELECT id,company_name AS companyName FROM company_identity').all()).results||[]).map(r=>[r.companyName,r.id]));
  let identities=await read();const missing=[...new Set(companies.map(c=>c.name))].filter(name=>!identities.has(name));
  if(missing.length){await db.prepare('INSERT OR IGNORE INTO company_identity(company_name) SELECT value FROM json_each(?)').bind(JSON.stringify(missing)).run();identities=await read();}
  return identities;
}

export async function ensureAgencyIdentities(db, agencies) {
 const read=async()=>new Map(((await db.prepare('SELECT id,agency_name AS agencyName FROM agency_identity').all()).results||[]).map(r=>[r.agencyName,r.id]));
 let identities=await read();const missing=[...new Set(agencies.map(a=>a.name))].filter(name=>!identities.has(name));
 if(missing.length){await db.prepare('INSERT OR IGNORE INTO agency_identity(agency_name) SELECT value FROM json_each(?)').bind(JSON.stringify(missing)).run();identities=await read();}
 return identities;
}
