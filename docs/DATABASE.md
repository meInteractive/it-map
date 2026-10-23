# База данных

Актуально на 23.10.2026, редакция документации 224. SQLite / Cloudflare D1, binding `DB`. Исходная схема: 84 SQL-миграции 0000–0083, последняя `0083_parser_candidate_intakes` — происхождение первичных вакансий. Бэкап v39, рейтинг v5. Наличие миграции в Git не доказывает её применение к конкретной базе: проверяйте журнал среды отдельно.

`vacancy_roles` хранит стабильный ключ, название, nullable parent_id и порядок; FK с RESTRICT не допускает удаления родителей. `vacancy_role_assignments` связывает вакансии и конечные роли отношением многие-ко-многим. Циклы и назначение категорий проверяет backend. Миграция0052 создаёт начальное дерево, нормализует известное «Менеджер продукта» в «Менеджер продуктов», а неизвестные значения сохраняет под технической категорией без смыслового перераспределения.

## Каталог и связи

`app_catalog.data_json` — источник истины компаний и агентств: общий JSON-каталог с офисами, отзывами, источниками, зарплатами и рейтингом. `src/data.json` — исторический серверный seed для отсутствующего каталога, не рабочая клиентская база. Компактные API формируют проекции этого каталога, а не вторую независимую копию.

`company_identity` и `agency_identity` задают постоянные числовые ID для Excel/API. Место в рейтинге `rank` не является ID. `company_crm` — общий узел связей по названию. Компания связана с вакансиями, откликами, задачами, контактами и личными состояниями. `real_company_name` — заявленный работодатель агентской/канальной вакансии, не всегда внешний ключ каталога.

Вакансия имеет личные состояния, письма, Telegram-источники и предположения о работодателе. SQL-удаление вакансии обнуляет `vacancy_id` в откликах/задачах и каскадно удаляет зависимые состояния/источники/письма; HTTP-обработчик может вместо удаления архивировать вакансию с откликами или задачами. Удаление отклика каскадно удаляет его историю. Точные FK и индексы приведены в справочнике ниже.

## Общие и личные данные

Таблицы с `user_name` фильтруются сервером по сессии. `USERS` и роли объявлены в `server/constants.js`, авторизация — в `server/auth/session.js`; SQL-таблицы аккаунтов нет. Администратор в обычных личных API работает со своими данными; полный бэкап охватывает всех пользователей.

| Сущность | Правила |
| --- | --- |
| user_profiles | Резюме Markdown до 20 000 Unicode-символов, числовая revision и дата. Отсутствующая строка означает пустой профиль |
| system_prompts | Общие cover_letter до 15 000 и relevance до 5 000 символов; каждый с собственной revision. Только админские API |
| prompt_model_settings | Общий provider/openai_model/reasoning_effort для каждого kind, независимая числовая revision |
| openai_settings | Зашифрованный ключ OpenAI, суффикс, revision и дата; ключ DB приоритетнее необязательного OPENAI_API_KEY среды |
| company_user_state | Личные CRM-поля, favorite/hidden организации; уникальная пара пользователь/организация |
| company_vacancy_user_state | Личные relevance/favorite/hidden вакансии; unique пользователь/вакансия |
| company_applications | Общий отклик на организацию либо связанный с вакансией; до 10 на организацию и один на вакансию для пользователя |
| company_tasks | Личные задачи; due_at в ISO UTC, UI вводит время МСК, поля синхронизации Google Calendar |
| vacancy_cover_letters | Личные subject до 512 и markdown до 2000 символов, override_shared 0/1 и revision ≥0; unique пользователь/вакансия |

При чтении письма личная запись с `override_shared=1` имеет приоритет, затем заполненное общее письмо из `company_vacancies`, затем обычное личное письмо. Автогенерация переиспользует сохранённый результат. Явная повторная генерация записывает новое личное письмо с override_shared=1 и увеличивает его revision, не меняя общее письмо и других кандидатов. Revision формы вакансии/отклика — SHA-256 снимка, не SQL-счётчик и не версия API.

## Значения и форматы

- `status`, `from_status`, `to_status`: пустая строка или этап из `CRM_STATUSES`, включая «Отказ».
- `application_sources`: JSON-массив hh, site, habr, email, telegram, hcm. HCM отображается как «HCM-система».
- `favorite`, `hidden`, `archived`, `completed`, `override_shared`: INTEGER 0/1. Архивность общая, скрытие личное.
- `relevance`: целое 0–100 или NULL; `score` каталога — отдельный рейтинг работодателя.
- `salary_from`, `salary_to`: целые рубли за месяц либо NULL; NULL отличается от 0.
- `published_at`, `loaded_at`: даты вакансии; `due_at`: момент времени; `created_at`, `updated_at`, `last_checked_at`: служебные даты. Точное восстановление сохраняет исходные значения, не переписывает их формат.
- `comment_md`, `description_md`, `cover_letter_md`: Markdown. Имена API могут отличаться, например comment/descriptionMd.
- `position`: слот 1–3 для контактов, 1–5 для предположений работодателя.
- `key_encrypted`, `refresh_token_encrypted`: AES-GCM; SESSION_SECRET хранится в среде и не экспортируется. OpenAI использует отдельный домен openai:v1.

## Структура каталога

| Группа | Поля |
| --- | --- |
| Корень | companies[], agencies[], updated и метаданные |
| Компания | name, sector, roles, score, rank, ratingFormulaVersion, rating, wlb, wlbScore, aiScore, aiEvidence, deferment, mobilization, confidence, risk |
| Зарплаты | middle, senior, lead, head: диапазоны [от, до] |
| Источники вакансий | vacancySources.companySite, hh, habr, getmatch, telegram, dreamJob, hCareers, rabota |
| Источники отзывов | reviewSources.habr, dreamJob, glassDoor, superJob, pravdaSotrudnikov, nahjob, bossNadzor, yandexMap |
| Отзывы | reviews.plusShort, minusShort, plus, minus |
| Выручка | revenue.amount, year, metric, legal, inn, composition, coverage, status, source1, source2, comment |
| Жильё | housing.status, form, terms, after, source |
| ИИ и VPN | vpnAi.summary, vpn, ai, chatgpt, claude, grok, qwen, deepseek, data, evidence, source1, source2, checked, recruiter |
| Офисы | offices[]: address, metro, role, main, status, source, lat, lon |
| Исторические вакансии | jobs[]: однократный перенос в SQL, отметка company_vacancy_state предотвращает повтор |
| Агентства | name, sector, agencyTypes, functionalSpecializations, positionLevels, оценки и другие поля из карты импорта |

`rabota` — необязательная строка URL, отсутствие в старом каталоге допустимо. Источник вакансий DreamJob и источник отзывов DreamJob — разные поля.

## Операционные таблицы и восстановление

`app_runtime_generation` хранит UUID поколения базы. Успешный restore меняет его в той же транзакции, даже при одинаковых данных. `application_creation_receipts` хранит результаты идемпотентного создания откликов: пользователь, UUID запроса, поколение, хеш тела и ID результата. `cover_letter_generation_leases` хранит право одной генерации по паре пользователь/вакансия, хеш входов, владельца и срок.

Эти 3 таблицы не входят в `storage.tables` бэкапа и не импортируются. Они переживают restore, но новое поколение делает прежние права и запросы недействительными. Receipt не содержит контакты/комментарий, lease не содержит резюме/промт. Защита применяется между разными Worker instances, не только внутри одного процесса.

Бэкап v35 переносит 49 прикладных таблиц, включая дерево и связи ролей. Старые полные v14–32 принимаются по правилам [формата](TRANSFER_FORMATS.md) и получают безопасное преобразование одиночных ролей. Частичный layout отвергается.

## Источник справочника SQL

`drizzle/*.sql` — история миграций; `db/schema.ts` — декларация Drizzle. Генератор `scripts/backup-schema.mjs` выполняет миграции в изолированной SQLite в памяти и читает PRAGMA/DDL, включая исторические поля. В рабочую базу генератор не обращается. `d1_migrations`, `__drizzle_migrations`, `__appgarden_migrations` — журналы среды, переносятся как metadata, не заменяются при restore; _cf_*/sqlite_* исключены, sqlite_sequence переносится отдельно.

Справочник ниже генерируется при prepare-server/build: поля, типы, NOT NULL, PRIMARY KEY, defaults, внешние ключи, DDL и индексы. Автоматические блоки вручную не редактируют.

<!-- AUTO:DATABASE_TABLES -->
## agency_identity

Постоянные числовые ID агентств для Excel.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| agency_name | TEXT | Да | — | — |

```sql
CREATE TABLE `agency_identity` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`agency_name` text NOT NULL
);
CREATE UNIQUE INDEX `agency_identity_agency_name_unique` ON `agency_identity` (`agency_name`);
```

## apify_settings

Apify HH: AES-GCM токены Free/Starter, разрешения, CAS, безопасные billing snapshots. После restore сбор и Starter выключены.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Нет | PRIMARY KEY | — |
| enabled | INTEGER | Да | — | 0 |
| paid_allowed | INTEGER | Да | — | 0 |
| revision | INTEGER | Да | — | 0 |
| free_key_encrypted | TEXT | Да | — | '' |
| free_key_suffix | TEXT | Да | — | '' |
| starter_key_encrypted | TEXT | Да | — | '' |
| starter_key_suffix | TEXT | Да | — | '' |
| free_account_json | TEXT | Да | — | '' |
| starter_account_json | TEXT | Да | — | '' |
| free_error | TEXT | Да | — | '' |
| starter_error | TEXT | Да | — | '' |

```sql
CREATE TABLE apify_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 paid_allowed INTEGER NOT NULL DEFAULT 0 CHECK(paid_allowed IN (0,1)), revision INTEGER NOT NULL DEFAULT 0,
 free_key_encrypted TEXT NOT NULL DEFAULT '', free_key_suffix TEXT NOT NULL DEFAULT '',
 starter_key_encrypted TEXT NOT NULL DEFAULT '', starter_key_suffix TEXT NOT NULL DEFAULT '',
 free_account_json TEXT NOT NULL DEFAULT '', starter_account_json TEXT NOT NULL DEFAULT '',
 free_error TEXT NOT NULL DEFAULT '', starter_error TEXT NOT NULL DEFAULT ''
);
```

## app_catalog

