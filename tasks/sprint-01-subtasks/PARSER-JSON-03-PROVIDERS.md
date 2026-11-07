# S11-DIAG-03 — JSON-диагностика затрат JEV/LLM, Apify и Firecrawl

**Статус:** [ ] Частично реализуется: снимки уже сохранённых данных Apify/Firecrawl в отдельном PR; полная история платных операций и JEV/LLM ещё не реализованы, итоговая приёмка не завершена. **Приоритет:** высокий для расходов JEV/LLM, средний для Apify/Firecrawl. **Зависимость:** S11-DIAG-01 (контракт и защита). Реализовать отдельным PR или двумя небольшими PR, не включать GO-433 (полную себестоимость вакансии) в эту работу.

## Где разместить

- «Автоматический сбор» → **«Бюджеты JEV и LLM»**, рядом с заголовком расходов и таблицей по стадиям.
- «Apify · HH» → **«Бюджет Apify»**, общий диагностический JSON для Free/Starter.
- «Firecrawl» → **«Бюджет Firecrawl»** (разрешённые Free/Hobby профили).
- Нельзя добавлять JSON-кнопку возле полей ввода API-ключей и настроек подключения.

## LLM / JEV

- [ ] **S11-DIAG-03-01.** Отразить `dayBudgetUsd`, `monthBudgetUsd`, `spending.dayUsd`, `monthUsd`, `timezone`, периоды и источник расчёта. **Явно назвать текущую верхнюю оценку**, включающую неизвестные резервы; не выдавать `dayUsd` за подтверждённый расход.
- [ ] **S11-DIAG-03-02.** Если ledger позволяет, вычислить `confirmedUsd`, `unknownReservedUsd`, `upperEstimateUsd` отдельно по OpenAI/OpenRouter/JEV, по типам `classify_title/extract_vacancy/assign_roles/score_relevance/fetch_full_text` и по моделям; `credits` не прибавлять к USD.
- [ ] **S11-DIAG-03-03.** Из `scheduler_attempts` — **10 последних попыток** по соответствующему фильтру с `jobId`, `type`, `provider`, `model`, `status`, `reasonCode`, `startedAt`, `reservedUsd`, `costUsd`. Не копировать raw prompt, ответ, входные документы, резюме, `result_json`, `message`.
- [ ] **S11-DIAG-03-04.** Показатель повторных вызовов включать только после проверки ключа idempotency, revision, model, prompt и provenance на одноразовой БД. Если строго доказать повторы нельзя, явно `duplicateAICalls: {"available":false}` вместо выдуманного нуля.
- [ ] **S11-DIAG-03-05.** Проверить что глобальные расходы `scheduler_attempts` не обнуляются при `generation` restore (как предусмотрено `dashboard_spending.go`), а локальные фильтры по текущему поколению не скрывают уже понесённые затраты. Никакого двойного суммирования резервов и подтверждённых оплат.

## Apify

- [ ] **S11-DIAG-03-06.** Экспорт из локального `GET /api/apify/usage`: `providerUsedUsd`, `ownConfirmedUsd`, `readReserveUsd`, `unknownReserveUsd`, `unknownRuns`, `limitUsd`, `remainingUsd`, дата provider snapshot, профиль. Провайдерский общесчётный расход и `ownConfirmedUsd` **перекрываются**, поэтому не суммировать их для «общей стоимости».
- [ ] **S11-DIAG-03-07.** **10 последних HH paid-operation записей** из `apify_runs`: `state`, `createdAt`, `updatedAt`, `chargedUsd`, `reservedUsd`, `errorCode`, безопасные стабильные идентификаторы операции для сопоставления с `scan_source`. Не копировать `key_hash`, `account_id`, provider token, ввод Actor и Dataset (в них могут быть персональные данные). `run_id` передавать только после оценки допустимости ссылки и режима приватного экспорта.
- [ ] **S11-DIAG-03-08.** Обязательно различать: `newActorStarts` и `GET/reconcile` известного Actor; `timeout` с частичным Dataset и `uncertain` без разрешения на повторный POST. Не делать внешнего provider refresh по кнопке копирования.

## Firecrawl

- [ ] **S11-DIAG-03-09.** Вывести `mode`, `limits.credits`, `providerRemaining`, `providerPlanCredits`, `localReported`, `localEstimated`, `reserved`, `effectiveUsed`, `remaining`, `periodStart/End`, `checkedAt`, `fresh`, `status/reason` по действительно имеющемуся read-only snapshot.
- [ ] **S11-DIAG-03-10.** Не переводить кредиты Firecrawl в USD без подтверждённого тарифа на дату операции; известный нулевой расход отличать от неизвестного. Любые последние 10 Firecrawl операций выводить только при наличии достоверного журнала. Не запускать `/api/firecrawl/check` или `/api/firecrawl/usage/refresh` при копировании.

## Приёмка

- [ ] **S11-DIAG-03-11.** Синтетический ledger: подтверждённая стоимость, unknown reserve, нулевая операция, два provider snapshots с задержкой, повторное чтение Actor, Firecrawl credits. Проверить reconcile сумм/периодов и отсутствие двойного счёта.
- [ ] **S11-DIAG-03-12.** Тест запрета копирования ключей/сырого ответа, 401/403, clipboard failure, отсутствие network POST, React desktop/mobile и обязательный CI на точном SHA.

**Файлы:** `frontend/features/settings/{Scheduler,Apify,Firecrawl}.tsx`, `internal/parser/dashboard_spending.go`, `internal/apifyruns/`, `internal/httpapi/`, существующие Firecrawl бюджетные сервисы (текущий Go маршрут проверять по коду; не считать JS маршрут действующим). Для расходов на **одну опубликованную вакансию** отдельная GO-433 в спринте 1.2.

**Отчёт (часть A — только сохранённые бюджеты):** ветка `codex/S11-DIAG-03-provider-snapshot-json`, отдельный PR в `main`. Реализованы кнопки JSON для «Бюджет Apify» и «Бюджет Firecrawl», безопасная проекция из уже полученных GET `/api/apify/usage` и `/api/firecrawl/usage`, тесты сериализатора и React, обязательный шаг web CI. Для Apify извлекаются два профиля и только активные операции `active`; **это не последние 10 платных операций из `apify_runs`**. Этот пробел (03-07 и части 03-08) остаётся. Для Firecrawl нет документированного полного журнала вызовов, отмечено `operationHistoryAvailable:false`. Пользователь не нажимает «Обновить статистику» для JSON — копирование только из кеша. Провайдерский расход и локальная подтверждённая стоимость **не суммируются**, неизвестное не превращается в ноль. JEV/LLM (03-01…05), серверная история Apify и проверка настоящего интерфейса — следующая отдельная часть. **PR, SHA и CI:** проверить после создания PR; чекбокс S11-DIAG-03 до конца оставлять открытым.
