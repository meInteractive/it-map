# IT-MAP: практический справочник диагностики парсера

**Дата составления:** 07.11.2026. **Назначение:** воспроизводимые методы, которые использовались при проверке живого HH/Apify-пилота, разборах MariaDB-очереди, расходов OpenAI/JEV и воронки публикаций. В документе нет закрытых данных аккаунтов, серверных адресов/секретов, реальных вакансий и полного вывода логов. При продолжении сравнивать с текущими `main`, [передачами дел](handoff/README.md) и [задачами спринта](../../tasks/SPRINT-01.md).

**Критически важно:** диагностировать **без изменения данных и без дополнительной оплаты**. Не использовать кнопки «Запустить», «Продолжить HH», «Повторить», «Отменить», «Проверить подключение», «Обновить статистику провайдера» или POST-маршруты как безопасные действия чтения: некоторые создают jobs, инициируют оплаченную операцию либо записывают снапшот. Запрещены реальный DDL, UPDATE/DELETE/INSERT, перезапуски служб и изменения systemd без отдельного разрешения. SQL на рабочей БД допустим только при выданном read-only доступе и явной цели.

## 1. Быстрая карта источников истины

| Вопрос | Где смотреть | Что это **не** доказывает |
|---|---|---|
| Жив ли сигнализатор? | «Обзор» → время последнего сигнала; `scheduler_runs`; при разрешении `systemctl list-timers/status` | Сигнал не доказывает, что запускался новый Actor или была публикация |
| Почему зависла обработка? | «Постоянная очередь» + «Журнал циклов и попыток»; `scheduler_jobs`, `scheduler_attempts` | `pending` не обязательно ошибка; `attempts=0` не обязательно не было deferred/оплаты |
| Создавался ли Actor повторно? | Кабинет Apify → run details/input и `apify_runs.operation_key/state/run_id` | Множественные GET Dataset не равны новым платным POST |
| Почему нет вакансий? | Actor Input/Dataset, сохранение кандидата, JEV, full text, extraction, roles, import receipts | Число результатов Dataset не равно новому количеству вакансий |
| Сколько реально потрачено? | У провайдера: фактическое usage; локально: `scheduler_attempts`, `apify_runs`, бюджетная панель | Верхняя оценка, содержащая резервы, **не** подтверждённые списания |
| Почему «Ошибки за 30 дней» / «Воронка» пусты? | DevTools → HTTP code/response, `internal/httpapi/handler.go` и `cmd/itmap/main.go` | Готовый React/JS-компонент не означает готовый Go endpoint |
| Должна ли быть новая публикация? | `vacancy_candidates.main_vacancy_id`, `vacancy_import_operations.action`, `company_vacancies`, provenance/identity | `processed`/ `succeeded` в журнале — **стадии**, не новые карточки |

**Граница достоверности:** `скриншот UI` → `структурированный JSON/API` → `read-only SQL` → `provider receipt` → `повторяемый тест на disposable DB`. Противоречия объяснять, а не выбирать удобную цифру. При выводах отдельно указывать «наблюдалось», «подтверждено кодом» и «гипотеза».

## 2. Метод «скриншот → временная ось → очередь → платная операция»

### Шаг 1. Зафиксировать состояние

1. Записать время **в Москве** (Europe/Moscow), временной диапазон, выбранные фильтры UI, момент снимка и SHA релиза, если он проверен. Не угадывать время из даты сообщения в другом часовом поясе.
2. «Автоматический сбор» → «Обзор»: статус диспетчера, `lastSignalAt`, `lastSuccessAt`, счётчики `pending/running`, затраты и резервы.
3. «Диагностика» → «Очередь и история»: отдельно **журнал циклов**, **журнал последних попыток** и **постоянная очередь**. Снимок окна из 10 строк не является полным журналом.
4. В очереди применять фильтры `status`, `type`, организацию и сохранять выбранную страницу/общее количество; сортировка текущего Go `/api/scheduler/queue` — `created_at DESC, id`, страница до 30 jobs. «Последние 10» пока не отдельная кнопка: это **планируется** ниже.
5. Перенести события в таблицу: `время тика → статус/processed/succeeded/errors/remaining → HH scan job → Actor/GET → classify → import`.

