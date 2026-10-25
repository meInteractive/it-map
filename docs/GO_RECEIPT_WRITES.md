# GO-058: durable receipts на disposable MariaDB

Критическая часть GO-031/GO-013 после готовности кода публикации GO-056/057. `TestMariaDBApplicationReceiptsDisposable` вызывает настоящие `applications.Repository` и MariaDB-драйвер, фиксирует транзакции и проверяет квитанции в реальной одноразовой CI-базе. Это не SQL mock и не изменение API/схемы.

Проверяются первый create/полный company response, retry с тем же ID без дубля/history, другой intent с прежним ключом, повторная привязка vacancy другим ключом, user isolation чтения/удаления, incomplete receipt и receipt чужого поколения, delete с сохранением квитанции и запрет resurrection, отсутствующая vacancy, 10 приложений на пользователя и отказ одиннадцатого. Исходный Eugene/его revision/history и UUID поколения должны сохраниться.

Порядок CI:

1. Полный storage/schema/rollback/locking suite на пустой схеме.
2. Прежние approved catalog/read-models фикстуры и полный readonly business suite.
3. **Отдельный обязательный** receipt write gate: `go test -race -tags mariadb,receiptwrites ./internal/storage -run '^TestMariaDBApplicationReceiptsDisposable$' -count=1 -json`.
4. Cleanup restores Demo=0; после этого существующие авторизованный API, все 14 browser-сценариев и native publish/rollback smoke.

Дополнительный build tag отделяет committed business writes от empty-schema suite. Старые тесты не отключены/не фильтруются из первой фазы. JSON gate требует фактический pass именованного теста и отсутствие skips: пустой набор/неправильный tag не считается успехом.

Защита: GITHUB_ACTIONS=true; при обязательном режиме отказ вне CI, отсутствие DSN/маркера — failure. Общий существующий helper проверяет parsed TCP 127.0.0.1:3306/user itmap_ci/schema itmap_ci до соединения и UUID текущего запуска до первой записи. SQL-пароли/nonce/полные записи не печатаются. Реальная staging/production БД этим тестам не подходит.

Тест использует предвари­тельно пустого synthetic Demo и существующий CI Synthetic Employer/vacancy=1. Минимальные incomplete/stale receipt строки синтетические и создаются только после guard. Cleanup с отдельным deadline удаляет только собственный user/company/keyspace и проверяет отсутствие остатков перед поздними read-only gates. Маркер поколения не меняется; никаких DDL, локальной БД или внешних провайдеров.

Локально доступны vet/tagged build и отрицательные guard-проверки: вне CI, отсутствующий DSN, неверный target и отсутствующий generation обязаны завершиться failure с нулём skips **до соединения**. Сам SQL round-trip локально не выполняется. Положительный результат требует обязательного live CI на точном SHA.

Границы: последовательный durable commit/replay/error/delete/limit ещё не доказывает конкурентных создателей, mid-write fault/crash или complete update/CAS. Полная GO-031/GO-013 остаётся открытой; эти критические части следуют отдельно, без декоративной доводки UI.
