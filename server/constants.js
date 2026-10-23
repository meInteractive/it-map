// Server-only; extracted from the v218 Worker.


export const COOKIE_NAME = "it_map_session";

export const GOOGLE_OAUTH_STATE_COOKIE = "google_calendar_oauth_state";

export const SESSION_TTL_SECONDS = 60 * 60 * 24 * 30;

export const GOOGLE_OAUTH_STATE_TTL_MS = 10 * 60 * 1000;

export const encoder = new TextEncoder();

export const USERS = Object.freeze({
  eugene: { name: "Eugene", role: "admin", passwordEnvKey: "SITE_PASSWORD" },
  nastya: { name: "Nastya", role: "user", passwordEnvKey: "NASTYA_PASSWORD" },
  demo: { name: "Demo", role: "user", passwordEnvKey: "DEMO_PASSWORD" }
});

export const CRM_STATUSES = ["", "Откликнулся", "Получил ответ", "Скрининг", "Собес 2", "Собес 3", "Собес 4", "Офер", "Отказ"];

// Kept as a legacy export for integrations; the application uses vacancy_roles.
export const VACANCY_ROLES = ["Менеджер проектов", "Менеджер продукта", "Руководитель разработки", "Директор по разработке"];

export const VACANCY_LEVELS = ["Senior", "Middle", "Lead", "Head"];

export const VACANCY_EXPORT_HEADERS = [
  "ID компании", "Название компании", "Название вакансии", "Ссылка на вакансию",
  "Зарплата от", "Зарплата до", "Уровень вакансии", "Роль", "Релевантность",
  "Дата публикации", "Дата загрузки", "Текст вакансии (Markdown)",
  "Тип владельца", "ID агентства", "Название агентства", "ID реального работодателя", "Реальный работодатель",
  "Заголовок сопроводительного письма", "Текст сопроводительного письма (Markdown)"
];

export const CRM_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_crm (
  company_name TEXT PRIMARY KEY NOT NULL,
  status TEXT NOT NULL DEFAULT '',
  comment_md TEXT NOT NULL DEFAULT '',
  favorite INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;

export const CRM_HISTORY_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_status_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  user_name TEXT NOT NULL DEFAULT 'Eugene',
  company_name TEXT NOT NULL,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL DEFAULT '',
  changed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE
)`;

export const CRM_HISTORY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_status_history_company_idx ON company_status_history(company_name, id)";

export const APPLICATION_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_applications (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  user_name TEXT NOT NULL DEFAULT 'Eugene',
  company_name TEXT NOT NULL,
  vacancy_id INTEGER,
  status TEXT NOT NULL DEFAULT 'Откликнулся',
  comment_md TEXT NOT NULL DEFAULT '',
  recruiter_name TEXT NOT NULL DEFAULT '',
  recruiter_phone TEXT NOT NULL DEFAULT '',
  recruiter_email TEXT NOT NULL DEFAULT '',
  recruiter_contact_type TEXT NOT NULL DEFAULT '',
  application_sources TEXT NOT NULL DEFAULT '["hh"]',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE,
  FOREIGN KEY (vacancy_id) REFERENCES company_vacancies(id) ON DELETE SET NULL
)`;

export const APPLICATION_COMPANY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_applications_user_company_idx ON company_applications(user_name, company_name, updated_at, id)";

export const APPLICATION_VACANCY_UNIQUE_SQL = "CREATE UNIQUE INDEX IF NOT EXISTS company_applications_user_vacancy_unique ON company_applications(user_name, vacancy_id) WHERE vacancy_id IS NOT NULL";

export const APPLICATION_LIMIT_TRIGGER_SQL = `CREATE TRIGGER IF NOT EXISTS company_applications_max_10
BEFORE INSERT ON company_applications
WHEN (SELECT COUNT(*) FROM company_applications WHERE user_name = NEW.user_name AND company_name = NEW.company_name) >= 10
BEGIN
  SELECT RAISE(ABORT, 'company application limit reached');
END`;

