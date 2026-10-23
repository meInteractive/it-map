// Server-only; extracted from the v218 Worker.
import {COMPANY_VACANCY_SOURCE_URLS,VACANCY_SOURCE_META,REVIEW_SOURCE_META,WLB_RATING_WEIGHT,AGENCY_RATING_INPUT_META,AGENCY_TYPE_OPTIONS,AGENCY_FUNCTION_OPTIONS,AGENCY_POSITION_LEVEL_OPTIONS,AGENCY_EXECUTIVE_SEARCH_OPTIONS,AGENCY_CLOSED_VACANCIES_OPTIONS,AGENCY_VACANCY_PUBLICITY_OPTIONS,AGENCY_EMPLOYER_DISCLOSURE_OPTIONS,AGENCY_SALARY_DISCLOSURE_OPTIONS,AGENCY_PRIMARY_CHANNEL_OPTIONS,AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS,AGENCY_VACANCY_FRESHNESS_OPTIONS,AGENCY_ACCEPTS_CV_OPTIONS,AGENCY_CONTACT_METHOD_OPTIONS,AGENCY_REVIEW_SOURCE_META,RATING_FORMULA_VERSION,COMPANY_APPLICATION_URLS,COMPANY_APPLICATION_EMAILS} from '../constants.js';
import {isValidCatalog} from '../repositories/catalog.js';
import {DATA} from '../data.js';

export function normalizeVacancySources(company, embeddedCompany) {
  const storedSources = company?.vacancySources && typeof company.vacancySources === "object"
    ? company.vacancySources
    : {};
  const embeddedSources = embeddedCompany?.vacancySources && typeof embeddedCompany.vacancySources === "object"
    ? embeddedCompany.vacancySources
    : {};
  const defaultSources = COMPANY_VACANCY_SOURCE_URLS[company?.name] || {};
  return Object.fromEntries(
    VACANCY_SOURCE_META.map(([key]) => {
      const storedValue = typeof storedSources[key] === "string" ? storedSources[key].trim() : "";
      const embeddedValue = typeof embeddedSources[key] === "string" ? embeddedSources[key].trim() : "";
      const defaultValue = typeof defaultSources[key] === "string" ? defaultSources[key].trim() : "";
      return [key, storedValue || embeddedValue || defaultValue];
    })
  );
}

export function normalizeReviewSources(company, embeddedCompany, sourceMeta = REVIEW_SOURCE_META) {
  const storedSources = company?.reviewSources && typeof company.reviewSources === "object"
    ? company.reviewSources
    : {};
  const embeddedSources = embeddedCompany?.reviewSources && typeof embeddedCompany.reviewSources === "object"
    ? embeddedCompany.reviewSources
    : {};
  return Object.fromEntries(
    sourceMeta.map(([key]) => {
      const storedValue = typeof storedSources[key] === "string" ? storedSources[key].trim() : "";
      const embeddedValue = typeof embeddedSources[key] === "string" ? embeddedSources[key].trim() : "";
      return [key, storedValue || embeddedValue];
    })
  );
}

export function normalizeCompanyReviews(company, embeddedCompany) {
  const storedReviews = company?.reviews && typeof company.reviews === "object" ? company.reviews : {};
  const embeddedReviews = embeddedCompany?.reviews && typeof embeddedCompany.reviews === "object" ? embeddedCompany.reviews : {};
  const value = (key) => {
    const storedValue = typeof storedReviews[key] === "string" ? storedReviews[key].trim() : "";
    const embeddedValue = typeof embeddedReviews[key] === "string" ? embeddedReviews[key].trim() : "";
    return storedValue || embeddedValue;
  };
  return {
    plusShort: value("plusShort"),
    minusShort: value("minusShort"),
    plus: value("plus"),
    minus: value("minus"),
  };
}

export function normalizeCompanySalaryRange(value) {
  if (!Array.isArray(value)) return [null, null];
  return [0, 1].map((index) => {
    if (value[index] == null || value[index] === "") return null;
    const number = Number(value[index]);
    return Number.isSafeInteger(number) && number >= 0 ? number : null;
  });
}

