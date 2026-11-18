# GO-431 P0 — повторный admission после получения lease

**Область:** один непрерывный Go Worker, `internal/parser/worker.go`. Это изменение не добавляет платных вызовов и не меняет budget/ledger/SQL schema. Фактический runtime на сервере и результаты текущего production неизвестны.

## Подтверждённый пробел в прежнем коде

`Worker.Cycle` проверял `StopClaims` и `MinRemaining` **до** `RunRepository.ClaimJob`. Сам claim может задержаться на SQL lock/IO. Между допуском и результатом claim контекст может приблизиться к deadline либо корневой worker может получить SIGTERM, пока профиль использует grace context. После claim прежний код немедленно создавал рабочий контекст и вызывал исполнитель этапа: оставшегося времени могло быть уже недостаточно для безопасного завершения, а сигнал остановки — не перепроверен.

Отдельный `pipelineCleanupReserve=12s` и `interruptWorkerJob` были реализованы ранее, как и pre-claim `MinRemaining=60s`. Здесь **не** меняем эти константы, не добавляем новый таймер и не удлиняем сетевой вызов.

## Поведение после исправления

1. После подтверждённого claim и **до** `workerExecute`/создания `Attempt` проверяем `StopClaims`, оставшееся время относительно исходного deadline, `MinRemaining` и отдельный cutoff для работы pipeline. Провал любого из условий запрещает исполнять только что полученную lease.
2. Тот же владелец с новым bounded cleanup context (максимум 10 секунд) пытается атомарно вернуть задание в `pending` через существующий `interruptWorkerJob`: `reason_code=deferred`, задержка 10 секунд, `Deferred=true` возвращает счётчик `attempts` к прежнему значению. Для нового claim оплаченных `scheduler_attempts` ещё нет.
3. Успешная deferral **не** увеличивает `WorkerResult.Processed` и не отправляет provider POST. Если lease/generation истёк или SQL/COMMIT не подтвердился, **не** заявляем успешный возврат: оставляем run на стандартное восстановление с запретом присвоить себе чужую lease.
4. Для ручного запуска `MinRemaining=0` сохраняется прежняя политика порога, но shutdown/deadline и pipeline cutoff всё равно проверяются. Само исполнение уже разрешённого и начатого платного этапа не прерывается этим дополнительным guard.

Важное ограничение: повторная проверка закрывает окно **между pre-claim и получением lease**. Сигнал остановки, пришедший уже после второго допуска, не может быть абсолютно атомарно синхронизирован с внешним HTTP POST этой проверкой: дальнейшую безопасность обеспечивают контексты вызовов, durable reservation/attempt и запрет повторов при неопределённом исходе. Не выдавать этот фикс за решение всех SIGTERM/SIGKILL/ACK гонок или за durable cross-candidate identity — это отдельная [задача #489](https://github.com/meInteractive/it-map/issues/489).

## Синтетическая приёмка

- `TestGO431PostClaimAdmissionRejectsLostTimeAndShutdown` — pure Go граничные случаи: ещё 60 секунд, одна наносекунда ниже, ранний work cutoff, истёкший deadline, закрытый `StopClaims`, ручной цикл без обязательного min.
- `TestMariaDBHHWorkerPostClaimFenceDisposable` — только disposable MariaDB: искусственно удерживается SQL row lock на ещё не запущенном AI job, пока исходный admission становится устаревшим. На возобновлении claim задание должно вернуться в `pending`, `attempts=0`, `reason_code=deferred`, без `scheduler_attempts`; счётчик обработанной работы остаётся 0.
- `.github/workflows/mariadb-staging-ci.yml` явно **требует** `PASS` именно нового теста, а не только общий зелёный job или `[no tests to run]`.
- Existing CI tests: `TestMariaDBHHWorkerTimeBudgetDisposable`, `TestMariaDBHHWorkerPipelineCleanupAdmissionDisposable`, `TestMariaDBAIStageFaultRecoveryNoPaidReplayDisposable`, SIGTERM/SIGKILL и lost-ACK должны оставаться зелёными.

**Команды:** `go test ./internal/parser -run 'TestGO431PostClaimAdmission|TestTimeBudget|TestPipelineWorkDeadline' -count=1`; `go test -race -tags 'mariadb schedulerwrites schedulerbilling' ./internal/storage -run '^TestMariaDBHHWorker(PostClaimFence|TimeBudget|PipelineCleanupAdmission)Disposable$' -count=1` **только на CI disposable MariaDB**.

Нет права выполнять этот tagged test на production; никаких production-migrations, deployments, ручных запусков provider/Actor, изменений ledger или запуска второго consumer. Факт слияния PR и обязательного CI не равен подтверждённому обновлению установленного worker: GO-447 решается отдельно.
