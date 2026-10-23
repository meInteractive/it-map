import fs from 'node:fs';
const data=JSON.parse(fs.readFileSync('src/data.json','utf8'));
if (data.companies.length !== 200) throw new Error(`Expected 200 companies, found ${data.companies.length}`);
const ranks = new Set(data.companies.map((company) => company.rank));
if (ranks.size !== 200 || !Array.from({ length: 200 }, (_, i) => i + 1).every((rank) => ranks.has(rank))) {
  throw new Error("Ranks 1–200 are incomplete or duplicated");
}
const officeCount = data.companies.reduce((total, company) => total + company.offices.length, 0);
const jobCount = data.companies.reduce((total, company) => total + company.jobs.length, 0);
if (officeCount !== 204) throw new Error(`Expected 204 offices, found ${officeCount}`);
if (jobCount !== 70) throw new Error(`Expected 70 vacancy entries, found ${jobCount}`);
if (!data.companies.every((company) => Number.isFinite(company.wlbScore) && company.wlbScore >= 0 && company.wlbScore <= 15)) {
  throw new Error("Legacy source WLB scores are missing or invalid");
}
const normalizedWlbScores = data.companies.map((company) => ({
  name: company.name,
  score: Math.round(company.wlbScore / 15 * 100),
  ratingContribution: Math.round(company.wlbScore / 15 * 20),
}));
if (
  !normalizedWlbScores.every(({ score, ratingContribution }) => score >= 0 && score <= 100 && ratingContribution >= 0 && ratingContribution <= 20) ||
  normalizedWlbScores.find(({ name }) => name === "ЮMoney")?.score !== 80
) throw new Error("WLB normalization audit failed");
const vacancySourceFields = ["companySite", "hh", "habr", "getmatch", "telegram"];
if (!data.companies.every((company) =>
  company.vacancySources &&
  vacancySourceFields.every((field) => typeof company.vacancySources[field] === "string") &&
  !Object.prototype.hasOwnProperty.call(company.vacancySources, "geekjob")
)) {
  throw new Error("Vacancy source fields are missing for one or more companies");
}
if (!data.companies.every((company) => typeof company.applicationEmail === "string")) {
  throw new Error("Application email field is missing for one or more companies");
}
const vk = data.companies.find((company) => company.rank === 9 && company.name === "VK");
const expectedVkSources = {
  companySite: "https://team.vk.company/vacancy/",
  hh: "https://hh.ru/employer/15478?tab=VACANCIES",
  habr: "https://career.habr.com/companies/vk/vacancies",
  getmatch: "https://getmatch.ru/companies/kdl1BRZV-vk",
  telegram: "https://t.me/s/vkjobs",
};
if (!vk || Object.entries(expectedVkSources).some(([field, value]) => vk.vacancySources[field] !== value)) {
  throw new Error("VK vacancy sources are missing or incorrect");
}
const kaspersky = data.companies.find((company) => company.rank === 8 && company.name === "Лаборатория Касперского");
if (
  !kaspersky ||
  kaspersky.applicationUrl !== "https://careers.kaspersky.ru/jointheteam" ||
  kaspersky.applicationEmail !== ""
) {
  throw new Error("Kaspersky application link or email state is incorrect");
}
const yoomoney = data.companies.find((company) => company.rank === 7 && company.name === "ЮMoney");
const expectedYoomoneySources = {
  companySite: "https://jobs.yoomoney.ru/",
  hh: "https://hh.ru/employer/655542?tab=VACANCIES",
  habr: "https://career.habr.com/companies/yoomoneyru/vacancies",
  getmatch: "",
  telegram: "https://t.me/s/yoomoneywork",
};
if (
  !yoomoney ||
  Object.entries(expectedYoomoneySources).some(([field, value]) => yoomoney.vacancySources[field] !== value) ||
  yoomoney.applicationUrl !== "https://jobs.yoomoney.ru/" ||
  yoomoney.applicationEmail !== "job@yoomoney.ru"
) {
  throw new Error("YooMoney vacancy sources, application link, or email are incorrect");
}
const expectedVacancySourceCompanies = [
  {
    rank: 24,
    name: "HeadHunter",
    sources: {
      companySite: "https://hh.ru/employer/1455",
      hh: "https://hh.ru/employer/1455?tab=VACANCIES",
      habr: "https://career.habr.com/companies/hh/vacancies",
      getmatch: "https://getmatch.ru/companies/OBlgAqvb-headhunter",
      telegram: "https://t.me/s/hh_ru_official",
    },
    applicationUrl: "https://hh.ru/employer/1455?tab=VACANCIES",
    applicationEmail: "",
  },
  {
    rank: 33,
    name: "Контур",
    sources: {
      companySite: "https://kontur.ru/career/vacancies",
      hh: "https://hh.ru/employer/41862?tab=VACANCIES",
      habr: "https://career.habr.com/companies/skbkontur/vacancies",
      getmatch: "https://getmatch.ru/companies/3pQ7p6lG-kontur?s=vacancy",
      telegram: "https://t.me/s/kontur_live",
    },
    applicationUrl: "https://kontur.ru/career/vacancies",
    applicationEmail: "",
  },
  {
    rank: 34,
    name: "Точка Банк",
    sources: {
      companySite: "https://hr.tochka.com/vacancies/",
      hh: "https://hh.ru/employer/2324020?tab=VACANCIES",
      habr: "https://career.habr.com/companies/bank-tochka/vacancies",
      getmatch: "https://getmatch.ru/companies/kdl1qxlZ-tochka-bank",
      telegram: "https://t.me/+mauMNYRGIk1mYzQy",
    },
    applicationUrl: "https://hr.tochka.com/vacancies/",
    applicationEmail: "hr@tochka.com",
  },
  {
    rank: 37,
    name: "MIND Software",
    sources: {
      companySite: "https://mindsw.io/career/vacancies/",
      hh: "https://hh.ru/employer/9495373?tab=VACANCIES",
      habr: "https://career.habr.com/companies/mindsw/vacancies",
      getmatch: "",
      telegram: "",
    },
    applicationUrl: "https://mindsw.io/career/",
    applicationEmail: "",
  },
];
for (const expected of expectedVacancySourceCompanies) {
  const company = data.companies.find(
    (candidate) => candidate.rank === expected.rank && candidate.name === expected.name
  );
  if (
    !company ||
    Object.entries(expected.sources).some(([field, value]) => company.vacancySources[field] !== value) ||
    company.applicationUrl !== expected.applicationUrl ||
    company.applicationEmail !== expected.applicationEmail
  ) {
    throw new Error(`${expected.name} vacancy sources, application link, or email state are incorrect`);
  }
}
if (!data.companies.every((company) => company.revenue && company.revenue.metric && company.revenue.status)) {
  throw new Error("Revenue details are missing for one or more companies");
}
const numericalRevenueCount = data.companies.filter((company) => Number.isFinite(company.revenue.amount)).length;
if (numericalRevenueCount !== 193) throw new Error(`Expected 193 numerical revenue values, found ${numericalRevenueCount}`);
if (!data.companies.every((company) => company.revenue.amount == null || company.revenue.amount >= 0)) {
  throw new Error("Invalid revenue amount");
}
const vpnAiFields = ["summary", "vpn", "ai", "chatgpt", "claude", "grok", "qwen", "deepseek", "data", "evidence", "checked", "recruiter"];
if (!data.companies.every((company) => company.vpnAi && vpnAiFields.every((field) => company.vpnAi[field]))) {
  throw new Error("VPN + AI details are missing for one or more companies");
}
const confirmedForeignAiCount = data.companies.filter((company) =>
  company.vpnAi.evidence.includes("корпоративный доступ") ||
  company.vpnAi.evidence.includes("корпоративный пилот") ||
  company.vpnAi.evidence.includes("уровне группы")
).length;
if (confirmedForeignAiCount !== 3) throw new Error(`Expected 3 confirmed/group foreign AI cases, found ${confirmedForeignAiCount}`);


console.log('Historical seed invariants: 200 companies / 204 offices / 70 jobs, salary/WLB/revenue/sources/VPN preserved.');