export function ratingNumber(value, fallback = null) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

export function ratingClamp(value, min, max) {
  return Math.min(max, Math.max(min, value));
}

export function ratingRound(value, maxScore = 100) {
  return Math.round(ratingClamp(value, 0, maxScore));
}

export function ratingReviewScore(value) {
  let rating = ratingNumber(value);
  if (rating == null) return 7;
  if (rating > 5 && rating <= 10) rating /= 2;
  if (rating >= 4.7) return 20;
  if (rating >= 4.5) return 17;
  if (rating >= 4.2) return 14;
  if (rating >= 3.9) return 11;
  if (rating >= 3.6) return 8;
  return 5;
}

export function ratingSalaryPart(range, threshold, maxScore, nearLower, nearScore, upperScore) {
  const [from, to] = normalizeCompanySalaryRange(range);
  if (from != null && from > threshold) return maxScore;
  if (from != null && from === threshold) return maxScore - 1;
  if (to != null && to > threshold && from != null && from >= nearLower) return nearScore;
  if (to != null && to > threshold) return upperScore;
  return 0;
}

export function ratingSalaryScore(company) {
  const middle = ratingSalaryPart(company.middle, 250000, 8, 230000, 6, 3);
  const senior = [company.senior, company.lead, company.head].reduce((best, range) =>
    Math.max(best, ratingSalaryPart(range, 300000, 12, 280000, 9, 6)), 0);
  return middle + senior;
}

export function ratingRevenueScore(revenue) {
  const amount = ratingNumber(revenue?.amount);
  if (amount == null || amount <= 0) return 0;
  if (amount >= 1_000_000_000_000) return 10;
  if (amount >= 500_000_000_000) return 9;
  if (amount >= 100_000_000_000) return 8;
  if (amount >= 50_000_000_000) return 7;
  if (amount >= 10_000_000_000) return 6;
  if (amount >= 1_000_000_000) return 5;
  return 3;
}

export function ratingText(value) {
  return String(value || "").toLocaleLowerCase("ru");
}

export function fallbackRoleFitScore(company) {
  const roles = ratingText(company.roles);
  if (!roles) return 2;
  const matches = ["product", "проект", "program", "delivery", "development", "разработ", "engineering", "техническ"]
    .filter((term) => roles.includes(term)).length;
  return matches >= 3 ? 5 : matches >= 1 ? 4 : 2;
}

export function fallbackDefermentScore(company) {
  const text = `${ratingText(company.deferment)} ${ratingText(company.mobilization)}`;
  if (text.includes("подтвержден") && text.includes("аккредитац")) return 15;
  if (text.includes("аккредитац") && (text.includes("вероят") || text.includes("уточнить") || text.includes("проверить"))) return 9;
  if (text.includes("аккредитац")) return 12;
  if (text.includes("не подтвержден")) return 3;
  return 5;
}

export function fallbackTransparencyScore(company) {
  const confidence = ratingText(company.confidence);
  if (confidence.includes("высок")) return 5;
  if (confidence.includes("средн")) return 3;
  if (confidence.includes("низк")) return 1;
  return 2;
}

export function fallbackOfficeScore(company) {
  const offices = Array.isArray(company.offices) ? company.offices : [];
  if (!offices.length) return 0;
  const statuses = offices.map((office) => ratingText(office.status));
  if (statuses.some((status) => status.includes("подтвержден") && !status.includes("не подтвержден"))) return 5;
  if (statuses.some((status) => status.includes("частично"))) return 3;
  return 2;
}

export function normalizeRatingInputs(company, embeddedCompany) {
  const inherited = embeddedCompany?.ratingInputs && typeof embeddedCompany.ratingInputs === "object" ? embeddedCompany.ratingInputs : {};
  const stored = company?.ratingInputs && typeof company.ratingInputs === "object" ? company.ratingInputs : {};
  return { ...inherited, ...stored };
}

