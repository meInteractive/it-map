# GO-434. Аудит расходов парсера: источники, повторы и границы достоверности

**Дата:** 8 ноября 2026 г.  
**Статус:** аудит исходного кода и публичной синтетической схемы. **Не является проверкой фактических расходов аккаунтов**: production, кабинеты провайдеров и рабочие БД не читались. Никаких сумм из личных аккаунтов в этом отчёте нет. Код парсера, настройки лимитов, фоновые задания и схемы БД не изменяются.

## 1. Что считать оплаченной единицей

**Apify:** платная единица — фактический Actor run в конкретном аккаунте; для локального сопоставления предпочтительна пара `(account_id, run_id)`. Запись `apify_runs.id` — *локальная операция/резерв*, не доказательство POST или списания. `dataset_id` — идентификатор набора данных, **не** отдельный Actor invoice ID. Успешный GET набора данных не равен новому Actor POST и может иметь отдельную стоимость, не доказанную журналом Actor.

**ИИ через scheduler:** подтверждённая локальная единица — строка `scheduler_attempts.id`, а **не** строка результата классификации, кандидата или вакансии. Один attempt может охватывать несколько вызовов модели: `Attempt.Run` фиксирует сумму в `reserved_usd`, `FinishWithin` переносит известное значение в `cost_usd`. Поэтому даже один attempt нельзя автоматически приравнивать к одному внешнему POST. `scheduler_attempts.run_id` — идентификатор **цикла планировщика**; `apify_runs.run_id` — **внешний Actor**. Совпадение значений не создаёт JOIN.

**Этапные ИИ-записи:** `vacancy_classification_attempts.cost_usd`, `vacancy_extractions.cost_usd` и `vacancy_role_results.cost_usd` — вспомогательные свидетельства затрат и результатов. Пока нет устойчивой связи каждой записи с конкретным платным provider request, складывать их с `scheduler_attempts.cost_usd` **запрещено**.

**Firecrawl:** `vacancy_raw_snapshots.firecrawl_credits` выражает наблюдаемые кредиты снимка. `firecrawl_settings.free_usage_json/hobby_usage_json` — снимки состояния аккаунта/плана, а не журнал денежных операций. `firecrawl_request_lease` — блокировка, не расходный ledger. **Кредиты не переводим в USD без подтверждённого тарифа и периода**.

## 2. Матрица наблюдаемости

| Источник | Первичный ключ / область | Подтверждение суммы | Неизвестное / ограничения |
| --- | --- | --- | --- |
| `apify_runs` | `id`; `account_id,run_id` для внешнего Actor, `operation_key,generation` для локального повторного запроса | `charged_usd IS NOT NULL` — локально зафиксированная сумма из `usageTotalUsd`, в том числе ноль | `reserved_usd` не списание; `NULL charged_usd` не ноль; отсутствие `run_id` означает невозможность GET reconciliation; дополнительная стоимость Dataset GET не подтверждена |
| `apify_account_usage` | `account_id,period_start,period_end` | `provider_used_usd` — сохранённый high-water счётчик аккаунта | Включает внешнее использование аккаунта; не складывать с `apify_runs`; не распределять между вакансиями |
| `scheduler_attempts` | `id`, `job_id`, `run_id` (цикл), `provider`, `type` | `cost_usd IS NOT NULL` — локально рассчитанный известный расход attempt, включая 0 | `cost_usd=NULL, reserved_usd>0` — неопределённый исход/резерв; оба 0/NULL — неизвестная стоимость, пока не доказана бесплатность; provider invoice может отсутствовать |
| `vacancy_classification_attempts` | `id`, `candidate_id`, `title_hash/revision`, `model_key` | `cost_usd` — этапное свидетельство | `vacancy_classifications.attempt_id` ссылается на **classification attempt**, не `scheduler_attempts.id`; не суммировать с scheduler |
| `vacancy_extractions` | `id`, `candidate_id`, `snapshot_id` | `cost_usd`, токены | Нет всеобщего provider request ID; `NULL` не равно нулю; повторное извлечение/ошибка могут быть оплачены |
| `vacancy_role_results` | `id`, `candidate_id`, `snapshot_id` | `cost_usd`, токены | Результат может быть reused; прямой денежный JOIN с scheduler не доказан |
| `openrouter_ai_logs` | `id`, `task`, `model`, `success` | Факт/статус технической попытки | **Нет USD/usage/provider request ID**; записи нельзя монетизировать; Go cover-letter логирует перед POST отдельно от scheduler |
| `vacancy_raw_snapshots` | `id`, `candidate_id`, `firecrawl_mode` | `firecrawl_credits` (кредиты, не USD) | Снимок/кеш ≠ всегда отдельный платный HTTP-вызов; `NULL` неизвестно |
| `firecrawl_settings` | `id=1`, usage JSON по профилю | Снимок кредитного баланса | Не снабжает факт потребления каждого запроса идентификатором и ценой; нельзя брать цену сегодняшнего тарифа для истории |

