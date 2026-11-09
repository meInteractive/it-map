> **DDL-прототип:** [GO-433-ISOLATED-SCHEMA-PROPOSAL.md](./GO-433-ISOLATED-SCHEMA-PROPOSAL.md). Пять таблиц тестируются только на одноразовой MariaDB; применять к v1-базе до отдельного schema-policy v2 нельзя.

# GO-433 — контракт сквозной связи расходов с когортами сканирования

**Статус на 9 ноября 2026 г.:** подготовка следующей миграции, **НЕ** включённая в runtime/production. Контракт проверяется синтетическими Go-тестами без сети и SQL. Действующая статистика остаётся частичным календарным ориентиром из [GO-433-PARTIAL-UNIT-COST.md](./GO-433-PARTIAL-UNIT-COST.md). Реальные счета провайдеров и GO-436 ещё не сверены.

## 1. Почему текущую схему нельзя использовать для окончательной себестоимости

Проверено в Go-коде и `db/testdata/empty-schema.sql`:

- `apify_runs` содержит `generation`, `operation_key`, локальный `id`, `account_id`, подтверждаемый Actor `run_id`, `dataset_id`, резерв и `charged_usd`. **Нет** надёжного поля `scheduler_job_id` или `discovery_cohort_id`. Actor может быть использован повторно без второго POST.
- `scheduler_attempts.job_id` связывается с `scheduler_jobs.id`; `scheduler_attempts.run_id` — это **цикл планировщика**, не внешний Actor и не ID LLM POST. Один attempt может включать несколько провайдерских запросов.
- `HHListingExecutor` сохраняет `RunID`, `DatasetID` и ограниченный список `CandidateIDs` в курсоре `vacancy_discovery_runs.result_json` при продвижении страниц. Однако `vacancy_discovery_runs.expires_at` ограничивает время жизни этого состояния. Оно может восстановить **текущую** операцию, но не является постоянной финансовой цепочкой. Нельзя строить финансовую историю по текущему курсору или считать его доказательством provenance после истечения.
- `parser_candidate_intakes` фиксирует исходный `candidate_token`, `parser`, `source_id`, `received_at`, но не Actor и не scan job; `vacancy_candidates.main_vacancy_id` — текущее состояние, не неизменное событие создания.
- `vacancy_import_operations` даёт историческое действие (`create/update/unchanged`) и основную вакансию, однако без точного события обнаружения нельзя доказать, какой Actor оплатил именно это создание.
- `openrouter_ai_logs` не хранит полный provider request ID/usage/сумму, а `vacancy_raw_snapshots.firecrawl_credits` — наблюдаемые кредиты, **не** денежная стоимость. Dataset GET может стоить дополнительно к Actor, но не имеет отдельного receipt.

Следовательно, `JOIN apify_runs.run_id=scheduler_attempts.run_id`, поиск по URL, `dataset_id`, названию вакансии или близости timestamp **запрещены** для денежной атрибуции. Старые записи без доказуемой связи должны сохранять статус «не распределено».

## 2. Какие идентификаторы нужны

**Обязательные области и единицы:**

| Сущность | Идентификатор | Не подменять |
|---|---|---|
| Цикл диспетчера | `scheduler_runs.id` | не scan cohort и не Actor |
| Задание скана | `scheduler_jobs.id` + `generation` | уникальное выполнение Actor, которое может повторно использовать старый результат |
| Фактический внешний Actor | (`provider=apify_actor`, `billing_account_scope`, `actor_run_id`) | `apify_runs.id`, `dataset_id`, URL |
| Локальная денежная операция | `apify_runs.id` или `scheduler_attempts.id` + явный тип ledger | не гарантирует 1 платный HTTP POST |
| Внешний платный LLM-запрос | `provider_request_id`, область аккаунта, модель/тариф на момент события | `scheduler_attempts.id`, попытка классификации роли |
| Отдельное платное чтение Dataset | отдельный provider usage/receipt ID + счёт/тариф | Actor run ID без отдельного подтверждения |
| Firecrawl | provider request/usage ID + кредитное и денежное свидетельство | snapshot ID или число кредитов |
| Когорта сканирования | неизменяемый `scan_cohort_id` + `generation`, `scan_job_id`, `source_revision`, `started_at` | дата публикации, текущий company/source |
| Событие обнаружения | `discovery_event_id` или устойчивые (`cohort_id`, Actor ID, Dataset ID, item index) + `candidate_id` | текущий `main_vacancy_id` |
| Результат импорта | `vacancy_import_operations.id`, `candidate_id`, `vacancy_id`, `action`, `processed_at` | количество кандидатов или ссылок |

Историческая смена `generation` не даёт права переопределять существующую внешнюю операцию. Повторно полученное задание может ссылаться на тот же внешний Actor **без нового списания**. Идентичность счёта обязана включать область billing-аккаунта.