export function normalizeWlbScore(company, embeddedCompany, inputs) {
  const formulaVersion = Number(company?.ratingFormulaVersion) || 0;
  const stored = ratingNumber(company?.wlbScore);
  if (formulaVersion >= 4 && stored != null) return ratingRound(stored, 100);
  const legacyBaseline = ratingNumber(inputs.wlbScore, ratingNumber(embeddedCompany?.wlbScore));
  if (formulaVersion >= 2 && stored != null) {
    const unchangedLegacyScore = legacyBaseline != null && stored === ratingRound(legacyBaseline * WLB_RATING_WEIGHT / 15, WLB_RATING_WEIGHT);
    return ratingRound(unchangedLegacyScore ? legacyBaseline / 15 * 100 : stored / WLB_RATING_WEIGHT * 100, 100);
  }
  return ratingRound((legacyBaseline == null ? 8 : legacyBaseline) / 15 * 100, 100);
}

export function calculateCompanyRating(company, embeddedCompany) {
  const inputs = normalizeRatingInputs(company, embeddedCompany);
  const roleFitRaw = ratingNumber(inputs.roleFit);
  const defermentRaw = ratingNumber(inputs.deferment);
  const aiScore = company.aiScore == null || company.aiScore === '' ? null : Number(company.aiScore);
  const aiEvidence = String(company.aiEvidence || '').trim();
  const validAiScore = [0, 5, 10].includes(aiScore) && (aiScore === 0 || aiEvidence) ? aiScore : null;
  const wlbScore = normalizeWlbScore(company, embeddedCompany, inputs);
  const breakdown = {
    roleFit: ratingRound(roleFitRaw == null ? fallbackRoleFitScore(company) : roleFitRaw / 4, 5),
    deferment: ratingRound(defermentRaw == null ? fallbackDefermentScore(company) : defermentRaw * 1.5, 15),
    reviews: ratingRound(ratingReviewScore(company.rating), 20),
    wlb: ratingRound(wlbScore / 100 * WLB_RATING_WEIGHT, WLB_RATING_WEIGHT),
    salary: ratingRound(ratingSalaryScore(company), 20),
    revenue: ratingRound(ratingRevenueScore(company.revenue), 10),
    ai: validAiScore ?? 0
  };
  return {
    score: ratingRound(Object.values(breakdown).reduce((total, value) => total + value, 0), 100),
    ratingBreakdown: breakdown,
    aiScore: validAiScore,
    aiEvidence,
    ratingInputs: inputs,
    wlbScore
  };
}

export function agencyRatingInputValue(value) {
  if (value == null || String(value).trim() === "") return null;
  const number = Number(value);
  if (!Number.isFinite(number) || number < 0 || number > 10 || Math.abs(number * 2 - Math.round(number * 2)) > 1e-9) return null;
  return Math.round(number * 2) / 2;
}

export function agencyReviewRatingsComplete(agency) {
  return AGENCY_RATING_INPUT_META.every(([key]) => agencyRatingInputValue(agency?.[key]) != null);
}

export function calculateAgencyReviewScore(agency) {
  if (!agencyReviewRatingsComplete(agency)) return null;
  const score = AGENCY_RATING_INPUT_META.reduce((total, [key, , weight]) => total + agencyRatingInputValue(agency[key]) * weight, 0);
  return Math.round(score * 10) / 10;
}

export function calculateAgencyExecutiveSearchScore(agency) {
  const value = String(agency?.executiveSearch || "").trim().toLocaleLowerCase("ru");
  if (value === "да") return 5;
  if (value === "частично") return 3;
  if (value) return 0;
  const types = (Array.isArray(agency?.agencyTypes) ? agency.agencyTypes : []).map((item) => String(item || "").trim().toLocaleLowerCase("ru"));
  return types.includes("executive search") ? 5 : 0;
}

