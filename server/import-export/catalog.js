// Server-only; extracted from the v218 Worker.
import {normalizeAgency,normalizeCatalog,normalizeCompanySalaryRange} from '../services/catalog.js';
import {AGENCY_TYPE_OPTIONS,AGENCY_FUNCTION_OPTIONS,AGENCY_POSITION_LEVEL_OPTIONS,AGENCY_EXECUTIVE_SEARCH_OPTIONS,AGENCY_CLOSED_VACANCIES_OPTIONS,AGENCY_VACANCY_PUBLICITY_OPTIONS,AGENCY_EMPLOYER_DISCLOSURE_OPTIONS,AGENCY_SALARY_DISCLOSURE_OPTIONS,AGENCY_PRIMARY_CHANNEL_OPTIONS,AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS,AGENCY_VACANCY_FRESHNESS_OPTIONS,AGENCY_ACCEPTS_CV_OPTIONS,AGENCY_CONTACT_METHOD_OPTIONS,MAX_HIRING_CONTACT_IMPORT_ROWS,RATING_FORMULA_VERSION,MAX_COMPANY_IMPORT_ROWS,MAX_COMPANY_OFFICE_IMPORT_ROWS} from '../constants.js';
import {readAppCatalog,ensureAgencyIdentities,recalculateAgencySalaryRanges,ensureCompanyIdentities} from '../repositories/catalog.js';
import {ensureCrmSchema} from '../repositories/schema.js';
import {hiringContactInput} from '../repositories/contacts.js';
import {readFavoriteCompanyNames,readHiddenCompanyNames} from '../repositories/applications.js';

export function agencyImportChoice(value, label, options) {
  const text = companyImportText(value, label, 256);
  if (!text) return "";
  const match = options.find((option) => option.toLocaleLowerCase("ru") === text.toLocaleLowerCase("ru"));
  if (!match) throw new Error(`${label}: допустимые значения — ${options.join(", ")}`);
  return match;
}

export function agencyImportMultiChoice(value, label, options) {
  const text = companyImportText(value, label, 2000);
  if (!text) return [];
  const values = text.split(";").map((item) => item.trim()).filter(Boolean);
  const optionMap = new Map(options.map((option) => [option.toLocaleLowerCase("ru"), option]));
  const invalid = values.filter((item) => !optionMap.has(item.toLocaleLowerCase("ru")));
  if (invalid.length) throw new Error(`${label}: недопустимые значения — ${invalid.join(", ")}`);
  return [...new Set(values.map((item) => optionMap.get(item.toLocaleLowerCase("ru"))))];
}

export function agencyImportFieldPresent(row, field) {
  return Array.isArray(row?.__presentFields)
    ? row.__presentFields.includes(field)
    : Object.prototype.hasOwnProperty.call(row || {}, field);
}

export function agencyImportRatingInput(row, field, label, fallback = null) {
  if (!agencyImportFieldPresent(row, field)) return fallback;
  if (row?.[field] == null || String(row[field]).trim() === "") return null;
  const number = companyImportNumber(row[field], label, { min: 0, max: 10 });
  if (Math.abs(number * 2 - Math.round(number * 2)) > 1e-9) throw new Error(`${label}: допустим шаг 0,5`);
  return Math.round(number * 2) / 2;
}

