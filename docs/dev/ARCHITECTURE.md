# Архитектура IT Map

> **Срез кода: 19 ноября 2026 года**, `main` [`2e670273`](https://github.com/meInteractive/it-map/commit/2e67027363534b54b16295fe541dfdb4b968e693) (после merge [PR #496](https://github.com/meInteractive/it-map/pull/496)). Документ описывает **фактически подключённый код** и его границы, но не удостоверяет установленный на сервере SHA или полную production-приёмку. Прежде чем использовать документ для развёртывания, сверить текущий `main`, [AGENTS.md](../../AGENTS.md), [задачи](../../tasks/README.md), [handoff](handoff/README.md) и CI.

## 1. Текущая модель: два основных Go-процесса

IT Map — приложение для работы с компаниями и агентствами, картой, вакансиями, откликами, контактами, задачами и резюме, с конвейером сбора и ИИ-обработки вакансий.

Приложение построено как **модульный Go-монолит с разделёнными runtime-процессами**, общими пакетами и MariaDB. Это не микросервисная система и не один процесс, в котором HTTP автоматически обрабатывает очередь.

| Исполняемый процесс / интерфейс | Код | Назначение и текущее ограничение |
| --- | --- | --- |
| **Go HTTP API + React SPA** | [`cmd/itmap/main.go`](../../cmd/itmap/main.go), [`internal/httpapi/`](../../internal/httpapi/), [`frontend/`](../../frontend/) | Авторизация, бизнес-операции, административные отчёты, отдача Vite-сборки; SQL/HTTP handlers подключены через `httpapi.Options`. Сам HTTP-процесс **не запускает** непрерывный Worker. |
| **Постоянный Go Worker** | [`cmd/itmap-worker/main.go`](../../cmd/itmap-worker/main.go), [`internal/workerengine/`](../../internal/workerengine/), [`internal/workerloop/`](../../internal/workerloop/) | Непрерывный последовательный обход трёх логических профилей обработки, claim заданий и обращение к провайдерам. Отдельная конфигурация, служба, binary и условия запуска. |
| **Ограниченная recovery-команда** | [`cmd/itmap-recovery/main.go`](../../cmd/itmap-recovery/main.go) | Отдельные `inspect` и явно разрешённый `apply` для известных операций Apify; **не** фоновый consumer и не универсальная автоматическая «починка» расходов. |
| **Staging/операторские команды** | [`cmd/itmap-parser-staging/`](../../cmd/itmap-parser-staging/), [`cmd/itmap-schema-audit/`](../../cmd/itmap-schema-audit/), `itmap schema-init` | Изолированная проверка worker и схемы; не являются постоянными production-исполнителями. |
| **Legacy-код для сверки** | [`server/`](../../server/), [`build/`](../../build/), [`db/schema.ts`](../../db/schema.ts) | Исторические JavaScript/Cloudflare/D1 API и контракты. Не запускать их как второй backend для новых Go-маршрутов. D1 и MariaDB не синхронизируются автоматически. |

Фронтенду не требуется постоянно работающий Node.js: сборка производится Vite, статические файлы раздаёт Go API. Общие библиотеки `internal/` не означают, что HTTP и Worker — один OS-процесс.

### Системная схема

```text
 Браузер / PWA
 React + TypeScript + Vite
         |
         | HTTPS, cookie/session, JSON
         v
 Reverse proxy
         |
         v
 +-----------------------------+
 | cmd/itmap                   |
 | net/http + httpapi.Options  |
 | CRM / вакансии / отклики    |---------> Google Calendar API
 | задачи / письма / диагностика|          (ручной sync по OAuth)
 +-----------------------------+
          | SQL / database/sql
          |
          v
 +-----------------------------------------------+
 | MariaDB 10.11+                                |
 | каталог, CRM, задачи, вакансии, профили       |
 | scheduler_jobs / runs / attempts / leases     |
 | HH cursors, Apify ledger, AI evidence         |
 | настройки, поколения, backup/restore          |
 +-----------------------------------------------+
          ^
          | SQL claim/settlement/diagnostic reads
          |
 +-----------------------------+
 | cmd/itmap-worker            |
 | workerengine + workerloop   |
 | launch → reconcile → pipeline                 |
 +-----------------------------+
          |             |
          v             v
     HH.ru / Apify   OpenRouter / OpenAI
     (listing/GET)   (классификация/извлечение)
```

Стрелки `launch → reconcile → pipeline` обозначают **логический порядок стадий**, а не три независимые RabbitMQ-очереди или обязательную последовательность каждого job. API и Worker координируются через долговечное состояние MariaDB; прямого внутреннего HTTP-вызова API → Worker для каждого задания нет. Публичные чтения HH, платные Apify/AI и отдельные пользовательские операции имеют собственные ограничения.

## 2. Runtime Worker: очередь, профили, жизненный цикл

Точка входа — [`cmd/itmap-worker/main.go`](../../cmd/itmap-worker/main.go). Он открывает проверенную MariaDB, создаёт `parser.Worker`, запускает [`workerengine.Engine`](../../internal/workerengine/engine.go) и отдельный loopback HTTP monitor. Старт **fail-closed**: нужны `ITMAP_WORKER_ENABLED=1`, `ITMAP_PARSER_EXECUTION_ENABLED=1` и подходящая среда; для production-профиля дополнительно `ITMAP_WORKER_PRODUCTION_ENABLED=1` и включённый health endpoint. Не копировать реальные значения секретов в репозиторий.

[`workerloop.RunProfilesWithGrace`](../../internal/workerloop/profiles.go) выполняет **по одному ограниченному шагу** в каждый момент; round-robin обходит доступные профили с раздельными интервалами, jitter и backoff при ошибках. Успех одной стадии не требует ждать следующего часового запуска HTTP-таймера.

| Логический профиль | Что делает | Особенности |
| --- | --- | --- |
| `launch` | Планирует подходящие HH-источники, принимает `scan_source` для нового Actor | Применяет проверку условий и cooldown платного старта; отсутствие готовых заданий не исключает проверку сроков источников/leases. |
| `reconcile` | Дочитывает **уже известный** Apify Actor/Dataset и продолжает сохранённый HH scan | Предпочитает безопасные GET по подтверждённому Run ID; неизвестный прежний POST не считается разрешением сделать новый. |
| `pipeline` | Продвигает `classify_title`, `fetch_full_text`, `extract_vacancy`, `assign_roles`, `import_vacancy`, `score_relevance` | Не запускает новый Actor вместо обработки кандидата. Учитывает deadline до нового claim и оставляет время для завершения/записи результата. |

Реализация профилей — [`workerengine/engine.go`](../../internal/workerengine/engine.go); набор стадий и диспетчер — [`parser/worker.go`](../../internal/parser/worker.go), [`worker_dispatch.go`](../../internal/parser/worker_dispatch.go). Используется **один долговечный SQL scheduler**, а профили — способы выбирать и обрабатывать разные классы заданий. `MaxJobs`, интервалы ожидания и `ShutdownGrace` настраиваются в [`workerconfig`](../../internal/workerconfig/); эти интервалы не равны лимиту расходов провайдера и не гарантируют SLA.

### Защита от сбоев

- `scheduler_runs`, `scheduler_jobs`, `scheduler_attempts`, leases, `generation` и durable provider evidence сохраняются в MariaDB. Claim и изменение состояния должны подтверждать актуальные сроки и владение; SQL-снимок готовности **не** заменяет атомарный claim.
- Worker перепроверяет `generation`; после restore или смены поколения прекращает старый цикл, а не продолжает обработку под прежним состоянием.
- При SIGTERM/отмене новые claims и **новый платный AI dispatch** прекращаются на нескольких границах: до/после claim, до reserve и после него при проверяемой отмене. Начатому запросу оставляется ограниченное graceful-окно для settlement и сохранения результата. Это проверено синтетическими сценариями, но не отменяет неопределённости после SIGKILL или lost ACK. См. [GO-431: stop-before-paid-dispatch](GO-431-STOP-BEFORE-PAID-DISPATCH.md), [post-reserve cancellation](GO-431-POST-RESERVE-CANCEL.md) и [PR #495](https://github.com/meInteractive/it-map/pull/495).
- [`consumercontrol/`](../../internal/consumercontrol/) и [`BoundRun/BoundAttempt`](../../internal/parser/bound_attempt.go) содержат отдельно проверяемые **candidate/guarded** контракты. Их нельзя описывать как полностью подключённый distributed fence для всех production-исполнителей: часть guarded API **не включена в обычный runtime**. Поэтому исключение старого HTTP-consumer и проверка единственности Worker остаются частью операторского переключения.
- **Неопределённый исход платного POST не равен нулевой стоимости**. `reserved_usd`, `cost_usd`, подтверждённый ответ, факт списания и импорт вакансии — разные факты. Устаревшую или противоречивую операцию нельзя сбрасывать/повторять без durable доказательства. Отдельный открытый архитектурный риск — межкандидатная устойчивая identity платной AI-операции: [дизайн](../../tasks/GO-431-DURABLE-AI-OPERATION-DESIGN.md), [Issue #489](https://github.com/meInteractive/it-map/issues/489).

### Наблюдаемость

В Worker имеется **отдельный loopback monitor** (не API-маршруты `/health/*`): `/healthz`, `/readyz`, `/profilez`, `/queuez`, `/recoveryz`, `/hh-schedulez`, `/activityz`. Код: [`workerengine/monitor.go`](../../internal/workerengine/monitor.go) и read-only readers. Эти endpoints показывают локальные исходы профилей, очередь, recovery/unknown ledger, HH и активность; `ready` не доказывает, что внешний провайдер доступен, все вакансии импортированы и расходы полностью атрибутированы. Операторский справочник: [WORKER-RECOVERY-DIAGNOSTICS](../../deploy/WORKER-RECOVERY-DIAGNOSTICS.md), [WORKER-ACTIVITY-DIAGNOSTICS](../../deploy/WORKER-ACTIVITY-DIAGNOSTICS.md).

## 3. Поток вакансии и HH-расписание

```text
Избранные HH-источники и настройки ролей
   |
   v
scan_source → создание/возобновление известного Actor → Dataset
   |
   v
candidate intake / фильтры города, HH-ролей и исходных ID
   |
   v
classify_title → fetch_full_text → extract_vacancy → assign_roles
   |
   v
import_vacancy (create / update / unchanged; дедупликация)
   |
   v
score_relevance
   |
   v
основной каталог вакансий → API → React
```

Это **основной реализованный Go-путь для HH**, но не обещание миграции всех legacy-парсеров. Некоторые кандидаты отклоняются, ожидают внешний ресурс, прерываются либо переходят на другие разрешённые ветки. Исходный фильтр периода задаётся Apify, но после Dataset вакансия не должна отбрасываться только из-за отсутствующей/нестандартной `date_posted` ([PR #251](https://github.com/meInteractive/it-map/pull/251)). Для новых платных выборок учитываются Москва, настройки HH `professional_role` по направлениям и безопасная проверка источника.

Запуск Actor, reconcile уже известного Run и AI/импорт не привязаны к одному часовому HTTP-циклу (см. [GO-430](../../tasks/SPRINT-01-PARSER-APIFY.md)). Задания с неподтверждёнными историческими фильтрами и непроверенным платным происхождением остаются **заблокированными для автоматического повтора**. Диагностика `nextDueAt` описывает план, а не подтверждённый будущий платный POST. Исторические пробелы интервала «раз в 7 дней» при трёхдневном окне — [GO-293](HH-HISTORICAL-COVERAGE-AUDIT.md): модель разрыва сама по себе не доказывает число пропущенных вакансий.

## 4. Прикладные модули и внешние интеграции

| Область | Точка входа / код | Состояние на срез |
| --- | --- | --- |
| HTTP, сессии, защищённые маршруты | [`httpapi/handler.go`](../../internal/httpapi/handler.go), [`auth/`](../../internal/auth/) | Go `net/http` / `ServeMux`; маршруты каталога, вакансий, CRM, задач, настроек, парсера и backup. Не перенесённые маршруты могут возвращать `503 backend_not_migrated`. |
| Каталог, карта, личные признаки | [`catalog/`](../../internal/catalog/), [`crm/`](../../internal/crm/), [`frontend/`](../../frontend/) | Каталог/поиск/карта, личное состояние. Отдельный фильтр компаний **«Избранные / Не избранные / Все»** для списка и карты ([PR #479](https://github.com/meInteractive/it-map/pull/479)); требует независимой продуктовой приёмки. |
| Вакансии, отклики, задачи, контакты | [`vacancies/`](../../internal/vacancies/), [`applications/`](../../internal/applications/), [`tasks/`](../../internal/tasks/), [`contacts/`](../../internal/contacts/) | MariaDB/HTTP CRUD, CAS, receipts и защита конкурентных изменений. |
| Настройки направлений и промтов | [`classification/`](../../internal/classification/), [`classification_settings_routes.go`](../../internal/httpapi/classification_settings_routes.go) | Административные Go GET/PUT, корневые роли, Markdown и revision/CAS (GO-438). |
| ИИ в парсере | [`aiproviders/`](../../internal/aiproviders/), [`parser/ai_stages.go`](../../internal/parser/ai_stages.go) | OpenRouter/JEV и OpenAI по сохранённым настройкам, учёт попыток/резервов, fail-closed. Без неизвестно-успешных платных retry. |
| Сопроводительные письма | [`coverletters/`](../../internal/coverletters/), [`vacancy_cover_letter_routes.go`](../../internal/httpapi/vacancy_cover_letter_routes.go) | Ручная генерация OpenRouter и **платный OpenAI** ([PR #493](https://github.com/meInteractive/it-map/pull/493)); дедупликация одинакового snapshot и evidence до POST через существующую таблицу, не полноценный provider cost ledger. Подробности: [GO-018](GO-018-PAID-OPENAI-LETTERS.md). |
| **Google Calendar (GO-015)** | [`integrations/googlecalendar/`](../../internal/integrations/googlecalendar/), [`google_calendar_routes.go`](../../internal/httpapi/google_calendar_routes.go) | В `main` с [PR #496](https://github.com/meInteractive/it-map/pull/496): OAuth с HMAC state/PKCE, зашифрованный refresh token, `status/connect/callback/sync/disconnect`. **Только ручная однонаправленная синхронизация IT Map → Google**; создание/перенос задач даёт `google_sync_status=pending`. Реальный OAuth/sync на тестовом аккаунте не принят. [Runbook](GO-015-GOOGLE-CALENDAR.md). |
| Диагностика и расходы | [`parser/funnel.go`](../../internal/parser/funnel.go), [`unit_cost.go`](../../internal/parser/unit_cost.go), [`expense_repeats.go`](../../internal/parser/expense_repeats.go), [`frontend/features/settings/`](../../frontend/features/settings/) | P1/P2, воронка, план HH, безопасный JSON-снимок, частичная себестоимость, признаки повторов; не подменять факты подтверждённой оплаты или новые уникальные вакансии событиями обработки кандидатов. |

**Важная семантика статистики:** `import receipt` / успешная обработка кандидата (`create/update/unchanged`) и **впервые созданная уникальная вакансия** — разные показатели. Время создания карточки IT Map не равно дате публикации HH. Историческое расхождение «61 публикация» исследовано на синтетических данных, но точное происхождение числа в реальной БД не доказано: [GO-436](GO-436-FUNNEL-RECONCILIATION.md). Полная когортная цена новой вакансии также ещё не доказана: [GO-433](GO-433-PARTIAL-UNIT-COST.md), [GO-434](GO-434-EXPENSE-AUDIT.md).

## 5. MariaDB, версии схемы, резервные копии

Целевая БД — **MariaDB 10.11+** через `database/sql` и Go-драйвер с build tag `mariadb`. [`storage/`](../../internal/storage/) проверяет совместимость схемы; SQL-операции используют транзакции, параметризованные запросы, FK, revision/CAS, leases и явные правила идемпотентности. [`dbschema/`](../../internal/dbschema/) содержит доверенный baseline, а `itmap schema-init` предназначен для контролируемого создания схемы, **не** для бесконтрольного DDL на заполненной базе. MariaDB 10.3.32 намеренно отвергается.

[`backup/`](../../internal/backup/), [`v39/`](../../internal/v39/) и SQL-интеграционные тесты проверяют экспорт/валидацию/restore v39 и совместимость исторических форматов; изменение `generation` защищает от работы под старым восстановленным состоянием. Эти тесты **не подтверждают**, что конкретный production backup можно восстановить без отдельного preflight и разрешения владельца. Новые candidate-таблицы consumer-control и миграции должны согласовываться с inventory, backup/restore и schema-version marker.

Legacy Cloudflare D1/SQLite оставлен как исторический эталон контрактов; автоматической двусторонней миграции/синхронизации D1 ↔ MariaDB нет. Операционная готовность требует отдельной сверки данных и разрешения изменений.

## 6. Доставка, версия API и версия Worker

Два pipeline **не взаимозаменяемы**:

1. [`.github/workflows/publish.yml`](../../.github/workflows/publish.yml) — публикация разрешённого `main`: проверки, `cmd/itmap` + Vite `dist/web`, versioned release, receiver [`publish_release.py`](../../deploy/publish_release.py) / [`receive_release.py`](../../deploy/receive_release.py), smoke/rollback **Go API**. Workflow ограничен флагом `ITMAP_DEPLOY_ENABLED`. В архив релиза входит **`itmap` и web**, но **не `itmap-worker`**. Успешный workflow этого контура не подтверждает обновление фонового процесса.
2. [`.github/workflows/worker-artifact.yml`](../../.github/workflows/worker-artifact.yml) — отдельная проверяемая сборка `itmap-worker` и `itmap-recovery` для Linux `amd64` / `arm64`: build-info/VCS SHA, checksums и GitHub artifact. **Автоматической установки Worker на сервер этим workflow нет**.

Текущий системный профиль Worker описан в [`deploy/WORKER-PRODUCTION.md`](../../deploy/WORKER-PRODUCTION.md) и [`itmap-worker.service.example`](../../deploy/itmap-worker.service.example). На 19.11 **не выполнялась новая read-only проверка установленного бинарника/процесса**, поэтому GitHub `main` и серверный Worker могут иметь разные SHA. [GO-447](../../tasks/GO-447-WORKER-DEPLOY-RESTART.md) / [Issue #472](https://github.com/meInteractive/it-map/issues/472) — будущая атомарная доставка того же commit SHA вместе с API, safe drain, остановка прежнего consumer, проверка версии и rollback. **Не реализована в текущем `publish.yml`.**

Особенность конфигурации: [`config.ValidateEnvironment`](../../internal/config/config.go) по-прежнему **отвергает `APP_ENV=production` для HTTP API**, а [`GET /health/ready`](../../internal/httpapi/handler.go) намеренно отвечает `503 business_api_not_migrated` даже при доступной БД. Эти старые migration gates **не отменяют** наличие работающих Go-маршрутов и native publish, но запрещают называть систему полностью принятой по одному `200` на бизнес-маршруте. Worker имеет **отдельные** правила `APP_ENV` и включения в [`cmd/itmap-worker`](../../cmd/itmap-worker/). Не смешивать readiness API, readiness Worker, успешность релиза и фактическое качество парсинга.

## 7. Тестирование и уровни доказательств

| Уровень | Подтверждение | Чего не доказывает |
| --- | --- | --- |
| Go unit/race/vet + React | `go test -race`, `go vet`, `npm run typecheck:web`, `npm run format:check`, `npm run build:web` ([GO-113](GO-113-TYPESCRIPT-CI.md), [GO-114](GO-114-FORMATTING.md)) | Установленную версию сервера и реальные внешние расходы. |
| Одноразовая MariaDB | [`mariadb-staging-ci.yml`](../../.github/workflows/mariadb-staging-ci.yml), `-tags mariadb`, synthetic HH/AI/Google mocks, fault injection, SQL/HTTP concurrency | Полный real-world provider outcome и восстановление рабочей БД. |
| Browser/контракты | `tests/browser/`, `scripts/test-*.mjs`, source-oracle JS→Go; desktop/mobile, изоляция отдельных Playwright-сценариев (GO-117) | Полную совместимость всех legacy API и реальный Google OAuth. |
| Safety/release | [`go-ci.yml`](../../.github/workflows/go-ci.yml), [`mariadb-103-ci.yml`](../../.github/workflows/mariadb-103-ci.yml), [`worker-artifact.yml`](../../.github/workflows/worker-artifact.yml), [`publish.yml`](../../.github/workflows/publish.yml) | Что Worker установлен вместе с API или что в реальном расписании не осталось старых jobs. |
| Операторская приёмка | Явная сверка установленного SHA, запущенных служб, read-only очереди, lineage, расходов и отдельно разрешённых тестовых интеграций | Не заменяется green CI или статусом «merged» в GitHub. |

У [PR #496](https://github.com/meInteractive/it-map/pull/496) все четыре обязательных PR workflow завершились успешно на `51ffa71d`, затем PR смержен в `2e670273`. Это подтверждает сборку и synthetic gates соответствующего изменения, **не** живую приёмку Google Calendar и **не** версию установленного Worker.

## 8. Открытые границы и план развития

- **GO-447:** подтверждаемая доставка Worker одним SHA с API; безопасный drain, rollback и исключение второго consumer. Операторский rollout не выполняется из документации.
- **GO-431 / Issue #489:** независимая durable identity платных AI-операций между разными кандидатами; согласованная schema migration, тесты и денежный lineage.
- **GO-015:** проверка Google OAuth, ручного sync, reconnect и отсутствия дублей на разрешённом тестовом аккаунте. Нет двусторонней фоновой синхронизации.
- **GO-433/434/436 и HH:** полная когортная атрибуция стоимости, read-only проверка исторических расхождений, старых blocked cursors и пропусков публикаций. Не повторять платные Actor/AI POST для диагностики.
- **Остальной migration backlog:** неперенесённые legacy endpoints, источники помимо HH, действующие release gates и согласованный перенос/restore реальных данных. RabbitMQ и transactional outbox **не внедрены**; SQL scheduler уже является действующей моделью, менять её нужно по отдельному ADR и доказанной потребности.

Работы в режиме ChatGPT, смерженные, но ещё не проверенные Codex, помечаются **«к проверке»**; это не статус production-ready и не замена acceptance criteria. [AGENTS.md](../../AGENTS.md), [SPRINT-01](../../tasks/SPRINT-01.md), [SPRINT-01-PARSER-APIFY](../../tasks/SPRINT-01-PARSER-APIFY.md).

## 9. Навигация

- [Карта структурных доказательств GO-119](GO-119-ARCHITECTURE-EVIDENCE.md), [сводка миграции](GO_MIGRATION_PROGRESS.md), [справочник исходников](GO_BACKEND_STRUCTURE.md), [архитектурные решения](DECISIONS.md).
- [Конвейер и безопасность платных операций](../../internal/parser/BILLING.md), [Worker production runbook](../../deploy/WORKER-PRODUCTION.md), [GO-431](../../tasks/GO-431-CONTINUOUS-WORKER.md).
- [Модель MariaDB](MARIADB.md), [восстановление](../user/BACKUP_FORMAT.md), [пользовательский API](../user/API.md) и [legacy baseline](LEGACY_API_BASELINE.md).
- [Доставка](../user/deployment/README.md), [handoff](handoff/README.md), [текущие задачи](../../tasks/README.md).

**Метод доказательств:** «есть код», «прошёл synthetic CI», «установлен конкретный SHA», «обработан реальный job» и «принято на production» — пять разных утверждений. Новая архитектура должна проверяться именно на нужном уровне.