export function agencyRolesContainItManagement(value) {
  const roles = String(value || "").trim().toLocaleLowerCase("ru");
  if (!roles) return false;
  const explicitRoles = [
    "engineering manager", "head of engineering", "head of development", "development manager", "technical product", "technical project",
    "cto", "cio", "технический директор", "техдир", "руководитель разработки", "директор по разработке", "руководитель инженер",
    "руководитель it", "директор it", "руководитель ит", "директор ит", "руководитель цифров", "директор по цифров"
  ];
  if (explicitRoles.some((role) => roles.includes(role))) return true;
  const hasTechnology = /(^|[^a-zа-я0-9])(it|ит)([^a-zа-я0-9]|$)|technolog|технолог|digital|цифров|engineering|инженер|development|разработ/.test(roles);
  const hasManagement = /head|lead|manager|director|chief|руковод|директор|начальник|лид/.test(roles);
  return hasTechnology && hasManagement;
}

export function calculateAgencyItScore(agency) {
  const types = (Array.isArray(agency?.agencyTypes) ? agency.agencyTypes : []).map((item) => String(item || "").trim().toLocaleLowerCase("ru"));
  const specializations = (Array.isArray(agency?.functionalSpecializations) ? agency.functionalSpecializations : []).map((item) => String(item || "").trim().toLocaleLowerCase("ru"));
  const strong = ["engineering / development", "it / cio", "data / ai", "cybersecurity", "infrastructure / devops"];
  if (types.includes("it recruitment") || strong.some((value) => specializations.includes(value))) return 5;
  const adjacent = ["product", "project / program / delivery", "digital transformation"];
  return adjacent.some((value) => specializations.includes(value)) && agencyRolesContainItManagement(agency?.roles) ? 3 : 0;
}

export function calculateAgencyRelevanceScore(agency) {
  return calculateAgencyExecutiveSearchScore(agency) + calculateAgencyItScore(agency);
}

export function calculateAgencyRevenueScore(revenue) {
  const amount = Number(revenue?.amount);
  if (!Number.isFinite(amount) || amount <= 0) return 0;
  if (amount < 25_000_000) return 1;
  if (amount < 50_000_000) return 2;
  if (amount < 100_000_000) return 3;
  if (amount < 200_000_000) return 4;
  if (amount < 400_000_000) return 5;
  if (amount < 800_000_000) return 6;
  if (amount < 1_500_000_000) return 7;
  if (amount < 3_000_000_000) return 8;
  if (amount < 10_000_000_000) return 9;
  return 10;
}

export function calculateAgencyRating(agency) {
  const reviewsScore = calculateAgencyReviewScore(agency);
  if (reviewsScore == null) return null;
  const relevanceScore = calculateAgencyRelevanceScore(agency);
  const revenueScore = calculateAgencyRevenueScore(agency?.revenue);
  let total = reviewsScore + relevanceScore + revenueScore;
  const ethics = agencyRatingInputValue(agency?.ethics);
  const vacancyHonesty = agencyRatingInputValue(agency?.vacancyHonesty);
  if (ethics === 0 || vacancyHonesty === 0) total = Math.min(total, 50);
  else if ((ethics > 0 && ethics <= 2) || (vacancyHonesty > 0 && vacancyHonesty <= 2)) total = Math.min(total, 60);
  return { score: Math.round(total), reviewsScore, relevanceScore, revenueScore };
}

export function normalizeAgencyMultiChoice(value, options) {
  const values = Array.isArray(value) ? value : String(value || "").split(";");
  const optionMap = new Map(options.map((option) => [option.toLocaleLowerCase("ru"), option]));
  return [...new Set(values.map((item) => optionMap.get(String(item || "").trim().toLocaleLowerCase("ru"))).filter(Boolean))];
}

export function normalizeAgencyChoice(value, options) {
  const normalized = String(value || "").trim().toLocaleLowerCase("ru");
  return options.find((option) => option.toLocaleLowerCase("ru") === normalized) || "";
}