Все даты и суммы сверяются **по собственной области ledger**. `scheduler_attempts.started_at`, `apify_runs.created_at/updated_at` — миллисекунды Unix. `vacancy_import_operations.processed_at` и время этапных результатов — строки даты, без гарантии совпадения с датой денежного события. Московские сутки в dashboard и локальный период биллинга Apify — **разные окна**.

## 3. Lineage: доказанные и недостающие рёбра

Существуют следующие подтверждаемые по SQL связи (использовать LEFT JOIN, предварительно проверив кратность):

- `scheduler_attempts.job_id → scheduler_jobs.id → scheduler_jobs.candidate_id → vacancy_candidates.id`; от кандидата возможен переход к `main_vacancy_id` и `company_vacancies.id`.
- `scheduler_attempts.run_id → scheduler_runs.id` относится **только к scheduler**.
- `vacancy_candidates.creation_token → parser_candidate_intakes.candidate_token`: источник/метод первоначального поступления. Он может отсутствовать для исторических строк.
- `vacancy_import_operations.candidate_id / vacancy_id` и `vacancy_publication_sources.candidate_id / vacancy_id` дают ссылки на результат импорта; `vacancy_publication_sources` уникален по `candidate_id`, **не по `vacancy_id`**.
- `vacancy_classifications.attempt_id → vacancy_classification_attempts.id`; `vacancy_extractions.snapshot_id → vacancy_raw_snapshots.id`; `vacancy_role_results.snapshot_id → vacancy_raw_snapshots.id`.

**Пробелы:** нет универсального сохраняемого ключа `apify_runs → scheduler job/run → candidate` для расчёта оплаченного Actor по найденным вакансиям; нет полного сквозного identity `provider HTTP request → scheduler attempt → stage receipt` для всех моделей; нет авторитетного денежного ledger Firecrawl по отдельным запросам. Кандидат, привязанный к существующей вакансии, не создаёт новую публикацию. Один Actor, многократно прочитанный через Dataset или породивший несколько кандидатов, не создаёт несколько оплаченных Actor.

## 4. Подтверждённые по коду риски (не утверждения о реальном перерасходе)

1. **Подмена расхода резервом в общей статистике.** `internal/parser/dashboard_spending.go` вычисляет `usd = cost_usd` при известной стоимости, иначе `reserved_usd`. Это корректно для консервативного *бюджетного использования*, но поле `monthUsd/dayUsd` нельзя публиковать как «фактически потрачено». Для GO-433 строго разделить confirmed / unknown / unpriced. `internal/parser/ai_spending_debug.go` уже разделяет эти состояния.
2. **Запаздывающая тарификация Apify Dataset.** `internal/apifyruns/record.go` фиксирует `usageTotalUsd` по Actor, но `internal/apifyruns/dataset.go` выполняет Dataset GET без отдельного receipts по чтениям. Текущая `updated_at` Actor не подтверждает отсутствие более поздних платных чтений. `apify_account_usage.provider_used_usd` — верхний снимок аккаунта, не распределяемый по операциям; см. `docs/dev/APIFY_RESERVE_INVESTIGATION.md`.
3. **Слепые POST после неизвестного исхода должны оставаться запрещены.** `internal/parser/attempts.go` намеренно сохраняет положительный резерв после ошибки/потери ACK, а `internal/apifyruns/service.go` отказывает в новом Actor POST для `starting/uncertain` без подтверждённого `run_id`. Не отменять это поведение ради экономии.
4. **Повторные JEV/LLM попытки видны не полностью.** Повторы `scheduler_attempts` одного `job_id` и классификаций одного `candidate_id/title_hash/model_key` — *кандидаты на проверку*. Без provider request IDs нельзя доказать, что это два платных POST, а не reused, повторное чтение или разные группы/версии промта.
5. **Нет достоверного денежного Firecrawl.** Кредиты можно показать отдельно, но денежная себестоимость остаётся частичной. Нулевой `cost_usd` без подтверждённой модели тарификации не выводится из отсутствия записей.
6. **Опасное размножение строк через JOIN.** Если связать оплаченный Actor или scheduler attempt с несколькими кандидатами, ролями или источниками, `SUM(cost_usd)` на результирующем JOIN завысит расход. Агрегировать денежные строки **до** присоединения происхождения и не приписывать весь Actor каждому потомку.

## 5. Правило сверки и классификация повторов

Набор SELECT-only запросов: [`GO-434-EXPENSE-AUDIT-QUERIES.sql`](./GO-434-EXPENSE-AUDIT-QUERIES.sql). При ручной проверке сначала использовать авторизованный read-only снимок/реплику. Не запускать Actor/LLM и не выполнять миграции. Результаты хранить приватно, а не в Git.