Основной JSON-каталог компаний и агентств. Исходные seed-данные находятся в src/data.json; рабочая копия хранится здесь.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| data_json | TEXT | Да | — | — |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `app_catalog` (
	`id` integer PRIMARY KEY NOT NULL,
	`data_json` text NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
```

## company_application_status_history

История смены статуса конкретного отклика.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| application_id | INTEGER | Да | — | — |
| from_status | TEXT | Да | — | '' |
| to_status | TEXT | Да | — | '' |
| changed_at | TEXT | Да | — | CURRENT_TIMESTAMP |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| application_id | company_applications.id | CASCADE |

```sql
CREATE TABLE `company_application_status_history` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`application_id` integer NOT NULL,
	`from_status` text DEFAULT '' NOT NULL,
	`to_status` text DEFAULT '' NOT NULL,
	`changed_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`application_id`) REFERENCES `company_applications`(`id`) ON UPDATE no action ON DELETE cascade
);
CREATE INDEX `company_application_status_history_application_idx` ON `company_application_status_history` (`application_id`,`id`);
```

## company_applications

Отклики пользователя на организацию или вакансию; содержит контакты рекрутера и application_sources (JSON).

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| company_name | TEXT | Да | — | — |
| vacancy_id | INTEGER | Нет | — | — |
| status | TEXT | Да | — | 'Откликнулся' |
| comment_md | TEXT | Да | — | '' |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| recruiter_name | TEXT | Да | — | '' |
| recruiter_phone | TEXT | Да | — | '' |
| recruiter_email | TEXT | Да | — | '' |
| recruiter_contact_type | TEXT | Да | — | '' |
| application_sources | TEXT | Да | — | '["hh"]' |
| user_name | TEXT | Да | — | 'Eugene' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| vacancy_id | company_vacancies.id | SET NULL |
| company_name | company_crm.company_name | CASCADE |

```sql
CREATE TABLE `company_applications` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`company_name` text NOT NULL,
	`vacancy_id` integer,
	`status` text DEFAULT 'Откликнулся' NOT NULL,
	`comment_md` text DEFAULT '' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `recruiter_name` text DEFAULT '' NOT NULL, `recruiter_phone` text DEFAULT '' NOT NULL, `recruiter_email` text DEFAULT '' NOT NULL, `recruiter_contact_type` text DEFAULT '' NOT NULL, `application_sources` text NOT NULL DEFAULT '["hh"]', `user_name` text DEFAULT 'Eugene' NOT NULL,
	FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE set null
);
CREATE INDEX `company_applications_user_company_idx` ON `company_applications` (`user_name`,`company_name`,`updated_at`,`id`);
CREATE UNIQUE INDEX `company_applications_user_vacancy_unique` ON `company_applications` (`user_name`,`vacancy_id`) WHERE `vacancy_id` IS NOT NULL;
CREATE TRIGGER `company_applications_max_10`
BEFORE INSERT ON `company_applications`
WHEN (SELECT COUNT(*) FROM `company_applications` WHERE `user_name` = NEW.`user_name` AND `company_name` = NEW.`company_name`) >= 10
BEGIN
  SELECT RAISE(ABORT, 'company application limit reached');
END;
```

## company_crm

Общие строковые ключи организаций и исторические CRM-поля. Личное состояние хранится отдельно.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| company_name | TEXT | Да | PRIMARY KEY | — |
| status | TEXT | Да | — | '' |
| comment_md | TEXT | Да | — | '' |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| favorite | INTEGER | Да | — | false |

```sql
CREATE TABLE `company_crm` (
	`company_name` text PRIMARY KEY NOT NULL,
	`status` text DEFAULT '' NOT NULL,
	`comment_md` text DEFAULT '' NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
, `favorite` integer DEFAULT false NOT NULL);
```

## company_identity

Постоянные числовые ID компаний для Excel. Не путать с местом в рейтинге.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| company_name | TEXT | Да | — | — |

```sql
CREATE TABLE `company_identity` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`company_name` text NOT NULL
);
CREATE UNIQUE INDEX `company_identity_company_name_unique` ON `company_identity` (`company_name`);
```

## company_status_history

История изменений CRM-статуса организации с владельцем.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| company_name | TEXT | Да | — | — |
| from_status | TEXT | Да | — | '' |
| to_status | TEXT | Да | — | '' |
| changed_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| user_name | TEXT | Да | — | 'Eugene' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| company_name | company_crm.company_name | CASCADE |

```sql
CREATE TABLE `company_status_history` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`company_name` text NOT NULL,
	`from_status` text DEFAULT '' NOT NULL,
	`to_status` text DEFAULT '' NOT NULL,
	`changed_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `user_name` text DEFAULT 'Eugene' NOT NULL,
	FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade
);
CREATE INDEX `company_status_history_company_idx` ON `company_status_history` (`company_name`,`id`);
```

## company_tasks

Задачи пользователя, сроки и состояние синхронизации с календарём.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| company_name | TEXT | Да | — | — |
| type | TEXT | Да | — | — |
| note | TEXT | Да | — | '' |
| due_at | TEXT | Да | — | — |
| completed | INTEGER | Да | — | false |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| completed_at | TEXT | Нет | — | — |
| vacancy_id | INTEGER | Нет | — | — |
| google_event_id | TEXT | Нет | — | — |
| google_event_html_link | TEXT | Нет | — | — |
| google_sync_status | TEXT | Да | — | '' |
| google_sync_error | TEXT | Да | — | '' |
| user_name | TEXT | Да | — | 'Eugene' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| company_name | company_crm.company_name | CASCADE |
| vacancy_id | company_vacancies.id | SET NULL |

```sql
CREATE TABLE `company_tasks` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`company_name` text NOT NULL,
	`type` text NOT NULL,
	`note` text DEFAULT '' NOT NULL,
	`due_at` text NOT NULL,
	`completed` integer DEFAULT false NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`completed_at` text, `vacancy_id` integer REFERENCES company_vacancies(id) ON DELETE SET NULL, `google_event_id` text, `google_event_html_link` text, `google_sync_status` text DEFAULT '' NOT NULL, `google_sync_error` text DEFAULT '' NOT NULL, `user_name` text DEFAULT 'Eugene' NOT NULL,
	FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade
);
CREATE INDEX `company_tasks_user_company_idx` ON `company_tasks` (`user_name`,`company_name`,`completed`,`due_at`);
CREATE INDEX `company_tasks_user_due_idx` ON `company_tasks` (`user_name`,`completed`,`due_at`);
CREATE INDEX `company_tasks_user_vacancy_idx` ON `company_tasks` (`user_name`,`vacancy_id`,`completed`,`due_at`);
```

## company_user_state

Личные статус, комментарий, избранное и скрытие организации; уникальна пара пользователь + организация.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | — | — |
| company_name | TEXT | Да | — | — |
| status | TEXT | Да | — | '' |
| comment_md | TEXT | Да | — | '' |
| favorite | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| hidden | INTEGER | Да | — | 0 |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| company_name | company_crm.company_name | CASCADE |

```sql
CREATE TABLE `company_user_state` (
  `user_name` text NOT NULL,
  `company_name` text NOT NULL,
  `status` text DEFAULT '' NOT NULL,
  `comment_md` text DEFAULT '' NOT NULL,
  `favorite` integer DEFAULT 0 NOT NULL,
  `updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `hidden` integer DEFAULT 0 NOT NULL,
  FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade
);
CREATE UNIQUE INDEX `company_user_state_user_company_unique` ON `company_user_state` (`user_name`,`company_name`);
CREATE INDEX `company_user_state_user_favorite_idx` ON `company_user_state` (`user_name`,`favorite`,`company_name`);
CREATE INDEX `company_user_state_user_hidden_idx` ON `company_user_state` (`user_name`,`hidden`,`company_name`);
```

## company_vacancies

Общие вакансии. company_name обозначает владельца: компанию, агентство или служебную сущность канала; entity_type уточняет тип.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| company_name | TEXT | Да | — | — |
| url | TEXT | Да | — | — |
| salary_from | INTEGER | Нет | — | — |
| salary_to | INTEGER | Нет | — | — |
| title | TEXT | Да | — | — |
| published_at | TEXT | Нет | — | — |
| description_md | TEXT | Да | — | '' |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| application_status | TEXT | Да | — | '' |
| level | TEXT | Да | — | '' |
| loaded_at | TEXT | Нет | — | — |
| role | TEXT | Да | — | '' |
| relevance | INTEGER | Нет | — | — |
| entity_type | TEXT | Да | — | 'company' |
| real_company_name | TEXT | Нет | — | — |
| cover_letter_subject | TEXT | Да | — | '' |
| cover_letter_md | TEXT | Да | — | '' |
| last_checked_at | TEXT | Нет | — | — |
| archived | INTEGER | Да | — | 0 |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| company_name | company_crm.company_name | CASCADE |

```sql
CREATE TABLE `company_vacancies` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`company_name` text NOT NULL,
	`url` text NOT NULL,
	`salary_from` integer,
	`salary_to` integer,
	`title` text NOT NULL,
	`published_at` text,
	`description_md` text DEFAULT '' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `application_status` text DEFAULT '' NOT NULL, `level` text DEFAULT '' NOT NULL, `loaded_at` text, `role` text DEFAULT '' NOT NULL, `relevance` integer
CHECK (`relevance` IS NULL OR (`relevance` >= 0 AND `relevance` <= 100)), `entity_type` text DEFAULT 'company' NOT NULL, `real_company_name` text, `cover_letter_subject` text DEFAULT '' NOT NULL, `cover_letter_md` text DEFAULT '' NOT NULL, last_checked_at TEXT, archived INTEGER NOT NULL DEFAULT 0,
	FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade
);
CREATE INDEX `company_vacancies_company_date_idx` ON `company_vacancies` (`company_name`,`published_at`,`id`);
CREATE INDEX `company_vacancies_entity_idx` ON `company_vacancies` (`entity_type`,`company_name`,`real_company_name`);
CREATE UNIQUE INDEX `company_vacancies_url_unique` ON `company_vacancies` (`url`);
CREATE TRIGGER parser_protect_description_md AFTER UPDATE OF description_md ON company_vacancies WHEN OLD.description_md IS NOT NEW.description_md AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='description_md'; END;
CREATE TRIGGER parser_protect_level AFTER UPDATE OF level ON company_vacancies WHEN OLD.level IS NOT NEW.level AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='level'; END;
CREATE TRIGGER parser_protect_published_at AFTER UPDATE OF published_at ON company_vacancies WHEN OLD.published_at IS NOT NEW.published_at AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='published_at'; END;
CREATE TRIGGER parser_protect_salary_from AFTER UPDATE OF salary_from ON company_vacancies WHEN OLD.salary_from IS NOT NEW.salary_from AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='salary_from'; END;
CREATE TRIGGER parser_protect_salary_to AFTER UPDATE OF salary_to ON company_vacancies WHEN OLD.salary_to IS NOT NEW.salary_to AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='salary_to'; END;
CREATE TRIGGER parser_protect_title AFTER UPDATE OF title ON company_vacancies WHEN OLD.title IS NOT NEW.title AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='title'; END;
CREATE TRIGGER parser_protect_url AFTER UPDATE OF url ON company_vacancies WHEN OLD.url IS NOT NEW.url AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='url'; END;
```

## company_vacancy_state

Отметка первоначального переноса встроенных вакансий компании в SQL.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| company_name | TEXT | Да | PRIMARY KEY | — |
| initialized_at | TEXT | Да | — | CURRENT_TIMESTAMP |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| company_name | company_crm.company_name | CASCADE |

```sql
CREATE TABLE `company_vacancy_state` (
	`company_name` text PRIMARY KEY NOT NULL,
	`initialized_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade
);
```

## company_vacancy_user_state

Личные релевантность, избранное и скрытие вакансии.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | — | — |
| vacancy_id | INTEGER | Да | — | — |
| relevance | INTEGER | Нет | — | — |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| favorite | INTEGER | Да | — | false |
| hidden | INTEGER | Да | — | false |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE `company_vacancy_user_state` (
  `user_name` text NOT NULL,
  `vacancy_id` integer NOT NULL,
  `relevance` integer,
  `updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `favorite` integer DEFAULT false NOT NULL, `hidden` integer DEFAULT false NOT NULL,
  FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE cascade,
  CONSTRAINT `company_vacancy_user_state_relevance_check` CHECK (`relevance` is null or (`relevance` between 0 and 100))
);
CREATE INDEX `company_vacancy_user_state_user_favorite_idx` ON `company_vacancy_user_state` (`user_name`,`favorite`,`vacancy_id`);
CREATE INDEX `company_vacancy_user_state_user_hidden_idx` ON `company_vacancy_user_state` (`user_name`,`hidden`,`vacancy_id`);
CREATE UNIQUE INDEX `company_vacancy_user_state_user_vacancy_unique` ON `company_vacancy_user_state` (`user_name`,`vacancy_id`);
```

## firecrawl_settings

Общее подключение Firecrawl: favorites_only (default1, избранные текущего admin), AES-GCM ключ, маска, лимиты Scrape, явный basic/enhanced, CAS revision и проверка кредитов.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| key_encrypted | TEXT | Да | — | '' |
| key_suffix | TEXT | Да | — | '' |
| enabled | INTEGER | Да | — | 0 |
| max_pages | INTEGER | Да | — | 3 |
| concurrency | INTEGER | Да | — | 1 |
| timeout_ms | INTEGER | Да | — | 45000 |
| proxy | TEXT | Да | — | 'basic' |
| enhanced_allowed | INTEGER | Да | — | 0 |
| revision | INTEGER | Да | — | 0 |
| connection_status | TEXT | Да | — | 'missing' |
| last_error | TEXT | Да | — | '' |
| checked_at | TEXT | Нет | — | — |
| mode | TEXT | Да | — | 'free' |
| hobby_key_encrypted | TEXT | Да | — | '' |
| hobby_key_suffix | TEXT | Да | — | '' |
| hobby_connection_status | TEXT | Да | — | 'missing' |
| hobby_last_error | TEXT | Да | — | '' |
| hobby_checked_at | TEXT | Нет | — | — |
| free_key_hash | TEXT | Да | — | '' |
| hobby_key_hash | TEXT | Да | — | '' |
| free_usage_json | TEXT | Да | — | '' |
| hobby_usage_json | TEXT | Да | — | '' |
| favorites_only | INTEGER | Да | — | 1 |
| scheduler_json | TEXT | Да | — | '{"enabled":false,"ownerName":"Eugene","dayBudgetUsd":0,"monthBudgetUsd":0,"revision":0,"cloudOwnerId":"","priorities":{"import_vacancy":100,"assign_roles":90,"extract_vacancy":80,"fetch_full_text":70,"classify_title":60,"scan_source":20,"diagnostic":0}}' |

```sql
CREATE TABLE "firecrawl_settings" (
 id INTEGER PRIMARY KEY NOT NULL CHECK(id=1), key_encrypted TEXT NOT NULL DEFAULT '',
 key_suffix TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 max_pages INTEGER NOT NULL DEFAULT 3 CHECK(max_pages BETWEEN 1 AND 10),
 concurrency INTEGER NOT NULL DEFAULT 1 CHECK(concurrency=1),
 timeout_ms INTEGER NOT NULL DEFAULT 45000 CHECK(timeout_ms BETWEEN 5000 AND 60000),
 proxy TEXT NOT NULL DEFAULT 'basic' CHECK(proxy IN ('basic','enhanced')),
 enhanced_allowed INTEGER NOT NULL DEFAULT 0 CHECK(enhanced_allowed IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 0, connection_status TEXT NOT NULL DEFAULT 'missing',
 last_error TEXT NOT NULL DEFAULT '', checked_at TEXT,
 mode TEXT NOT NULL DEFAULT 'free' CHECK(mode IN ('free','hobby')),
 hobby_key_encrypted TEXT NOT NULL DEFAULT '', hobby_key_suffix TEXT NOT NULL DEFAULT '',
 hobby_connection_status TEXT NOT NULL DEFAULT 'missing', hobby_last_error TEXT NOT NULL DEFAULT '', hobby_checked_at TEXT,
 free_key_hash TEXT NOT NULL DEFAULT '', hobby_key_hash TEXT NOT NULL DEFAULT '',
 free_usage_json TEXT NOT NULL DEFAULT '', hobby_usage_json TEXT NOT NULL DEFAULT '', favorites_only INTEGER NOT NULL DEFAULT 1 CHECK(favorites_only IN (0,1)), scheduler_json TEXT NOT NULL DEFAULT '{"enabled":false,"ownerName":"Eugene","dayBudgetUsd":0,"monthBudgetUsd":0,"revision":0,"cloudOwnerId":"","priorities":{"import_vacancy":100,"assign_roles":90,"extract_vacancy":80,"fetch_full_text":70,"classify_title":60,"scan_source":20,"diagnostic":0}}' CHECK(json_valid(scheduler_json)),
 CHECK(proxy='basic' OR (mode='hobby' AND enhanced_allowed=1)),
 CHECK(enabled=0 OR (mode='free' AND length(key_encrypted)>0) OR (mode='hobby' AND length(hobby_key_encrypted)>0))
);
```

## google_calendar_connection

Историческое общее подключение календаря; сохраняется ради совместимости.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| account_email | TEXT | Да | — | '' |
| refresh_token_encrypted | TEXT | Да | — | — |
| calendar_id | TEXT | Да | — | 'primary' |
| connected_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `google_calendar_connection` (
	`id` integer PRIMARY KEY NOT NULL,
	`account_email` text DEFAULT '' NOT NULL,
	`refresh_token_encrypted` text NOT NULL,
	`calendar_id` text DEFAULT 'primary' NOT NULL,
	`connected_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
```

## google_calendar_user_connection

Персональные подключения календаря; refresh-токен зашифрован.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | PRIMARY KEY | — |
| account_email | TEXT | Да | — | '' |
| refresh_token_encrypted | TEXT | Да | — | — |
| calendar_id | TEXT | Да | — | 'primary' |
| connected_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `google_calendar_user_connection` (
  `user_name` text PRIMARY KEY NOT NULL,
  `account_email` text DEFAULT '' NOT NULL,
  `refresh_token_encrypted` text NOT NULL,
  `calendar_id` text DEFAULT 'primary' NOT NULL,
  `connected_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
  `updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
```

## hiring_contacts

До трёх общих контактов ответственных за найм на организацию.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| company_name | TEXT | Да | — | — |
| position | INTEGER | Да | — | — |
| full_name | TEXT | Да | — | — |
| linkedin_url | TEXT | Да | — | '' |
| facebook_url | TEXT | Да | — | '' |
| telegram | TEXT | Да | — | '' |
| phone | TEXT | Да | — | '' |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| email | TEXT | Да | — | '' |
| specialization | TEXT | Да | — | '' |
| setka_url | TEXT | Да | — | '' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| company_name | company_crm.company_name | CASCADE |

```sql
CREATE TABLE `hiring_contacts` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`company_name` text NOT NULL,
	`position` integer NOT NULL,
	`full_name` text NOT NULL,
	`linkedin_url` text DEFAULT '' NOT NULL,
	`facebook_url` text DEFAULT '' NOT NULL,
	`telegram` text DEFAULT '' NOT NULL,
	`phone` text DEFAULT '' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `email` text DEFAULT '' NOT NULL, `specialization` text DEFAULT '' NOT NULL, `setka_url` text DEFAULT '' NOT NULL,
	FOREIGN KEY (`company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE cascade,
	CONSTRAINT "hiring_contacts_position_check" CHECK("hiring_contacts"."position" between 1 and 3)
);
CREATE INDEX `hiring_contacts_company_idx` ON `hiring_contacts` (`company_name`,`position`);
CREATE UNIQUE INDEX `hiring_contacts_company_position_unique` ON `hiring_contacts` (`company_name`,`position`);
```

## openai_settings

Общий административный ключ OpenAI: AES-GCM ciphertext, последние4 символа, редакция и дата. Ключ расшифровывается только на сервере через SESSION_SECRET с отдельным доменом openai:v1. Начиная с backup24 входит только зашифрованным; актуальный формат25.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| key_encrypted | TEXT | Да | — | '' |
| key_suffix | TEXT | Да | — | '' |
| revision | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `openai_settings` (
	`id` integer PRIMARY KEY NOT NULL,
	`key_encrypted` text DEFAULT '' NOT NULL,
	`key_suffix` text DEFAULT '' NOT NULL,
	`revision` integer DEFAULT 0 NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT "openai_settings_singleton" CHECK("openai_settings"."id" = 1),
	CONSTRAINT "openai_settings_revision" CHECK("openai_settings"."revision" >= 0)
);
```

