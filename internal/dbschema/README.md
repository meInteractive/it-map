# Trusted schema v1 — GO-310 / GO-100-D

`baseline_v1.json` содержит только публичные SQL/metadata структуры:72 таблицы,606 столбцов,73 явных индекса,50 FK и22 триггера,217 DDL действий. Действия воспроизводятся из принятой schema-only baseline скриптом `python3 scripts/trusted-schema-policy.py`. Uploaded backup metadata/SQL сюда не попадают.

`Inspect` читает только information_schema через интерфейс с одним QueryContext. Полный контракт включает engine/collation/row format; порядок, тип, NULL/default/generated/charset/collation столбцов; primary/unique/prefix/ignored indexes; CHECK expressions; FK columns/order/rules; trigger event/timing/body/SQL mode. Не сравниваются volatile AUTO_INCREMENT/cardinality/оценки строк и имя SQL definer. Для просмотра trigger body MariaDB требует TRIGGER privilege; CI использует guarded account и read-only transaction, не выдаёт дополнительные права SELECT-only пользователю.

`Preflight` не выполняет DDL и не читает бизнес-строки. Он отклоняет неизвестные/изменённые объекты и повреждённые определения существующих таблиц; отсутствующие таблицы и добавляемые объекты планирует в исходном проверенном порядке. `CheckFull` принимает только полную схему. Проверка пустоты partial schema, ownership, исполнение и повтор DDL относятся к отдельной GO-311.

До получения/проверки server-normalized metadata из disposable CI policy содержит `metadata:null`, а preflight возвращает `ErrUnverified`. Это промежуточный этап GO-310, не приёмка и не разрешение runtime DDL. CI capture читает проверенную schema-only fixture, публикует только чанки публичной структуры; production/личные backup не используются.

Для инициализации выбрана отдельная служебная команда перед запуском HTTP: существующий сервер требует готовую baseline уже при открытии DB pool. Автоматический startup DDL и новый migration framework не добавляются. Импорт структуры в реальную базу этим PR не разрешается и агентом не выполняется.
