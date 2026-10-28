# Устройство кода и доработка

Если rabota сохранил часть вакансий, но не дал продолжения, scan остаётся неполным (listing_incomplete), без отметки полного успеха. Следующая свежая проверка назначается через выбранный интервал; неполный результат не отключает источник навсегда и не вызывает немедленный повтор.

У rabota heading однозначного блока карточки имеет приоритет над остальными ссылками того же ID: зарплата тоже является ссылкой на вакансию и не должна конфликтовать с названием. Разные заголовки двух карточек с одним ID по-прежнему неоднозначны.

Для списков rabota.ru Firecrawl получает onlyMainContent=false: очистка основного контента может вырезать шапки самих карточек. Другие источники и detail сохраняют default=true. Это внутренний boolean транспорта/policy, без произвольных provider опций в HTTP. Card pairing ограничен настоящим закрывающим тегом root карточки, footer title не используется.

Rabota: заголовок карточки может находиться вне пустой мобильной ссылки. Адаптер связывает heading и vacancy URL только внутри одного блока vacancy-preview-card; карточка с несколькими разными ID не получает догаданный заголовок. Чужие hosts и обычные проверки пары остаются обязательными.

rabota-check queue использует source/favorites/generation guards и enqueueScheduledJob. Стабильный ключ rabota-html-v1 включает company/source revision/URL/scheduler revision, независимо от UUID запроса. Повтор завершённого задания не запускает provider снова; старое extraction failure сохраняется. Обычный scan сохраняет candidates и передаёт их JEV через штатный pipeline.


rabota список дополнительно запрашивает HTML и извлекает заголовки из HTML anchors; Markdown остаётся вторым каналом. Разрешены только явные канонические алиасы rabota.ru/www.rabota.ru вместе с исходным host, без wildcard внешних доменов. Числовые карточки нормализуются на www, search_id удаляется до dedupe. Переход страницы идёт только по реальной ссылке page на том же path/query (next label либо следующий числовой номер), без синтеза URL. Исчерпание page cap с оставшейся ссылкой — partial/listing_incomplete. Detail поддерживает те же два канонических алиаса. В probe добавлены bounded разметка/closed evidence; это диагностические данные, не инструкции.

rabota.ru: ссылки /company/<slug>/ при сборе направляются на /company/<slug>/vacancies/, сохраняя исходный catalog URL/revision в guards. Адаптер rabota извлекает пары названия/числового vacancy ID. Если текст явно сообщает диапазон меньше общего числа вакансий, а nextUrl не получен, результат partial/listing_incomplete; доступные пары сохраняются, завершение всего источника не подтверждается. Приоритет обычный scan_source, источник применяется только при наличии ссылки и выбранном rabota в общей области.

Актуально на21.10.2026, редакция документации209. Текущий frontend — React19 + strict TypeScript, backend — JavaScript в server/. Исходная схема0078 (79 миграций), backup38, рейтинг5; API без версии в URL, контракт21.10.2026. Иерархические роли реализованы в `server/repositories/vacancy-roles.js`, UI выбора и фильтра — в `frontend/features/vacancies/RoleTree.tsx`, административное дерево — в `frontend/features/settings/Roles.tsx`. Источник истины — `vacancy_role_assignments`; `company_vacancies.role` остаётся совместимым строковым представлением. JEV классифицирует по включённым корневым направлениям, затем выбирает допустимые конечные роли дерева. Персональная релевантность от справочника ролей не зависит. История переноса и исправлений хранится в docs/refactoring-progress.md, serious-issues-audit.md и visual-parity-audit.md.

## Принадлежность парсера компании или агентству

parser-owner.js нормализует старые numeric company refs и новые {agencyId} без пересечения пространств ID. В candidates/source/discovery/jobs отдельная nullable agency_id; company_id сохраняет исходный смысл. SQL0078 перестраивает только source/discovery (без входящих FK), сохраняет существующие company rows/cursors, добавляет agency column/index к candidates/jobs, отдельную active source uniqueness агентств. Все новые настройки читают agenciesEnabledfalse из scheduler defaults; переключатель хранится в existing scheduler_json, а не отдельной автоматизации.

parserCompanyDb сохраняет имя для compatibility, но возвращает owner/favorite/current-agency-switch generationBoundDb. Discovery/save/resume/Apify передают полный owner, source SQL выбирает только доверенную column через parserOwner, namespaceagency:id отделён отcompany:id. Candidate aliases атомарно блокируют изменение владельца между company/agency или разными agency. Scheduler claim пропускает OFF, job→candidate owner equality дополнительно fenced, все automatic stages получают parent owner DB guard. Manual candidate leases/classification/import используют agencyCandidateDb с live SQL switch; результаты выключенного этапа не сохраняются, usage/reservations не обнуляются. Глобальный Firecrawl lease для сбора/сохранения освобождается через исходный job/epoch DB (`PARSER_RESOURCE_DB`), даже при выключении owner scope; бизнес-записи по-прежнему защищены повторным owner guard. При неопределённом ответе credit reserve и короткое active_until окно сохраняются.

Импорт общий: тип/имя выбираются по реальной FK/catalog owner, duplicate matching включает entity_type, manual fields/provenance/личные данные сохраняют прежнюю защиту. Придуманный клиент агентства не создаётся. Personal relevance поддерживает agency с тем же JEV/config/budget/owner. Positive-company diagnostic/replay namespaces сохраняют исходное пространство компаний. Backup38 добавляет agency_id в два persistent storage раздела; old14–37 layouts сначала валидируются без этой колонки, затем получают null; operational jobs/discovery не восстанавливаются.

## Архитектура и границы

Sites/Vinext + Vite собирают один Cloudflare Worker с D1 (`DB`). Отдельный Node-сервер приложения или новая база не создавались. `build/sites-worker.ts` сначала вызывает `server/routes/index.js`: авторизация, API, автоматизация, OAuth. Ответ null передаёт запрос Vinext. Ошибка POST /auth идёт через внутренний x-app-login-error в оболочку входа HTTP401; адаптер добавляет security headers.

`app/page.tsx` подключает клиентский `frontend/Application.tsx`. SSR видит пустой snapshot API-кэша и не запрашивает данные разделов. После инициализации React выполняет GET /api/session; только успешная сессия монтирует рабочие компоненты. Нет импорта server/, docs/, worker/ или src/data.json во frontend/shared. Секреты, SQL и административный Markdown остаются на сервере. Карта использует npm MapLibre GL, OpenFreeMap и отдельные JSON географии метро.

## Карта файлов