### Шаг 2. Правильно интерпретировать цифры

- `scheduler_runs.processed` — количество обработанных **заданий/стадий**, может учитывать deferred; `succeeded` — успешные стадии; `errors` — ошибки попыток. Ни одно поле не равно новым вакансиям.
- `remaining` — остаток доступной очереди по семантике конкретного run; он **не** доказывает число всех HH-сканирований и не равен сумме `scheduler_jobs.status='pending'` без сверки.
- `pending` значит ожидает; причина ожидания может быть `run_after`, ограничение rate/budget, неподтверждённая legacy-запись, lease или неготовность Actor. `deferred` не равен `failed`.
- `attempts=0/4` не гарантирует отсутствие предыдущего реального POST: платный Actor мог завершиться вне текущего цикла, а job снова отложен. Проверять `apify_runs` и `scheduler_attempts` перед любым «Повторить».
- Полная история `scheduler_attempts` и `scheduler_runs` на несколько тысяч строк **не** означает такой же backlog.

### Шаг 3. Сверить реальные ограничения

После GO-430 `internal/parser/worker.go` разделяет `launch` новых Actor, `reconcile` существующих и `pipeline` AI/импорта. Старый безпараметрический tick выполняет `balanced`, последовательно. Проверить **реальный installed** `systemd` timer: auto-deploy Go-бинарника не обновляет установленный `tick.py` или systemd unit. У Go пустой idempotency `requestId` теперь привязан к **15-минутному слоту** вместо часа. Обновлённый Go не доказывает, что на сервере включено более частое независимое расписание.

Признаки, которые надо исследовать отдельно:
- слишком частый `launch` новых Actor;
- повторный POST по одному `operation_key` (опасный дефект);
- много `reconcile`/GET уже оплаченного Actor (не то же самое, что новый POST);
- очередь кандидатов растёт, а `pipeline` занят;
- старые курсоры без подтверждённых Москва/HH professional roles не допускаются и мешают новым scan — этот admission mismatch исправлялся в GO-430;
- `systemd` сигнал есть, но `Worker.Cycle` возвращает `reused`, `scheduler_busy`, timeout или бюджетную остановку.

## 3. Apify: отличить дорогое повторное сканирование от дешёвого дочитывания

1. Открыть **Apify console → Actor runs** за интересующий период (не запускать новый Actor). Сопоставить `startedAt`, `status` (succeeded/failed/timed-out), фактическую стоимость, `dataset item count`.
2. Для повторов **одной компании** сравнить только безопасные параметры Input: `employer_id`, `area`, `professional_role`, `date_from/date_to`, page/limit, версию Actor. Не публиковать token, private params и dataset с персональными данными.
3. Проверить, есть ли два *новых* запуска Actor с одинаковым входом и близким временем, либо это один `run_id` с несколькими GET/reconcile. Ledger истины для запуска: `apify_runs`, ключ `operation_key` — фактический `go398:` identity, а не сырая строка `listing:...`.
4. При таймауте с непустым Dataset отдельно выяснить, сохранены ли **частичные** результаты и до какой страницы/кандидата дошёл Go cursor. Нельзя автоматически отправлять новый платный POST только из-за timeout/неполученного ACK.
5. Помнить специфический вывод пилота: внешний Actor иногда возвращал HH вакансии **вне** дат `date_from/date_to`. GO-432 намеренно оставил upstream-фильтр, но убрал **локальное отбрасывание по `date_posted`**; поле `OutsideDateWindow` диагностическое. Ошибку отбора источника/города или повторное размещение этим не доказать.

Критерий для реального дефекта дубликатов: одинаковая уникальная HH identity после повторного scan породила **новые платные AI-попытки** или **новую опубликованную карточку**, когда неизменные входы/версии должны были использовать тот же receipt. Это требует подтверждения candidate/AI lineage, а не визуального сходства Dataset.

