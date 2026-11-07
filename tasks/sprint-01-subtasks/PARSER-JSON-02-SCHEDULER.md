# S11-DIAG-02 — обзор, очередь и журнал парсера: кнопки «Копировать JSON»

**Статус:** [ ] Реализация в черновом [PR #260](https://github.com/meInteractive/it-map/pull/260); исходный код добавлен, CI/приёмка ещё проверяются. **Не считать задеплоенным или принятым до merge и проверки.** **Приоритет:** высокий. **Зависимость:** S11-DIAG-01 (общий сериализатор/кнопка); выполнять после GO-107/108, если их реализация уже использует общий компонент. Отдельный PR, без изменений платного конвейера.

## Куда поставить кнопки

1. «Автоматический сбор» → вкладка **«Обзор»**, около заголовка или отдельной карточки диагностики.
2. «Диагностика» → **«Постоянная очередь заданий»**, с учётом выбранных пользователем фильтров статуса, типа и организации.
3. «Диагностика» → **«Журнал циклов и последних попыток»**, в заголовке раскрывающегося блока.
4. Мелкие карточки «Очередь», «Последний цикл», «Автоматический запуск» **не дублируют кнопки**, если их данные уже входят в обзор.

## Состав JSON: Overview

- [ ] **S11-DIAG-02-01.** Поля `enabled`, `cloudStatus`, `lastSignalAt`, `lastSuccessAt`, `nextSignalAt` с честными `null`; текущий статус Go API; `appVersion` только при достоверной сборочной метке; время чтения/период МСК.
- [ ] **S11-DIAG-02-02.** `counts` по `pending/running/completed/failed/cancelled` и стадиям; это **jobs**, а не вакансии. Поля `statistics.sources/found/accepted/imported` с пояснением единицы измерения: `processed` и `succeeded` в цикле тоже не равны публикациям.
- [ ] **S11-DIAG-02-03.** Отдельно вывести количество **допущенных** HH `launch/reconcile/pipeline` и число заблокированных legacy/неподтверждённых курсоров только если backend реально рассчитывает их по admission-предикатам GO-430. Нельзя называть все `scan_source` платными стартами или выводить «phase» только из job.type.

## Состав JSON: Queue

- [ ] **S11-DIAG-02-04.** Для последних 10 по выбранным фильтрам `jobId`, `type`, `companyId/agencyId` (минимально необходимые идентификаторы), `sourceId`, `status`, `priority`, `createdAt`, `runAfter`, `attempts/maxAttempts`, `reasonCode`, `lastAttemptAt` при наличии. Имя компании по умолчанию **не включать в экспорт для чата** — только при подтверждённой приватной области и праве, без персональных данных.
- [ ] **S11-DIAG-02-05.** На сервере обеспечивать выборку **10 последних по времени**, а не первых 10 текущей страницы `/api/scheduler/queue` (там сортировка по `created_at DESC`, `LIMIT 30 OFFSET`). Запрос должен сохранять все фильтры, передавать `totalMatched`, `totalReturned`, `sort`, `truncated`; проверить, что состояние `status=running` с более старым `createdAt` не скрыто логикой сортировки.
- [ ] **S11-DIAG-02-06.** Ожидание `deferred` показывать только при наличии надёжного признака (`reasonCode`/deadline), не придумывать `nextRetryAt`/поле `phase` при отсутствии в БД. Пользовательский `last_error` не копировать сырым: только нормализованный machine `errorCode`, с очищенным коротким summary при потребности.

## Состав JSON: Runs + Attempts

- [ ] **S11-DIAG-02-07.** Ровно **10 последних циклов**: `runId`, `trigger`, `startedAt`, `finishedAt`, `durationMs`, `status`, `ready`, `processed`, `succeeded`, `errors`, `remaining`, `reason`.
- [ ] **S11-DIAG-02-08.** Отдельно **10 последних попыток**: `attemptId/jobId/runId`, `type`, `startedAt/finishedAt`, `status`, `reasonCode`, `provider`, `model`, `reservedUsd`, `costUsd`, `credits`; без сырого `result_json`, полного `message` или данных вакансий. `costUsd: null` отличать от доказанного `0`.
- [ ] **S11-DIAG-02-09.** Если доступны надёжные stage receipts: отдельные `actorStarts`, `actorReconciliations`, `candidatesCreated`, `newVacanciesPublished`, `existingVacanciesLinked`; не выводить их вычисленными из `succeeded`, `ready` или случайной суммы JSON. Недоступные метрики обозначать `available: false`.

## Проверки

- [ ] **S11-DIAG-02-10.** Disposable MariaDB fixture с 25 jobs, несколькими страницами и фильтрами, `deferred`, legacy blocked HH, двумя циклам и 15 attempts; assert возвращает правильные последние 10/10 без дубликатов и без утечки несогласованных полей.
- [ ] **S11-DIAG-02-11.** Auth/admin, 401/403, refresh, stale snapshot, clipboard reject, смена фильтров/страницы/учётной записи и нулевые значения; React desktop/mobile, Go tests/CI если затронут endpoint. Копирование не изменяет очередь и не вызывает платных провайдеров.

**Ключевые файлы:** `frontend/features/settings/Scheduler.tsx`, `frontend/api/client.ts`, `internal/parser/{dashboard,queue,run_read}.go`, `internal/httpapi/scheduler_*routes.go`, `cmd/itmap/main.go`. Для дополнительных полей нужен безопасный bounded read-only Go endpoint; не раздувать существующий `GET /api/scheduler` сырыми записями.

**Отчёт:** [PR #260](https://github.com/meInteractive/it-map/pull/260), ветка `codex/S11-DIAG-02-scheduler-json-exports`. Изменены `frontend/features/settings/Scheduler.tsx`, `shared/scheduler-debug.mjs` (+ типы), `internal/parser/queue.go`, Go admin-only route и тесты Node/React/MariaDB. **Снимок очереди из всей SQL-области фильтрации**, `created_at DESC,BINARY id DESC LIMIT 10`, `total/coverage`, отдельный GET `/api/scheduler/queue/debug`. История берётся из уже возвращаемых Go dashboard 20 runs/50 attempts и обрезается до 10; *полное число всех исторических записей пока неизвестно и не должно подменяться длиной этого окна*. Подтверждённая стоимость не вычисляется из верхней оценки дневных резервов. **CI/конечный SHA:** перепроверить GitHub перед merge. Ни parser worker, ни провайдеры/таймеры не изменяются. Чекбокс не закрывать до успешного CI и браузерной приёмки.