| Путь | Назначение |
| --- | --- |
| app/page.tsx, app/layout.tsx | Оболочка Vinext и подключение стилей |
| build/sites-worker.ts | Backend-first Worker entry |
| build/sites-vite-plugin.ts | Сохранённая интеграция упаковки Sites, очередь общего outputDirectory |
| frontend/Application.tsx | Сессия, навигация, состояние поиска/фильтров, контекст карточек и lazy imports |
| frontend/api/client.ts | request/load/useApi/mutate, кэш, дедупликация, инвалидация, session-expired |
| frontend/features/companies | Компактные таблицы, фильтры, CompanyDrawer, офисы/отзывы/контакты |
| frontend/features/vacancies | Список, model.ts, Drawer/Editor/Conflict, CoverLetter и cover-letter.ts |
| frontend/features/applications | Формы, согласование конфликта, frozen intent нового отклика |
| frontend/features/tasks, channels | Личные задачи, общие каналы и их вакансии |
| frontend/features/map | Map.tsx, geometry.ts, MapSearch.tsx, labels.ts |
| frontend/features/settings | Profile/Prompts, PromptModel, OpenAI/OpenRouter, Documentation, Transfer/xlsx.ts, openaiCost.ts |
| frontend/components, hooks, lib | Markdown/editor, Drawer, PanelBoundary, useDirty, clipboard |
| frontend/styles/app.css, react.css | Сохранённые правила интерфейса и React-дополнения |
| shared/contracts.ts | Типы HTTP и клиентские справочники, без серверных импортов |
| server/routes/index.js, auth/session.js, http.js, constants.js | HTTP, сессии/роли, ответы, перечисления и лимиты |
| server/repositories | SQL: каталоги, вакансии, отклики, задачи, профили, ключевые operational guards |
| server/services/catalog-projection.js, vacancy-projection.js | Компактные проекции/detail из существующего источника |
| server/services/application-creation*.js, application-revision.js, vacancy-revision.js | HMAC-context, receipt и CAS снимков |
| server/services/cover-letter*.js | Снимок входов, генерация, поток прогресса, ожидание/lease |
| server/integrations | openai.js, openai-models.js, openai-pricing.js, openrouter.js, google-calendar.js |
| server/import-export, backup | XLSX/export, импорт каталогов, полный JSON и restore |
| server/data.js, src/data.json | Серверный исторический seed и generated metadata; не живая база пользователя |
| server/generated | documentation.json и backup-schema.json, пересобираются из исходников |
| db/schema.ts, drizzle/ | Drizzle и append-only история SQL0000–0051 |
| scripts/prepare-server.mjs, documentation.mjs, backup-schema.mjs | Автоблоки документации и доверенная схема восстановления |
| scripts/run-framework.mjs, build-verified.sh, vite.config.ts | portable/managed сборка dist/client и dist/server |
| scripts/validate.mjs, run-tests.mjs, test-*.mjs | Архитектурные, TypeScript, HTTP/React/D1 регрессии |
| docs/user/manifest.json, docs/*.md, AGENTS.md | Актуальные руководства, история и инструкции следующему агенту |
| .openai/hosting.json | Идентичность проекта и bindings, сохранять при обновлении |
| worker/, scripts/build.mjs | Исторический эталон; текущие npm build/start его не используют |

## Загрузка разделов и кэш

Сначала HTML/CSS/JS и /api/session. По умолчанию активны задачи: /api/tasks и /api/google-calendar/status. Первый переход в компании — /api/companies/list; карта — /api/companies/map; вакансии — /api/vacancies/list; отклики — /api/crm/applications; агентства/каналы — собственные API. Полная карточка компании открывается через detail?name либо :id, вакансии — :id. Её независимые вкладки не загружаются вместе с detail.

Company list содержит только колонки/локальные фильтры, office address/metro, признаки и сводный applicationStatus. Вилки Lead/Head сохранены ради существующих колонок. hasApplications учитывает любой личный отклик, общий или связанный, включая отказ. hasVacancies учитывает владельца/реального работодателя и ещё не перенесённые исторические jobs. URLs источников/тексты/координаты там отсутствуют. Map получает координаты без зарплат/описаний. Штатная проекция companies —6 SQL-операций, vacancies —4; bootstrap/backfill отдельные и пакетные, не N+1. См. [API](API.md).

Поиск списка — локально по имени. MapSearch использует search?q= с задержкой 180 мс; расширенные поля поиска остаются серверными, подсказки не меняют набор геометрии/счётчик. Ответы HTTP private/no-store, Service Worker обходит /api/.

`frontend/api/client.ts` хранит snapshot/pending Promise по полному URL в памяти страницы; useSyncExternalStore подписывает смонтированные компоненты. Повторные load объединяются, успешный GET переиспользуется. Mutation помечает связанные группы stale: активные подписчики перечитывают, неактивные ждут открытия. Generation защищает от позднего GET. Это групповая инвалидация, а не обязательное точечное изменение каждого поля.

DELETE ACK отклика сразу удаляет ID из закэшированных списков, даже если последующий GET падает. clearCache/session reset сбрасывают память; HTTP401 вне session отправляет session-expired и выход. Тексты писем хранятся только в detail-кэше. cover-letter.ts держит progress/error/completed/pending; done инвалидирует нужный detail, включая regenerate. PUT/DELETE вакансии, backup и clearCache abort/epoch блокируют поздний stream. Автогенерация ждёт готового detail, не запускается на временном пустом placeholder.

## Каталог, персональность и конкурентная запись

Рабочий каталог — app_catalog, серверный src/data.json лишь fallback/исторический seed. company_vacancy_state отмечает однократный перенос jobs в SQL. Не заменяйте рабочую базу seed и не удаляйте его в рамках косметической правки. Не создавайте второй каталог для компактного API.

Текущий пользователь определяется только подписанной cookie. Админские возможности не дают произвольный userName в личных API. Общие контакты/вакансии/источники/промты/модели отличаются от личных откликов/задач/резюме/состояний/писем. effective письмо всегда несёт scope, чтобы PUT не скопировал личный текст в общее поле.

PUT отклика/вакансии использует исходный SHA-256 revision и SQL CAS внутри batch; конфликты 428/409 сохраняют черновик. Не подменяйте базовый snapshot при обновлении props. Отклики поддерживают явное трёхстороннее согласование; вакансии — read-only latest, без автоматического merge. Создание отклика и apply-shortcut требуют неизменные UUIDv4+HMAC creationContext: receipt делает replay безопасным после lost ACK, удалённый результат 410, restore409. POST вакансии такого receipt не имеет. Точные границы транзакций приведены в [API](API.md).

## Генерация писем и провайдеры

Четыре входа: общий cover_letter промт, личное резюме, заголовок и полный текст вакансии. Нормальная генерация возвращает заполненное письмо без AI; explicit regenerate доступен при заполненных subject+text, меняет только личную запись с override_shared=1/revision+1. До успеха показывается старый текст. Приоритет: личный override, заполненный shared, обычный personal.

Снимок в cover-letter-input.js включает revisions профиля/промта/модели/ключа OpenAI и identity/тексты вакансии. Guard сравнивает его в SQL при сохранении. Durable app_runtime_generation блокирует старый результат после даже идентичного restore. D1 lease один на user/vacancy,90 с с heartbeat20 с, owner fencing; ожидающие получают общий результат, recovery только истёкшего права. Это не фоновая очередь: закрытый клиент/завершённый Worker не гарантирует сохранение.

Поток NDJSON отправляет progress каждые 8 с, done/error; frontend не имеет минутного таймаута. OpenRouter: последняя успешная тестовая → следующая по рейтингу,210 с/попытка. OpenAI: выбранная модель/effort, Responses API300 с, без смены провайдера/модели. Проверяются completed/output_text/model/формат. Subject≤512, Markdown≤2000; парсер может сокращать ответы 2001–4000, больший ответ отвергает. Ключи/входы/ответы в технические логи не копируются.

OpenAI key шифруется в D1 через SESSION_SECRET:openai:v1; DB приоритетнее env fallback. Модели /v1/models кэшируются 6 ч, profiles capabilities отдельные. Настройки prompt model общие и CAS; relevance пересчитывается по отдельной вакансии через services/vacancy-relevance.js по клику в списке. Историческая функция scoreEugeneResumeFit остаётся для первичного backfill и сохраняет существующие личные оценки.

Тарифный справочник openai-pricing.js проверен 09.10.2026: Standard USD1Minput/output, source/date, unknown=null без наследования цены snapshot/fine-tune. Refresh models не обновляет тарифы. openaiCost.ts считает RUB за1000 при85₽/USD: письмо20k/5kchars →10k/2.5ktokens, релевантность15kchars/одно число →7.5k/1token. Для JEV admin-only `/api/openrouter/jev` читает живые prompt/completion/request тарифы из каталога без Decisions-запроса; бесплатный OpenRouter равен0. Output reasoning/служебный формат могут оплачиваться сверх оценки. Конкретный лимит сохранённого письма2000 отличается от сценария оценки5000 — не увеличивать лимит молча. Balance UI показывает ссылку на кабинет без выдуманного остатка. Метрики обеих интеграций в openrouter_model_metrics; OpenAI IDs имеют префикс openai:.

## Вёрстка и компоненты: что сохранять

| Контекст | Правило |
| --- | --- |
| Lazy-карточки | Отдельный PanelBoundary/Suspense. Компания/агентство kind=company: skeleton и карточка .panel слева; вакансия/канал/редактор справа. Вложенная загрузка не скрывает компанию |
| Markdown/editor | compact crm-editor/crm-comment/crm-preview.show/crm-editor-foot; большие resume-toolbar/resume-editor/resume-preview/resume-footer. Сохранить переносы кода/таблиц и безопасное отображение без raw HTML |
| Документация | docs-layout/content/toolbar/article, docs-markdown; прежняя desktop/mobile геометрия app.css |
| Select настроек | Native select.settings-select, общий react.css; как crm-select, label/focus/disabled/keyboard/mobile |
| Файлы/AI-таблицы | Native settings-file-input::file-selector-button; settings-button; or-table внутри overflow wrapper, без произвольных overrides |
| Отклики | Полные поля при PUT, исходная revision, frozen intent; busy-lock сохранения/удаления; удаление соседа не стирает черновик |
| Вакансии | Полный editor справа, all fields/Markdown/вилка; контекст компании/агентства/канала сохраняет правильную расчётную зарплату и кнопки |
| Карта | Чистые labels/geometry, сохранённая легенда/цвета; выделяются офисы выбранной компании, не агентства с тем же именем; cleanup RAF/observer/instance |
| Каналы | Admin add/delete в List→Channels, кандидату comment read-only; запрет удаления последнего источника канальной вакансии |
| Отзывы | ReviewSources и общий AgencyRating без дублирования формул/нового CRM-запроса |

Основной app.css сохраняет все исторические рабочие блоки в прежнем порядке. Наличие класса в JSX не доказывает наличие правила CSS; успешный component test не подтверждает пиксельную идентичность. Ограничения visual QA записаны отдельно в аудите.

## Сборка и локальный запуск

Нужны Node≥24, npm, зависимости из package-lock.json. Для managed Linux используется установленный профиль исполнения и штатный Sites helper; не редактируйте lockfile ради особенностей среды.

```bash
npm ci --include=dev --include=optional
npm run build
npm start -- --port 8787
```

build сначала выполняет prepare-server, затем Vinext. Результат: dist/client и dist/server, Worker entry dist/server/index.js, конфигурация dist/server/wrangler.json, SQL в dist/.openai/drizzle. dev запускает исходники через run-framework; start запускает собранный Wrangler Worker и локальный D1 с --persist-to .wrangler/state. Запуск start требует предварительной сборки.

Wrangler читает локальные секреты относительно config: при данном start это `dist/server/.dev.vars`, не корневой .env автоматически. Нужны SITE_PASSWORD, NASTYA_PASSWORD, DEMO_PASSWORD, SESSION_SECRET; .env.example содержит только placeholders, не готовые пароли. Создавайте .dev.vars после сборки, держите вне Git; повторная сборка может заменить dist. Google/OAuth/automation/OpenAI env необязательны для базового интерфейса. API-ключи можно настроить из админки. SESSION_SECRET переносится отдельно от JSON-бэкапа.

Сгенерированный Wrangler config не задаёт migrations_dir. Для новой локальной базы можно создать отдельный непубликуемый override с теми же binding/ID/persistence, добавив путь к исходным SQL:

```bash
node --input-type=module <<'JS'
import fs from 'node:fs';
const config = JSON.parse(fs.readFileSync('dist/server/wrangler.json', 'utf8'));
config.d1_databases[0].migrations_dir = '../../drizzle';
fs.writeFileSync('dist/server/wrangler.local.json', JSON.stringify(config, null, 2));
JS
npx wrangler d1 migrations apply site-creator-d1 --config dist/server/wrangler.local.json --local --persist-to .wrangler/state
npm start -- --port 8787
```

Эти команды относятся к новой локальной базе, не к production; они не выполняются при обновлении документации. После миграций живые данные переносятся отдельным проверенным JSON-бэкапом. Не применять начальные миграции повторно к уже bootstrap-восстановленной БД. Чистый архив кода не содержит реальное состояние .wrangler/state/D1 или секреты. Secure cookie рассчитана на HTTPS production; для локального входа проверяйте поддержку secure localhost браузером/конфигурацией.

Активному runtime нужны D1 и внешние HTTP-сервисы для карты/AI/календаря. R2/KV/Queues/DO/cron в текущем Wrangler config не объявлены. Внешняя расписанная автоматизация не появляется из одного Git-архива. Sites-specific scripts управляют упаковкой/публикацией; production принимает сохранённую точную сборку из Git, а не произвольный локальный dist.

## Проверки и дальнейшие изменения

`npm run typecheck` — strict TS. `node scripts/test-documentation.mjs` —8sections/ACL/downloads/Markdown/SQL. `node scripts/test-react.mjs` —16 React-сценариев. `npm run validate` после build проверяет архитектуру/секреты/миграции/форматы, затем запускает TypeScript и 58 root test suites (число на 09.10; runner находит файлы автоматически). Проверки используют синтетические данные; для исторических Git fixtures нужен полный Git checkout, не shallow ZIP без .git.

При доработке меняйте текущий модуль, контракт shared и соответствующие документы. Не редактируйте generated JSON/AUTO-блоки вручную, старые SQL и историю пользовательских ТЗ. Делайте маленькие проверяемые задачи и push refactor/react-typescript с проверкой SHA. [Инструкция сопровождения](https://github.com/meInteractive/it-map/blob/main/docs/dev/MAINTENANCE.md) задаёт быстрый порядок обновления документации и публикации; [журнал](CHANGELOG.md) хранит дату/функциональность/затронутые данные.

`integrations/jev.js` проверяет `typesafe/jev-1.13` в каталоге OpenRouter с явным `output_modalities=decisions`: без этого параметра API по умолчанию возвращает только text-модели и скрывает доступную JEV. Затем адаптер отправляет Score через `/api/alpha/decisions`. `runPromptAI` для relevance/openrouterModel=jev возвращает числовую оценку 0–100, нормализуя десять уровней Score; сохраняет фактическую модель в журнале. Бесплатный режим по-прежнему использует текущий рейтинг/fallback. Неподдерживаемая задача, неверная модель/оценка или отсутствие средств возвращает ошибку без переключения. Адаптер запускается только при явном POST расчёта вакансии; сохранение настроек и открытие карточки его не вызывают.

`frontend/features/vacancies/Relevance.tsx`: общее состояние pending по vacancyId предотвращает повторный запрос из дублирующихся строк и сохраняет ошибки рядом с числом. Успешный POST обновляет релевантность в кэшированных списках/карточке; начатый раньше GET не может вернуть старое число. Полный список заново не загружается; detail помечается устаревшим для актуальной revision редактора. clearCache отбрасывает состояния и ответы предыдущего сеанса. Списки компаний/каналов/агентств и карточки продолжают получать личную оценку из одной SQL-таблицы.

## Классификация названий вакансий

server/repositories/classification-prompts.js расширяет механизм общих промтов связанными с ID корней записями: общий readPromptInput/MarkdownEditor/CAS, отдельная таблица vacancy_role_classification из-за ограниченного kind CHECK старой system_prompts. Инициализация INSERT OR IGNORE по stable_key, новая роль пустая/выключенная. Перемещение в дочерние не удаляет настройки; удаление настроенной роли требует выключения и пустого текста. server/services/vacancy-classification.js получает активные корни, generation-bound DB и проверяет актуальность версий после ответа. runJevTitleClassification в существующем jev.js переиспользует orSettings/orKey/orFetch/orLog и Decisions alpha transport. Один choice-вопрос с match/reject на role_ID; общий state содержит только title. Формат подтверждён по официальному https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-request 13.10.2026. runJevRelevance и настройки провайдеров не меняются. Интерфейс Classification.tsx использует общие стили/Markdown/ActionForm и защищает двойные нажатия/смену сеанса. Модуль классификации не выполняет Firecrawl или запись в основные вакансии; ручной Firecrawl описан ниже.

## Промежуточное хранилище

server/repositories/vacancy-candidates.js реализует upsertVacancyCandidate, getVacancyCandidates, getVacancyCandidate, linkCandidateToMainVacancy и candidateClassificationContext. Существующие company_identity и VACANCY_SOURCE_META переиспользуются; отдельного справочника источников нет. Источник, URL вакансии и способ получения независимы. sourceNamespace задаётся явно либо вычисляется как хеш пары «origin публикации + каноническая страница обнаружения»; для внешних ID без URL нужен явный namespace. importKey — устойчивый ID публикации, не название и не номер строки, меняющийся между выгрузками.

Уникальная vacancy_candidate_identities связывает все известные алиасы с одной записью. Транзакция сначала проверяет несовместимые ID, затем условно создаёт запись, добавляет алиасы и обновляет наблюдение. Программный SELECT-before-INSERT не используется как защита от гонки. Старый URL остаётся алиасом после переезда публикации. Разные внешние площадки с разными URL не объединяются по похожему названию. Raw title сохраняется буквально; хеш относится к заголовку после существующей trim-валидации JEV, title_revision меняется при изменении trim title hash; крайние пробелы и внешние метаданные не инвалидируют решения. Статусы не материализуются: SQL вычисляет их по небольшому снимку активных направлений; фильтр статуса не требует загрузки всей базы в JS. История постраничная. Generation-bound DB защищает записи от restore.

server/services/stored-vacancy-classification.js реализует classifyStoredCandidate(env,id,{force,signal}). Небольшой снимок активных корней содержит SHA256 Markdown. Валидный кэш проверяется по role_id/hash/model_key/title_hash/title_revision и последнему ID результата. Поэтому смена одного промта не затрагивает другой, выключение/включение неизменного промта переиспользует историю. configured model_key — typesafe/jev-1.13; dated фактический model_id сохраняется как ответ провайдера. Смена configured семейства в коде инвалидирует кэш, каталог не опрашивается ради уже актуальных результатов.

Классификацией владеет durable vacancy_candidate_classification_leases: ownerUUID/generation/expiry300сек. После приобретения кэш перечитывается; конкурентный worker не выполняет второй inference. Истёкший lease заменяется, старый владелец не может сохранить ответ или удалить lease преемника. Snapshot заголовка и владение проверяются в той же транзакции, что INSERT попытки и отдельных решений. Восстановление rotates generation атомарно. Технические ошибки хранятся в attempts, не вставляются как reject. При частичном ответе успешный match даёт accepted, недостающий корень требует повтора; все успешные reject дают rejected. История по постоянному root_role_id защищает удаление роли и сохраняется при её перемещении/переименовании. Стоимость одного пакетного запроса хранится только на attempt. Excel/main creation/scheduler отсутствуют; Firecrawl подключён отдельным модулем, описанным ниже.

## Сбор первичных вакансий Firecrawl, 14.10.2026

Клиент integrations/firecrawl.js отвечает за REST/ключ/тайм-аут/лимит ответа2MiB и безопасные ошибки. vacancy-listing-extractor.js содержит программный registry адаптеров, связанные Markdown/HTML пары и существующую ссылку «Следующая» в том же path с разрешённым query-параметром; порядок разных массивов не используется. vacancy-source-parsing.js связывает настройки company_identity+VACANCY_SOURCE_META, URL snapshot, off default/CAS, не пишет каталог. vacancy-discovery.js получает источник, удерживает глобальный generation/owner lease, делает последовательный Scrape, ограничивает страницы/200пар, записывает preview без HTML на1час и позднее сохраняет его через upsertVacancyCandidate. Config/key/restore fence проверяются перед платным запросом и при сохранении; успешный staging save затем ставит отдельные durable JEV jobs. Preview не классифицируется. Stable namespace companyId/sourceId/publisher origin держит внешние ID между страницами, canonical URL alias сохраняет обычную дедупликацию.

Стартовый URL берётся только из каталога. literal IP/local/metadata/credentials/ports блокируются. DNS A/AAAA проверяются через Cloudflare DoH, HEAD redirects (до3) проверяются по явным per-source доменам, а Firecrawl final metadata — повторно. Provider сам разрешает DNS/redirects: REST Scrape не документирует параметр принудительного запрета его redirects. Предварительная проверка и проверка результата не гарантируют отсутствие смены DNS/redirect между двумя независимыми обращениями; эту границу провайдера не скрывать. HEAD/DNS недоступность делает источник технически недоступным, а не поводом обходить защиту. HH исключён: официальный API — отдельная задача. Telegram/страницы отзывов excluded. Generic означает возможность проверки с правилом path, не доказательство поддержки всех платформ. Selectel имеет отдельный adapter, выделяющий bold heading из той же многострочной Markdown-ссылки; города/Подробнее не превращаются в должности.

## Единая политика Firecrawl, 14.10.2026, задача4.1

firecrawl.js — только безопасный фасад, выдаёт session из policy; строки ключей не принимаются для Scrape. firecrawl-transport.js используется только policy и изолированным транспортным тестом. Account GET — нулевая операция той же политики. Перед каждым Scrape и retry отдельный durable резерв4, живые статистика/период, SQL predicates revision/mode/enabled, общий concurrency1/rate10или100. Usage events/periods операционные, excluded backup, restore не удаляет их и не возвращает прежний бюджет. В журнале только SHA ключа, профиль, период, операции/статусы/кредиты, без URLs/HTML/ключей. Неизвестный расход не освобождается по TTL; active browser после timeout удерживается6минут. Нет асинхронных provider jobs: synchronous Scrape не имеет публичного cancel API, уже отправленный запрос может списать кредиты после выключения Hobby. Изменение режима не меняет ключ выполняющегося запроса; stop перед следующей страницей сохраняет partial preview. Политика Firecrawl независима от JEV; upsert сохраняет staging, основная база этим этапом не пополняется.

## Автоматическая обработка JEV (15.10.2026)

classification-jobs.js соединяет существующий stored classifier с сохранением Firecrawl и admin API. selectClassificationCandidates/Statistics использует общую SQL projection из vacancy-candidates.js: accepted не означает complete, imported пропускается, active roots и prompt hash/modelKey динамические. operational jobs/slots SQL0057 не изменяют backup31. RunJob владеет job по owner/expiry/generation, параллельно до2worker, каждую вакансию дополнительно защищает прежний candidate lease, actual Decisions requests занимают2общих slots. Multi-role request и relevance provider path остаются существующими.

build/sites-worker.ts передаёт ctx в backend.fetch; после staging commit route enqueue-ит успешные IDs и регистрирует один waitUntil slice24сек. В отсутствие Queue binding это явно bounded fallback: остаток durable, продолжение ручное/серверным API, текущая вкладка не нужна. Не запускайте бесконечную рекурсию self-fetch или неподтверждённый Cron. Один пакет<=50; save до200 разбивает IDs на несколько пакетов, общий slice не умножается на число пакетов. Прерывание/restore не повторяет успешные current decisions; force baseline сохраняет исходные ID решений и переиспользует уже обновлённые. Technical timeout/cost unknown не равны reject/0cost. Неопределённая network/timeout попытка удерживает slot до5минут.

## Полные тексты, задача6

Контур vacancy-fulltext → vacancy-extraction → vacancy-detail-jobs переиспользует проверенный JEV допуск, Firecrawl usage policy и runPromptAI. Исходные снимки/части, аудит загрузок и извлечения независимы от company_vacancies; отдельные операции не пишут основные данные. Общий интерфейс промтов адаптирует vacancy_detail_settings без перестройки старых CHECK. Подробнее — VACANCY_DETAILS_DELIVERY.md.


## Навигация списков и вакансий

EntityNavigation.tsx использует оформление выпадающего SettingsNavigation и отдельные IDs list-navigation/vacancies-navigation. Hover открывает меню без API-запросов, главный click вызывает company, touch также открывает подпункты; keyboard arrows/Home/End/Escape и outside/blur закрытие поддержаны. Application.Workspace владеет listType/vacanciesType и проверяет confirmNavigation до смены категории/закрытия панелей. CompanyList/Vacancies получают controlled type (defaultcompany), их прежние EntitySwitch удалены. CompanyList keyed по типу, поэтому подтверждённая смена закрывает форму добавления канала. Сортировка вакансий продолжает useSectionState; старые list:type/vacancies:type больше не читаются. API, правила принадлежности и кэш компактных списков не изменены.

## Период публикации вакансий

shared/publication-period.ts используется и сервером, и UI для строгих дат/оценочного fallback. Week включает сегодня и шесть предыдущих календарных дат MSK, month/twoMonths вычитают календарные месяцы с clamp конца месяца, archive использует строгую нижнюю границу twoMonths. Bounds/Intl вычисляются один раз на ответ, затем фильтруются уже компактные строки; дополнительные SQL и полные описания не нужны. GET /vacancies/list?period=... совместим с прежним bare endpoint. Vacancies хранит period в useSectionState('vacancies:period','week'), включает его в ключ useApi и сохраняет minute/focus revalidation и инвалидацию при мутациях. Это виртуальный возрастной архив, persisted archived не меняется.

## Серверный планировщик

Scheduler: repositories/scheduler-schema.js (DDL/runtime upgrade), scheduler.js (CAS/queue/leases), services/scheduler-dispatcher.js (≤50/110с/slices/recovery), scheduler-pipeline.js (вызовы прежних сервисов/постановка следующего этапа), scheduler-budget.js (атомарные резервы), integrations/scheduler-billing.js (optional hook перед inference), scheduler-route.js и scheduler-mcp.js (admin/token/owner identity). frontend/features/settings/Scheduler.tsx организует четыре локальные вкладки (overview/scope/services/diagnostics), отложенное первое подключение форм и сохранение черновиков scope/budget с исходной CAS revision. ParserIssues.tsx показывает локальную проекцию server/services/parser-issues.js: observed/verification/planned и P1/P2/P3 отдельно от приоритетов исполнения. ParsingTools.tsx сохраняет уже открытые инструменты при переключении. Поиск очереди по организациям нормализует кириллицу на сервере, agencyId/companyId независимы. Все стили ограничены parser.css. Все бизнес-решения на сервере; Sites Automation передаёт только сигнал.

Приоритеты на сервере0–100, к ним добавляется час ожидания (aging без ограничения). Сначала сервис повторно проверяет prerequisites/cache; следующая очередь имеет устойчивый version key. Следующий tick также сверяет до50 кандидатов с cursor, поэтому прерывание между сохранением результата и enqueue не теряет продолжение. Queue/attempts/runs живут в D1, старое поколение после restore не выполняется, денежные резервы сохраняются.

Общее расписание источников: sourceIds/intervalDays в scheduler_json, favorites_only в прежнем столбце. scheduleDueSources каждый тик читает актуальный каталог и личное избранное, создаёт настройки/очередь максимум для50 due источников без opt-in компании, сохраняя custom extraction config. Следующая дата/ошибки остаются у пары компания+источник. selectedSourceDb заново проверяет общий список в SQL при каждом действии: снятый источник откладывается без новых provider-запросов и расхода попыток. Новые компании и изменённые URL рассматриваются следующим тиком.

Перед лимитом50 reconciliation отбрасывает уже существующие dedupe-ключи текущего окна: failed/completed первых компаний не блокируют остальные. Для проверки читается история только активных jobs и текущего окна scheduleRevision/dueAt, а не все прошлые циклы.

Автоматическое продвижение (16.10, вечер): dispatcher при наличии ready queue пропускает подготовку и выполняет этапы сразу. Для пустой очереди preparationDeadline=min(now+12s,deadline−35s), sources≤5; до50 candidate IDs получают classification jobs пакетным INSERT OR IGNORE, отдельно проверяется один recovery hand-off. scan_source сохраняет preview и ставит classification для всех сохранённых IDs одним пакетом с общими role hashes; next stage по-прежнему определяется по актуальным результатам и кэшу после выполнения. ensureCompanyIdentities сначала читает IDs и вставляет только отсутствующие одним JSON statement, не повторяет no-op batches всех компаний на каждый lookup.

### Прерывания LLM (16.10.2026)

`services/operation-error.js` сначала распознаёт DOMException по имени TimeoutError/AbortError, затем принимает только строковый application code. `scheduler-dispatcher` сохраняет canonical timeout, использует существующий bounded retry/cooldown и требует90с для extraction/текстовых leaf roles,45с для выбранного OpenRouter JEV. Выбор ролей повторно читается после claim, до записи attempt; смена на Luna с недостаточным окном возвращает job без расхода попытки. Оба LLM слота выбирают только помещающиеся этапы разных кандидатов;15с cleanup сохранены. Numeric legacy23 не должен попадать в reason_code/error_code. Восстановление ошибочно dead-lettered23/23.0 ограничено SQL0063 текущим поколением и LLM stages; аудит/стоимость запросов неизменны. Спецификация: https://webidl.spec.whatwg.org/#idl-DOMException-error-names.

### Ограниченная серия и выбор подходящих заданий

`services/scheduler-series.js` сначала выдаёт cloud:<UUID>:1 без обработки, затем продолжения через scheduler_tick с durable scheduler_runs receipts без нового layout. Потерянная подготовка безопасна, рабочий retry всегда использует уже известный ID. Hour collision исключён; явные legacy series поддерживаются. Manual requestId больше не помечается automatic, dashboard signal ограничен generation, response/log содержит stop reason. Предыдущий run текущего поколения обязателен; максимум5 тиков/12мин. Automation prompt хранится в docs/SCHEDULER_AUTOMATION_PROMPT.md. Расписание остаётся одним hourly; изменение prompt выполняется только после публикации совместимого сервера.

Atomic claim фильтрует типы по доступному окну и исключает candidate с другим running stage. Dispatcher запускает пары independent extraction/roles, await allSettled до завершения цикла, общий ledger и lease guards сохраняются. Ручной NDJSON поток хранит счётчики после каждого этапа, heartbeat8с; ctx.waitUntil защищает cleanup при abort. Нельзя выдавать waitUntil за бесконечный фоновый исполнитель. normalizeVacancyExtraction очищает только неподтверждённые метаданные, сохраняет corrections в diagnostics, строгий validate по умолчанию сохраняет прежний контракт.


Надёжность парсера17.10: scheduler-mcp-stream.js реализует MCP SSE/progress/final, scheduler.js/recoverExpiredSchedulerRuns закрывает просроченный same-ID receipt без возрождения owner. structured-model-health.js хранит scoped метрики в openrouter_model_metrics (structured:task:generation:keyRevision:model). Маршрутизатор сохраняет один ranked snapshot и один запрос за этап, выбирает здоровую/измеренно быструю schema-capable бесплатную модель, после ошибки следующая попытка очереди может выбрать другую. История писем не меняется; содержимое промтов/вакансий/ключей не записывается в метрики.


Apify: repositories/apify-schema/settings.js и integrations/apify.js задают фиксированный Actor/HTTP/токены; services/apify-account/runs/discovery/vacancies/route.js отвечают за billing, durable async start/poll, cursor страниц/порций, raw snapshots и API. UI settings/Apify.tsx, общие настройки sourceIds/избранных/периода сохранены. Признак provenance raw snapshot хранится в diagnostics.provider/profile; legacy firecrawl_mode оставлен совместимым, USD-расход хранится только apify_runs. HH priority: vacancy-source-priority.js и vacancy-import.js; межплощадочная дедупликация — vacancy-duplicate-match.js (22.10). Точные vendor IDs проверяются и по main, и по прежним publication_sources. Содержательная проверка всех источников использует duties/requirements и title tokens, diverse-word/Dice/containment/trigram thresholds; общий boilerplate и различающиеся ID одного publisher не становятся тождеством. Несколько сильных совпадений дают duplicate_ambiguous, до auto-separate и без новой main. Метод identity/content/explicit/new сохраняется в audit. Автоматический import при possible_duplicate повторяет guarded preview с createNew и передаёт ту же resolution в транзакцию; точные ключи/конфликты проверяются снова. В ключ этапа добавлен auto-separate-v1, исторические title-only failures восстанавливаются ограниченным dependency recovery без стирания ошибки. Нормализация тождества сохраняет смысл текста, стирает Markdown/пробелы/URL, но не использует только название. Не смешивать две первичные публикации: они связываются с одной main vacancy и сохраняются в publication_sources. Сохранённый immutable extracted_data предыдущего источника читается через уже fenced extraction_id; он не обновляется в приложении, restore защищён epoch. Новые URL provenance и защитный trigger SQL0068, прежний backup36/рейтинг5/Excel12.10.


Диспетчер Apify: scheduler-dispatcher.js обрабатывает готовую очередь до полного обхода каталога. До неё допускаются только ограниченные сверки потерянных зависимостей импорта и отсутствующей релевантности. scheduleDueSources предпочитает другие площадки перед новым HH, поддерживает внутренний sourceIds-фильтр. claimScheduledJob сохраняет priorities+aging, штраф50 только scan_source HH; run_after и locks проверяются до сортировки.

Внутри HH lane при одинаковом priority+aging предпочитается scan с ledger точной текущей страницы. Этот tie-breaker не даёт безусловной гарантии справедливости всем старым источникам; наблюдение длительного ожидания остаётся отдельной задачей.

MCP: server/discover отвечает без DB и персональных данных на протокол2026-07-28: supportedVersions, capabilities, complete, serverInfo, TTL300000/public. Legacy initialize/tools/list сохранены. Современные JSON и SSE tool results содержат resultType=complete и serverInfo; data-bearing calls всегда проверяют исходную ChatGPT identity/owner.

Инициализация моделей: историческая SQL0069 переводила extraction и roles на OpenAI/gpt-6-luna. SQL0074 добавляет vacancy_role_settings.openrouter_model и переводит роли на OpenRouter/JEV с новой model_revision, сохраняя пользовательский промт и меняя только известный стандартный текст. Текущие fallback defaults readDetailSettings: extraction OpenAI/Luna, роли OpenRouter/JEV. Более поздний явный выбор администратора сохраняется. JEV использует независимый choice match/reject по разрешённым листьям; при ошибке ни он, ни выбранная Luna не заменяются бесплатной моделью. Backup37 сохраняет выбор роли,14–36 поддержаны с прежней моделью при старом OpenRouter config.


Диагностика задержки парсера: `server/services/parser-diagnostics.js` вызывается только защищённым MCP scheduler_status, группирует очередь/последние импорты и ограниченно читает существующие Apify detail runs. HTTP dashboard не вызывает этот сервис; provider GET не меняет локальный ledger. Возвращаются только структура и длины полей, не тела или ключи.

Политика Apify19.10: SCHEDULER_APIFY_POLICY задаёт priorityPenalty50, maxStagesPerCycle1 (допуск нового Actor), maxHhStagesPerCycle5 (все HH scan/fetch попытки), pollDelayMs60000. Общий mutable SCHEDULER_APIFY_ADMISSION передаётся из dispatcher во все стадии, включая fallback detail. executeApifyRun проверяет допуск до preflight и резервирует перед атомарным ledger INSERT; существующие operations не вызывают этот hook. Отказ не создаёт пустой ledger. claimScheduledJob(allowApifyStarts:false) оставляет публичный fetch и scans с ledger точной cursor page; allowApify:false закрывает всю HH acquisition lane после5попыток. Known/busy/uncertain/paidAllowed/денежные резервы не снимаются, повторного POST для lost ACK нет. Чтение порций10 может выполняться несколько раз в тик, апify_running ждёт60с. Реальные lease/scope/generation guards сохранены; политика видна в dashboard/MCP и пояснении интерфейса.

Apify proxy: hhInput useApifyProxy=true, detail operation key :proxy-v1. SQL0070 точечно requeue legacy data errors. Для bounded проверки есть namespace apify-proxy-check:<candidate>:<UUID> в scheduler-series: существующая scope-валидированная fetch_full_text job выполняется runDueTasks(jobId,limit1), без reconciliation/LLM fanout; lease и budget общие, pending poll1с только в явной проверке. Пониженный приоритет сохраняется только для новых HH scans, cap1 новых Actor на тик сохранён; готовые данные читаются в отдельном ограниченном окне. Detail сначала использует integrations/hh-public.js с точным JobPosting ID, fixed URL/8с/2MB/no auth/redirect; успешный snapshot provider=hh-public не запускает Actor. SQL0071 возвращает только старые free date failures на выбранную Luna с generation/latest-provider/valid-snapshot guards.

Список вакансий подписывает useApi('/api/vacancies/list',{refreshIntervalMs:60000}) на обновление при входе в раздел с cache старше60с, focus/visibility и минутный интервал только видимой вкладки. Успешные серверные импорты не проходят через локальный invalidateMutation; без этой подписки память браузера сохраняла снимок бессрочно. Все события объединяются с уже выполняющимся запросом, таймер/обработчики очищаются при размонтировании; прежние generation/session fences и показ сохранённых данных при ошибке сохраняются. Остальные useApi по умолчанию не опрашиваются.

scheduler-relevance.js управляет отдельным score_relevance после успешного import и bounded5 backfill импортированных NULL personal scores. Vacancy/owner/input snapshot формирует idempotency key без персональных исходников в payload. recalculateVacancyRelevance принимает внутренние onlyMissing/requireJev, ручной route их не задаёт. Scope facade сохраняет owner/company/archive/source/epoch на всём запросе; общий CAS защищает resume/prompt/model/key/manual changes. runJevRelevance использует withSchedulerBilling перед POST Decisions; этот шаг не входит в квоту HH acquisition. SQL0072 сохраняет jobs/leases/attempts при расширении type CHECK.

### Надёжность Apify HH, 18.10.2026

Рабочая пагинация apify-discovery.js использует raw rows без skipEmpty и отдельный dataset metadata GET. Cursor raw-v1 считает исходные строки, legacy clean cursor переигрывается с0 без нового Actor. Карантин хранит только ограниченные координаты/коды внутри discovery result; good rows сохраняются отдельно. Нельзя ослаблять employer/URL/ID validation. coverage.datasetComplete подтверждает чтение текущего dataset; hhComplete=true выставляется только при нормальном конце всех страниц без карантина, повторов и денежных/страничных ограничений. Это полнота обхода по сохранённому запросу, не доказательство всей выдачи работодателя вне выбранного фильтра. last_success_at обновляется только после полного сохранения complete результата; частичные предупреждения и незаконченные порции дату не меняют. Диагностика непривязанных ссылок сохраняет частичность между порциями; scheduler показывает last_result=partial и предупреждение вместо success. План дальнейшей сверки бюджета — APIFY_HH_FIX_PLAN.md.

### Бюджет и аудит Apify

`services/apify-budget.js` — единый SQL aggregate локальной экспозиции для usage и admission; account refresh использует SQL high-water mark с account/period identity. Admission дополнительно проверяет последнюю сохранённую статистику. `services/apify-budget-audit.js` читает только account/limits и официальную monthly usage, возвращает bounded numeric whitelist и не выводит покрытие по совпадению общих сумм/времени. Maintenance namespace apify-budget-check использует существующий глобальный lease/receipt, без обычной очереди. Текущее API Apify monthly usage агрегировано по service/day, без documented run watermark. Оценка max(providerUsedUsd,ownConfirmedUsd)+unknownReserveUsd+readReserveUsd устраняет двойное вычитание; неизвестная стоимость сохраняет полный cap. Это оценка, а не доказанный cutoff. Источники: https://docs.apify.com/api/v2/users-me-usage-monthly-get и https://docs.apify.com/api/v2/users-me-limits-get (проверены18.10).

High-water расхода дополнительно находится в apify_account_usage (SQL0073) вне backup/restore: два SQL statements одной batch атомарно объединяют известный usage и публикуют безопасный account cache, проверяя revision/generation. Admission и usage учитывают operational maximum, даже после restore или подключения второго профиля к тому же аккаунту.

### GET-only replay HH dataset

services/apify-listing-replay.js не вызывает executeApifyRun: network только apifyDatasetInfo/Items GET. Сначала проверяется server-owned parent UUID и account/key/profile/generation/source/current settings scope; фиксированный ledger/dataset не может породить новую страницу/Actor. Отдельный progress в существующей operational vacancy_discovery_runs (kind=apify-listing-replay) исключён из обычного resume/paused UI. Стабильный server-only creationToken в атомарном onlyNew upsert позволяет восстановить очередь после save/enqueue lost ACK; prefix range использует индекс creation_token и избегает лимита D1 LIKE. Существующие candidate data и aliases неизменны, raw snapshot только при фактической новой вставке. Classify queue ids принадлежат этому replay; model/extraction/import запускаются только штатным планировщиком. Schema0073/backup36 прежние.

Автоматический import handoff проверяет текущие зависимости и hash без построения полного плана записи. Для кандидата без mainVacancyId обычный nextCandidateStage сразу ставит import; разрешение дублей остаётся в самом импорте. Уже импортированный кандидат по-прежнему проходит prepare для обнаружения unchanged. Перед записью сохраняются обе полные проверки плана/token и атомарный SQL fence, включая модель ролей; оптимизация не сокращает защиту от конкурентных правок.


Rabota listing diagnostics.unpaired считает уникальные URL без найденного заголовка после обхода всех HTML/Markdown пар. Пустой overlay/зарплатная ссылка, для которой уже есть карточка с заголовком, не создаёт ложное partial. Реальная неоднозначность, неизвестные URL и предел извлечения сохраняют прежнюю защиту.

Задержки продолжения Apify задаёт `services/scheduler-wait.js`: busy60с, running60с, budget/uncertain/changed300с, slice/cycle_limit1с. Возвращённая пауза и исключение используют общую политику, не расходуют retry; admission/ledger/idempotency остаются прежними. Owner-only parser diagnostics содержит до20 waitingJobs с checkedAt/runAfter/reason без payload/текстов/секретов.

Ограниченное восстановление исторических import failures включает roles_unresolved/roles_stale: актуальность проверяется через nextCandidateStage и текущие snapshot/prompt/config/tree hashes. Устаревший ответ получает ровно одно deduplicated задание нужного этапа; текущий пустой ответ записывается в payload.dependencyRecovery как filtered/roles_no_match с inputHash и resultId, без модели/импорта. Исторический failure сохраняется. До3 записей за тик и8с подготовки, scope/epoch guards прежние. Owner diagnostics.roleRecovery выводит до20 связей восстановления и последнюю модель/роль без исходных текстов.

Если актуальное dependency задание уже терминально, recovery хранит blocked/jobId/reason вместо бесконечного повторного попадания старой ошибки в первые3строки. Самостоятельно не перезапускает исчерпанное платное задание.

Owner MCP candidate-step:<candidateId>:<UUID> выполняет максимум один актуальный pending этап через обычный dispatcher jobId/limit1. Не сбрасывает attempts/run_after/failed, не задаёт роли/модели и не запускает второй Actor. Повтор requestId читает receipt. Отсев/импорт возвращает явное terminal decision без provider вызова; exhausted dependency возвращает blocked. Все source/favorite/generation/budget gates прежние.

После хотя бы одного обычного этапа диспетчер может перепроверить один HH scan с прежней причиной apify_busy/apify_budget, просроченный минимум5мин. Это отдельная ограниченная выборка перед потоком title-only работ, только при отсутствии due import/extraction/roles/fulltext/relevance, свободных admission/HH лимитах и времени>=30с. Общий штраф50 и максимум1новый Actor/5HHопераций сохраняются; failed/будущие/выключенные задания не оживляются.

Перед normal reconciliation только pending scan_source/hh с legacy busy/budget run_after>updated_at+новая задержка получают сокращённый run_after. updated_at не меняется, поэтому waitingJobs.checkedAt остаётся последней фактической проверкой. История/attempts/paid identity сохранены, provider вызывается только обычным claim.

readVacancyParsingSources считает exact Sber bank feed несовместимым с компанией СберТех. Это runtime продолжение защитного правила0076; scheduleDueSources отбрасывает unsupported до auto-enable/INSERT. Регрессия проверяет, что disabled row/revision/last_error остаётся побайтно прежним после общего планирования; корректный Сбер и иные URL не блокируются.

### Публичный каталог ЮMoney

`services/yoomoney-public-listing.js` читает только jobs.yoomoney.ru и фиксированный публичный store.tildaapi.com/api/getproductslist/. Конфигурация t_store_init разбирается как текст без eval, единственный storepart/recid/size подтверждается страницей. Курсор source/store/catalog хранится в существующем discovery result вместе с provider=yoomoney-public, slice/total/fingerprints. Последовательность порций и неизменность total обязательны; собственные slug URL, product UID/title/buttonlink проверяются, чужие/непарные карточки не импортируются. До2MB/ответ, DNS/HEAD preflight, timeout, ручные redirects, один GET/порцию, без прокси и повторов при блокировке. Это публичное чтение без Firecrawl usage session, с creditsReported/Estimate=0.

`vacancy-discovery.js` сохраняет прежние source/favorite/epoch/lease fences и порционное сохранение до10jobs. Новые стандартные обходы используют публичный каталог; прежний resume.provider=firecrawl и manual selectors не переводятся на другой транспорт. Все порции плюс число уникальных jobs=total необходимы для complete. Источник карточки — собственный listingUrl, не API каталога. Повтор полностью сохранённого run не меняет last_success_at; fullSavedAt фиксирует единственное полное сохранение, legacy fully-saved receipts используют исходный savedAt без записи новой свежести. Неполный public-обход с пригодными jobs назначает следующую проверку по обычному интервалу.

### Проверка разметки источника

Owner MCP requestId `source-page-check:<companyId>:<companySite|getmatch>:<inspect|page|next-page>:<UUID>` работает через diagnostic receipt/общий scheduler lease. inspect читает текущие настройки, nextRunAt/последнюю попытку/исход адаптера в БД и три последних scan jobs. page загружает только сохранённый URL одной basic Scrape (HTML+Markdown). next-page допускает ровно одну ссылку продолжения из последнего page receipt той же generation/company/source URL/revision, при configured.ok=true; только другой URL того же origin. Нет fallback к более старой странице, произвольного URL, автоматического fanout, LLM/preview/candidate/main writes или обновления свежести. Проверяет selected/enabled/supported, личное избранное, source revision/URL, generation, Firecrawl lease/credits/rate; redirects/robots/DNS/allowedHosts остаются штатными. Receipt повторяется без второго Scrape. Ответ сравнивает configured mode с HTML, ограничивает пары8/Markdown12000/anchors12×1200/публичные pagination snippets12×680/store scripts4×4000; пустая JS/login оболочка не считается emptyConfirmed. Изменение источника в полёте освобождает глобальный Firecrawl lease через родительский facade; неопределённый денежный reserve не снимается.

### Уровень и исходный текст при структурировании

`vacancy-extraction-prompt.js` содержит стандарт и точный legacy-текст для безопасного выравнивания. SQL0077 пересоздаёт только settings-таблицу с CHECK10000, сохраняет модели/редакции, заменяет exact legacy revision0 и повышает prompt_revision. Runtime read повторяет идемпотентный UPDATE по тому же предикату; custom edits не заменяются. API/Unicode/body limits соответствуют10000/128KiB, роль/relevance сохраняют прежний лимит5000. JSON Schema2 использует общий VACANCY_LEVELS enum без null; cached extraction учитывает schema_version. В уровне разрешена оценка по содержанию с консервативной базовой категорией Middle, в description_md запрещено перефразирование. Старые тексты/результаты/основные карточки не переписываются миграцией. Backup37 совместим по прежним колонкам; доверенный restore DDL допускает10000.