## 4. Read-only SQL: безопасные примеры

Только с **уже выданным read-only доступом** в разрешённой среде, ограниченным периодом и объёмом; на disposable MariaDB запросы можно выполнять в CI. Не копировать реальные row payload, full-text, секреты, резюме. Колонки ниже соответствуют `db/testdata/empty-schema.sql` на дату документа; проверить схему перед запуском.

```sql
-- Общее число jobs по состоянию/стадии в текущем поколении.
SELECT j.type, j.status, COUNT(*) AS jobs
FROM scheduler_jobs j
JOIN app_runtime_generation g ON g.id = 1 AND g.generation = j.generation
GROUP BY j.type, j.status
ORDER BY j.type, j.status;

-- Последние 10 стадий с причиной, без payload и личных полей.
SELECT id, type, source_id, status, priority,
       run_after, attempts, max_attempts, reason_code,
       created_at, updated_at
FROM scheduler_jobs
WHERE generation = (SELECT generation FROM app_runtime_generation WHERE id = 1)
ORDER BY created_at DESC, BINARY id DESC
LIMIT 10;

-- Последние 10 циклов: processed/succeeded НЕ количество вакансий.
SELECT id, `trigger`, started_at, finished_at, status,
       ready, processed, succeeded, errors, remaining, reason
FROM scheduler_runs
WHERE generation = (SELECT generation FROM app_runtime_generation WHERE id = 1)
ORDER BY started_at DESC, BINARY id DESC
LIMIT 10;

-- Сводка сумм по этапам и провайдерам.
-- cost_usd IS NULL => неизвестная подтверждённая стоимость.
SELECT type, provider, model, status,
       COUNT(*) AS attempts,
       SUM(CASE WHEN cost_usd IS NOT NULL THEN cost_usd ELSE 0 END) AS confirmed_usd,
       SUM(CASE WHEN cost_usd IS NULL THEN reserved_usd ELSE 0 END) AS unknown_reserved_usd,
       SUM(CASE WHEN cost_usd IS NULL THEN 1 ELSE 0 END) AS unknown_cost_operations
FROM scheduler_attempts
WHERE started_at >= ? AND started_at < ?
GROUP BY type, provider, model, status
ORDER BY type, provider, model, status;

-- Последние 10 операций Apify, без account_id/key_hash/run_id/dataset_id.
SELECT state, profile, created_at, updated_at,
       reserved_usd, charged_usd, error_code
FROM apify_runs
ORDER BY created_at DESC, BINARY id DESC
LIMIT 10;

-- Создание новой вакансии vs связь с существующей.
SELECT action, status, COUNT(*) AS operations
FROM vacancy_import_operations
WHERE processed_at >= ? AND processed_at < ?
GROUP BY action, status
ORDER BY action, status;
```

**Примечания:** `scheduler_* .started_at` и `apify_runs.created_at` — epoch **миллисекунды**; `vacancy_import_operations.processed_at` — текстовая дата, поэтому параметры периода задавать в формате этой колонки и сверять UTC/МСК. `SUM(cost_usd)` отдельно от резервов — только по ledger попыток; при суммировании с `apify_runs` и внешним кабинетом не учитывать одну платную операцию несколько раз. Для диагностики **повторов кандидатов** использовать ограниченные агрегаты/ключи identity, не выводить `title_raw`, `vacancy_url` или `payload` в чат. SQL-проверка не заменяет сверку прав и исходных данных.

### Проверка identity, кандидатов и публикаций

- `vacancy_candidate_identities.identity_key` → `candidate_id` — каноническое сопоставление для повторных HH ID/URL; запросить количество уникальных candidates и вновь появившихся identity, а не только длину Dataset.
- `vacancy_candidates.main_vacancy_id` и `vacancy_import_operations.action/status` — отличают новое создание от связи/обновления. Связь одного кандидата с существующей карточкой — не новая публикация.
- `vacancy_classification_attempts` и `scheduler_attempts` показывают, не прошло ли одно и то же название/ревизия повторный JEV; для дорогого LLM нужны модель, snapshot, hash входа, успешность/резерв. Не объявлять любое второе обращение ошибкой — вход мог измениться.
- AI-расход считать по периоду и методу группировки, отличать «все операции за день» от «затраты когорты, породившей N публикаций». Полную себестоимость одной публикации отдельно описывает GO-433.

