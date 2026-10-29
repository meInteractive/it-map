# GO-339: общий список откликов в списке вакансий

Критическая часть GO-011, только спринт1.1. Предыдущая GO-338 находится в main: ce475316e119cc3a427daaaa1ff80e97efe004de/tree739564affe103db3bc65dbd05404622c6caddd28; дерево совпадает с принятой274c326, все три final CI успешны.

## Контракт и исправление

Неизменённый [readCompactVacancyList](../../server/services/vacancy-projection.js) строит Set из companyName, затем realCompanyName каждой вакансии в исходном порядке всего набора. Пустые имена исключаются, точные повторения удаляются. Затем applicationSummary содержит только общие отклики текущего пользователя в этом порядке; vacancyId всегда null. Фильтр периода применяется только к vacancies, уже после определения имён.

Прежняя Go BuildList сортировала summary по байтам. Meaningful differential regression воспроизвёл расхождение: исходные alpha,Zulu против Go Zulu,alpha; дополнительно пустое companyName ошибочно попадало в набор. Теперь BuildList сохраняет список первого появления имён вместе с существующим множеством wanted и выбирает из него общие отклики. SQL агрегация остаётся user-scoped; имена не обрезаются и не нормализуются. Пробельное имя сохраняется как непустое, регистр/Unicode/NUL различаются точно. Входные вакансии не меняются; пустой ответ — массив [], без изменения DTO.

## Проверки

[Source oracle](../../scripts/check-vacancy-application-summary.mjs) вызывает настоящий readCompactVacancyList на полной исходной SQLite3.53.3-схеме, Node24.19.0. Catalog пуст, профиль Eugene подготовлен до чтения; total_changes() доказывает отсутствие lazy writes при GET. Сеть запрещена.57 наборов×3 пользователя×6 периодов=1026 сравнений summary: ASCII, company/realCompany порядок, повторения, пустые/пробельные имена, Unicode/NUL, только связанные отклики, чужие пользователи, отсутствующие компании и случайные наборы. Тест воспроизвёл ошибку до исправления и проходит после, отдельно проверяет неизменность входных вакансий.

[Native proof](../../internal/storage/vacancy_application_summary_integration_test.go) использует девять именованных source-наборов и реальный Repository.List на MariaDB10.11 с SELECT-only подключением.162 обязательных сочетания (9×3×6) сравниваются с заново вычисленным исходным SQLite summary. Fingerprint всех72 таблиц должен остаться прежним после каждого чтения. Fixture и cleanup выполняются отдельным guarded writer исключительно в synthetic disposable CI; локально положительная MariaDB не запускается. Required workflow проверяет parent pass, ровно162 leaf pass и отсутствие skips.

Local source/default/tagged race, vet, сборки Go/React, Go1.23,320 ссылок документации, API audit, actionlint и history проходят. Native162 прошли без skips в [job123519067043](https://github.com/meInteractive/it-map/actions/runs/40894290169/job/123519067043); полный CI успешен: [Public40894290467](https://github.com/meInteractive/it-map/actions/runs/40894290467), [MariaDB40894290169](https://github.com/meInteractive/it-map/actions/runs/40894290169), [negative10.3.32 40894290637](https://github.com/meInteractive/it-map/actions/runs/40894290637). Весь прежний regression также проходит. Код опубликован в [PR #99](https://github.com/meInteractive/it-map/pull/99), fc4f0b6ea0e79b39219316311f6f285bc6747971/treec2ab5d13b4d0b3a7c6ac70600a3fb84719b9a534.

## Границы

Схема, запись откликов, роли, каталог и SQL порядок вакансий не меняются. Полная GO-011 остаётся открытой: остальные source/API чтения и фильтры требуют собственных доказательств. Реальные данные, провайдеры, сервер/SSH и деплой не затрагиваются.
