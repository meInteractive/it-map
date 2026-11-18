# GO-431 P0 — отмена сразу после подтверждённого reserve COMMIT

**Статус:** тестовая проверка существующего runtime, без изменения финансовой логики. Не разрешает production-действия или оплачиваемые POST.

## Граница

В `Attempt.Run` уже существуют две проверки: известная `ctx.Err()` **до** резервирования и `remaining(ctx)` / `workCtx.Err()` **после** резервирования, перед передачей исполнения адаптеру. Однако прежний набор synthetic-тестов отдельно подтверждал отмену до reserve, после provider POST, после settlement и SIGKILL, но не точный промежуток **между успешным reserve COMMIT и вызовом адаптера**.

Новый `TestMariaDBAIReserveCommittedCancellationNoReplayDisposable` удерживает точный порядок событий с тестовым SQL connector:

1. `scheduler_attempts` создаётся с действующей lease.
2. `reserveAttemptLocked` записывает `reserved_usd=0.26`, provider/model и успешно коммитит транзакцию в **одноразовой CI MariaDB**.
3. Test-only callback отменяет исходный контекст **после реального COMMIT**, но до возврата из `Attempt.Run.reserve`.
4. Ожидается `context.Canceled`, ноль mock POST, сохранённый `reserved_usd`, `cost_usd IS NULL`, `Unknown()=true`.
5. Повтор на том же handle запрещён; после истечения lease / `RecoverExpired` новый attempt не имеет права повторить POST, не теряется исходная денежная неопределённость.

Тест является отдельным сценарным gate CI, а не декларацией, что любое закрытие `StopClaims` атомарно отменяет сетевой вызов. **SIGTERM в graceful режиме может закрыть `StopClaims`, сохранив активный контекст текущего шага.** Проверка этой отдельной гонки остаётся дальнейшей работой. Не менять `unknown` на известный нулевой расход: подтверждение локального commit не является подтверждением settlement и не должно автоматически разблокировать повторную попытку.

## Приёмка

- `TestMariaDBAIReserveCommittedCancellationNoReplayDisposable`: отдельный `PASS` в обязательном `MariaDB isolated staging gate` на exact PR SHA, без `SKIP`.
- Старые тесты отмены до резервирования, после provider POST/ACK, SIGTERM, SIGKILL и неизвестного SQL COMMIT остаются обязательными.
- Никаких схем, миграций, настроек worker, бюджетов, CI secrets, production SQL, ключей, внешних provider вызовов, деплоя/перезапуска. Следующая проверка — `StopClaims` после post-claim admission, до резервирования, при продолжающемся grace context.
