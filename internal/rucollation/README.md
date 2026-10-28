# Канонический русский порядок для legacy v14–15

GO-100-C-P, решение [ADR-023](../../docs/dev/DECISIONS.md). `Keys([]uint16)` строит независимые primary/secondary/tertiary уровни, `Compare` сравнивает их без аллокаций. Ключ вычисляется один раз перед stable sort компаний: score DESC, затем русский порядок. Исходные UTF16 названия не меняются, равные ключи не объединяются. Порядок и provided rank агентств сохраняются.

Веса извлечены из Node **24.19.0**, ICU **78.3**, CLDR **48.0**, Unicode **17.0**, locale `ru`, caseFirst=false, sensitivity=variant, numeric=false. Используется полный NFD/CCC17, Hangul decomposition, longest/discontiguous contractions с блокировкой CCC и перестановка кириллицы. Непарные UTF16 сохраняются. Таблицы и алгоритм не используют Go Unicode version, системный locale, Node, CGO, сеть или SQL во время работы приложения.

Принятое отличие: эквивалентные перестановки комбинируемых знаков дают равные ключи. На исследованном Node исходный collator иногда различает И/и + Nag Mundari U+1E4EC/U+1E4ED + диакритику. Required oracle из **479072** пар содержит восемь таких отличий; это количество случаев корпуса, не всех возможных строк. Служебная последовательность ICU **FDD1+AC00** распознаётся до NFD и сохраняет native key. Для неё обещание обычной канонической эквивалентности не применяется. Детали — [исследование совместимости](../../docs/dev/LEGACY_COLLATION_COMPATIBILITY.md).

## Воспроизведение данных

На Linux x64 с указанным Node и C++17:

```sh
node scripts/collation/generate.mjs
# Только при намеренном обновлении и проверке контракта:
node scripts/collation/generate.mjs --write
go test -race -tags legacyinterop ./internal/rucollation
```

Генератор проверяет версии до загрузки addon. Собственный `generate.cc` вызывает ICU, встроенный в Node; это build-time private ABI probe с проверками длины/ёмкости. Его нельзя переносить на другую архитектуру/ABI без новой проверки. Он перечисляет 1114112 code point mappings и 1153 исходные contractions, включая prefixes. `compress.mjs` получает 77287 арифметических диапазонов, 1329 exceptions, 2081 NFD mappings вне алгоритмического Hangul, 968 CCC, 1134 нормализованных contractions, один protected key и script reorder. Промежуточные данные и addon создаются во временной папке и удаляются; в Git находится только собственный генератор и упакованная публичная таблица.

`weights.json`: gzip9/base64, little-endian unsigned base128 varints. Raw magic `ICURU78\x01`, семь counts (ranges, exceptions, NFD, CCC, contractions, protected keys, reorder). Ranges: gap от предыдущего end+1, length−1, primary, zigzag stride, secondary, tertiary. Exceptions: delta code point, CE count и triples. NFD: delta code point, length и code points. CCC: delta code point, class. Contractions/protected: text length/code points и CE count/triples. Reorder: пары lead bytes. Упаковка, raw длина 1026780, SHA256 и counts проверяются загрузчиком; required CI воспроизводит файл целиком byte-for-byte.

Лицензия публичных Unicode/ICU данных: [LICENSE-UNICODE](LICENSE-UNICODE), Unicode License V3; источник уведомления — [unicode-org/icu/LICENSE](https://github.com/unicode-org/icu/blob/main/LICENSE). Сохранять уведомление при распространении сборки.
