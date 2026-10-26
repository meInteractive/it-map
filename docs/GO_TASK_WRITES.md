# Критические части GO-033: задачи без Calendar

## GO-068: typed legacy create contract

scripts/check-task-create-contract.mjs читает неизменный POST /api/tasks validation block из server/routes/index.js и выполняет его с настоящим TASK_TYPES и fixed synthetic catalog lookup. 31 типизированный вектор проверяет заметки/ECMAScript whitespace/UTF-16, safe vacancy IDs, типы и поддерживаемые RFC3339 UTC миллисекунды. SQL, provider calls и сеть не используются; fetch принудительно запрещён. Изменение границ source block/fixture проваливает CI и требует рецензируемого обновления, fixture не переписывается автоматически.

Повтор прежних Go-правил на этих goldens воспроизвёл 10 расхождений: BOM сохранялся при trim, NEL ошибочно удалялся, лимит note считался в рунах вместо 1200 UTF-16 code units, int64 принимал unsafe JS IDs. CreateInput.normalize теперь выполняется перед транзакцией, сохраняет прежний fail-fast для неположительных IDs и ограничение company name хранилища; UTC формат и типы не меняются. Старый JS не редактируется.

Границы: это typed normalization/acceptance contract, а не полная JS coercion/Date.parse/HTTP error-order совместимость. Текущий Go HTTP по-прежнему требует явный RFC3339 timezone/секунды и максимум три дробных знака; существующая строгая грамматика не расширяется. Read filters/Calendar, real SQL writes/rollback/concurrency и browser create/reschedule/complete ещё не подтверждены этой порцией. Полные GO-014/033 остаются открыты.