export function agencyImportInput(row, current = null) {
  const provided = (field) => companyImportValueProvided(row, field);
  const name = provided("name") ? companyImportText(row.name, "Название агентства", 300) : String(current?.name || "").trim();
  if (!name) throw new Error("название агентства обязательно");
  const base = normalizeAgency(current || { name });
  const range = (level) => {
    const fromField = `${level}From`, toField = `${level}To`;
    if (!provided(fromField) && !provided(toField)) return base[level];
    const from = companyImportNumber(row[fromField], `${level} — от`, { integer: true, min: 0 });
    const to = companyImportNumber(row[toField], `${level} — до`, { integer: true, min: 0 });
    if (from != null && to != null && from > to) throw new Error(`${level}: граница «от» не может быть выше границы «до»`);
    return [from, to];
  };
  const url = (field, label, fallback = "") => provided(field) ? companyImportUrl(row[field], label) : fallback;
  const text = (field, label, max, fallback = "") => provided(field) ? companyImportText(row[field], label, max) : fallback;
  const rank = provided("rank") ? companyImportNumber(row.rank, "Место в рейтинге соискателей", { integer: true, min: 1 }) : base.rank;
  const email = text("applicationEmail", "Почта для отклика", 1000, base.applicationEmail);
  if (email && email.split(";").some((value) => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.trim()))) throw new Error("Почта для отклика: укажите корректный email или несколько адресов через точку с запятой");
  const salaryRangeSource = { ...base.salaryRangeSource };
  for (const level of ["middle", "senior", "lead", "head"]) if (provided(`${level}From`) || provided(`${level}To`)) salaryRangeSource[level] = "manual";
  return normalizeAgency({
    ...base,
    name,
    rank,
    sector: text("sector", "Отрасль", 1000, base.sector),
    agencyTypes: provided("agencyTypes") ? agencyImportMultiChoice(row.agencyTypes, "Тип агентства", AGENCY_TYPE_OPTIONS) : base.agencyTypes,
    functionalSpecializations: provided("functionalSpecializations") ? agencyImportMultiChoice(row.functionalSpecializations, "Функциональная специализация", AGENCY_FUNCTION_OPTIONS) : base.functionalSpecializations,
    positionLevels: provided("positionLevels") ? agencyImportMultiChoice(row.positionLevels, "Уровень позиций", AGENCY_POSITION_LEVEL_OPTIONS) : base.positionLevels,
    roles: text("roles", "Подходящие роли / направления", 2000, base.roles),
    ethics: agencyImportRatingInput(row, "ethics", "Этика", base.ethics),
    vacancyHonesty: agencyImportRatingInput(row, "vacancyHonesty", "Честность вакансий", base.vacancyHonesty),
    feedback: agencyImportRatingInput(row, "feedback", "Обратная связь", base.feedback),
    vacancyRelevance: agencyImportRatingInput(row, "vacancyRelevance", "Релевантность вакансий", base.vacancyRelevance),
    itExpertise: agencyImportRatingInput(row, "itExpertise", "IT-экспертиза", base.itExpertise),
    clientKnowledge: agencyImportRatingInput(row, "clientKnowledge", "Знание клиента", base.clientKnowledge),
    processOrganization: agencyImportRatingInput(row, "processOrganization", "Организация процесса", base.processOrganization),
    candidateSupport: agencyImportRatingInput(row, "candidateSupport", "Поддержка кандидата", base.candidateSupport),
    executiveSearch: provided("executiveSearch") ? agencyImportChoice(row.executiveSearch, "Executive Search", AGENCY_EXECUTIVE_SEARCH_OPTIONS) : base.executiveSearch,
    closedVacancies: provided("closedVacancies") ? agencyImportChoice(row.closedVacancies, "Закрытые вакансии", AGENCY_CLOSED_VACANCIES_OPTIONS) : base.closedVacancies,
    vacancyPublicity: provided("vacancyPublicity") ? agencyImportChoice(row.vacancyPublicity, "Публичность вакансий", AGENCY_VACANCY_PUBLICITY_OPTIONS) : base.vacancyPublicity,
    employerDisclosure: provided("employerDisclosure") ? agencyImportChoice(row.employerDisclosure, "Работодатель в вакансии", AGENCY_EMPLOYER_DISCLOSURE_OPTIONS) : base.employerDisclosure,
    salaryDisclosure: provided("salaryDisclosure") ? agencyImportChoice(row.salaryDisclosure, "Зарплата в вакансии", AGENCY_SALARY_DISCLOSURE_OPTIONS) : base.salaryDisclosure,
    middle: range("middle"), senior: range("senior"), lead: range("lead"), head: range("head"), salaryRangeSource,
    vacancySources: {
      companySite: url("companySite", "Сайт компании", base.vacancySources.companySite), hh: url("hh", "HH", base.vacancySources.hh), habr: url("habr", "Хабр", base.vacancySources.habr), getmatch: url("getmatch", "Getmatch", base.vacancySources.getmatch), telegram: url("telegram", "Telegram", base.vacancySources.telegram)
    },
    primaryVacancyChannel: provided("primaryVacancyChannel") ? agencyImportChoice(row.primaryVacancyChannel, "Основной канал вакансий", AGENCY_PRIMARY_CHANNEL_OPTIONS) : base.primaryVacancyChannel,
    publicSourcesQuality: provided("publicSourcesQuality") ? agencyImportChoice(row.publicSourcesQuality, "Качество публичных источников вакансий", AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS) : base.publicSourcesQuality,
    vacancyFreshness: provided("vacancyFreshness") ? agencyImportChoice(row.vacancyFreshness, "Актуальность вакансий", AGENCY_VACANCY_FRESHNESS_OPTIONS) : base.vacancyFreshness,
    acceptsCv: provided("acceptsCv") ? agencyImportChoice(row.acceptsCv, "Принимает CV в базу", AGENCY_ACCEPTS_CV_OPTIONS) : base.acceptsCv,
    recommendedContactMethods: provided("recommendedContactMethods") ? agencyImportMultiChoice(row.recommendedContactMethods, "Рекомендуемый способ контакта", AGENCY_CONTACT_METHOD_OPTIONS) : base.recommendedContactMethods,
    workingGuide: text("workingGuide", "Как работать с агентством", 512, base.workingGuide),
    score: provided("score") ? companyImportNumber(row.score, "Итоговая оценка", { min: 0, max: 100 }) : base.score,
    confidence: provided("confidence") ? agencyImportChoice(row.confidence, "Уверенность в данных", ["Высокая", "Средняя", "Низкая"]) : base.confidence,
    risk: text("risk", "Риски / что проверить", 2000, base.risk),
    reviewSources: {
      habr: url("reviewHabr", "Habr", base.reviewSources.habr), dreamJob: url("reviewDreamJob", "DreamJob", base.reviewSources.dreamJob), glassDoor: url("reviewGlassDoor", "GlassDoor", base.reviewSources.glassDoor), superJob: url("reviewSuperJob", "SuperJob", base.reviewSources.superJob), pravdaSotrudnikov: url("reviewPravdaSotrudnikov", "Правда Сотрудников", base.reviewSources.pravdaSotrudnikov), nahjob: url("reviewNahjob", "Nahjob", base.reviewSources.nahjob), bossNadzor: url("reviewBossNadzor", "BossNadzor", base.reviewSources.bossNadzor), yandexMap: url("reviewYandexMap", "YandexMap", base.reviewSources.yandexMap), twoGis: url("reviewTwoGis", "2GIS", base.reviewSources.twoGis), otzovik: url("reviewOtzovik", "Otzovik", base.reviewSources.otzovik), personAgency: url("reviewPersonAgency", "PersonAgency", base.reviewSources.personAgency), yell: url("reviewYell", "Yell", base.reviewSources.yell)
    },
    reviews: {
      plusShort: text("reviewsPlusShort", "Plus Short", 512, base.reviews.plusShort), minusShort: text("reviewsMinusShort", "Minus Short", 512, base.reviews.minusShort), plus: text("reviewsPlus", "Plus", 2000, base.reviews.plus), minus: text("reviewsMinus", "Minus", 2000, base.reviews.minus)
    },
    revenue: {
      ...(base.revenue || {}),
      amount: provided("revenueAmount") ? companyImportNumber(row.revenueAmount, "Выручка — сумма", { min: 0 }) : base.revenue?.amount ?? null,
      year: provided("revenueYear") ? companyImportNumber(row.revenueYear, "Выручка — год", { integer: true, min: 1900, max: 2100 }) : base.revenue?.year ?? null,
      metric: text("revenueMetric", "Выручка — метрика", 500, base.revenue?.metric),
      legal: text("revenueLegal", "Выручка — юрлицо", 500, base.revenue?.legal),
      inn: text("revenueInn", "Выручка — ИНН", 20, base.revenue?.inn),
      composition: text("revenueComposition", "Выручка — состав суммы", 2000, base.revenue?.composition),
      coverage: text("revenueCoverage", "Выручка — охват показателя", 1000, base.revenue?.coverage),
      status: text("revenueStatus", "Выручка — статус подтверждения", 500, base.revenue?.status),
      source1: url("revenueSource1", "Выручка — источник 1", base.revenue?.source1),
      source2: url("revenueSource2", "Выручка — источник 2", base.revenue?.source2),
      comment: text("revenueComment", "Выручка — комментарий", 2000, base.revenue?.comment)
    },
    applicationUrl: url("applicationUrl", "Ссылка для отклика", base.applicationUrl),
    applicationEmail: email
  });
}