## openrouter_ai_logs

Последние 200 технических результатов AI-запросов: время, тип задачи, запрошенная/фактическая модель, переключение, успех и код ошибки. Без промтов, ответов и ключей.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| created_at | TEXT | Да | — | — |
| task | TEXT | Да | — | — |
| requested_primary_model | TEXT | Нет | — | — |
| actual_model | TEXT | Нет | — | — |
| fallback_used | INTEGER | Да | — | 0 |
| success | INTEGER | Да | — | — |
| error_code | TEXT | Нет | — | — |

```sql
CREATE TABLE `openrouter_ai_logs` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`created_at` text NOT NULL,
	`task` text NOT NULL,
	`requested_primary_model` text,
	`actual_model` text,
	`fallback_used` integer DEFAULT 0 NOT NULL,
	`success` integer NOT NULL,
	`error_code` text
);
```

## openrouter_model_metrics

Агрегированные замеры писем по модели; structured:task:generation:keyRevision:model — отдельные latency/error метрики парсера. Число успехов/ошибок, сумма и последнее время, даты. Без текста, резюме и ключей.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| model_id | TEXT | Да | PRIMARY KEY | — |
| success_count | INTEGER | Да | — | 0 |
| total_duration_ms | INTEGER | Да | — | 0 |
| last_duration_ms | INTEGER | Нет | — | — |
| last_success_at | TEXT | Нет | — | — |
| failure_count | INTEGER | Да | — | 0 |
| last_failure_duration_ms | INTEGER | Нет | — | — |
| last_failure_at | TEXT | Нет | — | — |

```sql
CREATE TABLE `openrouter_model_metrics` (
	`model_id` text PRIMARY KEY NOT NULL,
	`success_count` integer DEFAULT 0 NOT NULL,
	`total_duration_ms` integer DEFAULT 0 NOT NULL,
	`last_duration_ms` integer,
	`last_success_at` text,
	`failure_count` integer DEFAULT 0 NOT NULL,
	`last_failure_duration_ms` integer,
	`last_failure_at` text
);
```

## openrouter_settings

Единая административная конфигурация OpenRouter. Ключ AES-GCM, только суффикс для интерфейса, JSON-кэш моделей, основная и последняя фактическая модели, время обновления, статус и редакция ключа.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| key_encrypted | TEXT | Да | — | '' |
| key_suffix | TEXT | Да | — | '' |
| connection_status | TEXT | Да | — | 'missing' |
| last_error | TEXT | Да | — | '' |
| cache_json | TEXT | Да | — | '[]' |
| primary_model_id | TEXT | Нет | — | — |
| models_synced_at | TEXT | Нет | — | — |
| last_used_model | TEXT | Нет | — | — |
| last_used_at | TEXT | Нет | — | — |
| revision | INTEGER | Да | — | 0 |

```sql
CREATE TABLE `openrouter_settings` (
	`id` integer PRIMARY KEY NOT NULL,
	`key_encrypted` text DEFAULT '' NOT NULL,
	`key_suffix` text DEFAULT '' NOT NULL,
	`connection_status` text DEFAULT 'missing' NOT NULL,
	`last_error` text DEFAULT '' NOT NULL,
	`cache_json` text DEFAULT '[]' NOT NULL,
	`primary_model_id` text,
	`models_synced_at` text,
	`last_used_model` text,
	`last_used_at` text,
	`revision` integer DEFAULT 0 NOT NULL
);
```

## parser_candidate_intakes

Неизменяемое происхождение первичной публикации: creation_token, парсер сбора списка, источник и дата первого получения. Кандидат и receipt пишутся одной транзакцией. Без FK на переиспользуемые ID; переносится начиная с backup39.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| candidate_token | TEXT | Да | PRIMARY KEY | — |
| parser | TEXT | Да | — | — |
| source_id | TEXT | Нет | — | — |
| received_at | TEXT | Да | — | — |

```sql
CREATE TABLE parser_candidate_intakes (
 candidate_token TEXT PRIMARY KEY NOT NULL,
 parser TEXT NOT NULL CHECK(parser IN ('apify','firecrawl','http','manual','excel','unknown')),
 source_id TEXT,
 received_at TEXT NOT NULL
);
```

## parser_error_daily

Дневные пики активных P1/P2 по владельцу парсинга и датам MSK. MAX-upsert, фактические дни наблюдений; без текстов/ключей; переносится начиная с backup39.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| owner_name | TEXT | Да | PRIMARY KEY | — |
| day | TEXT | Да | PRIMARY KEY | — |
| p1_peak | INTEGER | Да | — | — |
| p2_peak | INTEGER | Да | — | — |
| first_sample_at | INTEGER | Да | — | — |
| last_sample_at | INTEGER | Да | — | — |

