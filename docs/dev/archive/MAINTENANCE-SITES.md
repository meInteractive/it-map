# Инструкция по обновлению документации

Актуально на 23.10.2026, редакция 224. Этот файл — инструкция следующему разработчику или нейросети. Актуальные контракты: API22.10.2026 без /v1 в URL, SQL0083/84 миграции, backup39 (полные14–39), рейтинг5, Excel12.10.2026. Исторические этапы и первоначальные планы перенесены в роль архивных отчётов; не трактуйте старое «ещё не подключено» как текущий код.

## Быстрый порядок работы

1. Прочитайте AGENTS.md, docs/user/manifest.json и начало/последнюю запись docs/refactoring-progress.md; проверьте git status/branch/log. Работайте в refactor/react-typescript. Сначала установите фактическое состояние по коду, не только по отчёту.
2. Разбейте изменение на 2–3 небольших шага. Найдите затронутые маршруты через rg в server/routes/index.js, сервис/SQL repository, frontend и shared/contracts.ts. Для локального поиска исключайте server/generated и dist: там большие сгенерированные строки.
3. Обновите соответствующий основной раздел, а не добавляйте в конец опровергающую старый абзац заметку. Исторические CHANGELOG/аудиты/пользовательские ТЗ не переписывайте как новые факты; добавьте ясную текущую отметку/дату.
4. Обновите revision/updatedAt docs/user/manifest.json и CHANGELOG.md: дата, поведение, данные, актуальные версии. API/БД/backup/rating/Excel независимы от номера публикации Sites. Для чистого документационного изменения не повышайте версии форматов.
5. Выполните нужные проверки, prepare-server/build, проверьте diff и согласованность generated артефактов. После законченного шага commit + push + подтверждение remote SHA. Промежуточный checkpoint не требует production-публикации.
6. Для готового изменения выполните штатный Sites workflow: exact source push, архив из этого же состояния, одна saved version и deployment до terminal status. Сохраняйте project_id/audience/bindings/env; текущая задача пользователя определяет публикацию. Report-only фиксацию после успешного deploy повторно не собирайте/не публикуйте.

Не храните токены Git, пароли, API-ключи или рабочие личные данные в документах/коммитах. Краткоживущий credential передавайте helper только через скрытый stdin. Не обращайтесь к рабочей D1 ради обычной генерации документации: SQL-справочник строится из изолированных миграций.

## Что и где обновлять

