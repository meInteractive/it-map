// Server-only; extracted from the v218 Worker.
import {timingSafeEqual} from '../auth/session.js';
import {ensureCrmSchema} from '../repositories/schema.js';
import {readAppCatalog,ensureCompanyIdentities,ensureAgencyIdentities} from '../repositories/catalog.js';
import {readFavoriteCompanyNames} from '../repositories/applications.js';
import {vacancyInput,importVacancies} from '../repositories/vacancies.js';

export function automationAuthorized(request, env) {
  const expectedToken = String(env?.VACANCY_AUTOMATION_TOKEN || "");
  const authorization = String(request.headers.get("authorization") || "");
  const submittedToken = authorization.startsWith("Bearer ") ? authorization.slice(7) : "";
  return Boolean(expectedToken && submittedToken && timingSafeEqual(submittedToken, expectedToken));
}

export async function readFavoriteVacancyTargets(db) {
  await ensureCrmSchema(db);
  const [catalog, favoriteNames] = await Promise.all([
    readAppCatalog(db),
    readFavoriteCompanyNames(db, "Eugene")
  ]);
  const favorites = new Set(favoriteNames);
  return [
    ...catalog.companies.map((company) => ({ ...company, entityType: "company" })),
    ...catalog.agencies.map((agency) => ({ ...agency, entityType: "agency" }))
  ].filter((item) => favorites.has(item.name)).map((item) => ({
    name: item.name,
    entityType: item.entityType,
    vacancySources: item.vacancySources || {}
  }));
}

export function automationVacancyIsEligible(input) {
  const now = new Date();
  const sixMonthsAgo = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 6, now.getUTCDate()));
  if (!input.descriptionMd.trim()) throw new Error("Нужен полный текст вакансии.");
  if (input.publishedAt && new Date(`${input.publishedAt}T00:00:00Z`) < sixMonthsAgo) {
    throw new Error("Вакансия опубликована более шести месяцев назад.");
  }
  if (input.salaryFrom != null && input.salaryFrom < 200000) {
    throw new Error("Минимальная зарплата ниже 200 000 ₽.");
  }
}

export async function importAutomationVacancies(db, rows) {
  if (!Array.isArray(rows) || !rows.length) throw new Error("Не переданы вакансии для добавления.");
  if (rows.length > 100) throw new Error("За один запуск можно добавить не более 100 вакансий.");
  const [catalog, targets] = await Promise.all([readAppCatalog(db), readFavoriteVacancyTargets(db)]);
  const targetsByName = new Map(targets.map((target) => [target.name, target]));
  const [companyIds, agencyIds] = await Promise.all([
    ensureCompanyIdentities(db, catalog.companies),
    ensureAgencyIdentities(db, catalog.agencies)
  ]);
  const normalized = rows.map((row) => {
    const companyName = String(row?.companyName || "").trim();
    const entityType = String(row?.entityType || "company").trim().toLowerCase() === "agency" ? "agency" : "company";
    const target = targetsByName.get(companyName);
    if (!target || target.entityType !== entityType) throw new Error("Вакансия может быть добавлена только для избранной организации.");
    const input = vacancyInput({ ...row, companyName, entityType }, { allowEmptyFields: false });
    automationVacancyIsEligible(input);
    if (entityType === "agency") return { ...input, entityType: "Агентство", agencyId: agencyIds.get(companyName), agencyName: companyName };
    return { ...input, entityType: "Компания", companyId: companyIds.get(companyName) };
  });
  // First create genuinely new records; then refresh every known URL. Running the
  // update pass second preserves one record per URL while also keeping the full
  // text current when an existing vacancy is found again.
  const created = await importVacancies(db, "Eugene", normalized, "create");
  const refreshed = await importVacancies(db, "Eugene", normalized, "update");
  return {
    ...refreshed,
    imported: created.imported,
    duplicates: created.duplicates,
    errors: [...(created.errors || []), ...(refreshed.errors || [])],
    processedAt: new Date().toISOString()
  };
}