export async function importAgencies(db, rows, mode = "create") {
  if (!Array.isArray(rows) || !rows.length) return { agenciesAdded: 0, agenciesUpdated: 0, agenciesSkipped: 0, agencyErrors: [] };
  const catalog = await readAppCatalog(db);
  const identities = await ensureAgencyIdentities(db, catalog.agencies);
  const byId = new Map([...identities.entries()].map(([name, id]) => [Number(id), name]));
  const byName = new Map(catalog.agencies.map((agency, index) => [agency.name.toLocaleLowerCase("ru"), index]));
  const next = catalog.agencies.slice();
  const errors = [];
  let agenciesAdded = 0, agenciesUpdated = 0, agenciesSkipped = 0;
  for (let index = 0; index < rows.length; index += 1) {
    const row = rows[index], rowNumber = Number(row?.row || index + 2);
    try {
      const nameKey = String(row?.name || "").trim().toLocaleLowerCase("ru");
      const idName = Number.isSafeInteger(Number(row?.agencyId)) ? byId.get(Number(row.agencyId)) : "";
      const existingIndex = idName ? next.findIndex((agency) => agency.name === idName) : byName.get(nameKey);
      if (mode === "create") {
        if (existingIndex != null && existingIndex >= 0) { agenciesSkipped += 1; continue; }
        const agency = agencyImportInput(row);
        next.push(agency);byName.set(agency.name.toLocaleLowerCase("ru"), next.length - 1);agenciesAdded += 1;
      } else {
        if (existingIndex == null || existingIndex < 0) { agenciesSkipped += 1; continue; }
        next[existingIndex] = agencyImportInput(row, next[existingIndex]);agenciesUpdated += 1;
      }
    } catch (error) {
      agenciesSkipped += 1;
      if (errors.length < 100) errors.push({ sheet: "Агентства", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }
  const nextCatalog = normalizeCatalog({ ...catalog, agencies: next, updated: new Date().toISOString().slice(0, 10) });
  await db.prepare(`INSERT INTO app_catalog (id, data_json, updated_at) VALUES (1, ?, CURRENT_TIMESTAMP) ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`).bind(JSON.stringify(nextCatalog)).run();
  await ensureAgencyIdentities(db, nextCatalog.agencies);
  const names = nextCatalog.agencies.map((agency) => agency.name);
  for (let index = 0; index < names.length; index += 50) await db.batch(names.slice(index, index + 50).map((name) => db.prepare("INSERT OR IGNORE INTO company_crm (company_name) VALUES (?)").bind(name)));
  await recalculateAgencySalaryRanges(db);
  return { agenciesAdded, agenciesUpdated, agenciesSkipped, agencyErrors: errors };
}

export async function importHiringContacts(db, rows, mode = "create") {
  if (!Array.isArray(rows) || !rows.length) return { contactsAdded: 0, contactsUpdated: 0, contactsSkipped: 0, contactErrors: [] };
  if (!["create", "update"].includes(mode)) throw new Error("Указан неизвестный режим импорта контактов.");
  if (rows.length > MAX_HIRING_CONTACT_IMPORT_ROWS) throw new Error(`За один раз можно импортировать не более ${MAX_HIRING_CONTACT_IMPORT_ROWS} контактов.`);
  await ensureCrmSchema(db);
  const catalog = await readAppCatalog(db);
  const [companyIdentities, agencyIdentities] = await Promise.all([
    ensureCompanyIdentities(db, catalog.companies),
    ensureAgencyIdentities(db, catalog.agencies)
  ]);
  const companyNames = new Set(catalog.companies.map((company) => company.name));
  const agencyNames = new Set(catalog.agencies.map((agency) => agency.name));
  const companyById = new Map([...companyIdentities.entries()].filter(([name]) => companyNames.has(name)).map(([name, id]) => [Number(id), name]));
  const agencyById = new Map([...agencyIdentities.entries()].filter(([name]) => agencyNames.has(name)).map(([name, id]) => [Number(id), name]));
  const companyByName = new Map(catalog.companies.map((company) => [company.name.toLocaleLowerCase("ru"), company.name]));
  const agencyByName = new Map(catalog.agencies.map((agency) => [agency.name.toLocaleLowerCase("ru"), agency.name]));
  const existingResult = await db.prepare(
    `SELECT company_name AS companyName, position, full_name AS fullName,
            linkedin_url AS linkedinUrl, facebook_url AS facebookUrl, setka_url AS setkaUrl, telegram, phone, email, specialization
     FROM hiring_contacts`
  ).all();
  const existing = new Map((existingResult?.results || []).map((contact) => [`${contact.companyName}\u0000${contact.position}`, contact]));
  const statements = [];
  const errors = [];
  let contactsAdded = 0, contactsUpdated = 0, contactsSkipped = 0;

  for (let index = 0; index < rows.length; index += 1) {
    const row = rows[index] || {}, rowNumber = Number(row.row || index + 2);
    try {
      const type = String(row.entityType || "").trim().toLocaleLowerCase("ru");
      const isAgency = type === "агентство" || type === "agency";
      if (!isAgency && type !== "компания" && type !== "company") throw new Error("тип организации должен быть «Компания» или «Агентство»");
      const idText = String(row.entityId ?? "").trim();
      const entityId = idText ? Number(idText) : null;
      if (idText && (!Number.isSafeInteger(entityId) || entityId < 1)) throw new Error("ID организации должен быть положительным целым числом");
      const byId = isAgency ? agencyById : companyById;
      const byName = isAgency ? agencyByName : companyByName;
      const nameText = String(row.entityName || "").trim();
      const idName = entityId == null ? "" : byId.get(entityId);
      const enteredName = nameText ? byName.get(nameText.toLocaleLowerCase("ru")) : "";
      if (entityId != null && !idName && !enteredName) throw new Error("организация с таким ID не найдена");
      if (nameText && !enteredName) throw new Error("организация с таким названием не найдена");
      if (idName && enteredName && idName !== enteredName) throw new Error("ID и название указывают на разные организации");
      const companyName = enteredName || idName;
      if (!companyName) throw new Error("укажите ID или название организации");
      const position = Number(row.position);
      const key = `${companyName}\u0000${position}`;
      const current = existing.get(key);
      const inputRow = mode === "update" && current ? {
        ...row,
        fullName: String(row.fullName || "").trim() ? row.fullName : current.fullName,
        linkedinUrl: String(row.linkedinUrl || "").trim() ? row.linkedinUrl : current.linkedinUrl,
        facebookUrl: String(row.facebookUrl || "").trim() ? row.facebookUrl : current.facebookUrl,
        setkaUrl: String(row.setkaUrl || "").trim() ? row.setkaUrl : current.setkaUrl,
        telegram: String(row.telegram || "").trim() ? row.telegram : current.telegram,
        phone: String(row.phone || "").trim() ? row.phone : current.phone,
        email: String(row.email || "").trim() ? row.email : current.email,
        specialization: String(row.specialization || "").trim() ? row.specialization : current.specialization
      } : row;
      const input = hiringContactInput(inputRow, companyName);
      const hasExisting = Boolean(current);
      if (mode === "create" && hasExisting) {
        contactsSkipped += 1;
        continue;
      }
      statements.push(db.prepare("INSERT OR IGNORE INTO company_crm (company_name) VALUES (?)").bind(companyName));
      if (!hasExisting) {
        statements.push(db.prepare(
          `INSERT INTO hiring_contacts (company_name, position, full_name, linkedin_url, facebook_url, setka_url, telegram, phone, email, specialization)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
        ).bind(companyName, input.position, input.fullName, input.linkedinUrl, input.facebookUrl, input.setkaUrl, input.telegram, input.phone, input.email, input.specialization));
        existing.set(key, input);
        contactsAdded += 1;
      } else if (mode === "update") {
        statements.push(db.prepare(
          `UPDATE hiring_contacts
           SET full_name = ?, linkedin_url = ?, facebook_url = ?, setka_url = ?, telegram = ?, phone = ?, email = ?, specialization = ?, updated_at = CURRENT_TIMESTAMP
           WHERE company_name = ? AND position = ?`
        ).bind(input.fullName, input.linkedinUrl, input.facebookUrl, input.setkaUrl, input.telegram, input.phone, input.email, input.specialization, companyName, input.position));
        contactsUpdated += 1;
      }
    } catch (error) {
      contactsSkipped += 1;
      if (errors.length < 100) errors.push({ sheet: "Контакты", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }
  for (let index = 0; index < statements.length; index += 50) await db.batch(statements.slice(index, index + 50));
  return { contactsAdded, contactsUpdated, contactsSkipped, contactErrors: errors };
}

export function companyImportText(value, label, maxLength = 10000) {
  const text = String(value ?? "").trim();
  if (text.length > maxLength) throw new Error(`${label}: значение длиннее ${maxLength} символов`);
  return text;
}

export function companyImportNumber(value, label, options = {}) {
  if (value == null || String(value).trim() === "") return null;
  const number = typeof value === "number" ? value : Number(String(value).replace(/\s/g, "").replace(",", "."));
  if (!Number.isFinite(number)) throw new Error(`${label}: укажите число`);
  if (options.integer && !Number.isInteger(number)) throw new Error(`${label}: укажите целое число`);
  if (options.min != null && number < options.min) throw new Error(`${label}: значение должно быть не меньше ${options.min}`);
  if (options.max != null && number > options.max) throw new Error(`${label}: значение должно быть не больше ${options.max}`);
  return number;
}

export function companyImportUrl(value, label) {
  const text = companyImportText(value, label, 2048);
  if (!text) return "";
  try {
    const url = new URL(text);
    if (!["http:", "https:"].includes(url.protocol)) throw new Error();
    return url.href;
  } catch {
    throw new Error(`${label}: укажите полный URL, начинающийся с http:// или https://`);
  }
}

export function companyImportEnum(value, label, allowed) {
  const text = companyImportText(value, label, 100);
  if (text && !allowed.includes(text)) throw new Error(`${label}: используйте ${allowed.join(", ")}`);
  return text;
}

export function companyImportInns(value, label = "Выручка — ИНН") {
  const text = companyImportText(value, label, 256).replace(/\.0$/, "");
  if (!text) return "";
  const inns = text.split(/[;,]/).map((item) => item.trim().replace(/\.0$/, "")).filter(Boolean);
  if (!inns.length || inns.some((inn) => !/^(\d{10}|\d{12})$/.test(inn))) {
    throw new Error(`${label}: укажите один или несколько ИНН из 10 или 12 цифр, разделяя их точкой с запятой`);
  }
  return [...new Set(inns)].join("; ");
}

export function companyImportDate(value, label) {
  if (value == null || String(value).trim() === "") return "";
  if (typeof value === "number" && Number.isFinite(value)) {
    const date = new Date(Math.round((value - 25569) * 86400000));
    if (!Number.isNaN(date.getTime())) return date.toISOString().slice(0, 10);
  }
  const text = String(value).trim();
  const match = text.match(/^(\d{1,2})[.\/-](\d{1,2})[.\/-](\d{4})$/);
  const normalized = match ? `${match[3]}-${match[2].padStart(2, "0")}-${match[1].padStart(2, "0")}` : text;
  if (!/^\d{4}-\d{2}-\d{2}$/.test(normalized) || Number.isNaN(new Date(`${normalized}T00:00:00Z`).getTime())) {
    throw new Error(`${label}: используйте дату в формате ГГГГ-ММ-ДД`);
  }
  return normalized;
}

export function companyDuplicateTextKey(value) {
  return String(value || "").trim().replace(/\s+/g, " ").toLocaleLowerCase("ru");
}

export function companyDuplicateUrlKey(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  try {
    const url = new URL(text);
    const host = url.hostname.toLowerCase().replace(/^www\./, "");
    const path = decodeURIComponent(url.pathname).replace(/\/+$/, "").toLocaleLowerCase("ru") || "/";
    return `${host}${path}`;
  } catch {
    return text.toLocaleLowerCase("ru").replace(/^https?:\/\//, "").replace(/^www\./, "").replace(/[?#].*$/, "").replace(/\/+$/, "");
  }
}

export function companyAiImportUpdate(row, company = {}) {
  const scoreProvided = companyImportValueProvided(row, "aiScore");
  const evidenceProvided = companyImportValueProvided(row, "aiEvidence");
  const aiScore = scoreProvided ? companyImportNumber(row.aiScore, "Балл ИИ", { integer: true, min: 0, max: 10 }) : company.aiScore ?? null;
  const aiEvidence = evidenceProvided ? companyImportText(row.aiEvidence, "ИИ — обоснование и источники") : company.aiEvidence || "";
  if (aiScore != null && ![0, 5, 10].includes(aiScore)) throw new Error("Балл ИИ: допустимы только 0, 5 или 10");
  if (aiScore > 0 && !aiEvidence.trim()) throw new Error("Для баллов за ИИ укажите обоснование и источники");
  return { aiScore, aiEvidence, provided: scoreProvided || evidenceProvided };
}

export function companyImportInput(row) {
  const aiData = companyAiImportUpdate(row);
  const name = companyImportText(row?.name, "Название компании", 300);
  if (!name) throw new Error("не указано название компании");
  const score = companyImportNumber(row?.score, "Итоговая оценка", { integer: true, min: 0, max: 100 });
  const rating = companyImportNumber(row?.rating, "Рейтинг сотрудников", { min: 0, max: 5 });
  const wlbScore = companyImportNumber(row?.wlbScore, "Балл WLB", { integer: true, min: 0, max: 100 });
  const middleFrom = companyImportNumber(row?.middleFrom, "Middle — от", { integer: true, min: 0 });
  const middleTo = companyImportNumber(row?.middleTo, "Middle — до", { integer: true, min: 0 });
  const seniorFrom = companyImportNumber(row?.seniorFrom, "Senior — от", { integer: true, min: 0 });
  const seniorTo = companyImportNumber(row?.seniorTo, "Senior — до", { integer: true, min: 0 });
  const leadFrom = companyImportNumber(row?.leadFrom, "Lead — от", { integer: true, min: 0 });
  const leadTo = companyImportNumber(row?.leadTo, "Lead — до", { integer: true, min: 0 });
  const headFrom = companyImportNumber(row?.headFrom, "Head — от", { integer: true, min: 0 });
  const headTo = companyImportNumber(row?.headTo, "Head — до", { integer: true, min: 0 });
  if (middleFrom != null && middleTo != null && middleFrom > middleTo) throw new Error("Middle — от не может быть больше Middle — до");
  if (seniorFrom != null && seniorTo != null && seniorFrom > seniorTo) throw new Error("Senior — от не может быть больше Senior — до");
  if (leadFrom != null && leadTo != null && leadFrom > leadTo) throw new Error("Lead — от не может быть больше Lead — до");
  if (headFrom != null && headTo != null && headFrom > headTo) throw new Error("Head — от не может быть больше Head — до");
  const revenueAmount = companyImportNumber(row?.revenueAmount, "Выручка — сумма", { min: 0 });
  const revenueYear = companyImportNumber(row?.revenueYear, "Выручка — год", { integer: true, min: 1900, max: 2100 });
  const revenueInn = companyImportInns(row?.revenueInn);
  const applicationEmail = companyImportText(row?.applicationEmail, "Почта для отклика", 1000);
  if (applicationEmail && applicationEmail.split(";").some((email) => !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim()))) {
    throw new Error("Почта для отклика: проверьте адрес; несколько адресов разделяйте точкой с запятой");
  }
  return {
    name,
    aiScore: aiData.aiScore,
    aiEvidence: aiData.aiEvidence,
    sector: companyImportText(row?.sector, "Отрасль"),
    roles: companyImportText(row?.roles, "Подходящие роли / направления"),
    score,
    ratingFormulaVersion: RATING_FORMULA_VERSION,
    rating,
    wlb: companyImportText(row?.wlb, "Work-life balance"),
    wlbScore,
    middle: [middleFrom, middleTo],
    senior: [seniorFrom, seniorTo],
    lead: [leadFrom, leadTo],
    head: [headFrom, headTo],
    deferment: companyImportText(row?.deferment, "IT-аккредитация / отсрочка", 2000),
    mobilization: companyImportText(row?.mobilization, "Мобилизация", 2000),
    confidence: companyImportText(row?.confidence, "Уверенность в данных", 1000),
    risk: companyImportText(row?.risk, "Риски / что проверить"),
    jobs: [],
    housing: {
      status: companyImportText(row?.housingStatus, "Жильё / ипотека — статус", 1000),
      form: companyImportText(row?.housingForm, "Жильё / ипотека — формат льготы"),
      terms: companyImportText(row?.housingTerms, "Жильё / ипотека — условия"),
      after: companyImportText(row?.housingAfter, "Жильё / ипотека — что будет после увольнения"),
      source: companyImportText(row?.housingSource, "Жильё / ипотека — источник", 2048) || null
    },
    offices: [],
    revenue: {
      amount: revenueAmount,
      year: revenueYear,
      metric: companyImportText(row?.revenueMetric, "Выручка — метрика"),
      legal: companyImportText(row?.revenueLegal, "Выручка — юрлицо"),
      inn: revenueInn || null,
      composition: companyImportText(row?.revenueComposition, "Выручка — состав суммы"),
      coverage: companyImportText(row?.revenueCoverage, "Выручка — охват показателя"),
      status: companyImportText(row?.revenueStatus, "Выручка — статус подтверждения", 500),
      source1: companyImportUrl(row?.revenueSource1, "Выручка — источник 1") || null,
      source2: companyImportUrl(row?.revenueSource2, "Выручка — источник 2") || null,
      comment: companyImportText(row?.revenueComment, "Выручка — комментарий")
    },
    vpnAi: {
      summary: companyImportText(row?.vpnAiSummary, "VPN и AI — краткое резюме"),
      vpn: companyImportText(row?.vpn, "VPN", 2000),
      ai: companyImportText(row?.ai, "AI", 2000),
      chatgpt: companyImportText(row?.chatgpt, "ChatGPT", 2000),
      claude: companyImportText(row?.claude, "Claude", 2000),
      grok: companyImportText(row?.grok, "Grok", 2000),
      qwen: companyImportText(row?.qwen, "Qwen", 2000),
      deepseek: companyImportText(row?.deepseek, "DeepSeek", 2000),
      data: companyImportText(row?.vpnAiData, "Ограничения по данным"),
      evidence: companyImportText(row?.vpnAiEvidence, "VPN и AI — уровень подтверждения", 1000),
      source1: companyImportText(row?.vpnAiSource1, "VPN и AI — источник 1", 2048) || null,
      source2: companyImportText(row?.vpnAiSource2, "VPN и AI — источник 2", 2048) || null,
      checked: companyImportDate(row?.vpnAiChecked, "VPN и AI — дата проверки"),
      recruiter: companyImportText(row?.vpnAiRecruiter, "VPN и AI — вопрос рекрутеру")
    },
    vacancySources: {
      companySite: companyImportUrl(row?.companySite, "Сайт компании"),
      hh: companyImportUrl(row?.hh, "HH"),
      habr: companyImportUrl(row?.habr, "Хабр"),
      getmatch: companyImportUrl(row?.getmatch, "Getmatch"),
      telegram: companyImportUrl(row?.telegram, "Telegram"),
      dreamJob: companyImportUrl(row?.vacancyDreamJob, "DreamJob"),
      hCareers: companyImportUrl(row?.hCareers, "Эйч"),
      rabota: companyImportUrl(row?.rabota, "rabota.ru")
    },
    reviewSources: {
      habr: companyImportUrl(row?.reviewHabr, "Habr"),
      dreamJob: companyImportUrl(row?.reviewDreamJob, "DreamJob"),
      glassDoor: companyImportUrl(row?.reviewGlassDoor, "GlassDoor"),
      superJob: companyImportUrl(row?.reviewSuperJob, "SuperJob"),
      pravdaSotrudnikov: companyImportUrl(row?.reviewPravdaSotrudnikov, "Правда Сотрудников"),
      nahjob: companyImportUrl(row?.reviewNahjob, "Nahjob"),
      bossNadzor: companyImportUrl(row?.reviewBossNadzor, "BossNadzor"),
      yandexMap: companyImportUrl(row?.reviewYandexMap, "YandexMap")
    },
    reviews: {
      plusShort: companyImportText(row?.reviewsPlusShort, "Plus Short", 512),
      minusShort: companyImportText(row?.reviewsMinusShort, "Minus Short", 512),
      plus: companyImportText(row?.reviewsPlus, "Plus", 2000),
      minus: companyImportText(row?.reviewsMinus, "Minus", 2000)
    },
    applicationUrl: companyImportUrl(row?.applicationUrl, "Ссылка для отклика"),
    applicationEmail
  };
}

export function companyOfficeImportInput(row) {
  const companyName = companyImportText(row?.companyName, "Название компании", 300);
  if (!companyName) throw new Error("не указано название компании");
  const address = companyImportText(row?.address, "Адрес", 1000);
  if (!address) throw new Error("не указан адрес офиса");
  const lat = companyImportNumber(row?.lat, "Широта", { min: -90, max: 90 });
  const lon = companyImportNumber(row?.lon, "Долгота", { min: -180, max: 180 });
  if ((lat == null) !== (lon == null)) throw new Error("широта и долгота должны быть заполнены вместе");
  return {
    companyName,
    office: {
      role: companyImportText(row?.role, "Название / роль офиса", 500),
      address,
      metro: companyImportText(row?.metro, "Метро", 500),
      main: companyImportEnum(row?.main, "Основной офис", ["Да", "Нет"]),
      status: companyImportText(row?.status, "Статус подтверждения", 1000),
      source: companyImportUrl(row?.source, "Источник") || null,
      lat,
      lon
    }
  };
}

export function companySalaryUpdateRange(row, level, currentRange) {
  const fromKey = `${level}From`;
  const toKey = `${level}To`;
  const fromProvided = row?.[fromKey] != null && String(row[fromKey]).trim() !== "";
  const toProvided = row?.[toKey] != null && String(row[toKey]).trim() !== "";
  if (!fromProvided && !toProvided) return { provided: false, range: normalizeCompanySalaryRange(currentRange) };
  const label = level[0].toUpperCase() + level.slice(1);
  const current = normalizeCompanySalaryRange(currentRange);
  const from = fromProvided ? companyImportNumber(row[fromKey], `${label} — от`, { integer: true, min: 0 }) : current[0];
  const to = toProvided ? companyImportNumber(row[toKey], `${label} — до`, { integer: true, min: 0 }) : current[1];
  if (from != null && to != null && from > to) throw new Error(`${label} — от не может быть больше ${label} — до`);
  return { provided: true, range: [from, to] };
}

export function companyImportValueProvided(row, field) {
  return row?.[field] != null && String(row[field]).trim() !== "";
}

export function companyReviewsImportUpdate(row, company) {
  const sourceFields = [
    ["reviewHabr", "habr", "Habr"],
    ["reviewDreamJob", "dreamJob", "DreamJob"],
    ["reviewGlassDoor", "glassDoor", "GlassDoor"],
    ["reviewSuperJob", "superJob", "SuperJob"],
    ["reviewPravdaSotrudnikov", "pravdaSotrudnikov", "Правда Сотрудников"],
    ["reviewNahjob", "nahjob", "Nahjob"],
    ["reviewBossNadzor", "bossNadzor", "BossNadzor"],
    ["reviewYandexMap", "yandexMap", "YandexMap"],
  ];
  const reviewFields = [
    ["reviewsPlusShort", "plusShort", "Plus Short", 512],
    ["reviewsMinusShort", "minusShort", "Minus Short", 512],
    ["reviewsPlus", "plus", "Plus", 2000],
    ["reviewsMinus", "minus", "Minus", 2000],
  ];
  const reviewSources = { ...(company.reviewSources || {}) };
  const reviews = { ...(company.reviews || {}) };
  let provided = false;
  for (const [rowField, key, label] of sourceFields) {
    if (!companyImportValueProvided(row, rowField)) continue;
    reviewSources[key] = companyImportUrl(row[rowField], label);
    provided = true;
  }
  for (const [rowField, key, label, maxLength] of reviewFields) {
    if (!companyImportValueProvided(row, rowField)) continue;
    reviews[key] = companyImportText(row[rowField], label, maxLength);
    provided = true;
  }
  return { provided, reviewSources, reviews };
}

export function companyVpnAiImportUpdate(row, company) {
  const vpnAi = { ...(company.vpnAi || {}) };
  const fields = [
    ["vpnAiSummary", "summary", "VPN и AI — краткое резюме", (value, label) => companyImportText(value, label)],
    ["vpn", "vpn", "VPN", (value, label) => companyImportText(value, label)],
    ["ai", "ai", "AI", (value, label) => companyImportText(value, label)],
    ["chatgpt", "chatgpt", "ChatGPT", (value, label) => companyImportText(value, label)],
    ["claude", "claude", "Claude", (value, label) => companyImportText(value, label)],
    ["grok", "grok", "Grok", (value, label) => companyImportText(value, label)],
    ["qwen", "qwen", "Qwen", (value, label) => companyImportText(value, label)],
    ["deepseek", "deepseek", "DeepSeek", (value, label) => companyImportText(value, label)],
    ["vpnAiData", "data", "Ограничения по данным", (value, label) => companyImportText(value, label)],
    ["vpnAiEvidence", "evidence", "VPN и AI — уровень подтверждения", (value, label) => companyImportText(value, label)],
    ["vpnAiSource1", "source1", "VPN и AI — источник 1", (value, label) => companyImportText(value, label, 2048) || null],
    ["vpnAiSource2", "source2", "VPN и AI — источник 2", (value, label) => companyImportText(value, label, 2048) || null],
    ["vpnAiChecked", "checked", "VPN и AI — дата проверки", (value, label) => companyImportDate(value, label)],
    ["vpnAiRecruiter", "recruiter", "VPN и AI — вопрос рекрутеру", (value, label) => companyImportText(value, label)],
  ];
  let provided = false;
  for (const [rowField, key, label, parse] of fields) {
    if (!companyImportValueProvided(row, rowField)) continue;
    vpnAi[key] = parse(row[rowField], label);
    provided = true;
  }
  return { provided, vpnAi };
}

export function companyHousingImportUpdate(row, company) {
  const housing = { ...(company.housing || {}) };
  const fields = [
    ["housingStatus", "status", "Жильё / ипотека — статус", (value, label) => companyImportText(value, label)],
    ["housingForm", "form", "Жильё / ипотека — формат льготы", (value, label) => companyImportText(value, label)],
    ["housingTerms", "terms", "Жильё / ипотека — условия", (value, label) => companyImportText(value, label)],
    ["housingAfter", "after", "Жильё / ипотека — что будет после увольнения", (value, label) => companyImportText(value, label)],
    ["housingSource", "source", "Жильё / ипотека — источник", (value, label) => companyImportUrl(value, label) || null],
  ];
  let provided = false;
  for (const [rowField, key, label, parse] of fields) {
    if (!companyImportValueProvided(row, rowField)) continue;
    housing[key] = parse(row[rowField], label);
    provided = true;
  }
  return { provided, housing };
}

export function companyVacancySourcesImportUpdate(row, company) {
  const vacancySources = { ...(company.vacancySources || {}) };
  const fields = [
    ["companySite", "companySite", "Сайт компании"],
    ["hh", "hh", "HH"],
    ["habr", "habr", "Хабр"],
    ["getmatch", "getmatch", "Getmatch"],
    ["telegram", "telegram", "Telegram"],
    ["vacancyDreamJob", "dreamJob", "DreamJob"],
    ["hCareers", "hCareers", "Эйч"],
    ["rabota", "rabota", "rabota.ru"],
  ];
  let provided = false;
  for (const [rowField, key, label] of fields) {
    if (!companyImportValueProvided(row, rowField)) continue;
    vacancySources[key] = companyImportUrl(row[rowField], label);
    provided = true;
  }
  return { provided, vacancySources };
}

export function applyCompanyReviewRows(catalog, reviewRows, identities, errors) {
  const byName = new Map(catalog.companies.map((company, index) => [companyDuplicateTextKey(company.name), index]));
  const byId = new Map([...identities.entries()].map(([name, id]) => [Number(id), byName.get(companyDuplicateTextKey(name))]));
  const touched = new Set();
  let updated = 0;
  let skipped = 0;
  let invalid = 0;
  for (let index = 0; index < reviewRows.length; index += 1) {
    const row = reviewRows[index] || {};
    const rowNumber = Number(row.row || index + 2);
    try {
      const companyId = companyImportNumber(row.companyId, "ID компании", { integer: true, min: 1 });
      if (companyId == null) throw new Error("не указан ID компании");
      const companyIndex = byId.get(companyId);
      if (companyIndex === undefined) throw new Error("компания с таким ID не найдена");
      const reviewData = companyReviewsImportUpdate(row, catalog.companies[companyIndex]);
      if (!reviewData.provided) {
        skipped += 1;
        continue;
      }
      if (touched.has(companyId)) throw new Error("для этого ID уже обработана другая строка отзывов");
      catalog.companies[companyIndex] = {
        ...catalog.companies[companyIndex],
        reviewSources: reviewData.reviewSources,
        reviews: reviewData.reviews
      };
      touched.add(companyId);
      updated += 1;
    } catch (error) {
      invalid += 1;
      if (errors.length < 100) errors.push({ sheet: "Отзывы", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }
  return { updated, skipped, invalid };
}

export function findImportedCompanyIndex(row, byName, byId) {
  const name = companyImportText(row?.name ?? row?.companyName, "Название компании", 300);
  const companyId = companyImportNumber(row?.companyId, "ID компании", { integer: true, min: 1 });
  const indexById = companyId == null ? undefined : byId.get(companyId);
  const indexByName = name ? byName.get(companyDuplicateTextKey(name)) : undefined;
  if (indexById === undefined && indexByName === undefined) throw new Error("компания не найдена по ID или названию");
  if (indexById !== undefined && indexByName !== undefined && indexById !== indexByName) throw new Error("ID и название относятся к разным компаниям");
  return indexById ?? indexByName;
}

export async function updateCompanyFields(db, companyRows, officeRows, reviewRows) {
  const catalog = await readAppCatalog(db);
  const identities = await ensureCompanyIdentities(db, catalog.companies);
  const byName = new Map(catalog.companies.map((company, index) => [companyDuplicateTextKey(company.name), index]));
  const byId = new Map([...identities.entries()].map(([name, id]) => [Number(id), byName.get(companyDuplicateTextKey(name))]));
  const touched = new Set();
  const errors = [];
  let updated = 0;
  let invalid = 0;

  for (let index = 0; index < companyRows.length; index += 1) {
    const row = companyRows[index] || {};
    const rowNumber = Number(row.row || index + 2);
    try {
      const companyIndex = findImportedCompanyIndex(row, byName, byId);
      if (touched.has(companyIndex)) throw new Error("эта компания уже обновлялась другой строкой файла");
      const company = catalog.companies[companyIndex];
      const middle = companySalaryUpdateRange(row, "middle", company.middle);
      const senior = companySalaryUpdateRange(row, "senior", company.senior);
      const lead = companySalaryUpdateRange(row, "lead", company.lead);
      const head = companySalaryUpdateRange(row, "head", company.head);
      const ratingProvided = companyImportValueProvided(row, "rating");
      const wlbProvided = companyImportValueProvided(row, "wlb");
      const wlbScoreProvided = companyImportValueProvided(row, "wlbScore");
      const defermentProvided = companyImportValueProvided(row, "deferment");
      const mobilizationProvided = companyImportValueProvided(row, "mobilization");
      const reviewData = companyReviewsImportUpdate(row, company);
      const vpnAiData = companyVpnAiImportUpdate(row, company);
      const housingData = companyHousingImportUpdate(row, company);
      const vacancySourcesData = companyVacancySourcesImportUpdate(row, company);
      const aiData = companyAiImportUpdate(row, company);
      const rating = ratingProvided ? companyImportNumber(row.rating, "Рейтинг сотрудников", { min: 0, max: 5 }) : company.rating;
      const wlb = wlbProvided ? companyImportText(row.wlb, "Work-life balance") : company.wlb;
      const wlbScore = wlbScoreProvided ? companyImportNumber(row.wlbScore, "Балл WLB", { integer: true, min: 0, max: 100 }) : company.wlbScore;
      const deferment = defermentProvided ? companyImportText(row.deferment, "IT-аккредитация / отсрочка") : company.deferment;
      const mobilization = mobilizationProvided ? companyImportText(row.mobilization, "Мобилизация") : company.mobilization;
      if (![middle, senior, lead, head].some((item) => item.provided) && !ratingProvided && !wlbProvided && !wlbScoreProvided && !defermentProvided && !mobilizationProvided && !housingData.provided && !vpnAiData.provided && !vacancySourcesData.provided && !reviewData.provided && !aiData.provided) {
        throw new Error("не заполнено ни одно поле, разрешённое для обновления");
      }
      catalog.companies[companyIndex] = {
        ...company,
        middle: middle.range,
        senior: senior.range,
        lead: lead.range,
        head: head.range,
        rating,
        wlb,
        wlbScore,
        aiScore: aiData.aiScore,
        aiEvidence: aiData.aiEvidence,
        deferment,
        mobilization,
        housing: housingData.housing,
        vpnAi: vpnAiData.vpnAi,
        vacancySources: vacancySourcesData.vacancySources,
        reviewSources: reviewData.reviewSources,
        reviews: reviewData.reviews,
        ratingFormulaVersion: RATING_FORMULA_VERSION
      };
      touched.add(companyIndex);
      updated += 1;
    } catch (error) {
      invalid += 1;
      if (errors.length < 100) errors.push({ sheet: "Компании", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }

  const reviewUpdate = applyCompanyReviewRows(catalog, reviewRows, identities, errors);
  invalid += reviewUpdate.invalid;

  let officesUpdated = 0;
  let officesSkipped = 0;
  const touchedOffices = new Set();
  for (let index = 0; index < officeRows.length; index += 1) {
    const row = officeRows[index] || {};
    const rowNumber = Number(row.row || index + 2);
    try {
      const latProvided = companyImportValueProvided(row, "lat");
      const lonProvided = companyImportValueProvided(row, "lon");
      if (!latProvided && !lonProvided) {
        officesSkipped += 1;
        continue;
      }
      if (latProvided !== lonProvided) throw new Error("широта и долгота должны быть заполнены вместе");
      const companyIndex = findImportedCompanyIndex(row, byName, byId);
      const address = companyImportText(row?.address, "Адрес", 1000);
      if (!address) throw new Error("не указан адрес офиса для сопоставления");
      const officeIndex = (catalog.companies[companyIndex].offices || []).findIndex((office) =>
        companyDuplicateTextKey(office.address) === companyDuplicateTextKey(address)
      );
      if (officeIndex < 0) throw new Error("офис с таким адресом не найден");
      const officeKey = `${companyIndex}:${officeIndex}`;
      if (touchedOffices.has(officeKey)) throw new Error("координаты этого офиса уже обновлялись другой строкой файла");
      const lat = companyImportNumber(row.lat, "Широта", { min: -90, max: 90 });
      const lon = companyImportNumber(row.lon, "Долгота", { min: -180, max: 180 });
      const offices = [...catalog.companies[companyIndex].offices];
      offices[officeIndex] = { ...offices[officeIndex], lat, lon };
      catalog.companies[companyIndex] = { ...catalog.companies[companyIndex], offices };
      touchedOffices.add(officeKey);
      officesUpdated += 1;
    } catch (error) {
      officesSkipped += 1;
      invalid += 1;
      if (errors.length < 100) errors.push({ sheet: "Офисы", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }

  if (updated || officesUpdated || reviewUpdate.updated) {
    const nextCatalog = normalizeCatalog({ ...catalog, updated: new Date().toISOString().slice(0, 10) });
    await db.prepare(
      `INSERT INTO app_catalog (id, data_json, updated_at)
       VALUES (1, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`
    ).bind(JSON.stringify(nextCatalog)).run();
  }

  return {
    added: 0,
    updated,
    notAdded: invalid,
    duplicates: 0,
    invalid,
    officesAdded: 0,
    officesUpdated,
    officesSkipped,
    reviewsUpdated: reviewUpdate.updated,
    reviewsSkipped: reviewUpdate.skipped + reviewUpdate.invalid,
    duplicateDetails: [],
    errors
  };
}

export async function importCompanies(db, companyRows, officeRows, reviewRows = [], mode = "create") {
  await ensureCrmSchema(db);
  if (!Array.isArray(companyRows) || !companyRows.length) throw new Error("На листе «Компании» нет строк для импорта.");
  if (!Array.isArray(officeRows)) throw new Error("Не удалось прочитать лист «Офисы».");
  if (!Array.isArray(reviewRows)) throw new Error("Не удалось прочитать лист «Отзывы».");
  if (companyRows.length > MAX_COMPANY_IMPORT_ROWS) throw new Error(`За один раз можно импортировать не более ${MAX_COMPANY_IMPORT_ROWS} компаний.`);
  if (officeRows.length > MAX_COMPANY_OFFICE_IMPORT_ROWS) throw new Error(`За один раз можно импортировать не более ${MAX_COMPANY_OFFICE_IMPORT_ROWS} офисов.`);
  if (reviewRows.length > MAX_COMPANY_IMPORT_ROWS) throw new Error(`За один раз можно импортировать не более ${MAX_COMPANY_IMPORT_ROWS} строк отзывов.`);
  if (mode === "update") return updateCompanyFields(db, companyRows, officeRows, reviewRows);

  const catalog = await readAppCatalog(db);
  const duplicateIndexes = { name: new Map(), companySite: new Map(), hh: new Map(), habr: new Map() };
  const duplicateLabels = { name: "Название компании", companySite: "Сайт компании", hh: "HH", habr: "Хабр" };
  const addDuplicateKeys = (company, origin) => {
    const keys = {
      name: companyDuplicateTextKey(company.name),
      companySite: companyDuplicateUrlKey(company.vacancySources?.companySite),
      hh: companyDuplicateUrlKey(company.vacancySources?.hh),
      habr: companyDuplicateUrlKey(company.vacancySources?.habr)
    };
    for (const [field, key] of Object.entries(keys)) if (key) duplicateIndexes[field].set(key, origin);
  };
  catalog.companies.forEach((company) => addDuplicateKeys(company, company.name));

  const accepted = [];
  const acceptedByName = new Map();
  const duplicateDetails = [];
  const errors = [];
  let duplicates = 0;
  let invalid = 0;
  const acceptedSourceNames = new Map();
  for (let index = 0; index < companyRows.length; index += 1) {
    const row = companyRows[index] || {};
    const rowNumber = Number(row.row || index + 2);
    try {
      const rawKeys = {
        name: companyDuplicateTextKey(row.name),
        companySite: companyDuplicateUrlKey(row.companySite),
        hh: companyDuplicateUrlKey(row.hh),
        habr: companyDuplicateUrlKey(row.habr)
      };
      const duplicateField = Object.keys(rawKeys).find((field) => rawKeys[field] && duplicateIndexes[field].has(rawKeys[field]));
      if (duplicateField) {
        duplicates += 1;
        if (duplicateDetails.length < 50) duplicateDetails.push({
          row: rowNumber,
          name: String(row.name || "").trim(),
          reason: `поле «${duplicateLabels[duplicateField]}» совпадает с компанией «${duplicateIndexes[duplicateField].get(rawKeys[duplicateField])}»`
        });
        continue;
      }
      const company = companyImportInput(row);
      const sourceId = companyImportNumber(row.companyId, "ID компании", { integer: true, min: 1 });
      if (sourceId != null && acceptedSourceNames.has(sourceId)) throw new Error("ID компании уже используется другой новой компанией в этом файле");
      accepted.push(company);
      if (sourceId != null) acceptedSourceNames.set(sourceId, company.name);
      acceptedByName.set(companyDuplicateTextKey(company.name), company);
      addDuplicateKeys(company, `${company.name} (строка ${rowNumber} файла)`);
    } catch (error) {
      invalid += 1;
      if (errors.length < 100) errors.push({ sheet: "Компании", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }

  let officesAdded = 0;
  let officesSkipped = 0;
  const mainOfficeCompanies = new Set();
  for (let index = 0; index < officeRows.length; index += 1) {
    const row = officeRows[index] || {};
    const rowNumber = Number(row.row || index + 2);
    try {
      const input = companyOfficeImportInput(row);
      const company = acceptedByName.get(companyDuplicateTextKey(input.companyName));
      if (!company) throw new Error("компания не была добавлена из листа «Компании» (возможно, это дубликат или строка с ошибкой)");
      if (input.office.main === "Да") {
        const key = companyDuplicateTextKey(company.name);
        if (mainOfficeCompanies.has(key)) throw new Error("для компании уже указан другой основной офис");
        mainOfficeCompanies.add(key);
      }
      company.offices.push(input.office);
      officesAdded += 1;
    } catch (error) {
      officesSkipped += 1;
      if (errors.length < 100) errors.push({ sheet: "Офисы", row: rowNumber, message: error?.message || "некорректные данные" });
    }
  }

  let nextCatalog = normalizeCatalog({
    ...catalog,
    updated: new Date().toISOString().slice(0, 10),
    companies: catalog.companies.concat(accepted)
  });
  const identities = await ensureCompanyIdentities(db, nextCatalog.companies);
  const remappedReviewRows = reviewRows.map((row) => {
    const sourceId = Number(row?.companyId);
    const importedName = Number.isSafeInteger(sourceId) ? acceptedSourceNames.get(sourceId) : "";
    const assignedId = importedName ? identities.get(importedName) : null;
    return assignedId ? { ...row, companyId: assignedId } : row;
  });
  const reviewUpdate = applyCompanyReviewRows(nextCatalog, remappedReviewRows, identities, errors);
  invalid += reviewUpdate.invalid;

  if (accepted.length || reviewUpdate.updated) {
    nextCatalog = normalizeCatalog({ ...nextCatalog, updated: new Date().toISOString().slice(0, 10) });
    await db.prepare(
      `INSERT INTO app_catalog (id, data_json, updated_at)
       VALUES (1, ?, CURRENT_TIMESTAMP)
       ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = CURRENT_TIMESTAMP`
    ).bind(JSON.stringify(nextCatalog)).run();
  }

  return {
    added: accepted.length,
    notAdded: duplicates + invalid,
    duplicates,
    invalid,
    officesAdded,
    officesSkipped,
    reviewsUpdated: reviewUpdate.updated,
    reviewsSkipped: reviewUpdate.skipped + reviewUpdate.invalid,
    duplicateDetails,
    errors
  };
}

export async function companyVacancySourcesExport(db, userName, onlyUnranked = false) {
  const catalog = await readAppCatalog(db);
  const [identities, favorites, hiddenCompanies] = await Promise.all([
    ensureCompanyIdentities(db, catalog.companies),
    readFavoriteCompanyNames(db, userName),
    readHiddenCompanyNames(db, userName)
  ]);
  const favoriteNames = new Set(favorites);
  const hiddenNames = new Set(hiddenCompanies);
  const rankValue = (company) => {
    const rank = Number(company?.rank);
    return Number.isInteger(rank) && rank > 0 ? rank : null;
  };
  return catalog.companies
    .filter((company) => !onlyUnranked || rankValue(company) === null)
    .slice()
    .sort((left, right) => {
      const leftRank = rankValue(left), rightRank = rankValue(right);
      if (leftRank !== null && rightRank !== null && leftRank !== rightRank) return leftRank - rightRank;
      if (leftRank !== null) return -1;
      if (rightRank !== null) return 1;
      return String(left.name || "").localeCompare(String(right.name || ""), "ru");
    })
    .map((company) => ({
      id: identities.get(company.name),
      rank: company.rank,
      name: company.name,
      favorite: favoriteNames.has(company.name),
      hidden: hiddenNames.has(company.name),
      middle: company.middle,
      senior: company.senior,
      lead: company.lead,
      head: company.head,
      companySite: company.vacancySources?.companySite || "",
      hh: company.vacancySources?.hh || "",
      habr: company.vacancySources?.habr || "",
      getmatch: company.vacancySources?.getmatch || "",
      telegram: company.vacancySources?.telegram || "",
      dreamJob: company.vacancySources?.dreamJob || "",
      hCareers: company.vacancySources?.hCareers || "",
      rabota: company.vacancySources?.rabota || ""
    }));
}

export async function companyReviewSourcesExport(db, userName) {
  const catalog = await readAppCatalog(db);
  const [identities, favorites, hiddenCompanies] = await Promise.all([
    ensureCompanyIdentities(db, catalog.companies),
    readFavoriteCompanyNames(db, userName),
    readHiddenCompanyNames(db, userName)
  ]);
  const favoriteNames = new Set(favorites);
  const hiddenNames = new Set(hiddenCompanies);
  const rankValue = (company) => {
    const rank = Number(company?.rank);
    return Number.isInteger(rank) && rank > 0 ? rank : null;
  };
  return catalog.companies
    .slice()
    .sort((left, right) => {
      const leftRank = rankValue(left), rightRank = rankValue(right);
      if (leftRank !== null && rightRank !== null && leftRank !== rightRank) return leftRank - rightRank;
      if (leftRank !== null) return -1;
      if (rightRank !== null) return 1;
      return String(left.name || "").localeCompare(String(right.name || ""), "ru");
    })
    .map((company) => ({
      id: identities.get(company.name),
      rank: company.rank,
      name: company.name,
      favorite: favoriteNames.has(company.name),
      hidden: hiddenNames.has(company.name),
      habr: company.reviewSources?.habr || "",
      dreamJob: company.reviewSources?.dreamJob || "",
      glassDoor: company.reviewSources?.glassDoor || "",
      superJob: company.reviewSources?.superJob || "",
      pravdaSotrudnikov: company.reviewSources?.pravdaSotrudnikov || "",
      nahjob: company.reviewSources?.nahjob || "",
      bossNadzor: company.reviewSources?.bossNadzor || "",
      yandexMap: company.reviewSources?.yandexMap || "",
      plusShort: company.reviews?.plusShort || "",
      minusShort: company.reviews?.minusShort || "",
      plus: company.reviews?.plus || "",
      minus: company.reviews?.minus || ""
    }));
}