```sql
CREATE TABLE parser_error_daily (
 owner_name TEXT NOT NULL,
 day TEXT NOT NULL CHECK(length(day)=10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
 p1_peak INTEGER NOT NULL CHECK(p1_peak>=0),
 p2_peak INTEGER NOT NULL CHECK(p2_peak>=0),
 first_sample_at INTEGER NOT NULL,
 last_sample_at INTEGER NOT NULL,
 PRIMARY KEY(owner_name,day)
);
```

## prompt_model_settings

Общий выбор провайдера для каждого промта: OpenRouter или OpenAI, ID модели OpenAI, уровень рассуждений, независимая редакция и дата. Только администратор; по умолчанию OpenRouter. Не содержит ключей. Начиная с backup23 входит в копию; актуальный формат25.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| kind | TEXT | Да | PRIMARY KEY | — |
| provider | TEXT | Да | — | 'openrouter' |
| openai_model | TEXT | Да | — | '' |
| reasoning_effort | TEXT | Да | — | 'medium' |
| revision | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| openrouter_model | TEXT | Да | — | 'best_free' |

```sql
CREATE TABLE `prompt_model_settings` (
	`kind` text PRIMARY KEY NOT NULL,
	`provider` text DEFAULT 'openrouter' NOT NULL,
	`openai_model` text DEFAULT '' NOT NULL,
	`reasoning_effort` text DEFAULT 'medium' NOT NULL,
	`revision` integer DEFAULT 0 NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, openrouter_model TEXT NOT NULL DEFAULT 'best_free' CHECK (openrouter_model IN ('best_free','jev') AND (kind = 'relevance' OR openrouter_model = 'best_free')),
	CONSTRAINT "prompt_model_kind" CHECK("prompt_model_settings"."kind" IN ('cover_letter','relevance')),
	CONSTRAINT "prompt_model_provider" CHECK("prompt_model_settings"."provider" IN ('openrouter','openai')),
	CONSTRAINT "prompt_model_id" CHECK(length("prompt_model_settings"."openai_model") <= 160),
	CONSTRAINT "prompt_model_effort" CHECK("prompt_model_settings"."reasoning_effort" IN ('auto','none','minimal','low','medium','high','xhigh','max')),
	CONSTRAINT "prompt_model_revision" CHECK("prompt_model_settings"."revision" >= 0)
);
```

## system_prompts

Два общих Markdown-промта: сопроводительное письмо и релевантность. Только администратор читает и меняет их через API, сопроводительное письмо до 15000, релевантность до 5000 Unicode-символов, каждый со своей редакцией.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| kind | TEXT | Да | PRIMARY KEY | — |
| markdown | TEXT | Да | — | '' |
| revision | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE "system_prompts" (
  `kind` text PRIMARY KEY NOT NULL,
  `markdown` text DEFAULT '' NOT NULL,
  `revision` integer DEFAULT 0 NOT NULL,
  `updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
  CONSTRAINT "system_prompts_kind" CHECK("kind" IN ('cover_letter','relevance')),
  CONSTRAINT "system_prompts_length" CHECK(length("markdown") <= CASE WHEN "kind" = 'cover_letter' THEN 15000 ELSE 5000 END),
  CONSTRAINT "system_prompts_revision" CHECK("revision" >= 0)
);
```

## telegram_channels

Справочник Telegram-каналов с уникальным нормализованным username.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| name | TEXT | Да | — | — |
| url | TEXT | Да | — | — |
| normalized_username | TEXT | Да | — | — |
| username | TEXT | Да | — | '' |
| last_successful_check_at | TEXT | Нет | — | — |
| comment | TEXT | Да | — | '' |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `telegram_channels` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`name` text NOT NULL,
	`url` text NOT NULL,
	`normalized_username` text NOT NULL,
	`username` text DEFAULT '' NOT NULL,
	`last_successful_check_at` text,
	`comment` text DEFAULT '' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT "telegram_channels_comment_length_check" CHECK(length("telegram_channels"."comment") <= 512)
);
CREATE UNIQUE INDEX `telegram_channels_name_unique` ON `telegram_channels` (`name`);
CREATE UNIQUE INDEX `telegram_channels_normalized_username_unique` ON `telegram_channels` (`normalized_username`);
```

## user_profiles

Личное резюме пользователя в Markdown (до 20000 Unicode-символов), номер редакции для защиты от перезаписи и дата изменения. Доступ только по имени из сессии.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | PRIMARY KEY | — |
| resume_md | TEXT | Да | — | '' |
| revision | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `user_profiles` (
	`user_name` text PRIMARY KEY NOT NULL,
	`resume_md` text DEFAULT '' NOT NULL,
	`revision` integer DEFAULT 0 NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT "user_profiles_resume_length" CHECK(length("user_profiles"."resume_md") <= 20000),
	CONSTRAINT "user_profiles_revision" CHECK("user_profiles"."revision" >= 0)
);
```

## vacancy_audit_runs