export function normalizeAgencyCalculatedScore(value, max, decimal = false) {
  if (value == null || String(value).trim() === "") return null;
  const number = Number(value);
  if (!Number.isFinite(number) || number < 0 || number > max) return null;
  return decimal ? Math.round(number * 10) / 10 : Math.round(number);
}

export function normalizeAgency(agency) {
  const salaryRangeSource = agency?.salaryRangeSource && typeof agency.salaryRangeSource === "object" ? agency.salaryRangeSource : {};
  const rank = Number(agency?.rank);
  const scoreProvided = agency?.score != null && String(agency.score).trim() !== "";
  const score = scoreProvided ? Number(agency.score) : NaN;
  const revenue = agency?.revenue && typeof agency.revenue === "object" ? agency.revenue : {};
  return {
    ...agency,
    entityType: "agency",
    rank: Number.isSafeInteger(rank) && rank > 0 ? rank : null,
    sector: typeof agency?.sector === "string" ? agency.sector.trim() : "",
    roles: typeof agency?.roles === "string" ? agency.roles.trim() : "",
    score: Number.isFinite(score) && score >= 0 && score <= 100 ? score : null,
    ethics: agencyRatingInputValue(agency?.ethics),
    vacancyHonesty: agencyRatingInputValue(agency?.vacancyHonesty),
    feedback: agencyRatingInputValue(agency?.feedback),
    vacancyRelevance: agencyRatingInputValue(agency?.vacancyRelevance),
    itExpertise: agencyRatingInputValue(agency?.itExpertise),
    clientKnowledge: agencyRatingInputValue(agency?.clientKnowledge),
    processOrganization: agencyRatingInputValue(agency?.processOrganization),
    candidateSupport: agencyRatingInputValue(agency?.candidateSupport),
    reviewsScore: normalizeAgencyCalculatedScore(agency?.reviewsScore, 80, true),
    relevanceScore: normalizeAgencyCalculatedScore(agency?.relevanceScore, 10),
    revenueScore: normalizeAgencyCalculatedScore(agency?.revenueScore, 10),
    confidence: ["Высокая", "Средняя", "Низкая"].includes(agency?.confidence) ? agency.confidence : "",
    risk: typeof agency?.risk === "string" ? agency.risk.trim() : "",
    agencyTypes: normalizeAgencyMultiChoice(agency?.agencyTypes, AGENCY_TYPE_OPTIONS),
    functionalSpecializations: normalizeAgencyMultiChoice(agency?.functionalSpecializations, AGENCY_FUNCTION_OPTIONS),
    positionLevels: normalizeAgencyMultiChoice(agency?.positionLevels, AGENCY_POSITION_LEVEL_OPTIONS),
    executiveSearch: normalizeAgencyChoice(agency?.executiveSearch, AGENCY_EXECUTIVE_SEARCH_OPTIONS),
    closedVacancies: normalizeAgencyChoice(agency?.closedVacancies, AGENCY_CLOSED_VACANCIES_OPTIONS),
    vacancyPublicity: normalizeAgencyChoice(agency?.vacancyPublicity, AGENCY_VACANCY_PUBLICITY_OPTIONS),
    employerDisclosure: normalizeAgencyChoice(agency?.employerDisclosure, AGENCY_EMPLOYER_DISCLOSURE_OPTIONS),
    salaryDisclosure: normalizeAgencyChoice(agency?.salaryDisclosure, AGENCY_SALARY_DISCLOSURE_OPTIONS),
    primaryVacancyChannel: normalizeAgencyChoice(agency?.primaryVacancyChannel, AGENCY_PRIMARY_CHANNEL_OPTIONS),
    publicSourcesQuality: normalizeAgencyChoice(agency?.publicSourcesQuality, AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS),
    vacancyFreshness: normalizeAgencyChoice(agency?.vacancyFreshness, AGENCY_VACANCY_FRESHNESS_OPTIONS),
    acceptsCv: normalizeAgencyChoice(agency?.acceptsCv, AGENCY_ACCEPTS_CV_OPTIONS),
    recommendedContactMethods: normalizeAgencyMultiChoice(agency?.recommendedContactMethods, AGENCY_CONTACT_METHOD_OPTIONS),
    workingGuide: typeof agency?.workingGuide === "string" ? agency.workingGuide.trim().slice(0, 512) : "",
    middle: normalizeCompanySalaryRange(agency?.middle),
    senior: normalizeCompanySalaryRange(agency?.senior),
    lead: normalizeCompanySalaryRange(agency?.lead),
    head: normalizeCompanySalaryRange(agency?.head),
    salaryRangeSource: {
      middle: salaryRangeSource.middle === "vacancies" ? "vacancies" : "manual",
      senior: salaryRangeSource.senior === "vacancies" ? "vacancies" : "manual",
      lead: salaryRangeSource.lead === "vacancies" ? "vacancies" : "manual",
      head: salaryRangeSource.head === "vacancies" ? "vacancies" : "manual"
    },
    vacancySources: normalizeVacancySources(agency),
    reviewSources: normalizeReviewSources(agency, null, AGENCY_REVIEW_SOURCE_META),
    reviews: normalizeCompanyReviews(agency),
    revenue: {
      amount: revenue.amount ?? null,
      year: revenue.year ?? null,
      metric: String(revenue.metric || "").trim(),
      legal: String(revenue.legal || "").trim(),
      inn: String(revenue.inn || "").trim(),
      composition: String(revenue.composition || "").trim(),
      coverage: String(revenue.coverage || "").trim(),
      status: String(revenue.status || "").trim(),
      source1: String(revenue.source1 || revenue.source || "").trim(),
      source2: String(revenue.source2 || "").trim(),
      comment: String(revenue.comment || "").trim()
    },
    applicationUrl: typeof agency?.applicationUrl === "string" ? agency.applicationUrl.trim() : "",
    applicationEmail: typeof agency?.applicationEmail === "string" ? agency.applicationEmail.trim() : "",
    offices: [],
    jobs: Array.isArray(agency?.jobs) ? agency.jobs : []
  };
}

