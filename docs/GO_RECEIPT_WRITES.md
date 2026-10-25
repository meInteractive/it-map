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


## GO-061: ошибка внутри записи и post-commit read failure

TestMariaDBApplicationFaultsDisposable использует test-only Connector/Conn/Stmt/Tx обёртку настоящего mysql driver v1.9.3. Бизнес-SQL, параметризованные statement, INSERT, COMMIT и ROLLBACK выполняются MariaDB; одно искусственное клиентское исключение вставляется перед history, перед receipt или при первом generation read после успешного commit. Никаких production hooks, DDL/новых grants, отключения прежних gates или fake SQL engine. До открытия обёртки обязательны прежние CI/parsed target/current-generation guards.

Счётчики успешно выполненных реальных INSERT и commit подтверждают точку сбоя; тест требует именно вооружённую sentinel-ошибку. Независимое соединение проверяет ноль application/history/receipt после mid-write rollback либо ровно один комплект после потерянного ответа. Весь interrupted pool закрывается; новый repository/pool повторяет исходный key/context/body. После rollback это fresh create, после commit — replay исходного durable ID. Следующие replay/changed-intent, поля и единственная история сохраняются; общий cleanup полностью восстанавливает readonly-фикстуры.

Это проверка отката реальной транзакции при клиентской ошибке и восстановления по durable receipt на новом соединении. Она **не** доказывает аварийное завершение процесса/сервера, сетевой COMMIT с неизвестным исходом, полный restore или update/CAS. Полная GO-031 остаётся открытой. CI требует фактический pass третьего target без skips вместе с GO-058/060 перед прежними API/14 browser/native smoke.


Подтверждение GO-061: [PR #19](https://github.com/meInteractive/it-map/pull/19), код `62b8b46`, [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40237125022) и [MariaDB faults/concurrency/receipts/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40237125124) успешны. Три точки ошибки выполнены на настоящей disposable MariaDB, прежние gates сохранены.

## GO-062: принудительное завершение Go writer и новый процесс

TestMariaDBApplicationCrashDisposable запускает отдельный экземпляр того же Go test executable с настоящим applications.Repository/mysql driver. Test-only Tx wrapper останавливается на контролируемом барьере после всех успешных INSERT: перед вызовом реального COMMIT либо после его успешного возврата, до чтения ответа. Parent получает фиксированный marker из stdout и вызывает Process.Kill; нормальный exit/ошибка/истечение deadline не считаются успешным crash-сценарием. При ошибке parent обязательно завершает и дожидается child. DSN/context/записи не печатаются.

После kill SQL проверяет 0/0/0 до commit либо 1/1/1 после commit. Ещё один новый Go-процесс получает исходный неизменный synthetic key/context/body и повторяет create: fresh после rollback или replay исходного ID после commit. Parent проверяет повтор того же ID, поля и единственную историю, затем общий fixture восстанавливает Demo=0 и сохраняет Eugene/history/generation. Child тоже проходит GITHUB_ACTIONS/parsed target/current-generation guards; права/схема/сервер не меняются.

Это восстановление отдельного процесса-писателя, а не авария MariaDB/хоста и не моделирование неизвестного исхода сетевого COMMIT. HTTP-server kill/replay, CAS и смешанные create/update/delete операции ещё не проверены. Четвёртый target обязателен с pass/no-skip после прежних SQL phases и до всех прежних API/14 browser/native smoke.