Исторические результаты запусков проверки вакансий; количество удалений, обновлений дат и время завершения.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| run_id | TEXT | Да | PRIMARY KEY | — |
| deleted_count | INTEGER | Да | — | — |
| publication_date_update_count | INTEGER | Да | — | — |
| completed_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `vacancy_audit_runs` (
	`run_id` text PRIMARY KEY NOT NULL,
	`deleted_count` integer NOT NULL,
	`publication_date_update_count` integer NOT NULL,
	`completed_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
```

## vacancy_candidate_identities

Уникальные идентификаторы публикаций: внешние ID в пространстве источника, канонические URL и ключи импорта. Несколько алиасов одной записи; конкуренция защищена транзакцией.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| identity_key | TEXT | Да | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_candidate_identities (
 identity_key TEXT PRIMARY KEY NOT NULL,
 candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE
);
CREATE INDEX candidate_identity_candidate_idx ON vacancy_candidate_identities(candidate_id);
```

## vacancy_candidates

Первичные публикации отдельно от основной базы. Raw title/URLs, постоянные взаимоисключающие company_id/agency_id и main_vacancy_id, hash+revision заголовка, даты обнаружения. Статус вычисляется по текущим промтам.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| creation_token | TEXT | Да | — | — |
| company_id | INTEGER | Нет | — | — |
| company_name_raw | TEXT | Да | — | '' |
| title_raw | TEXT | Да | — | — |
| title_hash | TEXT | Да | — | — |
| title_revision | INTEGER | Да | — | 1 |
| vacancy_url | TEXT | Нет | — | — |
| canonical_url | TEXT | Нет | — | — |
| source_url | TEXT | Нет | — | — |
| source_id | TEXT | Нет | — | — |
| source_namespace | TEXT | Нет | — | — |
| external_job_id | TEXT | Нет | — | — |
| import_key | TEXT | Нет | — | — |
| acquisition_method | TEXT | Да | — | — |
| first_seen_at | TEXT | Да | — | — |
| last_seen_at | TEXT | Да | — | — |
| title_updated_at | TEXT | Нет | — | — |
| main_vacancy_id | INTEGER | Нет | — | — |
| created_at | TEXT | Да | — | — |
| updated_at | TEXT | Да | — | — |
| agency_id | INTEGER | Нет | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| agency_id | agency_identity.id | SET NULL |
| main_vacancy_id | company_vacancies.id | SET NULL |
| company_id | company_identity.id | SET NULL |

```sql
CREATE TABLE vacancy_candidates (
 id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
 creation_token TEXT NOT NULL UNIQUE,
 company_id INTEGER REFERENCES company_identity(id) ON DELETE SET NULL,
 company_name_raw TEXT NOT NULL DEFAULT '',
 title_raw TEXT NOT NULL CHECK(length(trim(title_raw)) BETWEEN 1 AND 500),
 title_hash TEXT NOT NULL CHECK(length(title_hash)=64),
 title_revision INTEGER NOT NULL DEFAULT 1 CHECK(title_revision>=1),
 vacancy_url TEXT, canonical_url TEXT, source_url TEXT, source_id TEXT,
 source_namespace TEXT, external_job_id TEXT, import_key TEXT,
 acquisition_method TEXT NOT NULL CHECK(acquisition_method IN ('site','firecrawl','excel','manual')),
 first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, title_updated_at TEXT,
 main_vacancy_id INTEGER REFERENCES company_vacancies(id) ON DELETE SET NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
, agency_id INTEGER REFERENCES agency_identity(id) ON DELETE SET NULL CHECK(agency_id IS NULL OR company_id IS NULL));
CREATE INDEX candidate_agency_seen_idx ON vacancy_candidates(agency_id,last_seen_at DESC,id DESC);
CREATE INDEX candidate_canonical_idx ON vacancy_candidates(canonical_url);
CREATE INDEX candidate_company_seen_idx ON vacancy_candidates(company_id,last_seen_at DESC,id DESC);
CREATE INDEX candidate_external_idx ON vacancy_candidates(source_namespace,external_job_id);
CREATE INDEX candidate_seen_idx ON vacancy_candidates(last_seen_at DESC,id DESC);
CREATE INDEX candidate_source_id_seen_idx ON vacancy_candidates(source_id,last_seen_at DESC,id DESC);
CREATE INDEX candidate_source_seen_idx ON vacancy_candidates(source_namespace,last_seen_at DESC,id DESC);
```

## vacancy_classification_attempts

История пакетных запросов JEV: снимок заголовка, проверенные версии промтов, технические ошибки и общая стоимость одного запроса. Без API-ключей.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Да | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |
| title_raw | TEXT | Да | — | — |
| title_hash | TEXT | Да | — | — |
| title_revision | INTEGER | Да | — | — |
| model_key | TEXT | Да | — | — |
| model_id | TEXT | Нет | — | — |
| requested_roles_json | TEXT | Да | — | — |
| errors_json | TEXT | Да | — | '[]' |
| outcome | TEXT | Да | — | — |
| classified_at | TEXT | Да | — | — |
| duration_ms | INTEGER | Нет | — | — |
| cost_usd | REAL | Нет | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_classification_attempts (
 id TEXT PRIMARY KEY NOT NULL,
 candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 title_raw TEXT NOT NULL, title_hash TEXT NOT NULL CHECK(length(title_hash)=64),
 title_revision INTEGER NOT NULL,
 model_key TEXT NOT NULL, model_id TEXT,
 requested_roles_json TEXT NOT NULL CHECK(json_valid(requested_roles_json)),
 errors_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(errors_json)),
 outcome TEXT NOT NULL CHECK(outcome IN ('complete','partial','error')),
 classified_at TEXT NOT NULL,
 duration_ms INTEGER CHECK(duration_ms IS NULL OR duration_ms>=0),
 cost_usd REAL CHECK(cost_usd IS NULL OR cost_usd>=0)
);
CREATE INDEX candidate_attempt_history_idx ON vacancy_classification_attempts(candidate_id,classified_at DESC,id DESC);
CREATE INDEX candidate_attempt_input_idx ON vacancy_classification_attempts(candidate_id,title_revision,title_hash,model_key);
```

## vacancy_classifications

Неизменяемая история match/reject по ID корневой роли и попытке; hash промта и версия заголовка. Только актуальные решения участвуют в статусе.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |
| attempt_id | TEXT | Да | — | — |
| root_role_id | INTEGER | Да | — | — |
| root_role_name | TEXT | Да | — | — |
| decision | TEXT | Да | — | — |
| model_key | TEXT | Да | — | — |
| model_id | TEXT | Да | — | — |
| prompt_version | TEXT | Да | — | — |
| prompt_revision | INTEGER | Да | — | — |
| title_hash | TEXT | Да | — | — |
| title_revision | INTEGER | Да | — | — |
| classified_at | TEXT | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| root_role_id | vacancy_roles.id | RESTRICT |
| attempt_id | vacancy_classification_attempts.id | CASCADE |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_classifications (
 id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
 candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 attempt_id TEXT NOT NULL REFERENCES vacancy_classification_attempts(id) ON DELETE CASCADE,
 root_role_id INTEGER NOT NULL REFERENCES vacancy_roles(id) ON DELETE RESTRICT,
 root_role_name TEXT NOT NULL,
 decision TEXT NOT NULL CHECK(decision IN ('match','reject')),
 model_key TEXT NOT NULL, model_id TEXT NOT NULL,
 prompt_version TEXT NOT NULL CHECK(length(prompt_version)=64),
 prompt_revision INTEGER NOT NULL,
 title_hash TEXT NOT NULL CHECK(length(title_hash)=64),
 title_revision INTEGER NOT NULL,
 classified_at TEXT NOT NULL,
 UNIQUE(attempt_id,root_role_id)
);
CREATE INDEX candidate_classification_current_idx ON vacancy_classifications(candidate_id,root_role_id,title_revision,title_hash,prompt_version,model_key,id DESC);
CREATE INDEX candidate_classification_history_idx ON vacancy_classifications(candidate_id,id DESC);
CREATE INDEX candidate_classification_role_idx ON vacancy_classifications(root_role_id,candidate_id);
```

## vacancy_content_fetches

История всех загрузок/ошибок Firecrawl, включая идентичное обновление без нового снимка; расходы только из API либо null.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |
| snapshot_id | TEXT | Нет | — | — |
| source_url | TEXT | Да | — | — |
| final_url | TEXT | Нет | — | — |
| firecrawl_mode | TEXT | Да | — | — |
| credits | REAL | Нет | — | — |
| started_at | INTEGER | Да | — | — |
| duration_ms | INTEGER | Да | — | — |
| status | TEXT | Да | — | — |
| error_code | TEXT | Нет | — | — |
| message | TEXT | Да | — | '' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| snapshot_id | vacancy_raw_snapshots.id | CASCADE |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_content_fetches (
 id TEXT PRIMARY KEY, candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 snapshot_id TEXT REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE, source_url TEXT NOT NULL, final_url TEXT,
 firecrawl_mode TEXT NOT NULL, credits REAL, started_at INTEGER NOT NULL, duration_ms INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('success','error')), error_code TEXT, message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX content_fetch_candidate_idx ON vacancy_content_fetches(candidate_id,started_at);
```

## vacancy_cover_letters

Личные письма, созданные ИИ для пары пользователь + вакансия. Заголовок до 512, Markdown до 2000 символов. Общие письма, заполненные вручную, остаются в company_vacancies. Личный override_shared=1 имеет приоритет над общим; revision защищает повторную генерацию от гонки.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | — | — |
| vacancy_id | INTEGER | Да | — | — |
| subject | TEXT | Да | — | — |
| markdown | TEXT | Да | — | — |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| override_shared | INTEGER | Да | — | 0 |
| revision | INTEGER | Да | — | 0 |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE `vacancy_cover_letters` (
	`user_name` text NOT NULL,
	`vacancy_id` integer NOT NULL,
	`subject` text NOT NULL,
	`markdown` text NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL, `override_shared` integer DEFAULT 0 NOT NULL CONSTRAINT `vacancy_cover_letters_override_shared` CHECK (`override_shared` in (0, 1)), `revision` integer DEFAULT 0 NOT NULL CONSTRAINT `vacancy_cover_letters_revision` CHECK (`revision` >= 0),
	FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE cascade,
	CONSTRAINT "vacancy_cover_letters_subject_length" CHECK(length("vacancy_cover_letters"."subject") between 1 and 512),
	CONSTRAINT "vacancy_cover_letters_markdown_length" CHECK(length("vacancy_cover_letters"."markdown") between 1 and 2000)
);
CREATE UNIQUE INDEX `vacancy_cover_letters_user_vacancy_unique` ON `vacancy_cover_letters` (`user_name`,`vacancy_id`);
```

## vacancy_detail_settings

Общий промт структурирования до10000 Unicode-символов и независимый выбор модели через существующие OpenRouter/OpenAI; без ключей. CAS промта/модели раздельные.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Нет | PRIMARY KEY | — |
| markdown | TEXT | Да | — | — |
| prompt_revision | INTEGER | Да | — | 0 |
| provider | TEXT | Да | — | 'openrouter' |
| openai_model | TEXT | Да | — | '' |
| reasoning_effort | TEXT | Да | — | 'medium' |
| model_revision | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE "vacancy_detail_settings" (
 id INTEGER PRIMARY KEY CHECK(id=1), markdown TEXT NOT NULL CHECK(length(markdown)<=10000), prompt_revision INTEGER NOT NULL DEFAULT 0,
 provider TEXT NOT NULL DEFAULT 'openrouter' CHECK(provider IN ('openrouter','openai')), openai_model TEXT NOT NULL DEFAULT '', reasoning_effort TEXT NOT NULL DEFAULT 'medium',
 model_revision INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

## vacancy_employer_guesses

До пяти предположений о реальном работодателе вакансии агентства; связь с компанией, вакансией либо внешней ссылкой.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| agency_vacancy_id | INTEGER | Да | — | — |
| position | INTEGER | Да | — | — |
| employer_name | TEXT | Да | — | '' |
| linked_company_name | TEXT | Нет | — | — |
| matched_vacancy_id | INTEGER | Нет | — | — |
| external_vacancy_url | TEXT | Нет | — | — |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| matched_vacancy_id | company_vacancies.id | SET NULL |
| linked_company_name | company_crm.company_name | SET NULL |
| agency_vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE `vacancy_employer_guesses` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`agency_vacancy_id` integer NOT NULL,
	`position` integer NOT NULL,
	`employer_name` text DEFAULT '' NOT NULL,
	`linked_company_name` text,
	`matched_vacancy_id` integer,
	`external_vacancy_url` text,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`agency_vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`linked_company_name`) REFERENCES `company_crm`(`company_name`) ON UPDATE no action ON DELETE set null,
	FOREIGN KEY (`matched_vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE set null,
	CONSTRAINT "vacancy_employer_guesses_position_check" CHECK("vacancy_employer_guesses"."position" between 1 and 5),
	CONSTRAINT "vacancy_employer_guesses_not_self_check" CHECK("vacancy_employer_guesses"."matched_vacancy_id" is null or "vacancy_employer_guesses"."matched_vacancy_id" <> "vacancy_employer_guesses"."agency_vacancy_id"),
	CONSTRAINT "vacancy_employer_guesses_content_check" CHECK(length(trim("vacancy_employer_guesses"."employer_name")) > 0 or "vacancy_employer_guesses"."linked_company_name" is not null or "vacancy_employer_guesses"."matched_vacancy_id" is not null or "vacancy_employer_guesses"."external_vacancy_url" is not null)
);
CREATE INDEX `vacancy_employer_guesses_linked_company_idx` ON `vacancy_employer_guesses` (`linked_company_name`);
CREATE INDEX `vacancy_employer_guesses_matched_vacancy_idx` ON `vacancy_employer_guesses` (`matched_vacancy_id`);
CREATE UNIQUE INDEX `vacancy_employer_guesses_vacancy_position_unique` ON `vacancy_employer_guesses` (`agency_vacancy_id`,`position`);
CREATE TRIGGER `vacancy_employer_guesses_agency_insert`
BEFORE INSERT ON `vacancy_employer_guesses`
WHEN COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`agency_vacancy_id`), '') <> 'agency'
BEGIN
	SELECT RAISE(ABORT, 'employer guesses require an agency vacancy');
END;
CREATE TRIGGER `vacancy_employer_guesses_agency_update`
BEFORE UPDATE OF `agency_vacancy_id` ON `vacancy_employer_guesses`
WHEN COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`agency_vacancy_id`), '') <> 'agency'
BEGIN
	SELECT RAISE(ABORT, 'employer guesses require an agency vacancy');
END;
CREATE TRIGGER `vacancy_employer_guesses_company_match_insert`
BEFORE INSERT ON `vacancy_employer_guesses`
WHEN NEW.`matched_vacancy_id` IS NOT NULL AND COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`matched_vacancy_id`), '') <> 'company'
BEGIN
	SELECT RAISE(ABORT, 'matched vacancy must belong to a company');
END;
CREATE TRIGGER `vacancy_employer_guesses_company_match_update`
BEFORE UPDATE OF `matched_vacancy_id` ON `vacancy_employer_guesses`
WHEN NEW.`matched_vacancy_id` IS NOT NULL AND COALESCE((SELECT `entity_type` FROM `company_vacancies` WHERE `id` = NEW.`matched_vacancy_id`), '') <> 'company'
BEGIN
	SELECT RAISE(ABORT, 'matched vacancy must belong to a company');
END;
```

## vacancy_extractions

Неизменяемые успешные и ошибочные обработки конкретного snapshot с prompt/model/schema keys и usage/cost. Основные вакансии не изменяют.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |
| snapshot_id | TEXT | Да | — | — |
| provider | TEXT | Да | — | — |
| model_id | TEXT | Нет | — | — |
| config_hash | TEXT | Да | — | — |
| prompt_version | TEXT | Да | — | — |
| schema_version | INTEGER | Да | — | — |
| extracted_data | TEXT | Нет | — | — |
| processed_at | TEXT | Да | — | — |
| duration_ms | INTEGER | Да | — | — |
| input_tokens | INTEGER | Нет | — | — |
| output_tokens | INTEGER | Нет | — | — |
| cost_usd | REAL | Нет | — | — |
| status | TEXT | Да | — | — |
| error_code | TEXT | Нет | — | — |
| diagnostics | TEXT | Да | — | '{}' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| snapshot_id | vacancy_raw_snapshots.id | CASCADE |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_extractions (
 id TEXT PRIMARY KEY, candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 snapshot_id TEXT NOT NULL REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE, provider TEXT NOT NULL, model_id TEXT,
 config_hash TEXT NOT NULL, prompt_version TEXT NOT NULL, schema_version INTEGER NOT NULL,
 extracted_data TEXT, processed_at TEXT NOT NULL, duration_ms INTEGER NOT NULL,
 input_tokens INTEGER, output_tokens INTEGER, cost_usd REAL,
 status TEXT NOT NULL CHECK(status IN ('success','error')), error_code TEXT, diagnostics TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX extraction_current_idx ON vacancy_extractions(candidate_id,snapshot_id,config_hash,prompt_version,schema_version,status);
```

## vacancy_import_operations

Минимальный технический аудит безопасного импорта.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| candidate_id | INTEGER | Нет | — | — |
| vacancy_id | INTEGER | Нет | — | — |
| batch_id | TEXT | Нет | — | — |
| action | TEXT | Да | — | — |
| status | TEXT | Да | — | — |
| processed_at | TEXT | Да | — | — |
| input_hash | TEXT | Нет | — | — |
| changes_json | TEXT | Да | — | — |
| error_code | TEXT | Нет | — | — |
| message | TEXT | Да | — | '' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| vacancy_id | company_vacancies.id | SET NULL |
| candidate_id | vacancy_candidates.id | SET NULL |

```sql
CREATE TABLE vacancy_import_operations (
 id TEXT PRIMARY KEY,candidate_id INTEGER REFERENCES vacancy_candidates(id) ON DELETE SET NULL,vacancy_id INTEGER REFERENCES company_vacancies(id) ON DELETE SET NULL,batch_id TEXT,action TEXT NOT NULL,status TEXT NOT NULL,processed_at TEXT NOT NULL,input_hash TEXT,changes_json TEXT NOT NULL,error_code TEXT,message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX vacancy_import_history_idx ON vacancy_import_operations(candidate_id,processed_at);
```

## vacancy_parser_fields

Происхождение управляемых полей, последняя автоматическая величина и защита ручной правки.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| vacancy_id | INTEGER | Да | PRIMARY KEY | — |
| field | TEXT | Да | PRIMARY KEY | — |
| value_json | TEXT | Да | — | — |
| manual | INTEGER | Да | — | 0 |
| candidate_id | INTEGER | Нет | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| candidate_id | vacancy_candidates.id | SET NULL |
| vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE vacancy_parser_fields (
 vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE,field TEXT NOT NULL,value_json TEXT NOT NULL,manual INTEGER NOT NULL DEFAULT 0 CHECK(manual IN (0,1)),candidate_id INTEGER REFERENCES vacancy_candidates(id) ON DELETE SET NULL,PRIMARY KEY(vacancy_id,field)
);
```

## vacancy_parser_roles

Автоматические назначения по первичной записи; ручные назначения сохраняются.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| vacancy_id | INTEGER | Да | PRIMARY KEY | — |
| role_id | INTEGER | Да | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | PRIMARY KEY | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| vacancy_id | vacancy_role_assignments.vacancy_id | CASCADE |
| role_id | vacancy_role_assignments.role_id | CASCADE |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_parser_roles (
 vacancy_id INTEGER NOT NULL,role_id INTEGER NOT NULL,candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,PRIMARY KEY(vacancy_id,role_id,candidate_id),FOREIGN KEY(vacancy_id,role_id) REFERENCES vacancy_role_assignments(vacancy_id,role_id) ON DELETE CASCADE
);
```

## vacancy_publication_keys

Уникальные устойчивые ключи публикаций; без объединения по заголовку.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| identity_key | TEXT | Нет | PRIMARY KEY | — |
| vacancy_id | INTEGER | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE vacancy_publication_keys (
 identity_key TEXT PRIMARY KEY,vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE
);
```

## vacancy_publication_sources

Все проверенные публикации одной основной вакансии, включая дополнительные URL.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| candidate_id | INTEGER | Нет | PRIMARY KEY | — |
| vacancy_id | INTEGER | Да | — | — |
| url | TEXT | Да | — | — |
| canonical_url | TEXT | Да | — | — |
| source_namespace | TEXT | Нет | — | — |
| external_job_id | TEXT | Нет | — | — |
| extraction_id | TEXT | Нет | — | — |
| updated_at | TEXT | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| extraction_id | vacancy_extractions.id | SET NULL |
| vacancy_id | company_vacancies.id | CASCADE |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_publication_sources (
 candidate_id INTEGER PRIMARY KEY REFERENCES vacancy_candidates(id) ON DELETE CASCADE,vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE,url TEXT NOT NULL,canonical_url TEXT NOT NULL,source_namespace TEXT,external_job_id TEXT,extraction_id TEXT REFERENCES vacancy_extractions(id) ON DELETE SET NULL,updated_at TEXT NOT NULL
);
CREATE INDEX publication_source_url_idx ON vacancy_publication_sources(canonical_url);
```

## vacancy_raw_snapshot_parts

Части больших исходных Markdown без потери символов и без превышения размера D1 строки. Собираются строго по part_index.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| snapshot_id | TEXT | Да | PRIMARY KEY | — |
| part_index | INTEGER | Да | PRIMARY KEY | — |
| markdown_raw | TEXT | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| snapshot_id | vacancy_raw_snapshots.id | CASCADE |

```sql
CREATE TABLE vacancy_raw_snapshot_parts (
 snapshot_id TEXT NOT NULL REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE, part_index INTEGER NOT NULL, markdown_raw TEXT NOT NULL,
 PRIMARY KEY(snapshot_id,part_index)
);
```

## vacancy_raw_snapshots

Неизменённые Markdown версии конкретной первичной вакансии, hash/URL/проверка/profile/известные кредиты. Не содержат HTML/изображений.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |
| source_url | TEXT | Да | — | — |
| final_url | TEXT | Да | — | — |
| markdown_raw | TEXT | Нет | — | — |
| content_hash | TEXT | Да | — | — |
| fetched_at | TEXT | Да | — | — |
| firecrawl_mode | TEXT | Да | — | — |
| firecrawl_credits | REAL | Нет | — | — |
| content_status | TEXT | Да | — | — |
| diagnostics | TEXT | Да | — | — |
| created_at | TEXT | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_raw_snapshots (
 id TEXT PRIMARY KEY, candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 source_url TEXT NOT NULL, final_url TEXT NOT NULL, markdown_raw TEXT, content_hash TEXT NOT NULL,
 fetched_at TEXT NOT NULL, firecrawl_mode TEXT NOT NULL CHECK(firecrawl_mode IN ('free','hobby')), firecrawl_credits REAL,
 content_status TEXT NOT NULL CHECK(content_status IN ('valid','invalid')), diagnostics TEXT NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX raw_snapshot_candidate_idx ON vacancy_raw_snapshots(candidate_id,created_at);
```

## vacancy_relevance_profile_state

Какая версия профиля релевантности уже применена для пользователя.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | PRIMARY KEY | — |
| profile_version | TEXT | Да | — | — |
| applied_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `vacancy_relevance_profile_state` (
	`user_name` text PRIMARY KEY NOT NULL,
	`profile_version` text NOT NULL,
	`applied_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL
);
```

## vacancy_role_assignments

Добавьте описание новой сущности в scripts/documentation.mjs.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| vacancy_id | INTEGER | Да | PRIMARY KEY | — |
| role_id | INTEGER | Да | PRIMARY KEY | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| role_id | vacancy_roles.id | RESTRICT |
| vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE `vacancy_role_assignments` (
	`vacancy_id` integer NOT NULL,
	`role_id` integer NOT NULL,
	PRIMARY KEY(`vacancy_id`, `role_id`),
	FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`role_id`) REFERENCES `vacancy_roles`(`id`) ON UPDATE no action ON DELETE restrict
);
CREATE INDEX `vacancy_role_assignments_role_idx` ON `vacancy_role_assignments` (`role_id`,`vacancy_id`);
```

## vacancy_role_classification

Общие промты корневых ролей и переключатели парсинга, привязка role_id; Markdown5000, CAS revision, дата. Инициализация по stable_key только для двух стандартных корней. При перемещении настройки сохраняются, дочерние не участвуют. Backup28.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| role_id | INTEGER | Да | PRIMARY KEY | — |
| markdown | TEXT | Да | — | '' |
| revision | INTEGER | Да | — | 0 |
| parsing_enabled | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| role_id | vacancy_roles.id | CASCADE |

```sql
CREATE TABLE "vacancy_role_classification" (
 role_id INTEGER PRIMARY KEY NOT NULL REFERENCES vacancy_roles(id) ON DELETE CASCADE,
 markdown TEXT NOT NULL DEFAULT '' CHECK(length(markdown)<=10000),
 revision INTEGER NOT NULL DEFAULT 0 CHECK(revision>=0),
 parsing_enabled INTEGER NOT NULL DEFAULT 0 CHECK(parsing_enabled IN (0,1)),
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

## vacancy_role_results

История выбора конечных ID, версии текста, дерева, промта/модели и стоимость.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| candidate_id | INTEGER | Да | — | — |
| snapshot_id | TEXT | Да | — | — |
| input_hash | TEXT | Да | — | — |
| tree_hash | TEXT | Да | — | — |
| prompt_version | TEXT | Да | — | — |
| config_hash | TEXT | Да | — | — |
| model_id | TEXT | Нет | — | — |
| provider | TEXT | Да | — | — |
| role_ids | TEXT | Нет | — | — |
| status | TEXT | Да | — | — |
| error_code | TEXT | Нет | — | — |
| message | TEXT | Да | — | '' |
| processed_at | TEXT | Да | — | — |
| duration_ms | INTEGER | Да | — | — |
| input_tokens | INTEGER | Нет | — | — |
| output_tokens | INTEGER | Нет | — | — |
| cost_usd | REAL | Нет | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| snapshot_id | vacancy_raw_snapshots.id | CASCADE |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_role_results (
 id TEXT PRIMARY KEY,candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,snapshot_id TEXT NOT NULL REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE,
 input_hash TEXT NOT NULL,tree_hash TEXT NOT NULL,prompt_version TEXT NOT NULL,config_hash TEXT NOT NULL,model_id TEXT,provider TEXT NOT NULL,role_ids TEXT,
 status TEXT NOT NULL CHECK(status IN ('success','error')),error_code TEXT,message TEXT NOT NULL DEFAULT '',processed_at TEXT NOT NULL,duration_ms INTEGER NOT NULL,input_tokens INTEGER,output_tokens INTEGER,cost_usd REAL
);
CREATE INDEX role_result_cache_idx ON vacancy_role_results(candidate_id,input_hash,status,processed_at);
```

## vacancy_role_settings

Промт и модель конечных ролей, отдельные CAS редакции.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Нет | PRIMARY KEY | — |
| markdown | TEXT | Да | — | — |
| prompt_revision | INTEGER | Да | — | 0 |
| provider | TEXT | Да | — | 'openrouter' |
| openai_model | TEXT | Да | — | '' |
| reasoning_effort | TEXT | Да | — | 'medium' |
| model_revision | INTEGER | Да | — | 0 |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| openrouter_model | TEXT | Да | — | 'best_free' |

```sql
CREATE TABLE vacancy_role_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),markdown TEXT NOT NULL CHECK(length(markdown)<=5000),prompt_revision INTEGER NOT NULL DEFAULT 0,
 provider TEXT NOT NULL DEFAULT 'openrouter' CHECK(provider IN ('openrouter','openai')),openai_model TEXT NOT NULL DEFAULT '',reasoning_effort TEXT NOT NULL DEFAULT 'medium',model_revision INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
, openrouter_model TEXT NOT NULL DEFAULT 'best_free' CHECK(openrouter_model IN ('best_free','jev')));
```

## vacancy_roles

Добавьте описание новой сущности в scripts/documentation.mjs.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| stable_key | TEXT | Да | — | — |
| name | TEXT | Да | — | — |
| parent_id | INTEGER | Нет | — | — |
| sort_order | INTEGER | Да | — | 0 |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| parent_id | vacancy_roles.id | RESTRICT |

```sql
CREATE TABLE `vacancy_roles` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`stable_key` text NOT NULL,
	`name` text NOT NULL,
	`parent_id` integer,
	`sort_order` integer DEFAULT 0 NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`parent_id`) REFERENCES `vacancy_roles`(`id`) ON UPDATE no action ON DELETE restrict,
	CONSTRAINT "vacancy_roles_name_length" CHECK(length(trim(`name`)) between 1 and 120),
	CONSTRAINT "vacancy_roles_sort_order" CHECK(`sort_order` >= 0),
	CONSTRAINT "vacancy_roles_not_self_parent" CHECK(`parent_id` is null or `parent_id` <> `id`)
);
CREATE UNIQUE INDEX `vacancy_roles_name_unique` ON `vacancy_roles` (`name` COLLATE NOCASE);
CREATE INDEX `vacancy_roles_parent_order_idx` ON `vacancy_roles` (`parent_id`,`sort_order`,`id`);
CREATE UNIQUE INDEX `vacancy_roles_stable_key_unique` ON `vacancy_roles` (`stable_key`);
```

## vacancy_source_parsing

Настройки существующего источника: ровно одна company/agency identity + ключ источника, сохранённый URL snapshot, выключено по умолчанию, правила извлечения и пагинации, диагностика.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| company_id | INTEGER | Нет | — | — |
| agency_id | INTEGER | Нет | — | — |
| source_id | TEXT | Да | — | — |
| source_url | TEXT | Да | — | — |
| enabled | INTEGER | Да | — | 0 |
| method | TEXT | Да | — | 'scrape' |
| config_json | TEXT | Да | — | '{}' |
| revision | INTEGER | Да | — | 0 |
| last_success_at | TEXT | Нет | — | — |
| last_error | TEXT | Да | — | '' |
| auto_enabled | INTEGER | Да | — | 0 |
| interval_days | INTEGER | Да | — | 7 |
| next_run_at | INTEGER | Нет | — | — |
| last_attempt_at | INTEGER | Нет | — | — |
| last_result | TEXT | Да | — | '' |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| agency_id | agency_identity.id | CASCADE |
| company_id | company_identity.id | CASCADE |

```sql
CREATE TABLE "vacancy_source_parsing" (
 company_id INTEGER REFERENCES company_identity(id) ON DELETE CASCADE,
 agency_id INTEGER REFERENCES agency_identity(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL, source_url TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 method TEXT NOT NULL DEFAULT 'scrape' CHECK(method='scrape'), config_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(config_json)),
 revision INTEGER NOT NULL DEFAULT 0, last_success_at TEXT, last_error TEXT NOT NULL DEFAULT '',
 auto_enabled INTEGER NOT NULL DEFAULT 0 CHECK(auto_enabled IN (0,1)),interval_days INTEGER NOT NULL DEFAULT 7 CHECK(interval_days IN (1,3,7)),next_run_at INTEGER,last_attempt_at INTEGER,last_result TEXT NOT NULL DEFAULT '',
 UNIQUE(company_id,source_id),UNIQUE(agency_id,source_id),CHECK((company_id IS NOT NULL)+(agency_id IS NOT NULL)=1)
);
```

## vacancy_telegram_sources

Связь вакансий с каналами и исходными сообщениями.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| vacancy_id | INTEGER | Да | — | — |
| channel_id | INTEGER | Да | — | — |
| message_url | TEXT | Да | — | — |
| telegram_contact | TEXT | Да | — | '' |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| channel_id | telegram_channels.id | CASCADE |
| vacancy_id | company_vacancies.id | CASCADE |

```sql
CREATE TABLE `vacancy_telegram_sources` (
	`id` integer PRIMARY KEY AUTOINCREMENT NOT NULL,
	`vacancy_id` integer NOT NULL,
	`channel_id` integer NOT NULL,
	`message_url` text NOT NULL,
	`telegram_contact` text DEFAULT '' NOT NULL,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	FOREIGN KEY (`vacancy_id`) REFERENCES `company_vacancies`(`id`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`channel_id`) REFERENCES `telegram_channels`(`id`) ON UPDATE no action ON DELETE cascade
);
CREATE INDEX `vacancy_telegram_sources_channel_idx` ON `vacancy_telegram_sources` (`channel_id`,`vacancy_id`);
CREATE UNIQUE INDEX `vacancy_telegram_sources_message_url_unique` ON `vacancy_telegram_sources` (`message_url`);
CREATE UNIQUE INDEX `vacancy_telegram_sources_vacancy_channel_unique` ON `vacancy_telegram_sources` (`vacancy_id`,`channel_id`);
```

## apify_account_usage

Добавьте описание новой сущности в scripts/documentation.mjs.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| account_id | TEXT | Да | — | — |
| period_start | TEXT | Да | — | — |
| period_end | TEXT | Да | — | — |
| provider_used_usd | REAL | Да | — | — |
| updated_at | INTEGER | Да | — | — |

```sql
CREATE TABLE apify_account_usage (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, period_start TEXT NOT NULL, period_end TEXT NOT NULL,
 provider_used_usd REAL NOT NULL CHECK(provider_used_usd>=0), updated_at INTEGER NOT NULL
);
```

## apify_runs

Несбрасываемый операционный журнал Apify: generation/operation key, run/dataset IDs, профиль/account/период, резервы и подтверждённые расходы; без токенов и без backup.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| operation_key | TEXT | Да | — | — |
| profile | TEXT | Да | — | — |
| account_id | TEXT | Да | — | — |
| key_hash | TEXT | Да | — | — |
| period_start | TEXT | Да | — | — |
| period_end | TEXT | Да | — | — |
| reserved_usd | REAL | Да | — | — |
| charged_usd | REAL | Нет | — | — |
| state | TEXT | Да | — | — |
| run_id | TEXT | Да | — | '' |
| dataset_id | TEXT | Да | — | '' |
| error_code | TEXT | Да | — | '' |
| created_at | INTEGER | Да | — | — |
| updated_at | INTEGER | Да | — | — |

```sql
CREATE TABLE apify_runs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, operation_key TEXT NOT NULL,
 profile TEXT NOT NULL CHECK(profile IN ('free','starter')), account_id TEXT NOT NULL, key_hash TEXT NOT NULL,
 period_start TEXT NOT NULL, period_end TEXT NOT NULL, reserved_usd REAL NOT NULL CHECK(reserved_usd>0),
 charged_usd REAL CHECK(charged_usd IS NULL OR charged_usd>=0),
 state TEXT NOT NULL CHECK(state IN ('starting','running','succeeded','failed','uncertain')),
 run_id TEXT NOT NULL DEFAULT '', dataset_id TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 UNIQUE(generation,operation_key)
);
CREATE INDEX apify_budget_idx ON apify_runs(account_id,period_start,period_end);
```

## app_runtime_generation

Служебное поколение живой базы: одна строка id=1 с UUID. Успешное восстановление меняет UUID в той же транзакции. Не экспортируется и не восстанавливается из бэкапа.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| updated_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `app_runtime_generation` (
	`id` integer PRIMARY KEY NOT NULL,
	`generation` text NOT NULL,
	`updated_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	CONSTRAINT "app_runtime_generation_singleton" CHECK("app_runtime_generation"."id" = 1),
	CONSTRAINT "app_runtime_generation_length" CHECK(length("app_runtime_generation"."generation") = 36)
);
```

## application_creation_receipts

Служебные результаты создания отклика: пользователь + ключ запроса, поколение БД, SHA-256 нормализованного тела и ID созданного отклика. Без исходных контактов/комментария и без FK/cascade. Строки переживают удаление и restore, не входят в пользовательский бэкап.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | PRIMARY KEY | — |
| idempotency_key | TEXT | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| request_hash | TEXT | Да | — | — |
| created_application_id | INTEGER | Нет | — | — |
| created_at | TEXT | Да | — | CURRENT_TIMESTAMP |

```sql
CREATE TABLE `application_creation_receipts` (
	`user_name` text NOT NULL,
	`idempotency_key` text NOT NULL,
	`generation` text NOT NULL,
	`request_hash` text NOT NULL,
	`created_application_id` integer,
	`created_at` text DEFAULT CURRENT_TIMESTAMP NOT NULL,
	PRIMARY KEY(`user_name`, `idempotency_key`),
	CONSTRAINT "application_receipt_user" CHECK(length("application_creation_receipts"."user_name") BETWEEN 1 AND 160),
	CONSTRAINT "application_receipt_key" CHECK(length("application_creation_receipts"."idempotency_key") = 36),
	CONSTRAINT "application_receipt_generation" CHECK(length("application_creation_receipts"."generation") = 36),
	CONSTRAINT "application_receipt_hash" CHECK(length("application_creation_receipts"."request_hash") = 64 AND "application_creation_receipts"."request_hash" NOT GLOB '*[^0-9a-f]*'),
	CONSTRAINT "application_receipt_id" CHECK("application_creation_receipts"."created_application_id" IS NULL OR "application_creation_receipts"."created_application_id" > 0)
);
```

## cover_letter_generation_leases

Служебное владение генерацией: пользователь+вакансия, поколение БД, SHA-256 входного snapshot, UUID владельца и срок в Unix milliseconds (UTC). Не содержит исходных резюме/промтов, не имеет FK/cascade, не экспортируется и не восстанавливается из пользовательского бэкапа. HTTP генерация использует claim/renew/release, operation guard и owner fencing; один результат для параллельных запросов текущего кандидата.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| user_name | TEXT | Да | PRIMARY KEY | — |
| vacancy_id | INTEGER | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| input_hash | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | — |
| expires_at | INTEGER | Да | — | — |

```sql
CREATE TABLE `cover_letter_generation_leases` (
 `user_name` text NOT NULL,
 `vacancy_id` integer NOT NULL,
 `generation` text NOT NULL,
 `input_hash` text NOT NULL,
 `owner_token` text NOT NULL,
 `expires_at` integer NOT NULL,
 PRIMARY KEY(`user_name`,`vacancy_id`),
 CONSTRAINT "letter_lease_user" CHECK(length(user_name) BETWEEN 1 AND 160),
 CONSTRAINT "letter_lease_vacancy" CHECK(vacancy_id > 0),
 CONSTRAINT "letter_lease_generation" CHECK(length(generation) = 36),
 CONSTRAINT "letter_lease_hash" CHECK(length(input_hash) = 64 AND input_hash NOT GLOB '*[^0-9a-f]*'),
 CONSTRAINT "letter_lease_owner" CHECK(length(owner_token) = 36),
 CONSTRAINT "letter_lease_expiry" CHECK(expires_at > 0)
);
```

## firecrawl_request_lease

Глобальное владение последовательным ручным сбором/проверкой подключения. Restore generation и owner fencing, срок; без backup.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | INTEGER | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | — |
| expires_at | INTEGER | Да | — | — |

```sql
CREATE TABLE firecrawl_request_lease (
 id INTEGER PRIMARY KEY NOT NULL CHECK(id=1), generation TEXT NOT NULL, owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL
);
```

## firecrawl_usage_events

Операционный несбрасываемый журнал резервов и расходов Firecrawl: SHA ключа/профиль/реальный период, запросы/ошибки, верхняя оценка неизвестного списания и active_until. Без backup и без секретов.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Да | PRIMARY KEY | — |
| key_hash | TEXT | Да | — | — |
| profile | TEXT | Да | — | — |
| period_start | TEXT | Да | — | — |
| period_end | TEXT | Да | — | — |
| operation | TEXT | Да | — | — |
| reserved_credits | INTEGER | Да | — | — |
| charged_credits | REAL | Нет | — | — |
| outcome | TEXT | Да | — | — |
| started_at | INTEGER | Да | — | — |
| finished_at | INTEGER | Нет | — | — |
| active_until | INTEGER | Да | — | — |
| provider_status | INTEGER | Нет | — | — |
| error_code | TEXT | Нет | — | — |
| generation | TEXT | Да | — | — |

```sql
CREATE TABLE firecrawl_usage_events (
 id TEXT PRIMARY KEY NOT NULL, key_hash TEXT NOT NULL, profile TEXT NOT NULL CHECK(profile IN ('free','hobby')),
 period_start TEXT NOT NULL, period_end TEXT NOT NULL,
 operation TEXT NOT NULL CHECK(operation='scrape'), reserved_credits INTEGER NOT NULL CHECK(reserved_credits>0),
 charged_credits REAL CHECK(charged_credits IS NULL OR charged_credits>=0),
 outcome TEXT NOT NULL CHECK(outcome IN ('reserved','complete','error','uncertain')),
 started_at INTEGER NOT NULL, finished_at INTEGER, active_until INTEGER NOT NULL,
 provider_status INTEGER, error_code TEXT, generation TEXT NOT NULL
);
CREATE INDEX firecrawl_budget_period_idx ON firecrawl_usage_events(period_start,period_end,profile,key_hash);
CREATE INDEX firecrawl_rate_idx ON firecrawl_usage_events(started_at);
```

## firecrawl_usage_periods

Операционные данные Credit Usage: реальный расчётный период, остаток, пакет и верхнее наблюдавшееся использование. Не переносятся назад при restore.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| key_hash | TEXT | Да | PRIMARY KEY | — |
| period_start | TEXT | Да | PRIMARY KEY | — |
| period_end | TEXT | Да | PRIMARY KEY | — |
| provider_used_high | INTEGER | Да | — | 0 |
| remaining_credits | INTEGER | Да | — | — |
| plan_credits | INTEGER | Да | — | — |
| checked_at | INTEGER | Да | — | — |

```sql
CREATE TABLE firecrawl_usage_periods (
 key_hash TEXT NOT NULL, period_start TEXT NOT NULL, period_end TEXT NOT NULL,
 provider_used_high INTEGER NOT NULL DEFAULT 0 CHECK(provider_used_high>=0),
 remaining_credits INTEGER NOT NULL CHECK(remaining_credits>=0), plan_credits INTEGER NOT NULL CHECK(plan_credits>0),
 checked_at INTEGER NOT NULL, PRIMARY KEY(key_hash,period_start,period_end)
);
```

## scheduler_attempts

Операционный журнал автоматических этапов и денежных резервов JEV/LLM. Содержимое вакансий и секреты не сохраняются; расходы не откатываются restore.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| job_id | TEXT | Да | — | — |
| run_id | TEXT | Да | — | — |
| type | TEXT | Да | — | — |
| company_id | INTEGER | Нет | — | — |
| source_id | TEXT | Нет | — | — |
| started_at | INTEGER | Да | — | — |
| finished_at | INTEGER | Нет | — | — |
| status | TEXT | Да | — | — |
| reason_code | TEXT | Да | — | '' |
| message | TEXT | Да | — | '' |
| provider | TEXT | Нет | — | — |
| model | TEXT | Нет | — | — |
| reserved_usd | REAL | Да | — | 0 |
| cost_usd | REAL | Нет | — | — |
| credits | REAL | Нет | — | — |
| result_json | TEXT | Да | — | '{}' |

```sql
CREATE TABLE scheduler_attempts (
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL, run_id TEXT NOT NULL, type TEXT NOT NULL,
 company_id INTEGER, source_id TEXT, started_at INTEGER NOT NULL, finished_at INTEGER,
 status TEXT NOT NULL, reason_code TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '',
 provider TEXT, model TEXT, reserved_usd REAL NOT NULL DEFAULT 0 CHECK(reserved_usd>=0),
 cost_usd REAL CHECK(cost_usd IS NULL OR cost_usd>=0), credits REAL,
 result_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(result_json))
);
CREATE INDEX scheduler_cost_date_idx ON scheduler_attempts(started_at,type);
CREATE INDEX scheduler_job_history_idx ON scheduler_attempts(job_id,started_at);
```

## scheduler_jobs

Постоянная операционная очередь этапов парсера: generation/idempotency, сроки/приоритеты, company_id/agency_id, ownership/lease и retry. Не экспортируется.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| dedupe_key | TEXT | Да | — | — |
| type | TEXT | Да | — | — |
| company_id | INTEGER | Нет | — | — |
| candidate_id | INTEGER | Нет | — | — |
| source_id | TEXT | Нет | — | — |
| payload | TEXT | Да | — | '{}' |
| status | TEXT | Да | — | 'pending' |
| priority | INTEGER | Да | — | — |
| run_after | INTEGER | Да | — | — |
| attempts | INTEGER | Да | — | 0 |
| max_attempts | INTEGER | Да | — | 4 |
| locked_by | TEXT | Да | — | '' |
| locked_until | INTEGER | Да | — | 0 |
| last_error | TEXT | Да | — | '' |
| reason_code | TEXT | Да | — | '' |
| created_at | INTEGER | Да | — | — |
| updated_at | INTEGER | Да | — | — |
| completed_at | INTEGER | Нет | — | — |
| agency_id | INTEGER | Нет | — | — |

```sql
CREATE TABLE scheduler_jobs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, dedupe_key TEXT NOT NULL,
 type TEXT NOT NULL CHECK(type IN ('scan_source','classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance','diagnostic')),
 company_id INTEGER, candidate_id INTEGER, source_id TEXT,
 payload TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(payload)),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','cancelled')),
 priority INTEGER NOT NULL, run_after INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 max_attempts INTEGER NOT NULL DEFAULT 4, locked_by TEXT NOT NULL DEFAULT '', locked_until INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', reason_code TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, completed_at INTEGER, agency_id INTEGER CHECK(agency_id IS NULL OR company_id IS NULL),
 UNIQUE(generation,dedupe_key)
);
CREATE UNIQUE INDEX scheduler_agency_source_active_idx ON scheduler_jobs(generation,agency_id,source_id) WHERE agency_id IS NOT NULL AND type='scan_source' AND status IN ('pending','running');
CREATE INDEX scheduler_due_idx ON scheduler_jobs(generation,status,run_after,priority);
CREATE UNIQUE INDEX scheduler_source_active_idx ON scheduler_jobs(generation,company_id,source_id) WHERE type='scan_source' AND status IN ('pending','running');
```

## scheduler_runs

Журнал циклов диспетчера и глобальная блокировка с ограничением времени; источник сигнала/счётчики/cursor.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| trigger | TEXT | Да | — | — |
| started_at | INTEGER | Да | — | — |
| finished_at | INTEGER | Нет | — | — |
| status | TEXT | Да | — | — |
| ready | INTEGER | Да | — | 0 |
| processed | INTEGER | Да | — | 0 |
| succeeded | INTEGER | Да | — | 0 |
| errors | INTEGER | Да | — | 0 |
| remaining | INTEGER | Да | — | 0 |
| reason | TEXT | Да | — | '' |
| cursor_id | INTEGER | Да | — | 0 |
| lease_until | INTEGER | Да | — | 0 |

```sql
CREATE TABLE scheduler_runs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, trigger TEXT NOT NULL,
 started_at INTEGER NOT NULL, finished_at INTEGER, status TEXT NOT NULL,
 ready INTEGER NOT NULL DEFAULT 0, processed INTEGER NOT NULL DEFAULT 0, succeeded INTEGER NOT NULL DEFAULT 0,
 errors INTEGER NOT NULL DEFAULT 0, remaining INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 cursor_id INTEGER NOT NULL DEFAULT 0, lease_until INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX scheduler_runs_date_idx ON scheduler_runs(started_at DESC);
