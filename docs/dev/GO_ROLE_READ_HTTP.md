# GO-341: GET ролей и отрицательные ID потомков

Критическая часть GO-011, только спринт1.1. Main9e4eff7e351b881c7a94862a3400458613698fa3/treeb2e1db9780ab4ae9d88a9631f5bd63baaa3b01ab совпадает с принятой340/c0403e6;99/100 доставлены, все final CI успешны.

## Проблема и поведение

Неизменённый [source router](../../server/routes/index.js) распознаёт путь потомков только как /api/vacancy-roles/[0-9]+/descendants. Для0/000 и чисел без соответствующей роли, включая переполнение, он возвращает404 с JSON «Роль не найдена.». Signed/percent-encoded/nondecimal сегменты не образуют этот маршрут и получают404 Not found. Прежний Go возвращал503 backend_not_migrated для0/overflow и принимал+1/%31 как ID1. При отключённой БД source отдаёт503 «Хранилище ролей пока не подключено.», Go — общий backend_not_migrated.

Теперь [VacancyRoleHandler](../../internal/httpapi/role_routes.go) проверяет EscapedPath до декодирования, сохраняя порядок auth→path→DB→lookup. Корректный путь с несуществующим/нулевым/переполненным ID возвращает исходный404; leading zeros принимаются. Кодированные aliases статических all/roots/leaves/descendants также не превращаются в действительные маршруты. Четыре GET routes регистрируются при настроенной авторизации даже без RoleReader, чтобы отсутствие БД имело исходный503 после auth/path. Пустой каталог сохраняет массивы[] и полную форму DTO. Ошибки SQL остаются безопасным503 без содержимого ошибки.

## Проверки

[Oracle](../../scripts/check-role-read-http.mjs) запускает настоящий unchanged router на полной исходной SQLite3.53.3-схеме с Node24.19.0/ICU78.3. Шесть ролей образуют несколько корней/вложенные ветви, включая канонически равные Unicode-имена; три назначения на две вакансии проверяют usageCount. Подготовительный source read сохраняет настоящий SQL read-set для Go stub даже при invalid/unauthorized/noDB запросах; счётчик SQL затем сбрасывается. В HTTP oracle разрешены только SELECT/WITH, total_changes() неизменен, сеть запрещена.

166 полных HTTP сравнений включают all/roots/leaves/descendants, порядок/paths/descendantIds/counts, пустой каталог, три аккаунта/anonymous/invalid cookie, неизвестные/нулевые/leading-zero/переполненные ID, signed/nondecimal/encoded paths, неизвестный query, Origin и отсутствие БД. Проверяются status, JSON либо текст404 и число обращений к RoleReader. Meaningful regression до изменения Go воспроизвёл ошибки; после исправления source166 проходят-race.

[Native test](../../internal/storage/role_read_http_integration_test.go) повторяет все166 запросов через настоящий Go handler и roles.Repository на SELECT-only MariaDB10.11. Fixture/cleanup — отдельный guarded writer только disposable GitHub CI. Fingerprint всех72 таблиц должен быть неизменен после каждого чтения. Required workflow проверяет parent pass, ровно166 leaf pass, нулевые skips. Локально положительная MariaDB не запускается. Local default/tagged race/vet/build, Go1.23, React,333 локальные ссылки, API audit/actionlint/history проходят. Native166 прошли без skips в [job123550239718](https://github.com/meInteractive/it-map/actions/runs/40903045429/job/123550239718); полный CI успешен: [Public40903045398](https://github.com/meInteractive/it-map/actions/runs/40903045398), [MariaDB40903045429](https://github.com/meInteractive/it-map/actions/runs/40903045429), [negative10.3.32 40903045292](https://github.com/meInteractive/it-map/actions/runs/40903045292). Весь прежний regression также проходит. Код опубликован в [PR #101](https://github.com/meInteractive/it-map/pull/101),66e63c58877957c17874247d98a3e17a53aff946/treef901b03d762b4817cf76a7e40439958fcb2887a7.

Статическая [API matrix](API_COVERAGE.md) обновлена после review регистрации четырёх GET routes; закреплён новый hash handler.go и добавлены test references. Матрица не объявляет семейство полностью перенесённым.

## Границы

POST/PUT/DELETE ролей, остальные HTTP методы, общий URL routing/redirect policy и неохваченные пути остаются отдельными частями GO-002/011. Не меняются source JS, схема, роли/назначения, Unicode ADR-023 или другие API. Критическая приёмка этого GET subset не закрывает родительские задачи. Реальные данные/ключи/providers, сервер/SSH и deploy не затрагиваются.
