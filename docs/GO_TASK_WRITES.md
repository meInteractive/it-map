# Критические части GO-033: задачи без Calendar

## GO-068: typed legacy create contract

scripts/check-task-create-contract.mjs читает неизменный POST /api/tasks validation block из server/routes/index.js и выполняет его с настоящим TASK_TYPES и fixed synthetic catalog lookup. 31 типизированный вектор проверяет заметки/ECMAScript whitespace/UTF-16, safe vacancy IDs, типы и поддерживаемые RFC3339 UTC миллисекунды. SQL, provider calls и сеть не используются; fetch принудительно запрещён. Изменение границ source block/fixture проваливает CI и требует рецензируемого обновления, fixture не переписывается автоматически.

Повтор прежних Go-правил на этих goldens воспроизвёл 10 расхождений: BOM сохранялся при trim, NEL ошибочно удалялся, лимит note считался в рунах вместо 1200 UTF-16 code units, int64 принимал unsafe JS IDs. CreateInput.normalize теперь выполняется перед транзакцией, сохраняет прежний fail-fast для неположительных IDs и ограничение company name хранилища; UTC формат и типы не меняются. Старый JS не редактируется.

Границы: это typed normalization/acceptance contract, а не полная JS coercion/Date.parse/HTTP error-order совместимость. Текущий Go HTTP по-прежнему требует явный RFC3339 timezone/секунды и максимум три дробных знака; существующая строгая грамматика не расширяется. Read filters/Calendar, real SQL writes/rollback/concurrency и browser create/reschedule/complete ещё не подтверждены этой порцией. Полные GO-014/033 остаются открыты.


Подтверждение GO-068: [PR #26](https://github.com/meInteractive/it-map/pull/26), published d5e820d/tree bc334320, [public](https://github.com/meInteractive/it-map/actions/runs/40287308900) и [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40287308885) успешны. GitHub-коннектор сохранил точное дерево локального исправления после отказа git/gh write 401.

## GO-069: real SQL task writes

TestMariaDBTaskWritesDisposable с отдельным mariadb,taskwrites tag запускается после empty-schema/full readonly/receiptwrites suite, перед прежними API/14 browser/native smoke. Требует GitHub CI, прежних parsed DSN/loopback/user/schema/current-generation guards; отсутствие prerequisite в required режиме — fail до подключения, не skip. Пустые Demo tasks/calendar namespace очищаются bounded cleanup; полный Eugene task response, shared CRM row и generation сохраняются.

Repository round-trip проверяет company/note trim, UTC milliseconds/переход через полночь, vacancy join, filters/includeCompleted/entities и изоляцию owner. Session-only clock сдвигается между двумя completed=true, чтобы проверить настоящую идемпотентность completed_at; reopen очищает timestamp без изменения deadline/metadata. Синтетический Calendar marker не содержит токена и никогда не расшифровывается: connected create/reschedule и linked-event reschedule должны отказать, completion сохраняет legacy local-only контракт и linkage.

Test-only mysql Connector/Stmt/Tx обёртка вставляет одну клиентскую ошибку ПОСЛЕ успешного реального INSERT/UPDATE. Repository обязано откатить task row/due/completion/timestamp; отдельный нормальный pool проверяет полную неизменность и успешный retry. Никакого fake SQL engine, DDL/grants, провайдеров или реальной БД вне disposable CI. Task HTTP/browser, фильтры ошибок произвольных типов и Calendar sync остаются отдельными критериями; новые revision/CAS поля в legacy task API не добавляются.


Подтверждение GO-069: [PR #27](https://github.com/meInteractive/it-map/pull/27), c9f89ec, [public](https://github.com/meInteractive/it-map/actions/runs/40288051112) и [MariaDB/task writes/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40288051119) успешны. Target реально выполнился без skips, все rollback и Calendar-guard сценарии проверены на MariaDB.

## GO-070: handler → auth → Repository → SQL

TestMariaDBTaskHTTPDisposable выполняет настоящие login/cookie и task routes через httptest recorder с реальным Repository/MariaDB. Проверяет 201/200 response shapes, private/no-store, owner isolation, Origin/strict JSON/duplicate-key/mixed-field ошибки, UTC/filters/includeCompleted/entities, completion/reopen, connected Calendar 409 и отказ без данных. Ошибка клиента после реального SQL write отображается generic 503 и откатывается, дальнейший retry после известного rollback успешен. Создание задач не имеет durable receipts, неизвестный исход COMMIT/потерянный success reply не выдаётся за идемпотентность.

34 исходных JS cases (31 GO-068 + три deadline cases) воспроизвели, что Go time.Parse принимал +24:00/+00:60/comma fractions, отвергаемые JS Date.parse. Оба HTTP unit contract тоже воспроизвели неверный success для offset. Private normalizeTaskDeadline общий для create/reschedule; HTTP regexp проверяет диапазон offset до SQL, поддерживаемый корректный RFC3339 набор не расширяется. Невозможный offset даёт 400, а не сохранённую неправильную дату.

Отдельный SQL subcase удерживает task row до наблюдения двух разных CONNECTION_ID в PROCESSLIST. Completion и reschedule оба завершаются; итог содержит новый deadline и completed_at, остальные поля сохранены. Это legacy независимые field mutations, без новых revision/CAS tokens. Browser/TCP write round-trip остаётся следующим отдельным критерием; Calendar sync/реальные токены не подключаются.