## 5. Бюджеты: четыре разных числа

Проверяем по отдельности:
1. **Provider actual spend** (реальный кабинет/снимок провайдера), может включать другие приложения аккаунта.
2. **Our confirmed cost** (`cost_usd`/`charged_usd` по нашим подтверждённым операциям).
3. **Unknown reserved** (`reserved_usd` при неизвестной стоимости, плюс подтверждённый механизм read reserve).
4. **Conservative upper estimate** (верхняя оценка в UI с резервами, **не** фактическое списание).

Не складывать провайдерское usage и подтверждённые собственные расходы дважды, не считать резерв списанием, не переводить Firecrawl credits в доллары без тарифа и периода. `internal/parser/dashboard_spending.go` агрегирует историю независимо от поколения; неожиданный restore не обязан обнулять денежную историю. Чтобы найти главный источник расходов, смотреть breakdown **по провайдеру, модели и стадии**, а не только по JEV/LLM одной цифрой.

## 6. Go API, React и браузерная диагностика

- Основной фронтенд: `frontend/features/settings/Scheduler.tsx`, `ParserIssues.tsx`, `ParserErrorHistory.tsx`, `ParserFunnel.tsx`, `Apify.tsx`, `Firecrawl.tsx`; backend: `internal/httpapi/handler.go`, `cmd/itmap/main.go`, `internal/parser/dashboard.go`.
- Проверить вкладку **Network** в браузере (авторизованная админ-сессия): URL, метод, HTTP статус, JSON `error.code`/несекретные поля, время, фильтры, кэш. **Не копировать Request Headers с Cookie/Bearer, тела приватных POST или raw response с документами**.
- `GET /api/scheduler` отвечает за summary, runs/attempts и Spending; `GET /api/scheduler/queue` — пагинированную очередь. Реализованный `GET /api/apify/usage` выдаёт сохранённый snapshot без запросов к провайдеру. Право и фактический статус проверять по текущему роутеру.
- На 07.11.2026 `GET /api/scheduler/issues` и `GET /api/scheduler/funnel` **не зарегистрированы** в текущем Go `internal/httpapi/handler.go`; общий fallback возвращает `503 backend_not_migrated`. Это задачи GO-107 и GO-108 в **спринте 1.1**. Существующий JS сервис и готовая React-карточка не означают, что Go уже выдаёт правильные значения.
- Общий startup readiness может возвращать 503 по незавершённому переносу бизнес-API, даже если parser API функционирует. Нельзя считать `ready503` прямым доказательством сбоя парсера.

## 7. Проверка сервера и CI, если оператор разрешил

**Только чтение**, не включать сервисы и не запускать платные обработчики:

```bash
# Выполняется уполномоченным оператором на сервере.
systemctl status itmap-scheduler-main.timer --no-pager
systemctl list-timers --all --no-pager
systemctl cat itmap-scheduler-main.timer
systemctl status itmap@main.service --no-pager
# Последние записи журнала могут содержать закрытые значения.
# Делать локальную очистку/агрегацию прежде, чем передавать лог кому-либо.
```

Проверить, что настоящее `OnCalendar` совпадает с ожидаемым, auto-deploy обновил Go binary, а установленный `tick.py`/unit — **не обязательно**. Для сценариев независимых `launch/reconcile/pipeline` предусмотрен `deploy/scheduler/README.md`; **не включать** дополнительные таймеры одновременно с прежним без согласованного cutover.

Для PR: точный `HEAD SHA`, статусы GitHub checks; `gofmt`, `go vet ./...`, `go test -race ./...`, `go build ./cmd/itmap`, `npm run build:web`, соответствующие Node и Playwright тесты, disposable MariaDB integration. Нельзя изменять ожидаемый результат теста, пока не разобрана причина падения; не считать CI с пропущенными критическими тестами успешной приёмкой.

