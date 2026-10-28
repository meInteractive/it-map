# Совместимость чтения каталога

GO-314 выявила разницу: `server/services/catalog-projection.js` вызывает normalizeCatalog при каждом чтении, а прежний Go Repository проецирует stored JSON напрямую. GO-315/316/317 разделяют source projections, source search и последующее readonly SQL/HTTP подключение. Частичная реализация не закрывает полный GO-009 и внешний snapshot gate GO-030.

## GO-315: pure list/map/detail

`backup.PrepareCatalogRead` строит JSON DTO через уже принятый ordered ECMAScript codec и catalog field normalizer из GO-100-C-P. Сортировка/rank сохраняют делегированный canonical Unicode выбор [ADR-023](DECISIONS.md) и [его границы](LEGACY_COLLATION_COMPATIBILITY.md). Names/IDs не заменяются comparison keys.

Входной CatalogReadState — уже выбранные строки identity/flags, vacancy names, application winners и initialization state. Функция не читает БД, не выбирает SQL application winner и не создаёт identity. Для detail отдельно передаются выбранные user flags/count. Источник сохраняет ID/name fallback, null для отсутствующего detail, удаление только jobs из полного detail и ограниченный набор полей compact ответа.

Нормализованные объекты сохраняют ordered scalar values до финального stringify DTO. Ранний JSON round-trip превращал1e400 в null до проверки truthiness embedded job.title; source при этом считает title истинным. Общие object builders теперь могут передать исходное значение consumer, а прежние entity/restore callers сохраняют свою финальную JSON сериализацию. Числовые OwnPropertyKeys после spread рекурсивно упорядочиваются на собственных копиях containers без промежуточного JSON round-trip. Отдельный embedded numeric-spread fixture отвергает отсутствие этого шага. Обычный omitted property не заменяется null; lone UTF16 и scalar types остаются в JSON без промежуточного Go string coercion.

Source gate `TestCatalogReadProjectionSource` запускает неизменённые readCompactEntities/readCompanyDetail на1 180 синтетических fixtures. Read-set stubs возвращают явно заданные query results, наблюдают, но не исполняют source INSERT OR IGNORE. Это проверка чистого преобразования, **не actual SQLite/MariaDB/identity-bootstrap proof**. Принимаются source value и source TypeError/SyntaxError boundary для невалидных offices/jobs/JSON. Проверяются normalization/rank/defaults, company/agency list/map/detail, null/omitted, typed/overflow values, prototype names, Unicode, короткие замыкания vacancy/initialized и personal state.

Runtime Repository/HTTP этой порцией не переключаются. Предыдущие restore entity/plan/source gates обязательны после общего object-builder refactor. Source search — GO-316; actual readonly MariaDB/HTTP, user isolation, application winner SQL и error/method contracts — GO-317. GET не будет создавать IDs: source SQL reference получает предварительно подготовленные identity rows, как предполагает существующая миграция.

## GO-316: pure source search

`backup.PrepareCatalogSearch` применяет тот же нормализованный ordered catalog, затем точную формулу searchCatalogNames: name/sector/roles, offices metro+address, embedded job titles, revenue legal/metric, Object.values(vpnAi).join. Пропущенное direct поле превращается в текст undefined, null — в null; Array.join обращается с ними иначе. Числовое переполнение остаётся Infinity до поиска, хотя финальный JSON выводит null. Невалидные offices/jobs и throwing coercions сохраняют исходную error boundary.

Запрос передаётся как JSON string, чтобы сохранить UTF16 units. Используется уже принятый полный Unicode17 lower с final sigma, без нового NFC/NFD/диакритического folding поиска. KMP сравнивает UTF16 units за линейное время и не находит ложный half-unit match в byte encoding. Agencies/search, как в исходнике, ищет компании; агентства не добавляются в результаты.

`TestCatalogSearchSource` сравнивает2551 синтетических результатов/error cases с неизменённым JS searchCatalogNames. Это тоже pure proof с единственным stub чтения app_catalog, без SQL, HTTP или сети. Repository/HTTP activation и alias/error/negative-rights проверяются в GO-317; полная GO-009 остаётся открытой.