```

## vacancy_candidate_classification_leases

Операционное владение классификацией одной первичной вакансии: поколение restore, owner token, срок. Исключено из бэкапа.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| candidate_id | INTEGER | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | — |
| expires_at | INTEGER | Да | — | — |

| Внешний ключ | Ссылается на | При удалении |
| --- | --- | --- |
| candidate_id | vacancy_candidates.id | CASCADE |

```sql
CREATE TABLE vacancy_candidate_classification_leases (
 candidate_id INTEGER PRIMARY KEY NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 generation TEXT NOT NULL, owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL CHECK(expires_at>0)
);
```

## vacancy_classification_jobs

Операционные пакеты JEV до50 ID: durable progress, owner/lease/generation, bounded retry, известная/неизвестная стоимость, pause/resume; без backup и без секретов.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| request_key | TEXT | Да | — | — |
| state | TEXT | Да | — | 'queued' |
| items_json | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | '' |
| lease_until | INTEGER | Да | — | 0 |
| created_at | INTEGER | Да | — | — |
| updated_at | INTEGER | Да | — | — |
| duration_ms | INTEGER | Да | — | 0 |
| reason | TEXT | Да | — | '' |

```sql
CREATE TABLE vacancy_classification_jobs (
 id TEXT PRIMARY KEY NOT NULL, generation TEXT NOT NULL, request_key TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','paused','completed')),
 items_json TEXT NOT NULL CHECK(json_valid(items_json)), owner_token TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 duration_ms INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 UNIQUE(generation,request_key)
);
CREATE INDEX classification_job_generation_idx ON vacancy_classification_jobs(generation,created_at);
```

## vacancy_classification_slots

Два общих слота классификации заголовков JEV. Atomic claim/expiry/owner/generation; не затрагивают Firecrawl или relevance, не экспортируются.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| slot | INTEGER | Да | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | — |
| expires_at | INTEGER | Да | — | — |

```sql
CREATE TABLE vacancy_classification_slots (
 slot INTEGER PRIMARY KEY NOT NULL CHECK(slot BETWEEN 1 AND 2), generation TEXT NOT NULL,
 owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL
);
```

## vacancy_detail_jobs

Операционные bounded пакеты полных текстов с durable progress и generation/owner fencing. Без backup.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| request_key | TEXT | Да | — | — |
| items_json | TEXT | Да | — | — |
| state | TEXT | Да | — | 'queued' |
| owner_token | TEXT | Да | — | '' |
| lease_until | INTEGER | Да | — | 0 |
| created_at | INTEGER | Да | — | — |
| updated_at | INTEGER | Да | — | — |
| duration_ms | INTEGER | Да | — | 0 |
| reason | TEXT | Да | — | '' |

```sql
CREATE TABLE vacancy_detail_jobs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, request_key TEXT NOT NULL, items_json TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued', owner_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, duration_ms INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 UNIQUE(generation,request_key)
);
```

## vacancy_detail_leases

Операционные блокировки candidate/job и два слота генеративной LLM; expiry/generation/owner, без backup.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| lease_key | TEXT | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | — |
| expires_at | INTEGER | Да | — | — |

```sql
CREATE TABLE vacancy_detail_leases (
 lease_key TEXT PRIMARY KEY, generation TEXT NOT NULL, owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL
);
```

## vacancy_discovery_runs

Операционные результаты проверки на 1 час: UUID, пользователь, поколение, snapshot источника и найденные пары. Без HTML и без backup.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Да | PRIMARY KEY | — |
| user_name | TEXT | Да | — | — |
| generation | TEXT | Да | — | — |
| company_id | INTEGER | Нет | — | — |
| agency_id | INTEGER | Нет | — | — |
| source_id | TEXT | Да | — | — |
| source_url | TEXT | Да | — | — |
| source_revision | INTEGER | Да | — | — |
| settings_revision | INTEGER | Да | — | — |
| result_json | TEXT | Да | — | — |
| created_at | INTEGER | Да | — | — |
| expires_at | INTEGER | Да | — | — |

```sql
CREATE TABLE "vacancy_discovery_runs" (
 id TEXT PRIMARY KEY NOT NULL, user_name TEXT NOT NULL, generation TEXT NOT NULL,
 company_id INTEGER, agency_id INTEGER, source_id TEXT NOT NULL, source_url TEXT NOT NULL,
 source_revision INTEGER NOT NULL, settings_revision INTEGER NOT NULL,
 result_json TEXT NOT NULL CHECK(json_valid(result_json)), created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE INDEX discovery_run_expiry_idx ON vacancy_discovery_runs(expires_at);
```

## vacancy_import_batches

Операционный durable progress подтверждённого пакета импорта, без backup.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| id | TEXT | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| request_key | TEXT | Да | — | — |
| items_json | TEXT | Да | — | — |
| state | TEXT | Да | — | 'pending' |
| created_at | TEXT | Да | — | — |

```sql
CREATE TABLE vacancy_import_batches (id TEXT PRIMARY KEY,generation TEXT NOT NULL,request_key TEXT NOT NULL,items_json TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',created_at TEXT NOT NULL,UNIQUE(generation,request_key));
```

## vacancy_import_write_context

Только транзакционный контекст авторизованного parser UPDATE для триггеров ручных полей.

| Поле SQL | Тип | NOT NULL | Ключ | По умолчанию |
| --- | --- | --- | --- | --- |
| vacancy_id | INTEGER | Нет | PRIMARY KEY | — |
| generation | TEXT | Да | — | — |
| owner_token | TEXT | Да | — | — |

```sql
CREATE TABLE vacancy_import_write_context (vacancy_id INTEGER PRIMARY KEY,generation TEXT NOT NULL,owner_token TEXT NOT NULL);
```
<!-- /AUTO:DATABASE_TABLES -->

### Операционный high-water расхода Apify

SQL0073 добавляет apify_account_usage (id=JSON tuple account/period, provider_used_usd, updated_at). При миграции переносится максимум известной статистики из Free/Starter account caches, старые SQL и ledger запусков не меняются. Account refresh атомарно объединяет provider response/cache/операционный максимум и обновляет cache под revision/generation guard. Точный account/period исключает смешивание разных циклов. Таблица без FK/cascade и исключена из export/restore: восстановление backup37 не снимает известные расходы. Доступ к токенам/платному профилю и новые старты по-прежнему требуют прежних permissions; наличие записи расхода не разрешает выполнение.


SQL0076_parser_recovery: точное обновление прежнего стандартного промта конечных ролей без замены custom/model; ограниченное исправление инцидента СберТех/общая страница Сбера для автоматических кандидатов, связанных основных карточек и ожидающих jobs. Main ID/заявки/тексты/roles/publication keys сохранены, repair_company фиксирует перенос. Дублирующий rabota источник СберТеха выключен с revision fence; исходный URL сохранён. Таблицы/формат backup37 не меняются. Повтор не отменяет последующий ручной перенос карточки.

Связанные с исправленной карточкой отклики и задачи, записанные на СберТех, также получают работодателя Сбер. Их ID, статусы, комментарии и календарные ссылки сохранены; внешние календарные события не обновляются этой миграцией.