export const APPLICATION_HISTORY_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_application_status_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  application_id INTEGER NOT NULL,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL DEFAULT '',
  changed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (application_id) REFERENCES company_applications(id) ON DELETE CASCADE
)`;

export const APPLICATION_HISTORY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_application_status_history_application_idx ON company_application_status_history(application_id, id)";

export const TASK_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  user_name TEXT NOT NULL DEFAULT 'Eugene',
  company_name TEXT NOT NULL,
  vacancy_id INTEGER,
  type TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  due_at TEXT NOT NULL,
  completed INTEGER NOT NULL DEFAULT 0,
  google_event_id TEXT,
  google_event_html_link TEXT,
  google_sync_status TEXT NOT NULL DEFAULT '',
  google_sync_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  completed_at TEXT,
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE,
  FOREIGN KEY (vacancy_id) REFERENCES company_vacancies(id) ON DELETE SET NULL
)`;

export const TASK_DUE_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_tasks_user_due_idx ON company_tasks(user_name, completed, due_at)";

export const TASK_COMPANY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_tasks_user_company_idx ON company_tasks(user_name, company_name, completed, due_at)";

export const TASK_VACANCY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_tasks_user_vacancy_idx ON company_tasks(user_name, vacancy_id, completed, due_at)";

export const GOOGLE_CALENDAR_CONNECTION_TABLE_SQL = `CREATE TABLE IF NOT EXISTS google_calendar_connection (
  id INTEGER PRIMARY KEY NOT NULL CHECK (id = 1),
  account_email TEXT NOT NULL DEFAULT '',
  refresh_token_encrypted TEXT NOT NULL,
  calendar_id TEXT NOT NULL DEFAULT 'primary',
  connected_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;

export const GOOGLE_CALENDAR_USER_CONNECTION_TABLE_SQL = `CREATE TABLE IF NOT EXISTS google_calendar_user_connection (
  user_name TEXT PRIMARY KEY NOT NULL,
  account_email TEXT NOT NULL DEFAULT '',
  refresh_token_encrypted TEXT NOT NULL,
  calendar_id TEXT NOT NULL DEFAULT 'primary',
  connected_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;

export const COMPANY_USER_STATE_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_user_state (
  user_name TEXT NOT NULL,
  company_name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT '',
  comment_md TEXT NOT NULL DEFAULT '',
  favorite INTEGER NOT NULL DEFAULT 0,
  hidden INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_name, company_name),
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE
)`;

export const COMPANY_USER_STATE_FAVORITE_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_user_state_user_favorite_idx ON company_user_state(user_name, favorite, company_name)";

export const COMPANY_USER_STATE_HIDDEN_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_user_state_user_hidden_idx ON company_user_state(user_name, hidden, company_name)";

export const VACANCY_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_vacancies (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  company_name TEXT NOT NULL,
  entity_type TEXT NOT NULL DEFAULT 'company',
  real_company_name TEXT,
  url TEXT NOT NULL,
  salary_from INTEGER,
  salary_to INTEGER,
  title TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT '',
  level TEXT NOT NULL DEFAULT '',
  relevance INTEGER CHECK (relevance IS NULL OR (relevance >= 0 AND relevance <= 100)),
  published_at TEXT,
  loaded_at TEXT,
  last_checked_at TEXT,
  archived INTEGER NOT NULL DEFAULT 0,
  description_md TEXT NOT NULL DEFAULT '',
  cover_letter_subject TEXT NOT NULL DEFAULT '',
  cover_letter_md TEXT NOT NULL DEFAULT '',
  application_status TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE
)`;

export const VACANCY_COMPANY_DATE_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_vacancies_company_date_idx ON company_vacancies(company_name, published_at, id)";

export const VACANCY_ENTITY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_vacancies_entity_idx ON company_vacancies(entity_type, company_name, real_company_name)";

export const VACANCY_URL_UNIQUE_SQL = "CREATE UNIQUE INDEX IF NOT EXISTS company_vacancies_url_unique ON company_vacancies(url)";