## 3. План таблиц (только для отдельного миграционного PR)

Ниже **логическая спецификация**, не SQL для выполнения. Не применять к production и не менять существующие `apify_runs` или `scheduler_attempts` до отдельного migration review.

### 3.1 `cost_scan_cohorts` (одно событие постановки исходного скана)

- PK: opaque `cohort_id`;
- immutable: `generation`, `scan_job_id`, `source_id`, `source_revision`, `company_id/agency_id`, `started_at_ms`, `origin` (`new_scan` или `reuse_existing_actor`);
- UNIQUE `(generation,scan_job_id)` **только если** один job соответствует ровно одной когорте; для возобновлённого скана отдельная попытка не должна порождать вторую когорту;
- не сохранять URL, названия вакансий или токены в финансовый журнал;
- вход только из доверенного серверного worker при фиксированной lease, не из body HTTP/AI-output.

### 3.2 `cost_provider_operations` (провайдерская платная единица, не строка кандидата)

- PK opaque `operation_id`; immutable `provider_kind`, `billing_scope`, `provider_operation_id` (nullable до подтверждения), `source_ledger`, `source_receipt_id`, `created_at_ms`;
- UNIQUE `(source_ledger,source_receipt_id)` для локальных чеков, а для подтверждённой внешней операции UNIQUE `(provider_kind,billing_scope,provider_operation_id)` с NULL до подтверждения. Учесть в MariaDB semantics NULL в уникальных индексах и не выводить ещё один Actor при unknown ACK;
- append-only `cost_operation_settlements` или fenced обновление с сохранением истории подтверждения: `cost_usd`, `currency`, `usage`, `credit_units`, `price_version`, `observed_at_ms`, `provider_verified`, `settlement_source` и `status`;
- `reserved_usd` хранить **отдельно** от settled charge. `NULL charge` ≠ 0; `provider_verified` не должен выставляться по локальному тарифу без независимого чека;
- разные `provider_kind`: `apify_actor`, `apify_dataset_read`, `ai_provider_request`, `firecrawl_request`; `ai_attempt` — только локальная диагностическая запись, **не** провайдерская квитанция;
- raw API keys, prompts, response bodies, персональные данные и финансовые идентификаторы аккаунта **не включать в JSON ответа UI**. В серверной БД — только необходимый billing scope.

### 3.3 `cost_operation_cohort_links` (многие ко многим, не денежный множитель)

- FK `operation_id`, `cohort_id`, `relation` (`initiated_by`, `reused_in`, `observed_from`), `captured_at_ms`, `evidence_source`;
- UNIQUE `(operation_id,cohort_id,relation)`. Повторное сохранение страницы Dataset создаёт **не более одного** линка;
- `captured_at_operation=true` только при атомарной вставке trusted server code. Поздние догадки по URL/датам = `unverified`, не для денежного распределения;
- если один Actor связан с несколькими когортами, денежная сумма **остаётся shared/unallocated**. Не суммировать её в каждой когорте, не выбирать наиболее раннюю или «ближайшую по времени» без явной утверждённой политики.

### 3.4 `cost_discovery_events` (точное происхождение конкретного кандидата)

- PK opaque event ID, FK `cohort_id`, `operation_id` (для Actor), `candidate_id`, `dataset_item_key`, `captured_at_ms`, `result` (`created`, `existing`, `rejected`);
- stable unique per (`cohort_id`, `operation_id`, `dataset_id`, item index) **с нормализованными не-NULL ключами**, иначе у MySQL/MariaDB несколько NULL нарушат смысл уникальности;
- запись в **той же транзакции**, что `persistHHListingCursor` и `vacancy_candidates`. Если checkpoint курсора откатился, событие обнаружения тоже откатывается;
- частичный скан и повторное чтение старого Dataset не дают новых платных операций. Один кандидат может встречаться в нескольких событиях, но в основной карточке остаётся один уникальный ID.

### 3.5 Импорт и счётчик новых вакансий

Не дублировать всю таблицу `vacancy_import_operations`: использовать её `id` с конкретным `candidate_id` и `vacancy_id` + доказанное `cost_discovery_events`. Перед финальной агрегацией выбрать **первый успешный `create` на весь исторический `vacancy_id`**, подтвердить наличие строки `company_vacancies`, исключить manual/Excel. Сверить semantics с GO-436: `daily.published` означает другое.

Возможное позднее создание основной вакансии не переносит расход на дату публикации. Отчёт по когорте фиксирует дату скана и может отражать создавшиеся позднее вакансии с `as_of` и статусом зрелости. Для полностью сопоставимых когорт отчёт обязан различать `in_progress`, `settled`, `unresolved` и ретроспективные изменения.

## 4. Где сохранять события

