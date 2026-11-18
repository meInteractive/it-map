# GO-431 P0 — StopClaims перед новым платным AI dispatch

## Пробел

Непрерывный Worker использует `RunProfilesWithGrace`. При SIGTERM закрывается `StopClaims`, а контекст уже начатого шага на некоторое время остаётся активным для завершения. Проверка в `Worker.cycleStep` после `ClaimJob` (PR #490) закрывает окно *до* `workerExecute`, но не задержку `AIStageExecutor.Prepare`, `StartAttempt` и построения запроса.

Следовательно, раньше после SIGTERM, но до самого `Billing.Run` мог создаваться новый платный reserve/POST, если этап уже прошёл post-claim admission.

## Защита

- Прокинуть `StopClaims` от `Worker.Cycle` через `AIStageDispatcher` к `AIStageExecutor`. При ручных и независимых вызовах `nil` сохраняет прежний контракт.
- До `Prepare`, после `Prepare` и после `StartAttempt` проверять остановку; если StopClaims закрыт — не отправлять запрос.
- `workerStopBilling` проверяет StopClaims **сразу до** передачи нового `Billing.Run` в durable reserve и provider. Сигнал, пришедший *после* входа в `Billing.Run`, не прерывает текущий вызов и его settlement: действует отдельный grace context с прежним deadline.
- Если ни одного провайдерского запроса не начато (`attempt.Requests()==0 && !attempt.Unknown()`), `ErrWorkerDrainBeforePaid` возвращается рабочему циклу. `workerExecute` через существующий `interruptWorkerJob` закрывает созданные attempts и возвращает job в `pending` с сохранением счётчика попыток; новых paid receipts нет. Если работа уже началась, существующие unknown/settlement/no-replay правила остаются в силе.

**Ограничение:** `StopClaims` и сетевой POST нельзя синхронизировать атомарно одним check без новой durable operation identity. Если сигнал приходит после допуска `Billing.Run`, остаётся действующее консервативное reserve/unknown и отсутствие повторного POST. Межкандидатная устойчивость — отдельная Issue #489 и не решается этим PR.

## Синтетическая приёмка

- `TestGO431StopBillingGrace`: уже закрытый StopClaims блокирует новый вызов; сигнал во время `Billing.Run` не обрывает его; второй вызов после остановки запрещён; nil сохраняет ручные операции.
- `TestGO431AIStageDispatcherPropagatesStopClaims`: сигнал доходит до AI executor.
- `TestMariaDBAIStopClaimsBeforePaidDispatchDisposable` с одноразовой MariaDB:
  - `already_draining`: не создано ни одной попытки;
  - `after_attempt_before_billing`: StartAttempt существует, reserve/provider POST отсутствуют; lease recovery не мешает безопасной новой попытке;
  - `during_inflight_paid_request`: mock POST уже начат, SIGTERM сигнал закрывается, подтверждённая стоимость и результат атомарно сохраняются в срок grace.
- Требовать `PASS` для родительского теста и всех трёх подслучаев в required MariaDB CI; никаких `SKIP`.
- Существующие SIGTERM/SIGKILL, lost ACK и неопределённый COMMIT должны пройти без изменений.

## Запреты

Без production DB/DDL/миграций, paid OpenAI/Apify, изменений бюджета, системных служб, релиза, перезапуска или автоматического merge. Наличие CI/PR не подтверждает установленный SHA отдельного Go Worker.