export const VACANCY_EMPLOYER_GUESS_TABLE_SQL = `CREATE TABLE IF NOT EXISTS vacancy_employer_guesses (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  agency_vacancy_id INTEGER NOT NULL,
  position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 5),
  employer_name TEXT NOT NULL DEFAULT '',
  linked_company_name TEXT,
  matched_vacancy_id INTEGER,
  external_vacancy_url TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (agency_vacancy_id) REFERENCES company_vacancies(id) ON DELETE CASCADE,
  FOREIGN KEY (linked_company_name) REFERENCES company_crm(company_name) ON DELETE SET NULL,
  FOREIGN KEY (matched_vacancy_id) REFERENCES company_vacancies(id) ON DELETE SET NULL,
  UNIQUE (agency_vacancy_id, position),
  CHECK (matched_vacancy_id IS NULL OR matched_vacancy_id <> agency_vacancy_id),
  CHECK (length(trim(employer_name)) > 0 OR linked_company_name IS NOT NULL OR matched_vacancy_id IS NOT NULL OR external_vacancy_url IS NOT NULL)
)`;

export const VACANCY_EMPLOYER_GUESS_COMPANY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS vacancy_employer_guesses_linked_company_idx ON vacancy_employer_guesses(linked_company_name)";

export const VACANCY_EMPLOYER_GUESS_MATCH_INDEX_SQL = "CREATE INDEX IF NOT EXISTS vacancy_employer_guesses_matched_vacancy_idx ON vacancy_employer_guesses(matched_vacancy_id)";

export const VACANCY_EMPLOYER_GUESS_AGENCY_INSERT_TRIGGER_SQL = `CREATE TRIGGER IF NOT EXISTS vacancy_employer_guesses_agency_insert
BEFORE INSERT ON vacancy_employer_guesses
WHEN COALESCE((SELECT entity_type FROM company_vacancies WHERE id = NEW.agency_vacancy_id), '') <> 'agency'
BEGIN
  SELECT RAISE(ABORT, 'employer guesses require an agency vacancy');
END`;

export const VACANCY_EMPLOYER_GUESS_AGENCY_UPDATE_TRIGGER_SQL = `CREATE TRIGGER IF NOT EXISTS vacancy_employer_guesses_agency_update
BEFORE UPDATE OF agency_vacancy_id ON vacancy_employer_guesses
WHEN COALESCE((SELECT entity_type FROM company_vacancies WHERE id = NEW.agency_vacancy_id), '') <> 'agency'
BEGIN
  SELECT RAISE(ABORT, 'employer guesses require an agency vacancy');
END`;

export const VACANCY_EMPLOYER_GUESS_MATCH_INSERT_TRIGGER_SQL = `CREATE TRIGGER IF NOT EXISTS vacancy_employer_guesses_company_match_insert
BEFORE INSERT ON vacancy_employer_guesses
WHEN NEW.matched_vacancy_id IS NOT NULL AND COALESCE((SELECT entity_type FROM company_vacancies WHERE id = NEW.matched_vacancy_id), '') <> 'company'
BEGIN
  SELECT RAISE(ABORT, 'matched vacancy must belong to a company');
END`;

export const VACANCY_EMPLOYER_GUESS_MATCH_UPDATE_TRIGGER_SQL = `CREATE TRIGGER IF NOT EXISTS vacancy_employer_guesses_company_match_update
BEFORE UPDATE OF matched_vacancy_id ON vacancy_employer_guesses
WHEN NEW.matched_vacancy_id IS NOT NULL AND COALESCE((SELECT entity_type FROM company_vacancies WHERE id = NEW.matched_vacancy_id), '') <> 'company'
BEGIN
  SELECT RAISE(ABORT, 'matched vacancy must belong to a company');
END`;

export const VACANCY_STATE_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_vacancy_state (
  company_name TEXT PRIMARY KEY NOT NULL,
  initialized_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE
)`;

export const VACANCY_USER_STATE_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_vacancy_user_state (
  user_name TEXT NOT NULL,
  vacancy_id INTEGER NOT NULL,
  relevance INTEGER CHECK (relevance IS NULL OR (relevance >= 0 AND relevance <= 100)),
  favorite INTEGER NOT NULL DEFAULT 0,
  hidden INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_name, vacancy_id),
  FOREIGN KEY (vacancy_id) REFERENCES company_vacancies(id) ON DELETE CASCADE
)`;