export function normalizeCatalog(catalog) {
  const source = isValidCatalog(catalog) ? catalog : DATA;
  const embeddedCompanies = new Map(DATA.companies.map((company) => [company.name, company]));
  const companies = source.companies.map((company) => {
    const embeddedCompany = embeddedCompanies.get(company.name);
    const rating = calculateCompanyRating(company, embeddedCompany);
    return {
      ...company,
      ...rating,
      ratingFormulaVersion: RATING_FORMULA_VERSION,
      rank: null,
      middle: normalizeCompanySalaryRange(company.middle),
      senior: normalizeCompanySalaryRange(company.senior),
      lead: normalizeCompanySalaryRange(company.lead),
      head: normalizeCompanySalaryRange(company.head),
      vacancySources: normalizeVacancySources(company, embeddedCompany),
      reviewSources: normalizeReviewSources(company, embeddedCompany),
      reviews: normalizeCompanyReviews(company, embeddedCompany),
      applicationUrl:
        (typeof company.applicationUrl === "string" && company.applicationUrl.trim()) ||
        (typeof embeddedCompany?.applicationUrl === "string" && embeddedCompany.applicationUrl.trim()) ||
        COMPANY_APPLICATION_URLS[company.name] ||
        "",
      applicationEmail:
        (typeof company.applicationEmail === "string" && company.applicationEmail.trim()) ||
        (typeof embeddedCompany?.applicationEmail === "string" && embeddedCompany.applicationEmail.trim()) ||
        COMPANY_APPLICATION_EMAILS[company.name] ||
        "",
    };
  });
  return {
    ...source,
    companies: companies
      .sort((left, right) => right.score - left.score || left.name.localeCompare(right.name, "ru"))
      .map((company, index) => ({ ...company, entityType: "company", rank: index + 1 })),
    agencies: (Array.isArray(source.agencies) ? source.agencies : [])
      .filter((agency) => agency && typeof agency.name === "string" && agency.name.trim())
      .map(normalizeAgency)
  };
}
