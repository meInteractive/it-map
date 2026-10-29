# Scheduler jobs — GO-312

`RunRepository.ClaimJob` работает только с уже приобретённым running cycle в текущем поколении. Generation singleton и run row блокируются в короткой Read Committed транзакции; выбор due jobs и смена pending → running атомарны. Два Go-владельца не получают одно задание или одновременно задания одного candidate. Existing source D1 и новый Go-процесс нельзя запускать совместно на одной очереди; cutover отдельно.

Выбор сохраняет условия `server/repositories/scheduler.js`: type/jobId/diagnostic/agency/candidate, пяти­минутный deferred lane, HH penalty50, aging, точное соответствие operation key текущей странице и стабильные SQL tie breakers. Статус существующего Apify ledger не даёт разрешения на новую страницу. Runtime providers, enqueue/API и автоматический tick этой порцией не включаются.

`JobLease` хранит приватные generation/run/job/owner fences. `Job()` возвращает независимый снимок, не credential для записи. `WithinOwned` проверяет generation/run/job с row locks и исполняет ограниченный callback в той же транзакции. Callback предназначен только для SELECT/DML; DDL, управление транзакцией и внешний I/O в нём запрещены. `AssertActive` — моментальная проверка перед внешней работой, она не заменяет transactional fence. `Finish` завершает/откладывает только текущую unexpired lease; deferred возвращает одну попытку, terminal statuses получают completed_at. Ошибка COMMIT не вызывает автоматического повтора.

JSON имеет два исходных контракта. SQLite JSON1 выбирает первое повторное поле и сохраняет текст объектов/массивов для `listing:<runId>:<page>`. Исходный `finishScheduledJob` использует JSON.parse/stringify: последнее поле, IEEE754, порядок integer keys и lone UTF16. Для второго переиспользуется уже принятый чистый codec `backup.StringifyParsedJSON`. Внешний вид исходного payload не заменяется до Finish.

Числовой JSON1 text воспроизводит SQLite3.53.3: AtoF significand и `%!.17g`/FpDecode с точными integer powers, а не приближённый shortest float. Алгоритм основан на public-domain SQLite [`src/util.c`](https://github.com/sqlite/sqlite/blob/version-3.53.3/src/util.c) и [`src/printf.c`](https://github.com/sqlite/sqlite/blob/version-3.53.3/src/printf.c); C/SQLite runtime в Go не добавляется. Source oracle закреплён Node24.19.0/SQLite3.53.3 и выполняется исключительно в SQLite `:memory:`.

Проверки:23 044 JSON scalar vectors,143 actual unchanged-source claim/finish fixtures и1 001 сравнение BIGINT score arithmetic; отдельный disposable MariaDB gate сравнивает полные строки142 представимых fixtures после операций. Одиночный UTF16 surrogate проходит source codec, но нарушает native MariaDB JSON CHECK:143-й case доказывает отказ без записи; Finish override с таким JSON откатывается и сохраняет действующую lease. Второй required parent проверяет два actual connections, независимые stale fences, late SQL faults/rollback и SIGKILL внутри owned TX с сохранением committed claim и recovery из нового pool. Положительные SQL-тесты разрешены только в disposable GitHub CI после target/nonce guard. Бюджеты/attempt ledger приняты GO-313/#73; durable-core GO-045 завершён. Полный dispatcher/pipeline остаётся GO-046.


## Команды retry/cancel (GO-329)

Администратор может изменить pending/failed/cancelled текущего поколения через
прежний HTTP API. Поколение фиксируется до чтения body. Команда меняет только
status/run_after/attempts/last_error/reason_code/updated_at по исходному SQL;
retry обнуляет attempts, cancel сохраняет счётчик. Времена берутся из SQL clock,
как в принятом core. Повтор в тот же clock tick сохраняет matched-row semantics
за счёт обязательного clientFoundRows=true.

Payload, lease token/deadline, completed_at и все scheduler_attempts сохраняются.
Неопределённые платные резервы остаются видны существующему GO-313 guard;
команда не создаёт execution handle и не вызывает worker/provider. Running,
completed, чужое поколение или отсутствующий ID дают changed. Смена поколения
операции даёт restored. Неопределённый COMMIT не повторяется автоматически.

Положительная приёмка требует 43 source router/SQLite frames (45 HTTP responses)
против actual Go/MariaDB и четырёх native fault/race сценариев без skips.
