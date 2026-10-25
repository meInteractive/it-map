# GO-055: совместимость нормализации и intent hash

Проверка относится к критическим частям GO-002/013: повтор одного создания не должен получать другой canonical hash при переходе JS→Go. Старые функции `normalizeApplicationCreation` и `applicationCreationHash` из `server/repositories/application-creation.js` не изменялись относительно очищенного gpt-site=f51aacb. Они вычисляют эталон для 43 полностью синтетических типизированных запросов, без БД, токенов и провайдеров.

`node scripts/check-creation-contract.mjs` проверяет committed JSON в `internal/applications/testdata/creation-contract.json` по настоящему JS. `--write` — явная регенерация для рецензируемого изменения контракта, а не обход failed CI. Обычные Go-тесты читают этот эталон, проверяют acceptance/rejection, нормализованные поля и SHA-256. Web CI повторно выполняет oracle, поэтому изменение legacy функций/fixture не остаётся незамеченным. Go-тестам Node во время запуска не требуется.

Первый запуск воспроизвёл 12 расхождений:

- Go `strings.TrimSpace` удалял NEL U+0085 и сохранял BOM U+FEFF, а JavaScript String.trim поступает иначе. Менялись company/contact/source и хеш намерения; некоторые источники принимались/отклонялись неверно.
- Go regexp `\s` имеет другой Unicode-набор, поэтому некоторые email с NBSP/разделителем строки/BOM проходили Go при отказе старого JS.
- Go int64 принимал vacancyId за пределами точного диапазона JS Number.safeInteger.

Go нормализация теперь использует ECMAScript WhiteSpace/LineTerminator для trim и email, а ID ограничен `2^53−1`. Поля/HTTP-схема/хеш-алгоритм не заменяются; общая нормализация update/create сохраняет старые правила. Длина комментария/контактов по UTF-16 уже была правильной — границы с emoji подтверждены теми же тестами. Проверены default/null sources, set/sort источников, статусы, строки/экранирование JSON, контакты/лимиты, Unicode, безопасные ID. Существующий fail-fast запрет неположительных ID не ослабляется: JS проверяет пригодность ссылки на следующем SQL-шаге.

Границы: это **не** полный HTTP/SQL parity, не тест durable receipts/конкурентности и не staging. JSON coercion произвольных нетипизированных значений, дробные/строковые ID и некорректные lone-surrogate Unicode входы не покрыты этим типизированным набором; такие случаи требуют отдельного контракта HTTP-декодирования. Пользовательские данные/production/вёрстка не менялись. Следующая критическая часть GO-031 — idempotent write/CAS/rollback/concurrency на disposable MariaDB в CI, после отдельного определения её фикстур и guard.