- Для Apify группировать фактические **оплаченные** Actor по `(account_id,run_id)`; несколько локальных receipts одной группы считать одной операцией. При конфликтующих суммах не брать произвольный максимум как проверенный счёт — пометить конфликт и сверить с провайдером. Если `run_id` отсутствует, не угадывать внешнюю идентичность по Dataset/URL.
- Для ИИ исходной единицей локального итога служит `scheduler_attempts.id`. `cost_usd` известное, `NULL + positive reserve` неизвестное, `NULL + zero reserve` неоценённое. Не прибавлять этапные таблицы, системные логи и счётчик Apify account к этим суммам.
- **Дубликат сканирования**: один `account_id,run_id` и несколько receipts → подтверждаем повтор *учёта*, но не дополнительную оплату; разные Actor IDs при одинаковом input → потенциальный новый платный запуск, только после сверки identity/source-revision.
- **Повтор JEV/LLM**: одинаковые job/candidate/title/model и несколько attempts → гипотеза; для оплаты нужны usage, разрешение provider и сведения об отсутствии cache/reuse. Ошибка с известной стоимостью — расход, даже без вакансии.
- **Unknown ACK / reserved**: нельзя классифицировать как списание или освобождать без подтверждения. Отдельный потолок риска. Отсутствие `cost_usd` не доказывает бесплатность.
- **Firecrawl**: отображать кредиты и unknown credit rows отдельно от USD; не приписывать стоимость снимка нескольким кандидатам или исходя из текущего тарифа.

Тестовый помощник `expenseAuditReconcile` не подключён к production: проверяет корректное схлопывание одного Actor и нескольких receipts, оплаченные пустые/ошибочные/отсеянные сценарии, нулевую подтверждённую стоимость, неизвестный резерв, отсутствие тарифа, credit-only Firecrawl, разные аккаунты и несовместимые суммы. При наличии одновременно Apify и scheduler receipts с `provider='apify'`, но без общего внешнего identity, выставляется `OverlapUnresolved`: числовой итог **не становится подтверждённой общей себестоимостью**.

## 6. Подготовка GO-433

**Достаточно достоверно уже сейчас:** отдельно публиковать подтверждённую *локальную* сумму Apify Actor по уникальному provider run ID, суммы известных `scheduler_attempts.cost_usd` для OpenAI/OpenRouter (не складывая этапные дубликаты), количество и сумму **неопределённых резервов**, количество операций без цены, наблюдённые кредиты Firecrawl и покрытия lineage. Показать, что это **частичный учёт, требующий provider reconciliation**.

**Знаменатель:** `COUNT(DISTINCT vacancy_import_operations.vacancy_id)` с `status='success' AND action='create'`, подтверждённым существованием `company_vacancies.id` и верной областью запуска. `update/unchanged`, количество `vacancy_candidates`, `scheduler_runs.succeeded` и график `daily.published` из GO-436 не являются числом новых вакансий. До разрешения GO-436 не подменять эти определения.

**Окно анализа:** предпочтительно когорта сканирования и её последующие стадии, а не совпадение суток расхода и публикации. При позднем импорте сохранять исходную когорту Actor/сканирования. Текущая схема не обеспечивает однозначную привязку *всех* денежных операций к такой когорте, поэтому необъяснённые расходы помечать «не распределено», а неполную себестоимость — «нижняя наблюдаемая граница, не полная стоимость». При нуле новых публикаций стоимость на единицу = **не определена**; затраты показывать.

**Минимальные дополнительные события для будущей полной метрики**, отдельным согласованным PR: durable provider operation ID для Dataset GET/Firecrawl и ИИ (с фиксируемым фактическим usage, статусом, тарифом/валютой и временем), явная связь `external operation → scheduler job/source/candidate/import` с учетом many-to-many, отдельные подтверждения от провайдера и уникальные платежные identities. Не менять работающий HH-парсер и лимиты в GO-434.

## 7. Проверка/приёмка

- `go test -race ./internal/parser -run '^TestExpenseAudit' -count=1` — чистые синтетические данные без сети/БД.
- `TestMariaDBSchedulerAISpendingGO434ReadOnlyDisposable` в имеющемся защищённом MariaDB CI: запускает **только SELECT** из SQL-файла в read-only транзакции и inline fixture с одним Actor в двух receipts, пустым платным Actor и несколькими импортами одной вакансии; проверяет нулевую мутацию БД.
- Все три обязательных GitHub workflow проверяются на точном HEAD PR.
- **Непроверено**: совпадение локальных сумм с реальными счетами Apify/OpenRouter/OpenAI, возможные доплаты Dataset, денежный тариф Firecrawl и истинные paid-repeat доли. Они требуют отдельного разрешённого read-only доступа и сверки, **без** реальных POST.

**Итог:** технический аудит и инструменты воспроизведения подготовлены. Утверждать «сколько действительно потрачено и сколько переплачено» до независимой сверки provider ledger нельзя; GO-433 разрешён только в режиме честно обозначенной частичной стоимости.
