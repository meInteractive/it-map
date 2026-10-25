# GO-058: durable receipts на disposable MariaDB

Критическая часть GO-031/GO-013 после готовности кода публикации GO-056/057. `TestMariaDBApplicationReceiptsDisposable` вызывает настоящие `applications.Repository` и MariaDB-драйвер, фиксирует транзакции и проверяет квитанции в реальной одноразовой CI-базе. Это не SQL mock и не изменение API/схемы.

Проверяются первый create/полный company response, retry с тем же ID без дубля/history, другой intent с прежним ключом, повторная привязка vacancy другим ключом, user isolation чтения/удаления, incomplete receipt и receipt чужого поколения, delete с сохранением квитанции и запрет resurrection, отсутствующая vacancy, 10 откликов на пользователя и отказ одиннадцатого. Исходный Eugene/его revision/history и UUID поколения должны сохраниться.

Порядок CI:

1. Полный storage/schema/rollback/locking suite на пустой схеме.
2. Прежние approved catalog/read-models фикстуры и полный readonly business suite.
3. **Отдельный обязательный** receipt write gate: `go test -race -tags mariadb,receiptwrites ./internal/storage -run '^TestMariaDBApplicationReceiptsDisposable$' -count=1 -json`.
4. Cleanup restores Demo=0; после этого существующие авторизованный API, все 14 browser-сценариев и native publish/rollback smoke.

Дополнительный build tag отделяет committed business writes от empty-schema suite. Старые тесты не отключены/не фильтруются из первой фазы. JSON gate требует фактический pass именованного теста и отсутствие skips: пустой набор/неправильный tag не считается успехом.

Защита: GITHUB_ACTIONS=true; при обязательном режиме отказ вне CI, отсутствие DSN/маркера — failure. Общий существующий helper проверяет parsed TCP 127.0.0.1:3306/user itmap_ci/schema itmap_ci до соединения и UUID текущего запуска до первой записи. SQL-пароли/nonce/полные записи не печатаются. Реальная staging/production БД этим тестам не подходит.

Тест использует предварительно пустого synthetic Demo и существующий CI Synthetic Employer/vacancy=1. Минимальные incomplete/stale receipt строки синтетические и создаются только после guard. Cleanup с отдельным deadline удаляет только собственный user/company/keyspace и проверяет отсутствие остатков перед поздними read-only gates. Маркер поколения не меняется; никаких DDL, локальной БД или внешних провайдеров.

Локально доступны vet/tagged build и отрицательные guard-проверки: вне CI, отсутствующий DSN, неверный target и отсутствующий generation обязаны завершиться failure с нулём skips **до соединения**. Сам SQL round-trip локально не выполняется. Положительный результат требует обязательного live CI на точном SHA.

Границы: последовательный durable commit/replay/error/delete/limit ещё не доказывает конкурентных создателей, mid-write fault/crash или complete update/CAS. Полная GO-031/GO-013 остаётся открытой; эти критические части следуют отдельно, без декоративной доводки UI.


Подтверждение кода `ab38269`, [PR #15](https://github.com/meInteractive/it-map/pull/15): [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40196634843) и [durable receipt write/API/14 browser/native publication smoke](https://github.com/meInteractive/it-map/actions/runs/40196634823) прошли. JSON gate подтвердил фактический pass без skips; cleanup восстановил readonly-фикстуры. Последующее присоединение общей DB-конфигурации GO-059 меняет только документацию/registry/manifest, не проверенный тест или API.

## GO-060: конкурирующие обычные create

Следующая узкая часть GO-031: TestMariaDBApplicationConcurrencyDisposable с теми же build tags/guards и отдельным обязательным pass/no-skip gate рядом с GO-058. Четыре сценария: одинаковый key/intent → один fresh и один replay того же ID; одинаковый key/разные intent → один fresh и ErrIntentConflict; разные key/одна vacancy → один fresh и ErrDuplicateVacancy; девять исходных записей/два новых intent → ровно десять записей и ErrApplicationLimit проигравшему. Повтор успешного intent сохраняет ID, поля и единственную начальную историю; orphan/incomplete receipts отсутствуют.

Две отдельные writer pools с MaxOpenConns=1 заранее подтверждают разные CONNECTION_ID(). Третья транзакция удерживает generation row; чтение information_schema.PROCESSLIST от того же CI-пользователя должно увидеть **оба** writer ID на точном FOR UPDATE до снятия барьера. Привилегия PROCESS не выдаётся: пользователь видит собственные сессии. Это подтверждает overlap внутри сервера, а не только старт goroutines/ожидание Go pool. Ожидание ограничено context; ранний выход отменяет writers, откатывает holder и дожидается goroutines.

Общий test-only fixture требует пустого Demo, очищает только его company/key namespace 9000, проверяет отсутствие любых остатков Demo и полную неизменность Eugene/history/generation. Исходный GO-058 с namespace 8000 и все прежние empty-schema/read/API/14 browser/native gates сохраняются. API, SQL-схема, зависимости и legacy не меняются. Смешанные create/update/delete races, one-click shortcut, mid-write error/crash/restart и CAS не покрыты этим результатом и не закрывают GO-031.

Подтверждение GO-060: [PR #18](https://github.com/meInteractive/it-map/pull/18), код `660c3d1`, [обязательный SQL/API/browser/native CI](https://github.com/meInteractive/it-map/actions/runs/40236682675) и [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40236682531) успешны. Все четыре concurrent сценария реально выполнены; локальный skip без DSN не использован как доказательство SQL.
