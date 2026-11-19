# Архитектура IT Map

> **Актуальный срез: 19 ноября 2026 года, `main` [`2e670273`](https://github.com/meInteractive/it-map/commit/2e67027363534b54b16295fe541dfdb4b968e693), после merge [PR #496](https://github.com/meInteractive/it-map/pull/496).** Это описание реализованной кодовой архитектуры, **не акт проверки установленного на сервере бинарника, версии БД или полного production cutover**. Для фактов о CI, операционной приёмке и незакрытых критериях см. [матрицу GO-119](GO-119-ARCHITECTURE-EVIDENCE.md), [план миграции](GO_MIGRATION_PROGRESS.md), [GO-431](../../tasks/GO-431-CONTINUOUS-WORKER.md) и [GO-447](../../tasks/GO-447-WORKER-DEPLOY-RESTART.md). Предыдущий текст от 09.11 содержал неактуальное утверждение, что отдельный Worker отсутствует.

## 1. Система в целом

IT Map объединяет каталог и карту работодателей, персональную CRM, вакансии, отклики, контакты, задачи, AI-оценку и сбор вакансий. Основная новая реализация — **модульный Go-монолит с двумя самостоятельными исполняемыми процессами**, общими MariaDB-репозиториями и React SPA. **RabbitMQ, Kubernetes и transactional outbox не используются как текущий механизм очередей**: задания, lease и денежные операции сохраняются в MariaDB.

| Компонент | Точка входа / исходники | Фактическая ответственность |
| --- | --- | --- |
| **Go API + React** | [`cmd/itmap/main.go`](../../cmd/itmap/main.go), [`internal/httpapi`](../../internal/httpapi/), [`frontend`](../../frontend/), [`web`](../../web/) | HTTP JSON/сессии, каталог, CRM, вакансии, задачи, админ-API, Google Calendar, письма, статистика; выдаёт собранную SPA |
| **Непрерывный Go Worker** | [`cmd/itmap-worker`](../../cmd/itmap-worker/), [`internal/workerengine`](../../internal/workerengine/), [`internal/workerloop`](../../internal/workerloop/), [`internal/parser`](../../internal/parser/) | Долгоживущий процесс, последовательно обслуживающий профили HH launch/reconcile и конвейер кандидатов/AI/импорта; не является HTTP handler API |
| **Восстановление и диагностика** | [`cmd/itmap-recovery`](../../cmd/itmap-recovery/), [`internal/workerengine/monitor.go`](../../internal/workerengine/monitor.go) | Отдельная recovery-команда/сборка, локальные read-only снимки состояния и оснований для восстановления; не предполагают автоматического повтора платного POST |
| **MariaDB** | [`internal/storage`](../../internal/storage/), [`internal/dbschema`](../../internal/dbschema/), [`internal/backup`](../../internal/backup/) | Основные данные, версии/generation, задания, claims/leases, попытки, учёт расходов, настройки и резервные копии |
| **Провайдеры** | [`internal/integrations`](../../internal/integrations/), [`internal/aiproviders`](../../internal/aiproviders/), [`internal/apifyruns`](../../internal/apifyruns/) | HH public, Apify, OpenAI/OpenRouter, Google Calendar; внешние вызовы из явно разрешённых пользовательских и фоновых путей |
| **Исходная версия** | [`server`](../../server/), [`db`](../../db/), [`build`](../../build/) | Исторический GPT Sites/Cloudflare D1/JS — эталон поведения при миграции, **не** Go Worker и не новый production backend |

```text
                                  Браузер / PWA
                              React + TS + Vite SPA
                                        |
                                  HTTPS / JSON
                                        |
                        +---------------v---------------+
                        | cmd/itmap  —  Go HTTP API      |
                        | net/http; auth; CRM; tasks    |
                        | vacancies; settings; calendar |
                        | cover letters; diagnostics    |
                        +---------------+---------------+
                                        |
                                  database/sql
                                        |
                        +---------------v---------------+
                        |            MariaDB            |
                        | вакансии, пользователи, jobs  |
                        | runs, leases, generation      |
                        | attempts, paid ledger, backup |
                        +---------------+---------------+
                                        ^
                                        | durable read / claim / settlement
                        +---------------+---------------+
                        | cmd/itmap-worker               |
                        | workerengine / workerloop      |
                        | launch -> reconcile -> pipeline|
                        | bounded steps, graceful drain  |
                        +---------------+---------------+
                                        |
                    +-------------------+------------------+
                    |                   |                  |
                HH / Apify         OpenAI/OpenRouter    HH public
                  (Actor)          (AI stages)          (read only)

         Отдельно: cmd/itmap-recovery + loopback worker health/diagnostics

          Историческая GPT Sites / D1 система — отдельный контур;
                    автоматической D1↔MariaDB синхронизации нет.
```

**Разделение обязанностей:** API отвечает за синхронные пользовательские запросы и администрирование, Worker — за регулярное потребление очереди; они используют один механизм SQL-владения и общие бизнес-сервисы. Наличие рабочей реализации HTTP-триггера в коде API **не означает**, что он может без контроля параллельно исполнять задания с Worker. Политика единственного активного consumer и ограничения переключения описаны в [GO-431 consumer fence](../../tasks/GO-431-CONSUMER-FENCE.md).

## 2. API, продуктовые модули и фронтенд

- **HTTP:** стандартный Go `net/http`/`ServeMux`, middleware сессий, проверки прав/Origin, явные ошибки; не Gin и не Node runtime. Основной wiring — [`cmd/itmap/main.go`](../../cmd/itmap/main.go), таблица маршрутов — [`internal/httpapi/handler.go`](../../internal/httpapi/handler.go).
- **Продукт:** [`internal/catalog`](../../internal/catalog/), [`crm`](../../internal/crm/), [`vacancies`](../../internal/vacancies/), [`applications`](../../internal/applications/), [`tasks`](../../internal/tasks/), [`channels`](../../internal/channels/), [`contacts`](../../internal/contacts/), [`profiles`](../../internal/profiles/), [`roles`](../../internal/roles/). Записи используют транзакции и, где требуется, revision/CAS и идемпотентные receipts.
- **React:** [`frontend/Application.tsx`](../../frontend/Application.tsx), [`frontend/api/client.ts`](../../frontend/api/client.ts), [`web/main.tsx`](../../web/main.tsx); Vite собирает статику, которую выдаёт Go API. Обязательные TypeScript/Prettier/браузерные проверки есть в CI. В списке и на карте компаний реализован независимый трёхпозиционный фильтр избранного ([GO-448, PR #479](https://github.com/meInteractive/it-map/pull/479)).
- **Настройки классификации:** корневые направления HH и промты доступны через перенесённые Go HTTP-обработчики с CAS ([GO-438, PR #276](https://github.com/meInteractive/it-map/pull/276)); существование маршрутов не доказывает эксплуатационную проверку каждой формы.
- **Сопроводительные письма:** [`internal/coverletters`](../../internal/coverletters/) и защищённый API генерации; OpenRouter и ручные OpenAI письма, включая платный путь, доставлены в коде ([PR #277](https://github.com/meInteractive/it-map/pull/277), [#493](https://github.com/meInteractive/it-map/pull/493)). Это **отдельный интерактивный путь** от Worker; полная GO-018 (включая релевантность и все денежные критерии) не закрыта автоматически.
- **Google Calendar:** [`internal/integrations/googlecalendar`](../../internal/integrations/googlecalendar/), [`internal/httpapi/google_calendar_routes.go`](../../internal/httpapi/google_calendar_routes.go); [PR #496](https://github.com/meInteractive/it-map/pull/496) в `main`. Схема — OAuth 2.0/PKCE + HMAC state/cookie, шифрованный refresh token, **ручная однонаправленная синхронизация** `IT Map → Google Calendar`, deterministic Event ID, GET/INSERT/PATCH и безопасное восстановление после lost ACK. Задачи при подключённом календаре могут сохраняться локально со статусом `pending`; это **не фоновый двусторонний календарный consumer**. Реальный OAuth на тестовом Google-аккаунте отдельно не принят. [Подробная инструкция](GO-015-GOOGLE-CALENDAR.md).

## 3. Постоянный Go Worker — текущий механизм очередей

### 3.1. Процессы и профили

[`cmd/itmap-worker/main.go`](../../cmd/itmap-worker/main.go) запускает [`workerengine.Engine`](../../internal/workerengine/engine.go) после проверки конфигурации и доступности MariaDB. [`workerloop`](../../internal/workerloop/profiles.go) выполняет **три логических профиля** ограниченными последовательными шагами:

1. **`launch`** — планирование/допуск новых HH Actor в рамках сохранённых настроек источников, расписания и лимитов. Новый платный старт не равен read-only проверке существующего Actor.
2. **`reconcile`** — чтение и согласование уже известного Actor/Run ID и его Dataset, продолжение сохранённых курсоров **без нового платного POST**, если допустимы сохранённые доказательства.
3. **`pipeline`** — стадии кандидата и вакансии: классификация, полный текст, извлечение, назначение ролей, импорт и релевантность. Может делать разрешённые AI-провайдерские обращения, отличая их от HH Actor.

Профили имеют отдельные idle-интервалы и ограничение работы на итерацию; локальный polling **не является** частотой запуска платного HH Actor или SLA публикации. По умолчанию в [`workerconfig`](../../internal/workerconfig/config.go): `LaunchIdle=30s`, `ReconcileIdle=30s`, `IdleDelay=5s` для pipeline, `MaxJobs=50`; значения ограничены конфигурацией. Ошибки приводят к bounded backoff. Реальная конфигурация установленной службы не установлена одним только содержимым репозитория.

### 3.2. Этапы обработки

[`internal/parser/worker_dispatch.go`](../../internal/parser/worker_dispatch.go) различает задания `scan_source`, `classify_title`, `fetch_full_text`, `extract_vacancy`, `assign_roles`, `import_vacancy` и `score_relevance`.

```text
HH employer/source (favorite, schedule, roles/date/city)
   |
   +-- launch: scan_source -> Apify Actor / сохранённый run/cursor
   |
   +-- reconcile: известный Actor -> Dataset -> candidate intake
                                             |
                    pipeline: classify_title -> fetch_full_text
                                             -> extract_vacancy
                                             -> assign_roles
                                             -> import_vacancy
                                             -> score_relevance
                                             |
                                    вакансии в MariaDB
                                             |
                                         Go API / React
```

Поток **не всегда линейный**: отдельные кандидаты могут быть отклонены на ранней стадии, ждать бюджета, доступного Actor/ответа или ручной сверки; повторный Dataset не должен порождать повторную новую карточку. У каждого этапа свой job/attempt/evidence. Для источников вне подтверждённого HH-маршрута нельзя переносить утверждение «весь конвейер полностью готов».

### 3.3. Владелец, остановка, восстановление

- В [`internal/parser`](../../internal/parser/) SQL-транзакции проверяют `generation`, consumer/owner, `run` и `job` leases, revision, очередность блокировок и право текущего исполнителя. Восстановление истёкшей lease **не разрешает** новую платную операцию без проверки её lineage и исхода.
- `SIGTERM` останавливает новые claims и допускает **bounded grace** уже начатой работы. GO-431 P0 ([#490](https://github.com/meInteractive/it-map/pull/490), [#491](https://github.com/meInteractive/it-map/pull/491), [#494](https://github.com/meInteractive/it-map/pull/494), [#495](https://github.com/meInteractive/it-map/pull/495)) предотвращает запуск нового платного AI dispatch после stop, в том числе на границе post-claim/post-reserve. Успешно начатый запрос не прерывается искусственно только ради остановки: settlement и сохранение результата получают ограниченное окно завершения.
- При `SIGKILL`, потере ACK, неопределённом SQL COMMIT, смене поколения или другом сбое состояние с **неподтверждённым** платным исходом сохраняется как неизвестное до независимого доказательства. Нельзя сбрасывать reserve, считать нулевую цену и повторять POST из одного только таймаута.
- Существуют защиты против повторов у того же job и candidate; **межкандидатная долговечная идентичность фактически одинакового платного AI-входа остаётся отдельным риском и дизайном**, а не завершённой миграцией ([GO-431 durable AI identity](../../tasks/GO-431-DURABLE-AI-OPERATION-DESIGN.md), Issue #489).
- Сохранённые HH Actor из старых поколений и неоднозначные legacy cursors не следует автоматически возобновлять/отменять или запускать заново; read-only диагностика предшествует операторскому решению.

### 3.4. Наблюдаемость

Worker может слушать **только loopback `127.0.0.1:18082`** при включённом диагностическом HTTP. [`workerengine/monitor.go`](../../internal/workerengine/monitor.go) предоставляет `GET /healthz`, `/readyz`, `/profilez`, `/queuez`, `/recoveryz`, `/hh-schedulez`, `/activityz`. Выдача ограничена по времени/одновременным запросам и очищена от секретов. `ready` означает свежий прогресс профилей, **не** доказывает успешно опубликованные вакансии, актуальность стоимости внешнего провайдера, отсутствие legacy jobs или соответствие установленного бинарника `main`.

В Go API есть отдельные администраторские блоки диагностики: 30-дневная история P1/P2, воронка событий кандидатов, HH-расписание, read-only учёт затрат и безопасный экспорт JSON. **`candidate import receipt` ≠ `new unique vacancy` ≠ дата публикации на HH**. Историческое расхождение показателя за 7 ноября требует отдельной сверки фактов, даже при корректной нынешней семантике ([GO-436](GO-436-FUNNEL-RECONCILIATION.md)).

## 4. Хранилище, бюджет и безопасность данных

**MariaDB** — источник истины новой версии. [`internal/storage`](../../internal/storage/) использует `database/sql`; транзакции и CAS обеспечивают сохранность и конкуренцию. [`dbschema`](../../internal/dbschema/) проверяет ожидаемые метаданные и совместимость; **не** следует выполнять `schema-init` над заполненной рабочей БД. Проверки MariaDB 10.3.32 намеренно отклоняют неподдерживаемую схему. [`internal/backup`](../../internal/backup/) и [`v39`](../../internal/v39/) содержат export/validate/restore с одноразовыми SQL/CI сценариями; production restore не принят только потому, что synthetic tests прошли.

**Монетарный учёт:** раздельно хранить подтверждённую стоимость (`cost_usd`/provider receipts), резерв и неопределённый исход. Provider-wide Apify usage нельзя прибавлять ещё раз к уже учтённым локальным расходам; кредиты Firecrawl не конвертировать в USD без доказанного тарифа. Наличие 167 старых резервов без `cost_usd` (если наблюдалось в отдельном snapshot) не равнозначно 167 оплачиваемым запросам. Числа старых диагностик не представлять как текущие. [GO-433](../../tasks/SPRINT-01.md) реализует частичную себестоимость; полная когортная атрибуция расходов не подтверждена.

**Принципы безопасности:** secrets/DSN, токены, реальные prompt/response, резюме и provider payload не публикуются в Git/CI/логах; неверная конфигурация должна завершаться отказом. Тесты используют синтетические данные, mock-провайдеры и одноразовую MariaDB. В старом Sites/D1 и новом MariaDB нет автоматической синхронизации.

## 5. Сборка, деплой и границы фактической эксплуатации

| Объект | Pipeline | Что реально доставляется |
| --- | --- | --- |
| **API + React SPA** | [`.github/workflows/publish.yml`](../../.github/workflows/publish.yml), [`deploy/publish_release.py`](../../deploy/publish_release.py), [`deploy/receive_release.py`](../../deploy/receive_release.py) | При включённой публикации только `./cmd/itmap` и `dist/web`, версия по commit SHA; receiver проводит собственные guards/smoke/rollback |
| **Worker и Recovery** | [`.github/workflows/worker-artifact.yml`](../../.github/workflows/worker-artifact.yml) | Собирает проверяемые `itmap-worker` и `itmap-recovery` для `linux/amd64` и `linux/arm64`, но **не устанавливает** их на сервер |
| **Отдельная служба Worker** | [`deploy/itmap-worker.service.example`](../../deploy/itmap-worker.service.example), [`deploy/WORKER-PRODUCTION.md`](../../deploy/WORKER-PRODUCTION.md) | Шаблон и правила запуска. Текущий installed SHA, владелец и время рестарта надо проверять непосредственно на сервере |

Таким образом, **успешный publish API не обновляет Worker**. Это не вопрос названия артефакта: `publish.yml` упаковывает другой исполняемый файл, а receiver перезапускает API. [GO-447 / Issue #472](../../tasks/GO-447-WORKER-DEPLOY-RESTART.md) отдельно требует доставку API и Worker из **одного** проверенного SHA, graceful drain, безопасный swap, health/ready/version verification, отказ от второго consumer и rollback. На этом срезе задача ещё **не реализована**.

Существуют отдельные `APP_ENV=production` и явные opt-in flags для Worker; их наличие в шаблоне не означает разрешение включить службу. Текущий исторический статус сервера, наличие старого таймера и установленная версия не выводятся из GitHub Actions. Не изменять production MariaDB, paid provider calls, systemd, секреты, бюджеты или worker restart без решения владельца.

## 6. Качество и источники инженерных доказательств

- **Go:** unit, `go vet`, race, сборка API/Worker, тесты по package и build tags.
- **Контракты и web:** `npm run typecheck:web`, `npm run format:check` (Prettier), React/Vite build, Node/source-compatibility, Playwright desktop/mobile с независимыми сценариями.
- **MariaDB:** [`mariadb-staging-ci.yml`](../../.github/workflows/mariadb-staging-ci.yml) на disposable DB, ошибки транзакций, crash/lease/attempt recovery, HTTP и интеграционные сценарии; отдельная [проверка MariaDB 10.3](../../.github/workflows/mariadb-103-ci.yml).
- **Публичная безопасность:** [`go-ci.yml`](../../.github/workflows/go-ci.yml) и `scripts/check-public-repo.py`.
- **Build artifacts:** отдельная матрица Worker/Recovery на `linux/amd64`, `linux/arm64` с `vcs.revision`/SHA-проверкой.

Четыре обязательных workflow PR #496 прошли на commit `51ffa71d`, а merge в `main` — `2e670273`. Это подтверждает тесты соответствующего PR, но не завершённый реальный Google OAuth или installed Worker. Более поздние PR проверяются отдельно на их собственном HEAD SHA.

## 7. Что осталось открытым

1. **GO-447:** автоматическая безопасная доставка Worker вместе с API, установленный SHA и процедура отказа/отката, без двух параллельных потребителей.
2. **GO-431:** cross-candidate durable identity платного AI-вызова, отдельная приёмка восстановления, неизвестных финансовых исходов, старых HH cursor и реальная операторская проверка.
3. **GO-015:** разрешённый Google Cloud OAuth и проверка однонаправленного sync/reconnect с тестовым аккаунтом; никакой фоновой синхронизации пока нет.
4. **GO-433/434/436:** полная провайдерская и когортная стоимость, реальный read-only аудит и историческая сверка числа уникальных вакансий.
5. **Полнота переноса:** оставшиеся legacy API, источники вне принятого HH-маршрута, настоящая процедура backup/restore/cutover. **Не** считать кодовый merge самостоятельным разрешением менять production.

## 8. Маршрут для следующего инженера

Начинать с [`AGENTS.md`](../../AGENTS.md), [реестра задач](../../tasks/README.md), [последнего handoff](handoff/README.md), [матрицы GO-119](GO-119-ARCHITECTURE-EVIDENCE.md). Затем сверять актуальный SHA `main`, код `cmd/itmap-worker` и `internal/workerengine`, CI на соответствующей версии, отдельно установленный Worker, сохранённые claims и денежные evidence. Старые проектные документы являются историческими при противоречии коду и актуальным задачам.

Полезные документы: [архитектура Worker](../../tasks/GO-431-CONTINUOUS-WORKER.md), [consumer fence](../../tasks/GO-431-CONSUMER-FENCE.md), [денежные инварианты](../../internal/parser/BILLING.md), [восстановление](../../deploy/WORKER-RECOVERY-DIAGNOSTICS.md), [эксплуатация Worker](../../deploy/WORKER-PRODUCTION.md), [Google Calendar](GO-015-GOOGLE-CALENDAR.md), [форматы MariaDB](MARIADB.md).
