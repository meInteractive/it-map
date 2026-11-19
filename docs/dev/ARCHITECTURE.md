# Архитектура IT Map

> **Актуальный срез исходников: 19.11.2026, `main` [`2e670273`](https://github.com/meInteractive/it-map/commit/2e67027363534b54b16295fe541dfdb4b968e693), после merge [#496](https://github.com/meInteractive/it-map/pull/496).** Это описание **реализованных компонентов и их контрактов**, а не акт приёмки установленной версии или состояния данных на сервере. Для текущих статусов задач — [реестр](../../tasks/README.md), для проверки код/CI/эксплуатация — [GO-119 evidence](GO-119-ARCHITECTURE-EVIDENCE.md). Исторические handoff и решения сохраняются отдельно.

## 1. Назначение и статус

IT Map — каталог и карта IT-работодателей, вакансий, агентств и источников, CRM откликов и задач, а также автоматизированный поиск и обработка вакансий. Основной перенос — из JavaScript/Cloudflare Workers + D1 (GPT Sites) в React + Go + MariaDB; старый код `server/`, `build/`, `db/schema.ts` остаётся эталоном миграции, **не вторым Go API**. Фактический статус старой инфраструктуры, полнота cutover и синхронизация живых данных не устанавливаются по GitHub-коду.

**Текущая инженерная модель:** **модульный Go-монолит с двумя основными исполняемыми процессами**, совместной MariaDB и разделёнными жизненными циклами:

- `cmd/itmap` — HTTP API, аутентификация, React SPA, CRM, отчёты, административные операции, ручная генерация писем и календарь.
- `cmd/itmap-worker` — самостоятельный постоянно работающий процесс фонового HH-конвейера с тремя последовательными профилями, durable jobs/leases, ограничениями платных запусков и loopback-наблюдаемостью.
- `cmd/itmap-recovery` — отдельная **операторская** утилита проверки и строго разрешённой сверки **известных** Apify операций; не часть обычного цикла worker. `cmd/itmap-parser-staging` и `cmd/itmap-schema-audit` — специальные диагностические команды.
- `MariaDB` — источник долговременного состояния для новой реализации: пользовательские записи, расписание, кандидаты, attempts, receipts, резервы, стоимость, generation и consumer control.

**Границы уверенности.** Исходники и синтетические тесты подтверждают наличие реализации. По прежним наблюдениям владельца отдельный Go Worker уже использовался в рабочем контуре, но **текущий бинарный SHA Worker, systemd unit, режим consumer и полнота реального ledger не сверялись на 19.11**. Публикация HTTP API **не обновляет** Worker. `APP_ENV=production` всё ещё запрещён в конфигурации **HTTP-приложения** (`internal/config/config.go`); у отдельной команды Worker собственный набор production opt-in флагов. `GET /health/ready` **HTTP API** намеренно возвращает `503 business_api_not_migrated`, а `/readyz` Worker — **другой, локальный** сигнал. Смешивать эти endpoints нельзя.

## 2. Схема процессов, данных и внешних систем

```text
Браузер / PWA
React 18 + TypeScript + Vite
          |
   HTTPS / reverse proxy
          |
  itmap (Go HTTP, net/http)
  ├─ auth/session + HTTP/JSON/SPA
  ├─ компании, агентства, карта, CRM
  ├─ вакансии, отклики, задачи, профили
  ├─ настройки/классификация/backup
  ├─ диагностика планировщика, воронка, бюджеты
  ├─ ручные сопроводительные → OpenRouter / OpenAI
  └─ Google Calendar OAuth + ручная sync → Google
          |
          | database/sql, транзакции/CAS
          v
       MariaDB
  ├─ предметные таблицы и пользовательские связи
  ├─ scheduler jobs/runs/attempts + leases
  ├─ candidate intake / snapshots / import receipts
  ├─ generation + consumer ownership/fence
  └─ provider ledger, confirmed / reserved / unknown
          ^
          | те же durability/ownership-инварианты
          |
  itmap-worker (отдельный Go процесс)
  ├─ workerengine + workerloop (один шаг за раз)
  ├─ launch: допуск нового HH scan / Actor
  ├─ reconcile: чтение ранее созданного Actor / Dataset
  ├─ pipeline: title → full text → extraction
  |            → leaf roles → import → relevance
  └─ loopback health/queue/recovery/activity diagnostics
          |
    HH.ru / Apify / Firecrawl* / OpenRouter / OpenAI
          |
  itmap-recovery — только операторский inspect/apply,
  с отдельными opt-in и проверкой известной операции

  GitHub Actions publish.yml → API + React release
  GitHub Actions worker-artifact.yml → worker/recovery artifacts
    (**НЕ** автоматическая установка worker на сервер)
```

`* ` Firecrawl/остальные исторические источники имеют собственные адаптеры/настройки и **не должны считаться полностью включёнными в непрерывный HH-only профиль**. В `workerengine.Engine.cycle` для `reconcile`/`pipeline` применяются `SourceID=hh` и `VerifiedHHOnly=true`. RabbitMQ/transactional outbox в работающую модель **не входят**: очереди хранятся в MariaDB.

## 3. Компоненты и точки входа

| Область | Основные файлы / пакеты | Функция и граница |
| --- | --- | --- |
| HTTP bootstrap | [`cmd/itmap/main.go`](../../cmd/itmap/main.go), [`internal/httpapi/handler.go`](../../internal/httpapi/handler.go) | Сборка `httpapi.Options`, `net/http ServeMux`, auth и маршрутов; `configureScheduler` — явный legacy/manual control за отдельным флагом, **не фоновый процесс** |
| UI | [`frontend/`](../../frontend/), [`web/`](../../web/), [`shared/`](../../shared/) | React SPA, список/карта, user filters, сохранение форм, JSON-контракты и API-кэш |
| CRM и предметные данные | [`catalog/`](../../internal/catalog/), [`crm/`](../../internal/crm/), [`vacancies/`](../../internal/vacancies/), [`applications/`](../../internal/applications/), [`tasks/`](../../internal/tasks/), [`profiles/`](../../internal/profiles/) | Карточки работодателей, избранное, вакансии/отклики, задачи, личные резюме; user isolation, CAS и idempotency в соответствующих хранилищах |
| Worker bootstrap | [`cmd/itmap-worker/main.go`](../../cmd/itmap-worker/main.go), [`workerconfig/`](../../internal/workerconfig/), [`workerengine/`](../../internal/workerengine/), [`workerloop/`](../../internal/workerloop/) | Независимая конфигурация/оповещение SIGTERM, run loop и диагностика; не запускать без opt-in |
| Worker бизнес-операции | [`internal/parser/`](../../internal/parser/), [`internal/storage/`](../../internal/storage/) | Планирование HH, jobs/leases, paid admission, AI stages, import, recovery и транзакции MariaDB |
| Consumer и schema | [`consumercontrol/`](../../internal/consumercontrol/), [`consumerpreflight/`](../../internal/consumerpreflight/), [`consumermigration/`](../../internal/consumermigration/), [`dbschema/`](../../internal/dbschema/), [`backup/`](../../internal/backup/) | Fence/generation, preflight и versioned backup/restore; **наличие code path не доказывает текущую установленную схему** |
| AI и счета | [`aiproviders/`](../../internal/aiproviders/), [`apifyruns/`](../../internal/apifyruns/), [`apifysettings/`](../../internal/apifysettings/), [`paidrecovery/`](../../internal/paidrecovery/) | Конфигурации провайдеров, лимиты/резервы, evidence, известная стоимость и неизвестные исходы |
| Google Calendar | [`internal/integrations/googlecalendar/`](../../internal/integrations/googlecalendar/), [`internal/httpapi/google_calendar_routes.go`](../../internal/httpapi/google_calendar_routes.go) | OAuth, token encryption, ручная однонаправленная sync задач; **нет фонового импорта или двусторонней sync** |
| Ручные письма | [`coverletters/`](../../internal/coverletters/), [`GO-018`](GO-018-PAID-OPENAI-LETTERS.md) | Явная генерация через бесплатный OpenRouter или выбранный OpenAI; отдельный fail-closed журнал paid evidence |
| Выпуск | [`publish.yml`](../../.github/workflows/publish.yml), [`worker-artifact.yml`](../../.github/workflows/worker-artifact.yml), [`deploy/`](../../deploy/) | API/SPA release и отдельная сборка артефактов worker; единый deploy API + Worker **ещё задача GO-447** |

Сервисный Go-код использует `database/sql`, параметризованные запросы, ограниченные транзакции, `context.Context` и стандартный `net/http`. Не заявлять наличие Gin, ORM, Kafka или RabbitMQ.

## 4. Постоянный Go Worker: планирование, восстановление, остановка

### Три профиля одного процесса

[`workerengine.Engine`](../../internal/workerengine/engine.go) создаёт три профиля `workerloop`: **`launch`**, **`reconcile`**, **`pipeline`**. Планировщик работает последовательно (`RunProfilesWithGrace`), обходит готовые профили по кругу, учитывает разные idle-interval, jitter и bounded exponential backoff ошибок. **Это логические очереди и профили, не три потока с параллельными оплачиваемыми запросами**. Каждый `Cycle` формирует уникальный `worker:<random>` request ID; отдельные cycles ограничены по времени и числу jobs.

- **Launch** проверяет включение scheduler, создаёт допустимые новые сканы по расписанию, восстанавливает истёкшие leases и планирует HH. Здесь действует допуск к новому платному Actor, общий cooldown, настройки источников, избранные компании, окно дат и профессиональные роли HH.
- **Reconcile** выбирает `scan_source` в фазе `reconcile`: проверяет уже известные операции/Run ID и дочитывает Dataset. **Чтение известного Actor не равно новому платному POST.** Старые неподтверждённые курсоры не возобновляются автоматически.
- **Pipeline** выбирает `classify_title`, `fetch_full_text`, `extract_vacancy`, `assign_roles`, `import_vacancy`, `score_relevance`. Работает с сохранёнными кандидатами и результатами стадий, независимо от частоты новых Actor.

Значения по умолчанию из [`workerconfig.Defaults`](../../internal/workerconfig/config.go): idle для pipeline — 5 секунд, launch/reconcile — 30 секунд, error backoff — 5–60 секунд, `MaxJobs=50`. Это **локальный polling**, а не гарантированный SLA или разрешённая частота платного Apify. `StepTimeout` профиля 2 минуты, время `Cycle` — до 100 секунд; при новом pipeline claim резервируется запас времени. Значения могут переопределяться валидированным runtime environment.

### Durable ownership, generation и paid fence

MariaDB удерживает `scheduler_jobs`, `scheduler_runs`, leases и attempts; `workerengine` не использует память процесса как источник истины. Consumer fencing и generation должны исключать недопустимую запись после restore/переключения. При смене generation Worker завершает цикл, чтобы следующий старт заново загрузил состояние; конфликт активного run отражается как `contended`, а не как подтверждённая ошибка платного провайдера.

Для платных HH/AI операций важны **разные состояния**: `reserved` (локальный резерв), `unknown` (возможно отправлено, нет подтверждения), `confirmed` (есть достоверное evidence расчёта). `cost_usd=NULL` не означает нулевых затрат; провайдерский snapshot и локальный ledger могут пересекаться. Нельзя повторять платный POST из-за timeout, потерянного ACK, ошибки COMMIT, SIGKILL или смены generation, если результат первого запроса неопределён. Сохранять lineage и причины блокировки.

[PR #490](https://github.com/meInteractive/it-map/pull/490), [#491](https://github.com/meInteractive/it-map/pull/491), [#494](https://github.com/meInteractive/it-map/pull/494) и [#495](https://github.com/meInteractive/it-map/pull/495) закрывают **отдельные** окна вокруг claim, отмены, reserve COMMIT и начала AI dispatch. При SIGTERM `StopClaims` прекращает допуск **новых** платных операций, а уже начатому вызову даётся ограниченное grace-окно для settlement; если запрос ещё не отправлен, job может безопасно вернуться в `pending`. Поведение проверено на синтетической MariaDB, **не** на реальном финансовом ledger.

**Остаточные риски:** долговечная идентичность одинаковых AI-вызовов **между разными кандидатами** пока только дизайн ([GO-431 durable identity](../../tasks/GO-431-DURABLE-AI-OPERATION-DESIGN.md), Issue #489). Наличие guard в пределах одного job/candidate не является полной глобальной дедупликацией. Для восстановлений используются отдельные [операторские процедуры](../../deploy/WORKER-RECOVERY-DIAGNOSTICS.md).

### Диагностика отдельного процесса

[`cmd/itmap-worker/main.go`](../../cmd/itmap-worker/main.go) при включённом `ITMAP_WORKER_HEALTH_ENABLED=1` слушает **`127.0.0.1:18082`** (loopback, не публичный API). [`workerengine.Monitor`](../../internal/workerengine/monitor.go) отдаёт:

- `/healthz` — наличие недавнего локального progress; `/readyz` — свежесть/результаты **всех** трёх профилей, а не только живость процесса; `/profilez` — ограниченные счётчики профилей.
- `/queuez`, `/recoveryz`, `/hh-schedulez`, `/activityz` — ограниченные read-only агрегаты, при ошибках/недоступном reader — отказ, без ключей и сырых данных.

`ready` не доказывает импорт новых уникальных вакансий, доставку верного бинарника, оплату/неоплату запросов или полноценную provider-работоспособность. Для production диагностики необходимы отдельные read-only доказательства установленного SHA, generation, consumer, очереди и неизвестных расходов; не перезапускать Worker только ради проверки.

## 5. HTTP API, CRM и новые интеграции

[`cmd/itmap/main.go`](../../cmd/itmap/main.go) собирает HTTP-зависимости через `httpapi.Options`, включает маршруты каталога, компаний, CRM, вакансий/откликов, задач, классификации корневых ролей и промтов (CAS), AI/Apify настроек, администратора, backup и read-only диагностики парсера. [`handler.go`](../../internal/httpapi/handler.go) авторизует переносимые маршруты; неперенесённый API не отвечает фиктивным успехом (`backend_not_migrated`).

**Фильтры списка и карты.** [GO-448 / PR #479](https://github.com/meInteractive/it-map/pull/479) добавил самостоятельный выбор **«Избранные / Не избранные / Все»** с общим React-состоянием, не смешивая признак вакансий с избранными компаниями.

**Сопроводительные письма.** [GO-018 / PR #493](https://github.com/meInteractive/it-map/pull/493): OpenAI разрешён **только при явном действии пользователя**, после fail-closed paid admission; бесплатный OpenRouter сохранён. `openrouter_ai_logs` содержит evidence возможного обращения (`paid_unknown`/`paid_response`), но **не является полноценным финансовым ledger** и не подтверждает цену запроса. Отдельные лимиты и provider reconciliation остаются эксплуатационным вопросом.

**Google Calendar.** [GO-015 / PR #496](https://github.com/meInteractive/it-map/pull/496) смержен: Go OAuth через `/api/google-calendar/{status,connect,callback,sync,disconnect}`, state/HMAC/cookie + PKCE, шифрованный refresh token и user isolation. Создание/перенос задач сохраняет `google_sync_status=pending`; Google events создаются/меняются **только по ручной команде sync**. Есть меры против повторного создания Event ID после потерянного ACK. **Нет** двусторонней, автоматической периодической синхронизации, импорта чужих событий или подтверждённой реальной OAuth-приёмки. Старое приложение не имело действовавшего подключения; старые реальные токены переносить не требуется. См. [инструкцию](GO-015-GOOGLE-CALENDAR.md).

## 6. Хранилище, backup и метрики

MariaDB обеспечивает CAS/revision, FK, leases, idempotency receipts и `generation`. Изменение схемы требует версионированных миграций и проверки совместимости `dbschema`, export/restore `v39`, связанных consumer markers и historical backups на **одноразовой** MariaDB. `schema-init` нельзя применять к заполненной рабочей БД как универсальный мигратор. По исходникам нельзя объявлять установленную production схему совместимой с новым GO-431.

Воронка и себестоимость строго различают: (1) события/успешные стадии кандидата, (2) успешные import receipts, включая `update/unchanged`, (3) **первые CREATE уникальной вакансии IT Map**, (4) дату публикации вакансии на исходном HH. [GO-436](GO-436-FUNNEL-RECONCILIATION.md) отделил импорт от новых вакансий; историческая аномалия «61» ещё требует проверенной read-only сверки. [GO-433](GO-433-PARTIAL-UNIT-COST.md) и [GO-434](GO-434-EXPENSE-AUDIT.md) пока дают **частичный** учёт и признаки потенциальных повторов, не полную доказанную цену каждой когорты. Unknown costs не превращать в 0; Firecrawl credits не пересчитывать в USD без достоверного тарифа.

## 7. Сборка, CI, выпуск и эксплуатация

- [`go-ci.yml`](../../.github/workflows/go-ci.yml) и [`mariadb-staging-ci.yml`](../../.github/workflows/mariadb-staging-ci.yml) — Go и React проверка, `typecheck:web` (GO-113), `format:check` Prettier (GO-114), build и browser/synthetic SQL-контракты. Отдельно [MariaDB 10.3 unsupported guard](../../.github/workflows/mariadb-103-unsupported.yml) — имя workflow сверить по содержимому каталога. CI на PR SHA **не является live-приёмкой**.
- [`publish.yml`](../../.github/workflows/publish.yml) при разрешающей переменной и успешных gates формирует release из **`./cmd/itmap` + `dist/web`**, доставляет API/SPA и выполняет server smoke/rollback через `deploy/publish_release.py` / `receive_release.py`. **Worker в этом release archive отсутствует.**
- [`worker-artifact.yml`](../../.github/workflows/worker-artifact.yml) отдельно собирает `itmap-worker` и `itmap-recovery` для Linux **amd64 и arm64** (`-tags mariadb`), проверяет встроенный `vcs.revision`, `vcs.modified=false` и публикует SHA256 артефактов в GitHub Actions. **Artifact build не устанавливает worker на сервер.**
- [GO-447 / Issue #472](../../tasks/GO-447-WORKER-DEPLOY-RESTART.md) — предстоящий безопасный совместный release API+Worker одного SHA, drain, проверка установленного бинарника, health и rollback без двойного consumer. Нельзя считать его завершённым по автоматическому API publish.
- Для Worker реальные opt-in флаги/параметры и systemd шаблон — [`deploy/WORKER-PRODUCTION.md`](../../deploy/WORKER-PRODUCTION.md). Код не подтверждает, что unit установлен и обновлён до `main`.

## 8. Остаточные работы и уровни доказательств

| Слой | Что подтверждено | Что ещё не следует утверждать |
| --- | --- | --- |
| Исходники `main` | API, отдельный continuous Worker, paid-fencing, Google Calendar Go-контур, диагностика и сборка | Установлен последний Worker SHA; все внешние платные/Google сценарии прошли |
| GitHub Actions | Детерминированные Go/Node/browser и disposable MariaDB тесты на конкретном commit | Эти проверки равны выполненной migration/cutover или финансовой сверке |
| Рабочий сервер | Исторические наблюдения оператора о действующем HH Worker и API | Актуальные systemd/flags/бинарники/leases, полнота транзакций и расходных журналов без read-only preflight |
| Эксплуатация | Механизмы fail-closed, отдельные release и recovery инструменты есть | GO-447 deploy, durable cross-candidate identity, все legacy API, полный D1→MariaDB cutover и recovery приняты |
| Технический долг | MariaDB очередь работает без RabbitMQ; текущая система модульная | RabbitMQ/outbox/распределённый масштаб — уже реализованы |

Работы ChatGPT, слитые в `main`, но ещё не проверенные Codex, получают статус **«к проверке»**; у больших частично реализованных задач — **«частично реализовано — к проверке»** (правило [`AGENTS.md`](../../AGENTS.md), аудит [19.11](../../tasks/CHATGPT-REVIEW-2026-11-19.md) после слияния соответствующего PR). `[x]` не ставится по одному merge.

## 9. Навигация

- [Правила разработки](../../AGENTS.md), [текущие задачи](../../tasks/README.md), [GO-431](../../tasks/GO-431-CONTINUOUS-WORKER.md), [GO-431 AI-инварианты](../../tasks/GO-431-AI-RELIABILITY-2026-11-17.md), [GO-447](../../tasks/GO-447-WORKER-DEPLOY-RESTART.md).
- [Матрица доказательств GO-119](GO-119-ARCHITECTURE-EVIDENCE.md), [статус миграции](GO_MIGRATION_PROGRESS.md), [архитектурные решения](DECISIONS.md), [MariaDB](MARIADB.md).
- [Production Worker](../../deploy/WORKER-PRODUCTION.md), [recovery diagnostics](../../deploy/WORKER-RECOVERY-DIAGNOSTICS.md), [Google Calendar](GO-015-GOOGLE-CALENDAR.md), [OpenAI letters](GO-018-PAID-OPENAI-LETTERS.md), [каталог handoff](handoff/README.md).
- [Исходный API-контракт](LEGACY_API_BASELINE.md) и [покрытие текущего HTTP](API_COVERAGE.md).

**Безопасность изменений:** никакого самостоятельного server rollout, рестарта Worker/старого scheduler, DDL/restore, реальных платных POST/Apify, исправления ledger или авто-merge. Для них нужен отдельный согласованный операторский план. Этот документ — только документация архитектуры.
