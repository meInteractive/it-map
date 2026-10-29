# GO-342: period query компактного списка вакансий

Критическая часть GO-011, спринт1.1. База — принятая GO-341/1c4531f, source166/native166 и полный CI pass.

## Контракт и исправление

Исходный GET /api/vacancies/list использует URLSearchParams.get: первое значение period, различие отсутствующего и пустого параметра, tolerant decoding, semicolon как часть значения. Go net/url.Query отбрасывал некорректные пары. Поэтому period=week;extra=1, period=week;bad&period=all и period=%ZZ давали200 с другим набором вакансий вместо исходного400.

[Handler](../../internal/httpapi/vacancy_routes.go) теперь переиспользует принятый backup.CatalogQueryValue, определяя presence/first-value без net/url.ParseQuery. Точный escaped path проверяется после авторизации; nil repository возвращает source503 до проверки period. GET регистрируется при настроенной авторизации и без БД, сохраняя auth→path→DB→period precedence. Alias /api/vacancies/%6cist не становится настоящим маршрутом. Production clock, predicate318, SQL/сортировки/проекции338–340 и другие API не меняются.

## Проверки

[Oracle](../../scripts/check-vacancy-list-http.mjs) выполняет настоящий unchanged router на полной SQLite3.53.3-схеме, Node24.19.0. Catalog пуст; Eugene relevance profile подготовлен до чтения, lazy writes запрещены. Разрешены только SELECT/WITH, total_changes() неизменен, сеть запрещена. Now фиксирован на29.10.2026. Две synthetic строки — свежая и архивная; разные favorite/hidden/relevance у Demo/Nastya и общие отклики на разные компании проверяют user isolation.

176 сценариев: все6 вариантов периода (omitted/all/week/month/twoMonths/archive), empty/invalid/case/whitespace/encoded/malformed/semicolon/duplicate query, unknown keys, пустая БД,3 пользователя/anonymous/bad cookie, Origin, nil DB и escaped path. До исправления actual source differential воспроизводит потерю period; после [pure HTTP test](../../internal/httpapi/vacancy_list_http_interop_test.go) проверяет status/error/text, вызовы repository, пользователя и точный nullable period. Для успешного непустого ответа полная data projection в этом stub не проверяется.

[Native test](../../internal/storage/vacancy_list_http_integration_test.go) повторяет все176 запросов через настоящий Go handler/Repository на SELECT-only MariaDB10.11 и сравнивает полные JSON DTO, включая массивы/null/даты/flags/applicationSummary. Test-only adapter фиксирует now при вызове Repository; production clock не меняется, HTTP query/auth/path и SQL reads настоящие. Fingerprint всех72 таблиц неизменен после каждого запроса. Fixture/cleanup выполняются отдельным guarded writer только disposable GitHub CI. Required workflow проверяет parent pass, ровно176 leaf pass, нулевые skips. Локально положительная MariaDB не запускается. Local default/tagged race/vet/build, Go1.23, React,340 локальных ссылок, API audit/actionlint/history и source regression341+342 проходят. Native acceptance и полный CI пока ожидаются.

API matrix сохраняет partial статус: этот proof охватывает bounded GET query и две заранее подготовленные строки. Все варианты каталога/источников/role graph, остальные методы, GET /api/vacancies и карточка не объявляются целиком принятыми. Source JS, схема, mutations/providers, реальные данные, сервер/SSH и deploy не меняются.
