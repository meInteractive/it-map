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


Подтверждение GO-062: [PR #20](https://github.com/meInteractive/it-map/pull/20), код `96f13a0`, [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40237455813) и [MariaDB kill/restart/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40237455777) прошли. Два crash/recovery сценария действительно выполнялись внутри disposable CI; локальные SQL skips не выдаются за этот результат.

## GO-063: revision/CAS и история при обновлении

TestMariaDBApplicationCASDisposable расширяет бизнес-gate пятым обязательным target. Общий concurrency helper удерживает нужную canonical row, проверяет разные CONNECTION_ID и оба SQL writer в PROCESSLIST; обычный create по-прежнему использует generation lock, CAS — application row lock. Для prepared statements допускаются команды Query/Execute, INFO фильтруется внутри SQL и не выводится.

Два Update с одной revision/разными статусами и комментариями дают ровно один commit и ErrConflict проигравшему. Сохраняются поля winner и ровно один переход истории; stale retries не меняют полный Record. Отдельная сессия SET timestamp (без global clock/grants) фиксирует время: content меняет SHA-256 revision даже при одинаковом updated_at; полностью неизменный UPDATE успешен благодаря clientFoundRows и не меняет snapshot/history; comment-only edit тоже не создаёт status history. Проверена изоляция другого пользователя.

Третий сценарий использует обёртку GO-061: одно искусственное клиентское исключение перед history после подтверждённого реального UPDATE. MariaDB должна откатить все поля/revision/history; после закрытия interrupted pool исходная revision остаётся пригодной для успешного сохранения. Receipts/другие пользователи/generation/readonly fixtures сохраняются. Mixed create/update/delete и смена vacancy, полный HTTP replay и server/host crash не закрыты; API/production-код/схема/legacy остаются прежними.


Подтверждение GO-063: [PR #21](https://github.com/meInteractive/it-map/pull/21), код `36b0133`, [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40237800951) и [MariaDB CAS/все receipt targets/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40237800814) успешны. Все пять именованных SQL targets реально прошли без skips. Код приложения/API/схема не потребовали изменения. GO-031 остаётся открытой для controlled HTTP write/replay, shortcut и смешанных операций/смены vacancy.


## GO-065: настоящий HTTP → Repository → MariaDB

TestMariaDBApplicationHTTPDisposable добавляет шестой обязательный pass/no-skip target. После прежних CI/parsed target/current-generation guards запускается loopback HTTPS httptest сервер с production auth.Manager, router и реальными applications.Repository. Вход выполняется через POST /auth, отдельные cookie jars сохраняют выданные Secure/HttpOnly/Strict cookies; клиент доверяет сертификату только этого test server. Репозиторий и HMAC используют один synthetic session secret. Локально без DSN сервер/записи не запускаются.

Проверяются opaque user-bound context, 401/403/400/428/409/404 и отсутствие лишних SQL-записей; 201 create/200 replay обоих aliases с полным company response и одним durable ID; игнорирование userName тела в пользу cookie; user isolation detail/delete/company list. PUT изменяет revision/history, stale PUT даёт 409; replay исходного create не перезаписывает позднее редактирование. DELETE удаляет application/history, сохраняет receipt; повтор create даёт 410 без resurrection. JSON/private/no-store проверяется для каждого API ответа.

Отдельный реальный INSERT/COMMIT через test-only fault wrapper GO-061 завершается искусственной ошибкой response read: HTTP должен вернуть generic 503 без внутренней ошибки. SQL подтверждает уже committed комплект; повтор по alias возвращает 200/replayed с тем же ID и единственной историей. Это HTTP обработка ошибки после commit, не имитация неопределённого сетевого COMMIT. Cleanup возвращает Demo=0, полностью сохраняет Eugene/history/generation и все прежние readonly API/14 browser/native gates. Shortcut, mixed concurrent mutations и полный browser write/deployment остаются вне этой порции.


Подтверждение GO-065: [PR #23](https://github.com/meInteractive/it-map/pull/23), код b0c97af; [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40279874891) и [MariaDB/HTTP/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40279874919) успешны. Шесть именованных targets фактически выполнились без skips. Новый HTTP round-trip не потребовал менять production API/Repository.

## GO-066: shortcut для company vacancy и concurrent ordinary create

TestMariaDBApplicationShortcutDisposable добавляет седьмой required target и подключает production VacancyApplyHandler в уже существующий test-only HTTPS router. Оба aliases, fixed intent (route vacancy, Откликнулся/пустые контакты/comment/[hh]), user/context/Origin/owner/missing-vacancy errors проверяются через настоящий HTTP/SQL. Привязка существующего отклика новым key сохраняет полный Record, даже после ручного изменения статуса/контактов и при лимите 10; обычный одиннадцатый create по-прежнему отклоняется. После delete оба sticky receipts дают 410.

Concurrent scenario использует два подтверждённых server connections на удерживаемой generation row: ordinary create и ApplyToVacancy начинают конкурирующие транзакции. Допустимы два исхода: ordinary создал, shortcut только claimed его с отдельным receipt; либо shortcut создал, ordinary получил ErrDuplicateVacancy без своего receipt. В обоих случаях один application и одна начальная история, поля winner сохранены, успешные intents replay того же ID. Это реальная SQL concurrency поверх существующих production repositories, без DDL/добавочных grants.

Все записи ограничены synthetic Demo/company/key namespace; cleanup сохраняет Eugene/history/generation и readonly API/14 browser/native smoke. Только company-owned vacancy=1: channel-owned/неизвестный каталогам owner, mixed delete/update/смена vacancy не покрыты этим результатом. Полный GO-031 открыт. GO-032 по уточнению пользователя средний; работа остаётся в спринте 1.1.


Подтверждение GO-066: [PR #24](https://github.com/meInteractive/it-map/pull/24), код 65a890a; [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40280586960) и [MariaDB/shortcut/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40280586955) успешны, семь required targets без skips.

## GO-067: channel-owned shortcut и delete races

TestMariaDBApplicationMixedDisposable добавляет восьмой required target. После прежнего CI/target/generation guard создаётся отдельный предварительно пустой synthetic owner/keyspace (company_crm + channel-owned vacancy=900001) вне app_catalog. Setup — два INSERT в одной реальной транзакции, без DDL; отдельный bounded cleanup зарегистрирован до первой записи, удаляет только собственный owner/vacancy и проверяет отсутствие остатков. Ordinary HTTP create возвращает 404 для неизвестного каталогу owner, но shortcut использует реального владельца вакансии и сохраняет/replay тот же ID. Delete выполняется через Repository, поскольку company response неизвестного каталогу owner — отдельный HTTP-контракт; sticky shortcut replay затем даёт 410.

Update/delete конкурируют на одной application row; допустим update до delete либо ErrNotFound после него. Existing receipt replay/delete удерживаются locking join по generation и application до наблюдения обоих разных writer IDs в PROCESSLIST; допустим replay прежнего ID до delete либо ErrApplicationDeleted. Никаких произвольных SQL failures вместо ожидаемого доменного исхода. В обоих сценариях после завершения writers ноль applications/history, один sticky receipt и отказ последующего retry без resurrection.

Прежний cleanup сохраняет Eugene/history/generation и readonly API/14 browser/native smoke. Смена vacancy/owner, concurrent import/restore и авария MariaDB/хоста этим результатом не подтверждаются; полный GO-031 остаётся открытым. Работа только в спринте 1.1, GO-032 средний.