## 8. Типовые диагнозы и следующие проверки

| Наблюдение | Первое действие | Чего **не** делать |
|---|---|---|
| Есть циклы, новых Actor нет | Проверить допустимые `scan_source`, фазу `launch`, `run_after`, rate, бюджет и `apify_runs` | Не вручную повторять платный POST |
| Actor завершился, кандидаты не появились | Проверить `reconcile`, dataset cursor, lease, `vacancy_candidates`, `scheduler_attempts` | Не очищать cursor/ledger |
| В очереди мало jobs, цикл «обработал много» | Разделить число processed stages и количество уникальных кандидатов/импортов | Не считать каждую стадию вакансией |
| Actor вернул старые даты | Сопоставить Input и Dataset, применённый GO-432, `OutsideDateWindow` | Не объявлять старые вакансии невалидными автоматически |
| Публикаций мало, стоимость LLM велика | Отдельно actual/reserve; проверить retry, обновление prompt/model/snapshot и число AI attempts | Не делать вывод из верхней оценки на 1 карточку |
| 30-дневная история равна нулю | Проверить есть ли реальные снимки, число observed days и status API | Не заменять отсутствующий день 0 |
| Воронка больше числа карточек | Разделить candidates, stage attempts, create, link/update | Не складывать обновления в новые публикации |

## 9. Планируется: кнопки «Копировать JSON» (ПОКА НЕ РЕАЛИЗОВАНО)

**Статус на 07.11.2026: только спецификация.** Кнопки не существуют в работающем Go/React. Полные задачи лежат в [`tasks/sprint-01-subtasks/README.md`](../../tasks/sprint-01-subtasks/README.md) и отдельных `PARSER-JSON-01…05.md`:

- `S11-DIAG-01`: общий безопасный React serializer/button, schema version, allowlist, ограничения списков, admin auth.
- `S11-DIAG-02`: JSON обзора, очереди, последних 10 циклов + последних 10 попыток.
- `S11-DIAG-03`: JEV/LLM confirmed/reserved, Apify `apify_runs`, Firecrawl credits, provider snapshots.
- `S11-DIAG-04`: GO-107 P1/P2 и GO-108 funnel, **все 30 агрегированных дней**.
- `S11-DIAG-05`: единая кнопка «Копировать всю диагностику» с максимумом 10 событий **в каждом списке**, без секретов; все 30 дневных агрегатов остаются.

Плановый JSON: `schemaVersion`, `section`, `capturedAt`, `timezone`, `filters`, `summary`, `recentItems {total,returned,limit,truncated,items}`, `dataAsOf`. В недоступном разделе `available:false,errorCode`; неизвестные суммы `null`, а не ноль. Сортировка событий детерминированная по времени и ID, охват `limit=10` берётся из всей отфильтрованной истории, **не из текущей страницы**. Никаких сырых `result_json`, вакансий, текстов промптов или ключей.

**После фактического merge каждой задачи** перенести соответствующий пункт из «Планируется» в «Реализовано» с PR, SHA, тестами и контрактом JSON. Не менять статус на основании документации.

## 10. Что сообщать следующему чату

Минимальная запись по инциденту:

- Москва, абсолютное время и диапазон; `main`/runtime SHA (если известен).
- Что именно сломалось, число фактов vs скриншот, выбранные фильтры.
- Статусы последних циклов, задачи `jobId`/типы, `reason_code`, без payload.
- Оплаченный Actor `status` и `charged_usd` vs local reserve; при возможности — уникальный safe run reference без приватного input.
- Одна подтверждённая гипотеза, одна альтернативная и следующий **read-only** способ проверки.
- Какой PR/commit уже исправил часть проблемы, а что осталось непроверенным.

Сначала искать свежий [handoff](handoff/README.md), читать актуальные [задачи 1.1](../../tasks/SPRINT-01.md), сверять HTTP/Go-код и реальные права. Справочник методов не заменяет текущую инженерную диагностику и не разрешает автоматические production-операции.