1. **Apify Reserve / Start**: после fenced reservation сохранять **локальную** финансовую identity и ссылку на scan cohort. Состояние `starting/uncertain` никогда не даёт права второго POST. Событие с run ID сохранять только после успешной верификации и COMMIT `Repository.record`. При восстановлении выполнять только GET, без нового Actor.
2. **`HHListingExecutor`**: после подтверждения Actor/metadata продвинуть курсор и записать link `operation→cohort` с доказанным run identity; в одной транзакции с кандидатом/курсорным checkpoint сохранять page/item discovery event. Атомарность важнее показателя стоимости; ошибка записи lineage должна откатывать checkpoint, а не терять страницу.
3. **ИИ**: `StartAttempt` создаёт лишь локальную запись. У провайдерского вызова отдельно сохранить durable `provider_request_id`, actual usage/response и модель в подтверждённой цене. Многократные HTTP запросы внутри одного attempt — отдельные provider operations. При ошибке/потере ACK оставить неопределённость; **не** создавать synthetic paid receipt.
4. **Firecrawl**: сохранять кредитное usage и отдельную цену по подтверждённому plan/tariff на момент вызова, либо оставить USD unknown. Не реконструировать из сегодняшнего тарифа или существующего snapshot ID.
5. **Сверка**: отдельно забрать подтверждённый ledger провайдера или официальную квитанцию read-only, не ретраить платные POST. Любой конфликт оценок сохраняется как ошибка сверки, а не исправляется `MAX` или `SUM`.

## 5. Правила будущей когортной метрики

**Числитель:** сумма **проверенных и однозначно связанных с одной когортой** платных provider operations. Расходы с конфликтующими ценами, неизвестным ACK, необоснованной связью и с несколькими когортами показывать **отдельно**, не списывая их на каждую вакансию. Пустой/неуспешный **оплаченный** Actor включается в расходы своей когорты даже при нуле уникальных вакансий.

**Знаменатель:** число **первых уникальных успешных CREATE** по `vacancy_id` (не число кандидатов, не stage events и не число источников). Событие должно иметь доказуемую исходную когорту. Если одна основная карточка создана из нескольких кандидатов/сканов и выбор источника нельзя подтвердить, пометить неразрешённую атрибуцию и не считать одну карточку в двух когортах.

**Показатель:** делить только при положительном знаменателе и достаточной полноте подтверждённых связей. Пока есть unallocated/provider-unknown costs или недоказанные связи, **не называть итог полной себестоимостью**. Отдельно сохранить уже работающий календарный partial-period ratio — это другая метрика.

Пример: один Actor за $0.60, повторно прочитанный 3 раза и обнаруживший 10 кандидатов с 2 впервые созданными карточками, даёт **$0.60**, не $1.80 и не $6.00. Ещё один оплаченный пустой Actor за $0.20 в той же когорте даёт $0.80 / 2 = **$0.40** без учёта иных неизвестных расходов. Если тот же Actor фактически reused другой когортой, сумма $0.60 переходит в категорию shared, пока не согласована явная политика.

## 6. Предмиграционные испытания

`internal/parser/cohort_cost_contract.go` и `cohort_cost_contract_test.go` — **чистая Go-модель, не подключённая к БД или серверу**. Покрывает:

- одинаковый Actor в нескольких локальных receipts и трёх чтениях Dataset: один расход;
- разные billing accounts при одинаковом provider run ID: разные операции;
- оплаченный пустой скан;
- одна операция в двух когортных ссылках: shared/unallocated без двойного списания;
- локальный `ai_attempt` с известной стоимостью, но без provider request ID: local-unverified, не provider-verified;
- отдельные provider-confirmed AI/Firecrawl/Dataset расходы (только в синтетическом будущем формате);
- неизвестное списание/резерв, недатированная квитанция, подтверждённый ноль, конфликт сумм;
- недоверенная ссылка по URL/датам: никакой денежной атрибуции;
- запрет rebinding одного локального receipt на другой внешний идентификатор.

### Следующие PR

1. **GO-433 schema plan:** отдельная аддитивная миграция, тест схемы на disposable MariaDB, индексы и каскады, no production writes; предварительно проверить влияние на `dbschema` golden и backup/restore v39. Не переписывать исходный `apify_runs` и не активировать новые POST.
2. **GO-433 capture:** атомарная запись trusted lineage рядом с Apify cursor/candidate и отдельными AI/Firecrawl receipts; outbox/unknown ACK и тесты recovery. Подключать только под выключенным флагом до проверки.
3. **GO-433 cohort read:** отдельный админский read-only endpoint и UI с разбиением verified/local/unknown/shared. Не заменять старую календарную аналитику до provider reconciliation и GO-436.

**Приёмка каждой порции:** зелёные GitHub CI и одноразовая MariaDB, отдельно отказ от платных вызовов, отсутствие новых scheduler timers, проверка backup/restore; merge только пользователем. Нельзя считать документацию разрешением на production миграцию.
