# GO-337: компании в списке и карточке вакансии

Критическая часть GO-011. Источник `readEntityDirectory(..., true)` вызывает `normalizeCatalog` до фильтрации имён; ранги компаний определяются во всём каталоге. Go читал сырые salary/rank/applicationUrl и поля агентств, а карточка сериализовала четыре лишних флага списка. Это давало другой JSON даже для минимального каталога.

`backup.PrepareVacancyEntities` переиспользует принятый `storedReadCatalog`/нормализатор315/317 и возвращает только name/rank/entityType/middle/senior/lead/head/applicationUrl в исходном порядке компаний и агентств. Имена сравниваются точно, без trim/case folding/NFC. JSON_NUMBERS и Unicode проходят через принятый codec. Общие сортировка и ранги выполняются до отбора нужных имён.

List и detail используют эту проекцию в своих существующих readonly RR-транзакциях. List сохраняет все четыре пользовательских флага, определённые SQL. Для detail выделен `DirectoryEntity` с восемью полями, без favorite/hidden/hasVacancies/hasApplications. Отсутствующий app_catalog использует тот же source fallback; malformed JSON остаётся ошибкой. Исходная одноразовая инициализация/оценка резюме на GET не переносится; чтение не пишет данные.

`check-vacancy-entities.mjs` вызывает неизменённый readEntityDirectory с реальной SQLite app_catalog. 180 кадров: defaults/global ranks/зарплатные диапазоны/типы/пробелы ссылок/agency, отсутствующий/пустой/неполный/ошибочный каталог, точные Unicode имена, повторённый/неизвестный выбор. До исправления regression воспроизводит ошибку полей/порядка/формы; после180 exact DTO comparisons проходят-race. Никакие приватные поля полного каталога не проецируются.

`TestMariaDBVacancyEntitySourceDisposable`:5 вариантов каталога ×3 пользователя ×list/detail =30 required случаев, SELECT-only account, исходная SQLite задаёт ожидаемые восемь полей и порядок. В list отдельно проверяются четыре флага; в detail сравнивается вся форма. Есть malformed/missing catalog и проверка неизменности всех72 таблиц, skips запрещены. Локально native compile/skip не является приёмкой реального SQL; положительный CI ещё ожидается.

Полная GO-011 остаётся открытой: весь vacancy SQL/API source parity, отбор/порядок строк, Telegram и публикации/роль-период-фильтры ещё требуют своих gates. Schema/backup/provider/worker/deploy не меняются. GO-336/#95 принята по полному CI; её source167 и native9 остаются регрессией.