export const VACANCY_USER_STATE_FAVORITE_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_vacancy_user_state_user_favorite_idx ON company_vacancy_user_state(user_name, favorite, vacancy_id)";

export const VACANCY_COVER_LETTERS_TABLE_SQL = `CREATE TABLE IF NOT EXISTS vacancy_cover_letters (
  user_name TEXT NOT NULL, vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE,
  subject TEXT NOT NULL, markdown TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT vacancy_cover_letters_subject_length CHECK(length(subject) BETWEEN 1 AND 512),
  CONSTRAINT vacancy_cover_letters_markdown_length CHECK(length(markdown) BETWEEN 1 AND 2000)
)`;

export const VACANCY_COVER_LETTERS_INDEX_SQL = 'CREATE UNIQUE INDEX IF NOT EXISTS vacancy_cover_letters_user_vacancy_unique ON vacancy_cover_letters(user_name,vacancy_id)';

export const VACANCY_USER_STATE_HIDDEN_INDEX_SQL = "CREATE INDEX IF NOT EXISTS company_vacancy_user_state_user_hidden_idx ON company_vacancy_user_state(user_name, hidden, vacancy_id)";

export const VACANCY_RELEVANCE_PROFILE_STATE_TABLE_SQL = `CREATE TABLE IF NOT EXISTS vacancy_relevance_profile_state (
  user_name TEXT PRIMARY KEY NOT NULL,
  profile_version TEXT NOT NULL,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;

export const EUGENE_RELEVANCE_PROFILE_VERSION = "resume-2026-09-03-v1";

export const APP_CATALOG_TABLE_SQL = `CREATE TABLE IF NOT EXISTS app_catalog (
  id INTEGER PRIMARY KEY NOT NULL CHECK (id = 1),
  data_json TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;

export const COMPANY_IDENTITY_TABLE_SQL = `CREATE TABLE IF NOT EXISTS company_identity (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  company_name TEXT NOT NULL UNIQUE
)`;

export const AGENCY_IDENTITY_TABLE_SQL = `CREATE TABLE IF NOT EXISTS agency_identity (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  agency_name TEXT NOT NULL UNIQUE
)`;

export const TELEGRAM_CHANNEL_TABLE_SQL = `CREATE TABLE IF NOT EXISTS telegram_channels (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  name TEXT NOT NULL UNIQUE,
  url TEXT NOT NULL,
  normalized_username TEXT NOT NULL UNIQUE,
  username TEXT NOT NULL DEFAULT '',
  last_successful_check_at TEXT,
  comment TEXT NOT NULL DEFAULT '' CHECK (length(comment) <= 512),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;

export const VACANCY_TELEGRAM_SOURCE_TABLE_SQL = `CREATE TABLE IF NOT EXISTS vacancy_telegram_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  vacancy_id INTEGER NOT NULL,
  channel_id INTEGER NOT NULL,
  message_url TEXT NOT NULL UNIQUE,
  telegram_contact TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (vacancy_id) REFERENCES company_vacancies(id) ON DELETE CASCADE,
  FOREIGN KEY (channel_id) REFERENCES telegram_channels(id) ON DELETE CASCADE,
  UNIQUE (vacancy_id, channel_id)
)`;

export const VACANCY_TELEGRAM_SOURCE_CHANNEL_INDEX_SQL = "CREATE INDEX IF NOT EXISTS vacancy_telegram_sources_channel_idx ON vacancy_telegram_sources(channel_id, vacancy_id)";

export const HIRING_CONTACT_TABLE_SQL = `CREATE TABLE IF NOT EXISTS hiring_contacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
  company_name TEXT NOT NULL,
  position INTEGER NOT NULL CHECK (position BETWEEN 1 AND 3),
  full_name TEXT NOT NULL,
  linkedin_url TEXT NOT NULL DEFAULT '',
  facebook_url TEXT NOT NULL DEFAULT '',
  setka_url TEXT NOT NULL DEFAULT '',
  telegram TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  specialization TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (company_name) REFERENCES company_crm(company_name) ON DELETE CASCADE,
  UNIQUE (company_name, position)
)`;

export const HIRING_CONTACT_COMPANY_INDEX_SQL = "CREATE INDEX IF NOT EXISTS hiring_contacts_company_idx ON hiring_contacts(company_name, position)";

export const TASK_TYPES = ["call", "email", "messenger", "meeting"];

export const RECRUITER_CONTACT_TYPES = ["", "direct", "agency"];

export const APPLICATION_SOURCES = ["hh", "site", "habr", "email", "telegram", "hcm"];


export const MAX_APPLICATIONS_PER_COMPANY = 10;

export const MAX_VACANCY_DESCRIPTION_LENGTH = 100000;

export const MAX_COVER_LETTER_SUBJECT_LENGTH = 512;

export const MAX_COVER_LETTER_LENGTH = 2000;

export const MAX_VACANCY_IMPORT_ROWS = 2000;

export const MAX_COMPANY_IMPORT_ROWS = 500;

export const MAX_COMPANY_OFFICE_IMPORT_ROWS = 2000;

export const MAX_HIRING_CONTACT_IMPORT_ROWS = 1500;

export const MAX_HIRING_CONTACTS_PER_COMPANY = 3;

export const MAX_TELEGRAM_CHANNEL_COMMENT_LENGTH = 512;

export const TELEGRAM_ONLY_OWNER_PREFIX = "__telegram_channel__:";

export const AGENCY_TYPE_OPTIONS = ["Executive Search", "IT Recruitment", "Professional Recruitment", "Mass Recruitment", "RPO", "Staffing / Outstaffing"];

export const AGENCY_FUNCTION_OPTIONS = ["Top Management", "Engineering / Development", "Product", "Project / Program / Delivery", "IT / CIO", "Data / AI", "Cybersecurity", "Infrastructure / DevOps", "Digital Transformation", "Sales / Business Development", "Finance", "HR", "Operations"];

export const AGENCY_POSITION_LEVEL_OPTIONS = ["Middle", "Senior", "Lead", "Head", "Director", "C-level", "Board"];

export const AGENCY_EXECUTIVE_SEARCH_OPTIONS = ["Да", "Частично", "Нет", "Не подтверждено"];

export const AGENCY_CLOSED_VACANCIES_OPTIONS = ["Основной формат", "Значительная часть", "Есть", "Практически нет", "Не подтверждено"];

export const AGENCY_VACANCY_PUBLICITY_OPTIONS = ["Большинство публичные", "Значительная часть публична", "Небольшая часть публична", "Публичных почти нет", "Неизвестно"];

export const AGENCY_EMPLOYER_DISCLOSURE_OPTIONS = ["Обычно указан", "Иногда скрыт", "Обычно скрыт", "Неизвестно"];

export const AGENCY_SALARY_DISCLOSURE_OPTIONS = ["Обычно указана", "Иногда указана", "Редко указана", "Практически никогда", "Неизвестно"];

export const AGENCY_PRIMARY_CHANNEL_OPTIONS = ["Сайт", "HH", "Хабр", "Getmatch", "Telegram", "LinkedIn", "Через консультантов", "Закрытая база"];

export const AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS = ["Высокое", "Среднее", "Низкое", "Публичных источников нет"];

export const AGENCY_VACANCY_FRESHNESS_OPTIONS = ["Высокая", "Средняя", "Низкая", "Не удалось оценить"];

export const AGENCY_ACCEPTS_CV_OPTIONS = ["Да", "Нет", "Не найдено"];

export const AGENCY_CONTACT_METHOD_OPTIONS = ["Форма / база CV", "Email", "Telegram", "Конкретный консультант", "Отклик на вакансию"];

export const VACANCY_SOURCE_META = [
  ["companySite", "Сайт компании"],
  ["hh", "HH"],
  ["habr", "Хабр"],
  ["getmatch", "Getmatch"],
  ["telegram", "Telegram"],
  ["dreamJob", "DreamJob"],
  ["hCareers", "Эйч"],
  ["rabota", "rabota.ru"],
];

export const REVIEW_SOURCE_META = [
  ["habr", "Habr"],
  ["dreamJob", "DreamJob"],
  ["glassDoor", "GlassDoor"],
  ["superJob", "SuperJob"],
  ["pravdaSotrudnikov", "Правда Сотрудников"],
  ["nahjob", "Nahjob"],
  ["bossNadzor", "BossNadzor"],
  ["yandexMap", "YandexMap"],
];

export const AGENCY_REVIEW_SOURCE_META = [
  ...REVIEW_SOURCE_META,
  ["twoGis", "2GIS"],
  ["otzovik", "Otzovik"],
  ["personAgency", "PersonAgency"],
  ["yell", "Yell"],
];

export const AGENCY_RATING_INPUT_META = [
  ["ethics", "Этика", 1.6],
  ["vacancyHonesty", "Честность вакансий", 1.4],
  ["feedback", "Обратная связь", 1.2],
  ["vacancyRelevance", "Релевантность вакансий", 1.0],
  ["itExpertise", "IT-экспертиза", 1.0],
  ["clientKnowledge", "Знание клиента", 0.8],
  ["processOrganization", "Организация процесса", 0.5],
  ["candidateSupport", "Поддержка кандидата", 0.5],
];

export const COMPANY_VACANCY_SOURCE_URLS = {
  "Авито": {
    companySite: "https://career.avito.com/vacancies/",
    hh: "https://hh.ru/employer/84585?tab=VACANCIES",
    habr: "https://career.habr.com/companies/avito/vacancies",
    getmatch: "https://getmatch.ru/companies/ZDv9oblG-avito",
    telegram: "https://t.me/s/avito_career",
  },
  "МегаФон": {
    companySite: "https://job.megafon.ru/vacancy/all/any",
    hh: "https://hh.ru/employer/3127?tab=VACANCIES",
    habr: "https://career.habr.com/companies/megafon/vacancies",
    getmatch: "https://getmatch.ru/companies/m6QOjDvr-megafon",
    telegram: "https://t.me/s/megafonjobs",
  },
  PPR: {
    companySite: "https://career.pprcard.ru/catalog",
    hh: "https://hh.ru/employer/594824?tab=VACANCIES",
    habr: "https://career.habr.com/companies/petroplus/vacancies",
    getmatch: "https://getmatch.ru/companies/1wRLypvX-peredovye-platezhnye-resheniia",
    telegram: "https://t.me/s/careerppr",
  },
};

export const COMPANY_APPLICATION_URLS = {
  "Авито": "https://career.avito.com/vacancies/",
  "МегаФон": "https://job.megafon.ru/vacancy/all/any",
  PPR: "https://career.pprcard.ru/catalog",
  VK: "https://team.vk.company/feedback/44870/",
};

export const COMPANY_APPLICATION_EMAILS = {
  "МегаФон": "career@megafon.ru",
  PPR: "recruitment@pprcard.ru",
};

export const BACKUP_FORMAT = "moscow-it-employers-backup";

export const BACKUP_VERSION = 39;

export const BACKUP_MIGRATION_TABLES = ['d1_migrations', '__drizzle_migrations', '__appgarden_migrations'];

export const SUPPORTED_BACKUP_VERSIONS = new Set([14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, BACKUP_VERSION]);

export const RATING_FORMULA_VERSION = 5;

export const WLB_RATING_WEIGHT = 20;

export const BACKUP_V2_REQUIRED_COLUMNS = {
  company_tasks: ["id", "company_name", "vacancy_id", "type", "note", "due_at", "completed", "created_at", "completed_at"],
  company_vacancies: ["id", "company_name", "url", "title", "role", "level", "relevance", "loaded_at", "description_md", "application_status"],
  company_applications: ["id", "company_name", "vacancy_id", "status", "comment_md", "recruiter_name", "recruiter_phone", "recruiter_email", "recruiter_contact_type"],
  company_application_status_history: ["id", "application_id", "from_status", "to_status", "changed_at"]
};

export const MAX_BACKUP_BYTES = 25 * 1024 * 1024;

export const MAX_BACKUP_TABLES = 52;

export const MAX_BACKUP_ROWS = 200000;

export const schemaState = {ready:false};