| Изменение | Документ и источник |
| --- | --- |
| Действия кандидата/формы/письма | USER_GUIDE.md; frontend/features и реальные API |
| Админские операции/права/ключи | ADMIN_GUIDE.md; Settings/Channels, маршруты auth/role |
| HTTP поля, ошибки, CAS/intent/stream | API.md; server/routes/index.js, services/repositories, shared/contracts.ts |
| Новая сущность/поле/индекс/миграция | DATABASE.md; drizzle/*.sql, db/schema.ts, scripts/documentation.mjs descriptions |
| Excel формат, aliases, пустые поля | TRANSFER_FORMATS.md; frontend/features/settings/xlsx.ts, server/import-export/{catalog,xlsx}.js, repositories/vacancies.js |
| JSON backup/совместимость/операционные данные | BACKUP_FORMAT.md + TRANSFER_FORMATS.md; server/backup/index.js, constants.js, scripts/backup-schema.mjs |
| Архитектура/границы/локальный запуск | CODE_GUIDE.md, README.md, AGENTS.md; package/vite/scripts/build entry |
| AI модели/лимиты/цены | ADMIN_GUIDE/API/CODE; integrations/openai*.js/openrouter.js, settings/PromptModel.tsx/openaiCost.ts |
| Любое завершённое изменение | CHANGELOG.md, manifest.json и refactoring-progress.md |

В приложении доступны 8 разделов из manifest. BACKUP_FORMAT.md — исходник встроенной спецификации, не девятая вкладка. Отчёты аудита/планов — отдельные файлы репозитория, не текущая API-спецификация.

## Генерация: один источник для автоматических таблиц

`node scripts/prepare-server.mjs` и `npm run build` вызывают buildDocumentation. Генератор сверяет версии manifest с BACKUP_VERSION/RATING_FORMULA_VERSION и последней SQL-миграцией; несовпадение останавливает сборку.

| Автоблок | Источник |
| --- | --- |
| DATABASE_TABLES | Все SQL0000–0083 в SQLite памяти: поля/defaults/FK/DDL/индексы + descriptions в documentation.mjs |
| COMPANY_FIELDS | Заголовки server/import-export/xlsx.js, FieldMap браузерного xlsx.ts и инструкции колонок |
| VACANCY_FIELDS | VACANCY_EXPORT_HEADERS + карта 19 ключей в documentation.mjs |
| NARROW_EXPORTS | Заголовки выгрузок источников вакансий/отзывов |
| BACKUP_DETAILS | docs/user/BACKUP_FORMAT.md |

Не редактируйте текст между AUTO-маркерами: сборка перезапишет его. Меняйте исходник/описание и пересобирайте. После изменения состава/порядка столбцов вакансий обновите и карту vacancyKeys в генераторе; не считайте её автоматически выведенной из SQL. При новой таблице добавьте содержательное descriptions, проверьте прикладная она или операционная. server/generated/*.json — результат генерации; не самостоятельное хранилище документации или живых данных.

## Проверки по области изменения

```bash
node scripts/test-documentation.mjs
npm run typecheck
npm run build
npm run validate
```

Первый тест сам вызывает prepare-server: проверяет 8 разделов, admin-only API/скачивание, read-only методы, отсутствие DB-запросов, безопасный Markdown и все SQL-поля. Build проверяет фактическую упаковку; validate — границы клиента/секреты, неизменность старых миграций, типы и полный runner. Используйте релевантные проверки; не повторяйте весь gate без причины после изменения только отчёта. На 09.10 runner содержит 58 test-*.mjs suites; test-react включает 16 сценариев. Это не число отдельных assertions. Тесты коммитятся в scripts/, фикстуры/логи .tests/ не коммитятся.

| Область | Существующие регрессии |
| --- | --- |
| Компактные проекции/загрузка | test-catalog-loading, test-vacancy-loading, test-react |
| Scope/кэш/параллельная генерация | test-vacancy-letter-preservation, test-letter-cache, test-cover-letter.mjs, test-cover-letter-concurrency.mjs, test-cover-letter-deduplication.mjs, test-cover-letter-recovery.mjs |
| AI/restore и ключи/тарифы | test-ai-restore-barrier, test-generation-bound-db, test-runtime-generation, test-openai, test-openrouter, test-react |
| Отклики: CAS/создание/потерянный ACK | test-application-concurrency/editing/creation-http/lost-response, test-vacancy-apply-http/intent/retry |
| Транзакции вакансии/вилки | test-vacancy-atomicity/concurrency, test-agency-salary-atomicity |
| Форматы/restore | test-transfer-formats/help, test-backup-roundtrip, test-backup-miniflare |
| UI/карта/упаковка | test-client-parity, test-map-selection, test-sites-packaging, test-react |

Полный validate использует historical git show и требует полную историю; ZIP исходников без .git или shallow checkout недостаточен. Native D1/workerd тесты требуют Miniflare из установленного lockfile. Не используйте платный AI или рабочие данные для обычных синтетических проверок.

При managed Linux browser QA разрешена через штатный control-browser skill. Если он недоступен, не запускайте preview и не подменяйте инструмент: выполните доступные HTTP/component/build проверки и честно сохраните ограничение. Статическая проверка DOM/CSS не подтверждает pixel parity. Ошибка managed-профиля устраняется настройкой ignored execution profile штатным helper, не изменением source/lockfile.

## Инварианты, которые легко нарушить

- Личная информация определяется сессией; промты/модели/ключи/docs только админские. effective письмо со scope нельзя автоматически записать в shared поля.
- Резюме 20000; cover_letter prompt15000; relevance prompt5000; сохранённое письмо subject512/text2000; описание вакансии 100000. Unicode-символы, не байты/UTF-16 length. Сценарии стоимости: письмо20k/5kchars, релевантность15kchars/одно число; они не меняют лимиты сохранённых данных.
- Revision форм — frozen SHA-256 snapshot. Не меняйте её при обновлении props. Полный PUT отклика сохраняет все поля; busy/delete/reload не стирают соседние черновики.
- Новый отклик/apply сохраняет UUID+creationContext до конца намерения; replay не меняет существующие поля. Не создавать новый context после lost ACK/restore автоматически.
- app_runtime_generation/receipt/lease не входят в storage бэкапа. Restore меняет epoch атомарно. Не заменять SQL fencing двумя отдельными SELECT/write.
- Normal letter GET/POST использует saved; regenerate явный, personal override и revision. Snapshot, epoch, owner guard и прошлый текст до успеха обязательны. OpenRouter next по рейтингу; OpenAI без fallback выбранной модели.
- Списки используют компактные API; полные карточки/вкладки lazy. Не включать DATA.companies, seed, SQL, Markdown docs или секреты в client/SSR. Локальные фильтры не вызывают новый HTTP на каждое нажатие.
- Канал add/delete только List→Channels/admin. SQL запрещает последний источник канальной вакансии. rabota.ru переносится JSON/полным Excel/выгрузкой источников; старые файлы без поля совместимы.
- Native select.settings-select, file selector и общие кнопки/Markdown классы должны сохранять label/focus/mobile/disabled. Отдельный PanelBoundary компании слева предотвращает прыжок панели при lazy-загрузке.

Промт релевантности описывает семантику входов, без формата генерации. JEV получает score/10 criteria и серверный перевод round(score×100/9); текстовые модели получают отдельное JSON требование. SQL0066 и runtime alignment меняют только наблюдённую revision1/date2026-10-16 19:51:15/prefix, дальнейшие админские изменения не переписываются. Исходник новой редакции и SQL: services/relevance-prompt.js; полная прежняя запись не извлекалась из усечённого DB preview.

## Цены и внешние сведения

openai-pricing.js хранит проверенную дату/source и Standard USD1Minput/output. Для неизвестного ID/snapshot/fine-tune возвращает null. /v1/models даёт доступность, не цены/capabilities; refresh не обновляет справочник. При изменении тарифов сначала проверьте официальные источники, затем обновите дату/цену и соответствующие тесты/документы. Не выводите расход Admin Costs API как остаток счёта.

Курс85₽/USD и объём1000 фиксированы по просьбе пользователя; сценарии20k/5kchars для письма и15kchars/одно число для релевантности задаются в openaiCost.ts. Не меняйте их автоматически из внешнего курса. JEV получает prompt/completion/request из живого каталога через admin-only GET без inference; запрос каталога обязан явно включать `output_modalities=decisions`, потому что OpenRouter по умолчанию возвращает только text-модели. Не кэшируйте тариф как постоянный факт. Размышления и служебный формат могут увеличить output billing, точное количество неизвестно. Укажите изменение цены отдельно от изменения логики модели.

Фильтр компаний парсера: firecrawl_settings.favorites_only default1 (SQL0060). parser-company-scope.js применяет личный favorite admin как guard к операциям сбора, но не к завершению операционного usage/lease: расход и освобождение слота должны фиксироваться и при снятии favorite. Старые backup14–33 получают1. Уже сохранённые кандидаты обрабатываются отдельными стадиями.

Вёрстка Парсера изолирована .parser-settings в frontend/styles/parser.css; компонент Classification.tsx подключает файл. Отступы карточек/форм/результатов задаются gap, без изменения глобального ActionForm или CSS других вкладок. При добавлении блоков проверьте соседние формы, раскрытые details, длинные кнопки/URL и узкие экраны.

## Журнал и передача следующей сессии

Каждая запись CHANGELOG: дата, добавленная/изменённая функциональность, затронутые сущности, API/schema/backup/rating/Excel, проверки и материальные ограничения. Исторические версии остаются историческими. В progress укажите завершённый шаг, checkpoint SHA, следующий шаг; после deploy — точный source/version/deployment/status/URL. Номер Sites не версия API/БД.

README и CODE_GUIDE содержат актуальный локальный запуск. REFACTOR_REQUEST/REFACTOR_SESSION_RULES сохраняют исходные пользовательские промты; текущая отметка отделяет их от позднейших разрешений публикации. Не начинайте заново уже закрытый этап по старой архивной записи. Если актуальный код расходится с текстом — выясните факт и исправьте основной абзац, не добавляйте ещё одно противоречие в конец.

## Полные тексты, задача6

Настройки → Парсер → Обработка полных текстов. Детали и ограничения описаны в VACANCY_DETAILS_DELIVERY.md. API /api/vacancy-details, /api/vacancy-details/:id, /api/vacancy-details/jobs и jobs/:uuid/(resume|run). Только администратор, Origin и ограниченный JSON. Большие исходники хранятся по частям; вход LLM свыше 120000 символов не усекается и остаётся ожидающим с причиной.

## Безопасный импорт, задача7

Конечные роли: vacancy-leaf-roles.js; main import: vacancy-import.js; preview/confirmation/durable packet/API: vacancy-import-batches.js; UI: VacancyImport.tsx. SQL0059 добавляет sidecars, шесть AFTER UPDATE защитных триггеров и операционный write context. Не обходите guard-transaction и не используйте createVacancy для этого контура: там иной пользовательский workflow. Обычные ручные/Excel изменения сохраняют прежние функции; триггеры помечают изменившиеся поля, составной FK снимает автоматические роли после ручного переназначения. Политика консервативная; восстановление32 обнуляет provenance. Пакетный review hash допускает только изменения, эквивалентные просмотренным, при новом дубле требуется re-preview. Полный отчёт — SAFE_VACANCY_IMPORT_DELIVERY.md.


Scheduler: см. SCHEDULER_DELIVERY.md и исходный SCHEDULER_REQUEST.md. SCHEDULER_TOKEN — runtime secret только для защищённого service HTTP, не включать в source/prompt/клиент. MCP owner binding выдаётся исключительно admin+Sites identity. Cloud task не активировать до проверки unattended доступа; task creation не доказывает выполнение.

Глобальное расписание парсера (16.10): scheduler_json хранит sourceIds и intervalDays; favorites_only остаётся общей личной областью. Не возвращать индивидуальные opt-in компаний. Изменение favorites через Firecrawl синхронизирует scheduler revision; изменение через scheduler синхронизирует Firecrawl revision. Выбранный тип источника автоматически применяется к новым компаниям/ссылкам.

Автоматический конвейер: ready backlog обрабатывается до reconciliation. Подготовка пустой очереди ограничена12с и максимум5источниками, с резервом35с на actual stage. Постановка classification для найденных IDs пакетная; запрещено возвращать serial getCandidate/roles на каждый найденный заголовок. ensureCompanyIdentities читает готовые IDs без no-op INSERT для всех компаний на каждом этапе.


Структурирование/конечные роли: structured-model-health.js использует существующий openrouter_model_metrics с model_id structured:task:generation:keyRevision:modelId. Это отдельные latency/error счётчики, не метрики писем. Последний сбой получает15мин пониженного приоритета, минимум2 успешных замера дают среднюю задержку для ранжирования. Если все охлаждаются, выбирается самый давно ошибавшийся допустимый вариант; если альтернатив нет, остаётся единственный. Запрос автоматического structured этапа ограничен90с с общим окном110с, maxAttempts1 за этап. Invalid output/exhausted unavailable повторяется только ограниченным queue retry; прежние failed не сбрасываются. Free max_price0/require_parameters и выбранный OpenAI сохраняются. Новые поля/таблицы не нужны, backup35 прежний; новый restore epoch/key revision изолирует прежнюю health.


Apify: apify_settings входит в backup37, apify_runs операционный несбрасываемый ledger без токенов. Старые14–35 сохраняют текущее подключение, после любого restore enabled/paid_allowed=0, billing cache очищается; epoch защищает старые writes. При утрате ACK POST повтор запрещён, резерв сохраняется. Проверенные источники17.10: https://apify.com/pricing и https://apify.com/shahidirfan/hh-ru-job-scraper, docs.apify.com/api/v2/actors-runs-post, users-me-get, users-me-limits-get. Запросы только фиксированному Actor, Bearer только в header, redirect:manual с отказом на3xx. Не давать browser произвольный Actor/input/API URL.


Приоритет HH (17.10): одинаковые публикации разных источников связываются с одной основной вакансией, если есть единственное совпадение компании, нормализованного названия и полного содержательного описания (от250символов после нормализации Markdown/пробелов). Используется также неизменяемый сохранённый вариант предыдущей публикации: ручная правка основной карточки не ломает идентификацию. Одного названия недостаточно; неоднозначность и разные HH vacancy IDs остаются для ручного решения. HH URL, автоматические поля и роли приоритетны при любом порядке поступления; остальные публикации сохраняются дополнительно, поздний не-HH не заменяет HH. Ручные поля/URL/роли, отклики и персональные значения защищены. SQL0068 добавляет URL provenance/trigger для ручной правки; backup37 прежний. Уже существующие две основные карточки автоматически не сливаются.


Apify production safeguards: Cloudflare-compatible redirect:manual rejects3xx without following URLs or forwarding the token. DOM TimeoutError/AbortError is mapped to string timeout, never numeric legacy23. An Actor ending at the per-run monetary cap with fewer than a full100-item source page remains partial/error (apify_run_limit), even if Apify reports SUCCEEDED; it must not advance next_run_at as a complete source. HH IDs in valid vacancy URLs are stable across hh.ru subdomains and independently checked before text-based duplicate matching.

HH title prefilter: hh-title-filter.js builds one broad NAME:(… OR …) for active standard root stable keys, not from LLM-generated criteria or a sector. New Apify listing cursors persist titleFilter before paid start; all offsets/pages reuse it even when prompts/toggles change. Legacy cursors remain unfiltered until finished; no operation-key changes/relaunch. Unknown active root falls back to unfiltered to avoid silently discarding its vacancies. Detail URLs and Firecrawl sources never receive this filter. Existing configured search text is combined with AND; employer/area/sort remain scoped. Safe INPUT/log evidence exposes preset identity/nameOnlyQuery, never raw source query. Owner hh-filter-check executes one normal scan stage, stable dedupe, normal admission1/cap/budgets/leases. Tests test-hh-title-filter + Apify legacy/native D1/evidence.

По указанию17.10.2026 SQL0069 явно устанавливает OpenAI/gpt-6-luna для структурирования и конечных ролей, сохраняет промты и тексты пользовательских промтов; отсутствие настроек тоже означает Luna, никогда не неявный OpenRouter. JEV для классификации/релевантности прежний. Технические failed старой free модели возобновляются только в текущем поколении и только если последняя попытка действительно OpenRouter/:free.

HH detail: integrations/hh-public.js читает только фиксированный публичный HH URL, точный JobPosting.identifier, без redirect/auth/cookies, максимум8с/2MB. Provider hh-public в diagnostics не означает расход Apify. Не снимать существующие scope/generation/lease/semantic проверки. Low-priority penalty50 только scan_source, общий acquisition cap1 остаётся. SQL0071 выборочно возвращает date-invalid_output старого :free при текущей Luna и валидном snapshot; история попыток не удаляется.

EXTRACTION_SCHEMA.published_at явно задаёт YYYY-MM-DD pattern и unknown=null: сервер требует этот формат и календарную/source-evidence проверку с самого schema1. Изменение лишь описывает уже существовавший контракт модели, не инвалидирует все совместимые успешные extraction caches. Не ослаблять validator или подставлять дату загрузки. Поддержка string pattern проверена по https://developers.openai.com/api/docs/guides/structured-outputs (17.10.2026).

Автоматическая relevance: scheduler-relevance.js ставит score_relevance после импорта и порционно восстанавливает пропуски перед ready queue. requireJev/onlyMissing сохраняют выбранную модель и существующий0. Inputs/старыйscore сравниваются атомарно общим vacancy-relevance, дополнительный guard owner/company/archive/source/epoch. jev.js использует withSchedulerBilling для score и choice; не обходить budget при relevance. Операционный CHECK расширен только append-only0072, история jobs/attempts/расходы сохраняются; новых прикладных таблиц нет, backup37.
