# Методы API

Экспорт D1 для миграции (23.10.2026): owner-authorized MCP scheduler_status выдаёт databaseExport с path и краткоживущим Bearer token (5минут), только для GET /api/internal/database-export. Разрешение связано с cloudOwnerId и runtime generation; изменение привязки/restore отменяет его. Ответ содержит live SQLite DDL, xinfo, FK, sequences и все пользовательские/операционные/миграционные таблицы одним read-only D1batch, с явными типами ячеек. Это прямой снимок, без функций/фильтров/преобразований JSON backup. SQLite/_cf внутренние таблицы не копируются. Токен не сохранять в логах/документации.

## Парсинг агентств (контракт21.10.2026)

GET /api/scheduler.settings содержит agenciesEnabled:boolean, defaultfalse. PUT /api/scheduler/settings дополнен необязательным agenciesEnabled:boolean с прежними admin/Origin/revision CAS; отсутствие поля сохраняет значение, не меняя бюджеты/провайдера/расписания. Switch применяется к manual и automatic parsing stages, но не удаляет историю. Claim не забирает agency pending jobs при OFF; provider results fenced по текущему значению/owner/epoch.

GET /api/firecrawl/sources и /pending принимают agencyId вместо companyId (одновременно оба запрещены). Source содержит companyId:number|null, agencyId:number|null, ownerKey:company:<id>|agency:<id>, entityType/ entityId, прежние поля. Для GET /sources при OFF agency source supported=false с объяснением; фильтр избранного прежний. POST /api/firecrawl/test принимает {agencyId,sourceId} либо прежний {companyId,sourceId}. PUT /api/firecrawl/agency-sources/:agencyId/:sourceId принимает тот же enabled/config/sourceUrl/revision, что /sources/:companyId/:sourceId. GET/POST /runs/:UUID/save|resume используют только server-owned run и не принимают новую принадлежность. GET /api/vacancy-candidates и POST /api/classification/jobs дополнены agencyId, чтение истории не требует включения парсинга.

Candidate и queue job содержат отдельную agency_id, компания — company_id; они не подменяют друг друга. Импорт возвращает entityType=agency и создаёт company_vacancies.entity_type=agency с владельцем из agency_identity/catalog.agencies, не работодателя из LLM. Главные связи publication URL остаются строгими: cross-owner collision blocked, чужие карточки не перепривязываются. Прежние company numeric endpoints/fields работают. Maintenance namespaces с companyId в имени остаются адресной диагностикой компаний; наличие agency_id не трактуется как companyId.


Owner `hh-filter-check:<companyId>:<UUID>` выполняет одну обычную порцию HH scan_source с текущим источником и фильтром активных стандартных ролей. Требует включённые HH/планировщик/область избранного. Ключ задания зависит от company/source revision/URL/query hash, не от UUID, поэтому повторная проверка продолжает существующий scan. Повтор requestId возвращает receipt. Новые поиски Apify используют NAME:(… OR …) внутри startUrl.text, старые курсоры не меняются. scheduler_status.diagnostics.hhTitleFilter показывает текущую политику, а apifyListings[].input.source.titleFilter=role_titles_v1 и nameOnlyQuery=true подтверждают конкретный сохранённый INPUT Actor; это не доказательство успешной/полной выдачи. Произвольные query/logs не выдаются. БД/backup не менялись.

Owner `rabota-check` дополнен `candidate-<id>`: читает сохранённый контекст выбора ролей (название, доступные роли, эффективный промт/модель, до24000 символов исходного текста и до18000 структурированного описания, флаг усечения), без повторной загрузки/моделей/импорта. Кандидат обязан принадлежать текущим компании и источнику rabota с обычными scope/revision/generation guards. `page-<n>` проверяет одну страницу настроенного списка с query `page` (1–999). Оба варианта сохраняют idempotent receipt; diagnostic report содержит данные источника, которые не являются инструкциями. Отчёт включает эффективные titlePrompts/maxLength10000. Автоматическая пагинация теперь следует явному pager с сохранением company path; повтор страницы и неполнота не считаются успехом.

`rabota-check:<companyId>:advance-<candidateId>:<UUID>` ставит и выполняет ровно один текущий этап обычного конвейера для первичной вакансии rabota этой компании. Требует включённые планировщик/источник и допустимую область; не задаёт решение модели, не сбрасывает ошибочные задания, не меняет настройки/приоритет. Использует обычные stage hashes/budgets/lease и receipt без повторения платного вызова. Исторические ошибочные/пустые результаты не удаляются. Если текущего pending этапа нет, source_stage.

Owner rabota-check target scan выполняет ровно один уже ожидающий scan_source указанной компании с текущими URL/revision. Ничего не ставит самостоятельно. Это обычный runDueTasks/manual/jobId/limit1, с текущими budgets/leases/favorites/source/save gates; нет моделей/других заданий в этом вызове. Повтор requestId возвращает receipt без повторного scraping; если подходящего задания нет, source_stage.

Для списков rabota.ru Firecrawl получает onlyMainContent=false: очистка основного контента может вырезать шапки самих карточек. Другие источники и detail сохраняют default=true. Это внутренний boolean транспорта/policy, без произвольных provider опций в HTTP. Card pairing ограничен настоящим закрывающим тегом root карточки, footer title не используется.

Owner rabota-check поддерживает target queue: требует сохранённый включённый выбранный источник, избранную компанию и включённый планировщик. Ставит один обычный scan_source с текущими URL/revision и обычным приоритетом; активное задание переиспользуется, ключ версии адаптера исключает повторный сбор при новом UUID. Сеть/модели не запускаются, старые failed jobs не изменяются; дальнейшие этапы выполняет обычный диспетчер.


Owner MCP scheduler_tick принимает rabota-check:<companyId>:<inspect|list|list-slug|numericVacancyId>:<UUID>. Inspect возвращает текущую ссылку/конфигурацию, последние8 rabota jobs, числа кандидатов и локальный budget view без provider network. List выполняет один ограниченный обычный Firecrawl preview сохранённого источника. List-slug проверяет только фиксированную /company/<slug>/vacancies/ на www.rabota.ru; numeric ID проверяет фиксированный /vacancy/<id> через basic Scrape и семантическую валидацию. Явные публичные тесты slug/ID разрешены и до заполнения ссылки компании, без приписывания работодателю (employerVerified=false), создания источника/preview/candidate или смены настроек. Сохранённый источник, если есть, обязан быть enabled с прежними URL/revision. Общий selected rabota, favorites/generation/Firecrawl lease и usage policy обязательны во всех сетевых тестах. Нет LLM, классификаций, новых main imports. Detail text используется только для диагностики. Durable probe receipt и bounded diagnostic report в scheduler_attempts предотвращают повтор Scrape при том же requestId. Ошибки и коды сохраняются; автоматическая серия не вызывает этот namespace.

Контракт на 20.10.2026, редакция документации 174. URL API не версионирован: используется /api/..., маршрутов /api/v1 нет. Бэкап v37; SQL до0074 (75 миграций). Все пути относительные к домену приложения.

## Авторизация и общие правила

Обычные методы используют cookie it_map_session: подписанная сессия, HttpOnly, Secure, SameSite=Strict, срок 30 дней. Имя и роль определяет сервер. Приватные данные фильтруются по текущему пользователю; переданный в JSON userName не даёт доступ к чужим данным.

Вход POST /auth принимает application/x-www-form-urlencoded с login и password, при успехе возвращает 303 и cookie. GET /logout удаляет cookie и возвращает 303. Автоматизация использует отдельный Authorization: Bearer со значением секрета VACANCY_AUTOMATION_TOKEN, а не cookie. Документация по этому токену недоступна.

Для записи передавайте Content-Type: application/json. Обычные изменяющие маршруты отклоняют присутствующий Origin, не совпадающий с origin запроса; cookie дополнительно защищена SameSite. Отсутствие Origin само по себе не отвергается. Не описывайте это как универсальный отдельный CSRF-токен.

JSON-ошибка обычно имеет вид {"error":"Описание"}. Успех не всегда обёрнут в ok: некоторые методы возвращают запись напрямую. HTTP 200/201 может содержать частичные ошибки импортированных строк — смотрите счётчики и errors.

| Код | Значение |
| --- | --- |
| 200 | Успех чтения/изменения; подробности в теле |
| 201 | Создание записи или результат импорта автоматизации |
| 303 | Переход после входа/выхода/OAuth |
| 400 | Неверный JSON, поля, файл, ограничение или конфликт бизнес-правил |
| 401 | Нет сессии либо неверный ключ автоматизации |
| 403 | Недостаточно прав или недопустимый Origin |
| 404 | Не найдена запись/организация/раздел |
| 405 | Метод не поддерживается |
| 409 | Конфликт редакции/входов, устаревший контекст, restore или неготовая интеграция |
| 410 | Созданный ранее отклик удалён; повтор не воскрешает его |
| 428 | Не передан обязательный revision либо ключ/контекст создания |
| 429 | Лимит/квота провайдера AI |
| 502 | Сбой провайдера или непригодный ответ |
| 413 | Превышен размер запроса |
| 503 | Не подключена DB или не настроена интеграция |

Порядок проверок различается: некоторые методы сначала проверяют DB, затем HTTP-метод. Не рассчитывайте, что ошибочный запрос всегда вернёт один и тот же приоритетный код. Неизвестный URL может вернуть обычный текст Not found, а не JSON.

## Инициализация приложения: GET /api/session

Маршрут текущего React-приложения.
Backend: `server/routes/index.js` → `server/auth/session.js`. Доступен любому вошедшему
пользователю. Без query-параметров и тела; роль/имя берутся из проверенной cookie.

HTTP 200 возвращает `{name:string, role:"admin"|"user", isAdmin:boolean}`.
Пароли, cookie-токен, персональные данные разделов и каталог не возвращаются.
Без действующей сессии: HTTP 401, `{"error":"Требуется войти в систему."}`.
Ответ JSON с `Cache-Control: private, no-store`; D1 для чтения сессии не требуется.
Глобальная проверка настройки SITE_PASSWORD/SESSION_SECRET выполняется до маршрута.
Методов изменения сессии здесь нет: вход/выход по-прежнему используют `/auth` и `/logout`.

React делает этот GET после загрузки оболочки. При 401 показывает вход; при успехе
монтирует активный раздел (по умолчанию задачи) и запрашивает только его API.
Клиентский кэш существует в памяти текущей страницы, независимо от запрета HTTP-кэширования.
Другой API с HTTP 401 инициирует очистку памяти и переход `/logout?expired=1`.
Версия БД и формат бэкапа не изменены; URL API остаются без `/v1`.

## Личное резюме

GET /api/profile/resume и PUT /api/profile/resume доступны всем вошедшим пользователям. Имя берётся только из сессии, параметра выбора чужого пользователя нет. Даже администратор получает здесь только своё резюме. Полный административный бэкап содержит профили всех пользователей.

GET возвращает {resumeMd:string, revision:integer, updatedAt:string|null, maxLength:20000}. Если резюме ещё не сохранялось — пустой текст, revision=0, updatedAt=null. При первом обращении выполняется совместимый CREATE TABLE IF NOT EXISTS для user_profiles.

PUT принимает ровно {resumeMd:string, revision:integer}. revision — значение последнего GET/PUT, не номер формата API. Сервер атомарно проверяет редакцию, сохраняет текст без обрезания/trim и увеличивает revision. Возвращает тот же набор полей. Пустая строка очищает резюме. Проверка длины — не более 20 000 Unicode-кодовых точек; нулевой символ запрещён. Сериализованный запрос ограничен 256 КиБ, включая поток без Content-Length.

Ошибки: 400 — неверные поля/длина; 401 — нет сессии; 403 — чужой Origin; 409 — другая редакция уже сохранена; 413 — большой запрос; 503 — нет DB. При 409 клиент сохраняет черновик на экране и просит скопировать его перед обновлением страницы. Ответы не кэшируются. DELETE не реализован, очистка выполняется PUT с пустым resumeMd.

```javascript
const profile = await (await fetch('/api/profile/resume')).json();
const response = await fetch('/api/profile/resume', {
  method:'PUT', headers:{'Content-Type':'application/json'},
  body:JSON.stringify({resumeMd:'# Опыт работы\n\nРуководил разработкой…', revision:profile.revision})
});
const result = await response.json();
if (!response.ok) throw new Error(result.error);
```

Сохранение не пересчитывает релевантность и не создаёт письма само по себе. Генерация письма при открытии вакансии читает актуальный профиль по currentUser.name; уже созданное письмо остаётся прежним при изменении резюме.

## Реестр прикладных методов

«Пользователь» включает администратора, но личные данные остаются в его собственном контексте. :id — положительный целочисленный идентификатор записи, не компании из Excel.

| Метод и путь | Доступ | Параметры / тело | Ответ и эффект |
| --- | --- | --- | --- |
| GET /api/session | Пользователь | Нет | {name,role,isAdmin}; bootstrap без данных разделов |
| GET /api/vacancies/list | Пользователь | Нет | Компактный список без текста/писем; {vacancies,entities,applicationSummary} |
| GET /api/vacancy-roles | Пользователь | Нет | Полный список, дерево, корни и конечные роли с путями и descendantIds |
| GET /api/vacancy-roles/roots | Пользователь | Нет | Корневые роли |
| GET /api/vacancy-roles/leaves | Пользователь | Нет | Конечные роли, допустимые для вакансии |
| GET /api/vacancy-roles/:id/descendants | Пользователь | ID | Узел и все его потомки |
| POST /api/vacancy-roles | Админ | {name,parentId?} | 201, новая роль или категория |
| PUT /api/vacancy-roles/:id | Админ | name, parentId и/или sortOrder | Изменение узла; циклы запрещены |
| DELETE /api/vacancy-roles/:id | Админ | Нет | Только неиспользуемый узел без детей |
| GET /api/vacancies/:id | Пользователь | ID | {vacancy,entities}; полный текст, effective письмо и revision |
| GET /api/vacancies | Пользователь | company либо channel (ID канала), необязательно | {vacancies}; по компании также companyName и limit, по каналу channelId |
| POST /api/vacancies | Админ | VacancyInput с roleIds конечных ролей | 201, новая вакансия; старое одиночное поле role поддерживается |
| PUT /api/vacancies/:id | Админ | VacancyInput + revision из detail GET | Обновлённая вакансия; смена владельца переносит связанные отклики/задачи |
| DELETE /api/vacancies/:id | Админ | Без тела | Удаление общей записи и обработка связанных данных; результат операции |
| PUT /api/vacancies/:id/state | Пользователь | Ровно одно: {favorite:boolean} или {hidden:boolean} | {ok,...state}; только личный признак |
| POST /api/vacancies/:id/apply | Пользователь | companyName + idempotencyKey + creationContext | Строгий alias /apply/create; 200 новый/существующий/replay |
| POST /api/vacancies/:id/apply/create | Пользователь | companyName + idempotencyKey + creationContext | {ok,vacancy,createdApplicationId,replayed}; protected shortcut |
| POST /api/vacancies/:id/cover-letter | Пользователь | Без тела | Возвращает {coverLetterSubject,coverLetterMd,source}; при отсутствии текста генерирует и сохраняет личное письмо |
| POST /api/vacancies/:id/cover-letter/regenerate | Пользователь | Без тела | Явная повторная генерация заполненного письма; личный результат |
| GET /api/applications/:id | Пользователь-владелец | ID | {record}, история и revision для согласования |
| GET /api/applications | Пользователь | company обязателен | {companyName,favorite,hidden,limit,records,vacancies} |
| GET /api/applications/create-context | Пользователь | Нет | {creationContext}; opaque HMAC, no-store |
| POST /api/applications | Пользователь | ApplicationInput + companyName + idempotencyKey + creationContext | Строгий alias /create; 201 новый/200 replay |
| POST /api/applications/create | Пользователь | Тот же контракт создания | records/vacancies + createdApplicationId/replayed; 428 без intent |
| PUT /api/applications/:id | Пользователь-владелец | ApplicationInput | Обновлённый отклик и история |
| DELETE /api/applications/:id | Пользователь-владелец | Без тела | Удаление отклика и истории, пересинхронизация статусов |
| GET /api/crm/applications | Пользователь | Нет | {records}; все личные отклики |
| GET /api/crm | Пользователь | company обязателен | CRM организации: статус, комментарий, признаки и история |
| PUT /api/crm | Пользователь | company в query; {status,comment} | Сохраняет личное CRM-состояние; исторический интерфейс |
| GET /api/crm/favorites | Пользователь | Нет | {companyNames} |
| PUT /api/crm/favorite | Пользователь | company в query; {favorite:boolean} | Личный признак компании |
| GET /api/crm/hidden-companies | Пользователь | Нет | {companyNames} |
| PUT /api/crm/hidden-company | Пользователь | company в query; {hidden:boolean} | Личное скрытие компании |
| GET /api/tasks | Пользователь | company, vacancy; includeCompleted=1 | {tasks}; без includeCompleted только незавершённые |
| POST /api/tasks | Пользователь | TaskInput | 201, задача с полями состояния календаря |
| PUT /api/tasks/:id | Пользователь-владелец | {completed:boolean} или {dueAt:ISO_datetime} | {ok:true}; при переносе также dueAt в UTC и calendarSync. Дата и статус изменяются отдельными запросами |
| GET /api/hiring-contacts | Пользователь | company обязателен | {companyName,limit,contacts} |
| POST /api/hiring-contacts | Админ | ContactInput + companyName | 201, общий контакт |
| PUT /api/hiring-contacts/:id | Админ | ContactInput | Обновлённый контакт |
| DELETE /api/hiring-contacts/:id | Админ | Без тела | Удаление общего контакта |
| GET /api/channels | Пользователь | Нет | {channels} |
| POST /api/channels | Админ | ChannelInput | 201, новый канал |
| PUT /api/channels/:id | Админ | ChannelInput | Обновлённый канал |
| DELETE /api/channels/:id | Админ | Без тела | Удаление канала, обработка связей источников |
| POST /api/companies/recalculate-rating | Админ | Без тела | {ok,companies,agencies,agenciesSkipped}; изменяет каталог и рейтинг |


## Компактные компании, карта и карточки

Все методы требуют сессию, персональные признаки берутся только из неё. Ответы private/no-store. Нет серверной пагинации.

| Метод | Ответ |
| --- | --- |
| GET /api/companies/list | `{companies:[...]}`: id, name, rank, entityType, offices:[{address,metro}], middle, senior, lead, head, deferment, rating, wlbScore, hasVacancySource, favorite, hidden, hasVacancies, hasApplications, applicationStatus |
| GET /api/companies/map | `{companies:[...]}`: id/name/rank/entityType, offices:[{address,metro,lat,lon}], персональные favorite/hidden, hasVacancies/hasApplications/applicationStatus; без зарплат и подробностей карточки |
| GET /api/companies/:id | `{company:{...}}`: полная карточка, личные favorite/hidden и vacancyCount; без массива jobs, откликов, истории и задач |
| GET /api/companies/detail?name=... | Та же карточка по точному имени для ссылок из разделов, где ещё не загружен ID |
| GET /api/companies/search?q=... | `{matches:[{name,sector}]}`: поиск карты по прежним расширенным полям, запрос до 256 символов; список компаний этот метод не использует |
| GET /api/agencies/list | Аналог списка, дополнительно reviewsScore, salaryRangeSource, revenue.amount, functionalSpecializations для таблицы агентств |
| GET /api/agencies/:id и /api/agencies/detail?name=... | Полная карточка агентства |

Общий router также распознаёт GET /api/agencies/map и /api/agencies/search: map использует agency-проекцию, search вызывает тот же поиск компаний, что /api/companies/search; текущий интерфейс эти два agency-маршрута не использует.

Названия middle/senior/lead/head сохраняют прежний тип зарплатного диапазона. Lead/Head сохранены по решению пользователя для неизменности колонок. applicationStatus — существующий сводный статус: связанные с вакансией отклики приоритетнее общих, внутри группы действует порядок CRM_STATUSES с низким приоритетом отказа. hasApplications=true для любого личного отклика, включая общий и отказ. hasVacancies=true при наличии связанной вакансии, включая real_company_name. hasVacancySource вычисляется из источников на сервере; сами URL не возвращаются. Рейтинг и WLB передаются только числовыми значениями, без полных обоснований и tooltip WLB.

При первоначальном открытии по умолчанию активны задачи: `/api/tasks`, `/api/google-calendar/status`. Список впервые вызывает только `/api/companies/list`. Карточка впервые вызывает только detail; её вакансии, отклики, задачи и контакты загружаются по вкладкам. Повторные открытия используют память страницы, а фильтры и поиск названия работают без HTTP. После изменений допускается один повторный компактный запрос, без полного каталога. Штатная проекция компаний выполняет 6 SQL-операций независимо от числа строк, без N+1. Прикладной bootstrap схемы при первом обращении — отдельный этап.

GET `/api/tasks` и `/api/crm/applications` дополнены `entities:[{name,rank,entityType}]` для организаций из соответствующего ответа. GET `/api/vacancies` без фильтра дополнен компактным справочником `entities` и `applicationSummary:[{companyName,vacancyId}]` для сохранения пользовательской сортировки; тексты/история откликов туда не входят.

Ошибки каталога: 401 без сессии, 404 отсутствующая карточка, 405 неверный метод, 503 без DB. Контракт проекций не содержит полных независимых сущностей карточки.

## Компактные вакансии

GET `/api/vacancies/list` возвращает `{vacancies,entities,applicationSummary}` без пагинации. Необязательный query period принимает week/month/twoMonths/archive/all; неизвестное/пустое значение даёт400. Без period прежний полный компактный список сохраняется. UI по умолчанию запрашивает period=week. period=all возвращает все даты, включая отсутствующие/невалидные и будущие; другие фильтры интерфейса не сбрасываются. Границы: семь календарных дат включая сегодня по Москве; месяцы вычитаются календарно с ограничением дня последним числом месяца; archive строго раньше двухмесячной границы, недавние периоды включают обе границы и исключают будущее. Дата: валидная publishedAt, затем loadedAt, затем createdAt; в UI fallback отмечен~. Это read-only фильтр ответа, не UPDATE archived и не удаление. Поля вакансии: id, companyName, entityType, realCompanyName, title, salaryFrom, salaryTo, role, level, relevance, favorite, hidden, publishedAt, loadedAt, archived, createdAt, applicationId, applicationStatus, telegramSources:[{channelId,channelName}].

Нет url, descriptionMd, писем, revision, updatedAt/lastCheckedAt, полных Telegram-ссылок/контактов, рекрутера, истории и задач. entities: name/rank/entityType, personal favorite/hidden, hasVacancies/hasApplications, middle/senior/lead/head и applicationUrl для существующих колонок/кнопки сайта. applicationSummary содержит только общие личные отклики `{companyName,vacancyId:null}`; связанный отклик представлен в вакансии.

Штатное чтение использует 4 пакетные SQL-операции; однократный перенос исторических jobs и backfill профиля релевантности выполняются отдельно при необходимости. Нет SQL/HTTP на каждую строку ради признаков. Фильтры, поиск, сортировка локальные. GET `/api/vacancies/:id` возвращает одну полную карточку и минимальные entities владельца/работодателя. Отклики, задачи и история загружаются своими API по вкладкам.

Полное письмо имеет `coverLetterScope`: personal либо shared. Личный override_shared=1 приоритетнее заполненного общего письма, затем обычное личное. Чужие письма никогда не возвращаются. Старый GET `/api/vacancies` сохранён для карточек company/channel и совместимости; основной список использует /list. Excel экспортирует общие поля, а не effective личное письмо.

## VacancyInput и ответ вакансии

| Поле | Тип / обязательность | Правила |
| --- | --- | --- |
| companyName | string | Название владельца из каталога; для channel служебный владелец формируется обработчиком |
| entityType | string | company / agency / channel |
| realCompanyName | string, необязательно | Работодатель вакансии агентства/канала; правила ручного API и Excel различаются |
| url | string, обязательно | http/https, до 2048, уникальный URL |
| title | string, обязательно при ручной записи | Заголовок вакансии до 300 символов |
| role | string | Одна из четырёх VACANCY_ROLES; обязательно при ручной записи |
| level | string | Senior / Middle / Lead / Head; обязательно при ручной записи |
| salaryFrom, salaryTo | integer/null | Рубли ≥0, от ≤ до |
| relevance | integer/null | 0–100, личный контекст пользователя |
| publishedAt, loadedAt | string/null | YYYY-MM-DD |
| descriptionMd | string | Полный Markdown, обязателен вручную, до 100000 символов |
| coverLetterSubject | string | До 512 |
| coverLetterMd | string | Markdown до 2000 |
| coverLetterScope | personal / shared | Для PUT с полями письма обязательно; оба текстовых поля передаются вместе |
| telegramChannelId | integer/null | Существующий канал |
| telegramMessageUrl | string | Ссылка на сообщение выбранного канала; уникальная связь |
| telegramContact | string | Контакт в публикации до 256 символов |

PUT не является универсальным PATCH: основные поля проходят vacancyInput заново. Для владельца и некоторых связей/полей письма есть сохранение текущего значения при отсутствии. Передавайте полный редактируемый объект и не очищайте поля случайно. Форма сохраняет имеющиеся Telegram-связи при пустом выборе; удаление источника требует явного отдельного контракта обработчика. Канальную вакансию нельзя оставить без источников.

VacancyRecord включает id, companyName, entityType, realCompanyName, title, url, salaryFrom/To, role, level, relevance, publishedAt, loadedAt, descriptionMd, coverLetterSubject/Md, applicationStatus, favorite, hidden, archived, telegramSources[], lastCheckedAt, applicationId, createdAt/updatedAt и связанные значения, выбранные запросом. Точный набор преобразований — vacancyRecord; числовые/boolean значения нормализуются сервером. GET /api/vacancies/:id возвращает полную карточку отдельно от компактного списка.

## ApplicationInput

| Поле | Правила |
| --- | --- |
| companyName | Только для POST: существующая организация |
| vacancyId | ID подходящей вакансии или null для общего отклика |
| status | Пустая строка либо Откликнулся, Получил ответ, Скрининг, Собес 2, Собес 3, Собес 4, Офер, Отказ |
| comment | Markdown до 1000 символов |
| recruiterName | До 160 |
| recruiterPhone | До 64 |
| recruiterEmail | Корректный email до 254 либо пусто |
| recruiterContactType | Пустая строка, direct (прямой контакт), agency (агентство) |
| applicationSources | Непустой массив из hh, site, habr, email, telegram, hcm (HCM-система); при отсутствии используется ["hh"] |

GET /api/crm/applications возвращает владельцу все отклики, включая «Отказ»; скрытие по умолчанию действует только в интерфейсе раздела «Отклики».

Максимум 10 откликов пользователя на организацию. Вакансия должна относиться к организации или реальному работодателю; пара user_name/vacancy_id уникальна. PUT требует revision исходной записи и принимает все редактируемые поля: опущенный comment станет пустым, опущенный vacancyId — null. Отклик содержит id, компанию, связь с вакансией, статус, контакты, источники, createdAt/updatedAt и history[].

Синтетический пример создания общего отклика (организация должна существовать). Сохраните UUID и creationContext для повторов этого же намерения, включая потерянный успешный ответ:

```javascript
const {creationContext} = await (await fetch('/api/applications/create-context')).json();
const intent = {idempotencyKey: crypto.randomUUID(), creationContext};
const payload = {companyName:'Example Company', vacancyId:null,
  status:'Откликнулся', comment:'Уточнить состав команды',
  recruiterName:'', recruiterPhone:'', recruiterEmail:'', recruiterContactType:'',
  applicationSources:['site'], ...intent};
const response = await fetch('/api/applications/create', {
  method:'POST', headers:{'Content-Type':'application/json'},
  body:JSON.stringify(payload)
});
const result = await response.json();
if (!response.ok) throw new Error(result.error);
```

## TaskInput, ContactInput, ChannelInput

TaskInput: companyName, vacancyId (null допустим), type (call/email/messenger/meeting), note (≤1200), dueAt (разбираемая дата со временем и часовым поясом). Сервер приводит dueAt к ISO UTC. Рекомендуемый пример: 2026-10-01T10:00:00+03:00. Ответ: id, companyName, vacancyId, vacancyTitle, type, note, dueAt, completed, googleEventId, googleEventHtmlLink, googleSyncStatus, googleSyncError, createdAt, completedAt. DELETE задачи не реализован.

ContactInput: position (целое 1–3), fullName (обязательно, ≤256), linkedinUrl/facebookUrl/setkaUrl (http/https до 2048), telegram (≤128), phone (≤64), email (≤254), specialization (≤512). Для POST добавьте companyName. Контакты общие и имеют уникальный слот внутри организации.

ChannelInput: name (обязательно, ≤160), url или username публичного Telegram-канала, comment (≤512), lastSuccessfulCheckAt (дата/время либо null). PUT сохраняет текущие опущенные значения. Нормализуются url, username, normalizedUsername. Канал, уже указанный среди источников компании/агентства, нельзя добавить повторно как независимый. Ответ канала включает число вакансий и даты; внешняя отправка сообщений не выполняется.

Go GO-073 реализует этот CRUD в принятой GO-072 границе typed UI: текстовые поля и RFC3339-даты, JSON до16KiB. PUT сохраняет omitted/null поля; пустая строка очищает дату, comment сохраняет пробелы. Дубли имени сравниваются по исходному SQLite ASCII LOWER, список — по ASCII NOCASE; кириллический регистр не сворачивается. POST отвечает `{ok,channelId,channels}`, PUT — `{ok,channels}`. Readback входит в транзакцию. Удаление последнего источника канальной вакансии возвращает409; новые связи и параллельные удаления защищены блокировками. Контакты GO-074 и общая SQL/API приёмка GO-075 приняты #63/#64. SQL/HTTP/rollback/concurrent source proof принят в required guarded CI #62 (code d457a59); локальная MariaDB не использовалась.

Go GO-074 реализует `/api/hiring-contacts` (GET для сессии, POST админ/201) и `/api/hiring-contacts/:id` (PUT/DELETE админ), same-origin и JSON до32KiB. GET проверяет company/agency каталог и возвращает `{companyName,limit:3,contacts}`; записи общие для организации. POST/PUT/DELETE возвращают `{ok,contacts}` в порядке позиции. PUT сохраняет только omitted/null `position`; остальные поля обязательны или очищаются, `companyName` не переносит контакт. Дубли позиции и превышение трёх контактов возвращают400 с исходными сообщениями; неизвестный owner/ID —404. Все записи/readback атомарны. MariaDB/SQL/HTTP/rollback/concurrency приняты #63; общая GO-075/#64 прошла семь required targets и весь старый regression.

## Импорт, экспорт и бэкап

| Метод | Тело / параметры | Ответ |
| --- | --- | --- |
| GET /api/exports/companies | Нет | XLSX полного шаблона организаций |
| GET /api/exports/companies-vacancies | unranked=1 необязательно | XLSX источников вакансий |
| GET /api/exports/companies-reviews | Нет | XLSX источников отзывов |
| GET /api/exports/vacancies | Нет | XLSX 19 столбцов |
| POST /api/companies/import | {mode,companies,offices,reviews,agencies,contacts} | Счётчики организаций/офисов/отзывов/контактов и errors |
| POST /api/vacancies/import | {mode,rows} | imported,updated,duplicates,missingSkipped,limitSkipped,invalid,errors и дополнительные счётчики |
| GET /api/backup | Нет | JSON-файл v27 с Content-Disposition |
| POST /api/backup/validate | JSON-бэкап как тело | {ok,tables,rows,warnings}; без изменения данных |
| POST /api/backup | JSON-бэкап как тело | {ok,sourceVersion,version,companies,agencies,tables,rows,warnings}; заменяет данные транзакционно |

Все перечисленные методы — только для администратора. Точные столбцы, JSON-ключи, лимиты, правила пропусков и совместимость описаны в [форматах импорта/экспорта](TRANSFER_FORMATS.md). Бэкап затрагивает всех пользователей. Успешная предварительная проверка не отменяет повторной проверки при восстановлении.

## Google Calendar

| Метод | Доступ и параметры | Результат |
| --- | --- | --- |
| GET /api/google-calendar/status | Сессия | Состояние настройки/подключения текущего пользователя |
| GET /api/google-calendar/connect | Сессия; настроенный OAuth и DB | 303 на Google, cookie состояния OAuth |
| GET /api/google-calendar/callback | code/state от Google, проверка состояния и cookie | Сохраняет зашифрованный токен подключения, переход в приложение |
| POST /api/google-calendar/sync | Сессия, без тела | Синхронизирует задачи пользователя; 409 без подключения |
| POST /api/google-calendar/disconnect | Сессия, без тела | Удаляет подключение пользователя, пробует отозвать токен |

OAuth callback обрабатывается до общей проверки сессии, но проверяет собственное состояние привязки. Не передавайте через клиент секреты GOOGLE_CALENDAR_CLIENT_ID/SECRET и ключ шифрования. Токены не возвращаются браузеру как обычные поля API.

## Автоматизация вакансий

Все четыре маршрута требуют Bearer VACANCY_AUTOMATION_TOKEN. Этот доступ относится к задачам автоматизации; не заменяет административную сессию.

| Метод | Вход | Ответ и побочные эффекты |
| --- | --- | --- |
| GET /api/automation/favorites | Нет | {targets:[{name,entityType,vacancySources}]} — избранные организации Eugene |
| POST /api/automation/vacancy-import | {rows:[VacancyInput]} до 100 | 201, результат проходов create и update; только избранные организации Eugene |
| GET /api/automation/vacancies | limit от 1 до 30 (по умолчанию 30) | {vacancies}; неархивные, не проверенные либо проверенные более 7 дней назад |
| POST /api/automation/vacancies/:id | {outcome,publishedAt?} | {ok,action,publicationUpdated} |

outcome: active, closed, protected. active может обновить publishedAt (YYYY-MM-DD), protected отмечает проверку без вывода о закрытии. closed архивирует вакансию, если есть отклик или задача любого пользователя; иначе удаляет. Это существенное изменение данных, учитывайте при автоматизации.

Автоимпорт требует полный текст, не принимает публикации старше шести месяцев и указанную нижнюю зарплату ниже 200000. Работает только с избранными компаниями/агентствами Eugene. Сначала добавляет новые URL, затем обновляет найденные. Это не одна общая транзакция двух проходов.

## Документация

| Метод | Ответ |
| --- | --- |
| GET /api/docs | JSON {revision,updatedAt,versions,sections:[{id,title,file}]} |
| GET /api/docs/:id | Markdown выбранного раздела, Content-Type text/markdown; charset=utf-8 |
| GET /api/docs/:id?download=1 | Тот же Markdown с Content-Disposition attachment |

Только администратор: 401 без сессии, 403 для обычного пользователя, 404 для неизвестного id, 405 для записи. Cache-Control: private, no-store для Markdown. Документы не зависят от DB. Разрешённые id берутся из manifest, путь к файлу от клиента не используется.

## Конкурентное редактирование

Отклик и полная карточка вакансии возвращают `revision` — непрозрачный SHA-256 снимка сохранённых полей. Это не updatedAt, числовой SQL-счётчик или версия схемы. Передавайте исходный токен формы; не вычисляйте его из преобразованного ответа. PUT без корректного токена получает 428, устаревший — 409, отсутствующая/чужая запись — 404.

Отклик: WHERE UPDATE повторно сравнивает сырой SQL-снимок; история создаётся в том же batch только при changes()=1. Вакансия: снимок включает общую запись, Telegram-источники, собственную релевантность и личное письмо (даже скрытое общим), но не favorite/hidden или чужие личные данные. SQL CAS/guard откатывает весь batch при гонке. Поздняя SQL-ошибка не маскируется под конфликт, если исходное состояние не изменилось.

PUT вакансии с изменением письма требует `coverLetterScope` и оба поля письма. personal пишет только пользователю сессии; shared — общие поля. Отсутствующие поля письма не меняют его; отсутствие scope при переданном тексте — 400. POST/import не принимают personal scope. UI сохраняет исходный scope, не копирует личное письмо в общую запись автоматически.

POST вакансии объединяет создание, Telegram-источник, личную релевантность и SQL-пересчёт вилок агентства в batch. PUT также объединяет перенос связанных откликов/задач при смене владельца, источники и письмо. Проектор считает MIN/MAX по agency/level, сохраняет ручные непустые вилки; при отсутствии изменений JSON-каталог не переписывается. Ленивая инициализация до batch, чтение ответа после commit, DELETE/import остаются отдельными границами. POST вакансии не имеет receipt-защиты потерянного ответа: повтор может вернуть дубликат URL.

После 409/428 отклик можно согласовать через GET `/api/applications/:id`: трёхстороннее сравнение исходной записи, черновика и текущего состояния; «Применить к форме» не выполняет PUT. Редактор вакансии позволяет только прочитать актуальную карточку, сохраняя черновик; автоматического слияния нет. UI не заменяет исходный токен молча и не повторяет PUT с новым токеном.

## Идемпотентное создание откликов

GET `/api/applications/create-context` выдаёт `{creationContext}`: opaque ac1/HMAC-SHA256 контекст пользователя и поколения базы, private/no-store. UUID поколения и имя не раскрываются. GET может инициализировать операционную строку поколения, но не создаёт отклик/receipt. Context одинаков внутри одного пользователя/поколения, без независимого TTL; меняется после restore или смены SESSION_SECRET.

POST `/api/applications/create` и POST `/api/applications` требуют ApplicationInput, companyName, UUIDv4 `idempotencyKey` и `creationContext`. Один D1 batch создаёт CRM-узел, отклик, историю и receipt. Новый результат — 201, повтор — 200; `{companyName,favorite,hidden,limit,records,vacancies,createdApplicationId,replayed}`. Replay выполняется до лимита/изменившейся валидации каталога и не перезаписывает последующие правки. Повтор с другим телом/старым контекстом — 409, удалённый результат — 410, отсутствующий intent — 428, некорректный — 400; SQL/read failure — 503. Ответ после commit защищён поколением, потерянный ответ можно повторить с тем же intent.

POST `/api/vacancies/:id/apply` и `/apply/create` — строгие aliases того же механизма. Тело: companyName, UUID/key/context; route ID определяет вакансию. Новый отклик: «Откликнулся», пустой комментарий/контакты, источники [hh]. Другие поля тела не подменяют их. Уже существующий личный отклик возвращается без изменения полей/истории, даже при лимите 10. Ответ 200 `{ok,vacancy,createdApplicationId,replayed}`. Смена владельца — 409, удалённая вакансия нового intent — 404. Кнопка приложения не отправляет резюме работодателю.

Не создавайте новый ключ после ошибки/потерянного ответа. После restore старое намерение не получает автоматически свежий context. Ключи живут в открытой форме; после перезагрузки сначала проверьте список, а не создавайте дубль вслепую.

## Письма: генерация и повторное создание

POST `/api/vacancies/:id/cover-letter` без тела возвращает сохранённое effective письмо без модели. Если текста нет, читает 4 входа: общий промт cover_letter, личное резюме, заголовок и Markdown вакансии. При отсутствии резюме/промта/описания — 409; неизвестный ID — 404. Результат `{coverLetterSubject,coverLetterMd,source}`, source personal либо vacancy. Subject ≤512, сохранённый Markdown ≤2000 Unicode-символов. Парсер требует subject/markdown; ответ 2001–4000 символов сокращается до допустимой границы, >4000 отвергается. Изменение исходных данных не перегенерирует уже сохранённый текст автоматически.

POST `/api/vacancies/:id/cover-letter/regenerate` разрешён только для письма с непустыми заголовком и текстом, иначе 409. Использует актуальные четыре входа и сохранённый provider/model/effort. Прежний текст остаётся до успеха. Новый ответ заменяет только личную строку, override_shared=1 и revision+1; общие/чужие письма не меняются. Параллельная успешная генерация возвращает сохранённый результат вместо повторной замены.

Оба метода с `Accept: application/x-ndjson` возвращают поток HTTP200: progress с stage (preparing/waiting/model/saving), elapsedSeconds и при model — model/attempt/total; затем done с letter либо error с сообщением. Первый кадр сразу, heartbeat каждые 8 секунд. Ошибка после начала потока определяется кадром error, не HTTP-статусом. Без Accept используется обычный JSON и HTTP-ошибка.

По паре пользователь/вакансия D1 lease даёт право на одну реальную генерацию между Worker instances. Lease 90 секунд, heartbeat 20 секунд; ожидающие проверяют результат примерно раз в секунду до 15 минут. Истёкшее право можно атомарно перехватить; ошибка владельца возвращается ожидающим без автоматического запуска всех моделей каждым клиентом. Это HTTP-процесс, не надёжная фоновая очередь: закрытие страницы не гарантирует выполнение.

Входы и revisions профиля/промта/модели/ключа OpenAI, identity/тексты вакансии, personal snapshot при regenerate и поколение restore проверяются в SQL в момент записи. Изменившиеся входы, потеря владельца или restore — 409; удаление — 404. Generation-bound DB защищает письма, AI-кэш/логи/метрики и admin-операции. Даже идентичный restore блокирует поздний результат; failed restore не отменяет текущий запрос. Тексты входов/ответов не записываются в технические логи.

## Общие системные промты

| Метод | Права | Вход и результат |
| --- | --- | --- |
| GET /api/profile/prompts | Администратор | {prompts:{cover_letter:{kind,markdown,revision,updatedAt},relevance:{...}},maxLength:5000,maxLengths:{cover_letter:15000,relevance:5000,vacancy_extraction:10000,vacancy_role:5000}}; общие промты системы |
| PUT /api/profile/prompts/:kind | Администратор | kind=cover_letter или relevance, {markdown:string,revision:integer}; возвращает сохранённый промт и новую редакцию. 400 при неверном тексте, 409 при устаревшей редакции, 413 при превышении размера запроса |

Промты общие для системы и не привязаны к пользователю или вакансии. Кандидатам оба метода возвращают 403. Каждый kind сохраняется отдельно. Пустой Markdown разрешён, NUL запрещён; лимит cover_letter 15 000, vacancy_extraction 10 000, relevance и vacancy_role 5 000 Unicode-символов. GET возвращает maxLengths по видам; legacy maxLength=5000 сохранён для старых клиентов, PUT возвращает maxLength соответствующего вида. Предельный размер тела запроса: 128 КиБ для cover_letter/vacancy_extraction, 32 КиБ для relevance/vacancy_role. PUT проверяет Origin и не принимает имя пользователя или ID вакансии от клиента. Сохранение промта не запускает AI-функции.

## OpenRouter — административные методы

Все маршруты /api/openrouter доступны только по сессии администратора (401 без входа, 403 другим пользователям). Изменяющие запросы с чужим Origin отклоняются. Ответы не кэшируются. OpenRouter API v1 вызывается только сервером.

| Метод и путь | Вход | Результат |
| --- | --- | --- |
| GET /api/openrouter | — | configured, key_mask, connection_status, warning, models, primary_model_id, active_test_model_id, models_synced_at, last_used_model, last_used_at, empty_message, cache_hours; ленивое обновление после 6 часов |
| PUT /api/openrouter/key | JSON {apiKey: строка sk-or-…} | Сохранение зашифрованного ключа, попытка обновления списка, публичное состояние |
| POST /api/openrouter/check | — | Проверка /api/v1/key и публичное состояние |
| POST /api/openrouter/refresh | — | Принудительное обновление списка и публичное состояние |
| POST /api/openrouter/test | — | Фиксированная служебная фраза; ok, model, fallback_used, attempted_models (ID в порядке попыток), state. Текст ответа не возвращается |
| GET /api/openrouter/logs | — | logs: последние 50 технических записей |
| GET /api/openrouter/jev | — | Админ; текущие id/name/contextLength/pricing/source/fetchedAt модели `typesafe/jev-1.13` из каталога OpenRouter с явной модальностью `decisions`; платный Decisions-запрос не выполняется |

connection_status: missing, unchecked, connected, error. Ошибки содержат error (русский текст) и code; 400 — ключ/ввод, 409 — настройки изменились параллельно, 502 — сбой провайдера, 503 — нет моделей/все недоступны/нет БД. Полный ключ и ciphertext через эти методы не возвращаются. Модель в models содержит id, name, canonical_slug, context_length, pricing, supports_reasoning, intelligence_index, agentic_index, coding_index, rank, is_eligible, fetched_at. Отсутствующая оценка — null.

OpenRouter выбирает бесплатные текстовые модели с контекстом ≥262144 и поддержкой рассуждений. Порядок: Intelligence, Agentic, Coding, контекст; отсутствующие оценки после известных. Тест начинает с первой, письмо — с последней успешной тестовой, затем строго следующая по текущему рейтингу. Случайного выбора и провайдерного model fallback нет. Непригодный формат считается неудачной попыткой. Таймаут одной генерации 210 секунд, служебных GET 25 секунд.

У models есть letter_timing: success_count/average_ms/last_ms/last_success_at/failure_count/last_failure_ms/last_failure_at, либо null. Логи cover_letter_attempt описывают реальную попытку, итог cover_letter — результат операции; хранятся последние 200, GET выдаёт 50. Время измеряется от запроса к модели до проверки полного ответа. Замеры не меняют автоматически базовый порядок Intelligence.

## OpenAI и выбор модели каждого промта

| Метод | Доступ и контракт |
| --- | --- |
| GET /api/profile/prompt-models | Админ; models по kind: provider/openaiModel/openrouterModel/reasoningEffort/revision/updatedAt |
| PUT /api/profile/prompt-models/:kind | Админ; три настройки + исходная числовая revision, CAS409, до 4096 байт |
| GET /api/openai | Админ; configured/key_mask/models/requestedDefault/defaultModel/warning |
| PUT /api/openai/key | Админ; ровно {apiKey:string}, sk-… кроме sk-or-…, поток до 2048 байт; {configured,key_mask} |
| POST /api/openai/refresh | Админ; обновляет доступный аккаунту каталог, TTL6 часов |
| POST /api/openai/test | Админ; проверяет OpenAI с конфигурацией письма, фактическая model и state |

kind cover_letter/relevance; provider openrouter/openai. Промты/модели общие, не принимают пользователя/вакансию, кандидат получает 403. Сохранение текста и модели — отдельные операции/revisions. Расчёт relevance вызывается явно через POST /api/vacancies/:id/relevance, используя выбранную модель.

Ключ OpenAI из DB приоритетнее OPENAI_API_KEY среды; AES-GCM через SESSION_SECRET с доменом openai:v1. Полный ключ/ciphertext не возвращаются браузеру. Сервер вызывает только GET `/v1/models` и POST `/v1/responses`, store:false; нет произвольного base URL. Выбранная модель/провайдер при ошибке не заменяются. Таймаут 300 секунд, служебный GET25 секунд. До записи проверяются completed, assistant output_text, отсутствие refusal, ID выбранной модели либо её dated snapshot и формат письма. Ошибки очищены от секрета/текста запроса.

GPT-6.1 Luna — запрошенный default только если доступна аккаунту. Иначе default выбирается из доступных рекомендованных и показывается warning. Неизвестные capabilities разрешают только auto, который не передаёт reasoning; подтверждённые effort перечислены в server/integrations/openai-models.js. API списка моделей не служит источником capabilities или цен.

Каждая models[] содержит reasoningEfforts/recommended, letterTiming и pricing: null либо {inputUsdPerMillion,outputUsdPerMillion,currency:"USD",tier:"standard",verifiedAt:"2026-10-09",source:URL}. Это локальный проверенный справочник Standard из openai-pricing.js, неизвестные snapshots/fine-tunes не наследуют цену. Refresh обновляет доступность, не тарифы. Метрики OpenAI хранятся в общей таблице под model_id `openai:<id>`.

Оценка под промтом письма вычисляется только клиентом:20k/5k русских символов, условно 2 символа/токен →10kinput/2.5koutput на письмо. RUB за 1000 = (10000×inputUSD+2500×outputUSD)/1e6×1000×85. Токены рассуждений добавляют расходы, число заранее неизвестно. При неподтверждённой цене оценки нет. Отдельных методов цены/баланса/оплаты нет; UI баланса даёт ссылку на кабинет OpenAI без числового остатка. API-использование отдельно от ChatGPT-подписки.

Оценка под промтом релевантности также вычисляется только клиентом: 15k русских символов/2 →7.5k входных токенов, число0–100 → один выходной токен, 1000 ответов, 85 ₽/USD. Для OpenAI используются USD/1M Standard; для JEV поля каталога OpenRouter `prompt`/`completion` в USD за токен и необязательное `request` в USD за запрос. Бесплатный OpenRouter имеет нулевую оценку. Служебный формат и токены рассуждений заранее не известны и могут увеличить фактическую стоимость.

## Перенос задачи и удаление канала

PUT `/api/tasks/:id` с dueAt принимает ISO-дату/время с секундой и явным поясом Z или ±HH:MM. Одновременное completed, невозможная/пустая дата —400. Чужая/отсутствующая задача —404. Меняется только due_at владельца; calendarSync.status synced/not_connected/skipped/error. Ошибка календаря не отменяет DB-сохранение: HTTP200, google_sync_status=error, можно синхронизировать повторно.

DELETE `/api/channels/:id` только для админа. Если канал — единственный источник канальной вакансии, SQL DELETE с NOT EXISTS блокирует удаление:409, без изменения связей. Если есть другой источник, удаление допустимо; у вакансий компаний/агентств последний Telegram-источник необязателен. Нет канала —404; успех `{ok,channelId,name,detachedVacancies}`. UI добавления/удаления расположен в «Список → Каналы», не «Настройки».

## Оболочка и публичные файлы

GET `/` и `/index.html` возвращают Vinext/React оболочку без данных разделов; при отсутствии сессии React показывает вход после /api/session. Неверный POST /auth возвращает оболочку входа HTTP401 через внутренний x-app-login-error. PWA-файлы: /manifest.webmanifest, /service-worker.js и /icons/*.png из public. MapLibre входит в клиентскую сборку npm, старые /maplibre-gl-csp.js пути не контракт текущей сборки. Service Worker не кэширует /api/. После /api/session запрашиваются только активный раздел (по умолчанию задачи + calendar/status); память страницы сохраняет успешные данные при переключении, HTTP-ответы остаются private/no-store.

OpenRouter: `openrouterModel` = `best_free` (по умолчанию, включая старые клиенты) или `jev` (только kind=relevance). GET/PUT возвращают это поле. При выборе JEV сервер проверяет платную `typesafe/jev-1.13` в каталоге OpenRouter через `GET /api/v1/models?output_modalities=decisions`; обычный каталог без параметра содержит только text-модели и не является отрицательной проверкой доступности JEV. Неподдерживаемый выбор, в том числе JEV для cover_letter, возвращает 400; настройки не изменяются. CAS и admin-only сохранены.

### Расчёт релевантности одной вакансии

POST `/api/vacancies/:id/relevance` доступен авторизованному пользователю, тело не требуется. Сервер читает сохранённые заголовок, полное описание и фактические salary_from/salary_to (если есть), личное резюме пользователя, общий промт relevance и выбранную модель. Возвращает `{vacancyId,relevance:number,model}`; relevance — целое0–100 без знака%. Записывает только company_vacancy_user_state.relevance текущего пользователя, не общую вакансию и не оценки других пользователей. Сохранение проверяет единый снимок вакансии/резюме/промта/модели/ключа/предыдущей оценки и поколение БД. Изменённые входы или restore дают409, ошибочный ответ/провайдер — ошибку без замены прежнего числа. Origin проверяется; пустой POST принимается даже если среда исполнения представляет его нулевым потоком, реальные байты тела дают400, неизвестная вакансия404, неавторизованный401, GET405. Пустое резюме/промт/описание409 до вызова модели. JEV вызывается через Decisions API, числовой Score нормализуется в0–100; бесплатный/OpenAI режим возвращает проверенный JSON с числом. Вызовы выполняются только по действию пользователя.

## Классификация по корневым ролям (13.10.2026)

Admin-only GET /api/classification/settings возвращает roles: roleId, name, markdown, revision, updatedAt, enabled, defaultMarkdown, descendants. PUT /api/classification/prompts/:id принимает markdown/revision; общий лимит 10000 Unicode-символов, revision CAS. Конфликт версии или перевод в дочернюю роль:409. Посторонний Origin:403. Ключей/вакансий в ответе нет.

Admin-only PUT /api/classification/roles/:id принимает enabled:boolean и revision:int; CAS409, дочерние роли отклоняются. Изменение не удаляет промты/вакансии. activeClassificationRoles(db) возвращает только включённые корни с непустым промтом, включая ID и revision.

Admin-only POST /api/classification/test принимает только {title:string}, ограниченный JSON32KiB/title500 Unicode. Возвращает title, checkedRoleIds, results[{roleId,name,promptRevision,decision:match|reject|null,error}], model, status:complete|incomplete|no_active_roles, accepted:boolean|null, durationMs, requestDurationMs, costUsd/costRub:null|number, rubPerUsd85. Технический error не является решением. accepted:true при любом match, false только если все ответы успешны/reject, иначе null. При отсутствии направлений статус no_active_roles без inference. Поколение restore409 и изменения промтов/переключателей блокируют устаревшие результаты. HTTP400 ввод,403 права/Origin,405 метод,413 размер. Серверный classifyVacancyTitle(env,title,{signal}) можно использовать без HTTP/UI.

## Первичные вакансии (13.10.2026)

Все /api/vacancy-candidates доступны только администратору; изменения проверяют Origin, тело JSON ограничено32KiB. POST /api/vacancy-candidates принимает titleRaw(500), vacancyUrl?, companyId?, companyNameRaw?, sourceUrl?, sourceId?, sourceNamespace?, externalJobId?, importKey?, acquisitionMethod:site|firecrawl|excel|manual. Нужна устойчивая идентичность: URL либо namespace+externalJobId/importKey. Только существующий company_identity ID; sourceId — ключ существующего VACANCY_SOURCE_META. Ответ201 для новой записи,200 для повтора: id,created,candidate. Несовместимые алиасы/внешние ID409, без автоматического объединения. Нормализация сохраняет значимые query, удаляет fragment/utm_*/gclid/fbclid/yclid/msclkid, исходный URL и заголовок сохраняются отдельно.

GET /api/vacancy-candidates: page1,pageSize50(max100),order:asc|desc по lastSeenAt/id, companyId?,sourceId?,sourceNamespace?,status?. Ответ candidates,total,page,pageSize; LIMIT/OFFSET и COUNT выполняются на сервере. GET /:id возвращает candidate,currentResults,needsClassification,history,attempts,identityKeys. История/попытки имеют отдельную страницу historyPage/historyPageSize(max100). Нет автоматического обращения к JEV при чтении. PUT /:id/link принимает только mainVacancyId, связывает с существующей основной вакансией без её создания/изменения. Другая уже заданная связь или несоответствие известной компании409.

processingStatus вычисляется из текущих корней/непустых промтов и их SHA256, выбранного семейства JEV, title_hash и title_revision. imported имеет приоритет связи; no_active_roles не равен rejected; accepted при любом актуальном match; rejected только когда у всех активных ролей есть актуальное решение и все reject; classification_error при актуальных технических ошибках без match; иначе pending_classification. Исторические записи не удаляются.

POST /api/vacancy-candidates/:id/classify принимает {force?:boolean}, по умолчаниюfalse. Вызывает существующий classifyVacancyTitle только по недостающим корням (внутренние roleIds/expectedPromptVersions, недоступны в тестовом HTTP-маршруте заголовка). Ответ дополнительно reused,requestedRoleIds,attemptId. При полном актуальном кэше или отсутствии настроенного классификатора нет даже запроса каталога OpenRouter. force:true повторяет все активные корни и дописывает историю. Технические ошибки возвращаются в attempts.errors_json; vacancy_classifications содержит только match/reject. Один batch_cost_usd хранится в попытке и не распределяется выдуманно по ролям; history.batch_cost_usd относится к общей попытке, не складывайте повторно строки одного attempt_id. Фактический model_id сохраняется отдельно от настроенного model_key семейства JEV. Переключатели не инвалидируют кэш при неизменном хеше Markdown, а редактирование одного промта инвалидирует только соответствующую роль.

Параллельная обработка одной записи409 candidate_busy, истёкший lease можно заменить. Изменение заголовка/владельца до записи409 candidate_changed; restore409 restored. Решения и попытка записываются одним D1 batch. Detail включает транзакционно согласованные состояние/текущие решения/историю; identityKeys ограничены100 алиасами. Список никогда сам не обращается к JEV. Большие наборы первичных записей не загружаются во frontend.

Пространство внешнего ID задавайте явно, если адаптер объединяет разные страницы одного издателя. Без явного sourceNamespace сервер разделяет origin публикации и конкретную страницу обнаружения: одинаковые ID разных площадок/каналов не смешиваются. URL-алиасы продолжают объединять повторное обнаружение той же публикации.

## Firecrawl: подключение (14.10.2026)

Admin-only `/api/firecrawl/settings` GET и PUT `{enabled,favoritesOnly?,maxPages,concurrency:1,timeoutMs,proxy,enhancedAllowed,revision}`. `/api/firecrawl/key` PUT `{apiKey,revision,mode?}` сохраняет AES-GCM и возвращает только маску. `/api/firecrawl/check` POST `{mode?}` читает `/v2/team/credit-usage`, без Scrape. Проверка Origin, bounded JSON, generation и CAS; глобальный lease защищает параллельность1. 1–10 страниц (по умолчанию3), timeout5–60с (45с), basic по умолчанию; enhanced только в Hobby с глобальным разрешением и явным useEnhancedProxy конкретного источника, auto отсутствует.

GET settings возвращает favoritesOnly:boolean (defaulttrue). PUT проверяет boolean; отсутствие поля сохраняет прежнее значение для старых клиентов. При true источники фильтруются по company_user_state.favorite текущего admin; test/resume/read/save и транзакционные записи проверяют тот же фильтр, отказ403 code=favorites_only. Pending скрывает исключённые компании. Снятие favorite или включение фильтра во время запроса блокирует последующие операции.

Сбор (этап Б): GET `/api/firecrawl/sources?companyId=` возвращает только существующие непустые ссылки и настройки, не отдельный справочник. PUT `/api/firecrawl/sources/:companyId/:sourceId` `{enabled,sourceUrl,revision,config}` сохраняет настройки без изменения URL каталога. config: useEnhancedProxy (boolean, defaultfalse), adapter(generic/habr/getmatch/rabota/selectel), html, titleClass (один HTML-класс заголовка внутри a), detailPathPrefix, allowedHosts (до3 явных доменов), pagination:null или {queryParam}. POST `/api/firecrawl/test` `{companyId,sourceId}` создаёт серверный preview TTL1час/200пар, не candidates. GET `/api/firecrawl/runs/:uuid` возвращает preview текущего admin и поколения. POST `/api/firecrawl/runs/:uuid/save` `{}` upsert-ит серверные пары без повторного Scrape и затем инициирует отдельную классификацию JEV; отдельные ошибки сохранения не откатывают ранее успешные записи. Повторное сохранение не дублирует публикации. Ошибки: disabled400, busy409, changed409, expired410, blocked/target/extraction в result.errors; result.status complete/partial/error, statistics/pages/diagnostics. Один общий lease, страниц<=maxPages, общий request budget maxPages+1 (один резерв на explicit429/503); timeout/network POST не replay-ятся.

## Режимы Firecrawl (задача4.1, 14.10.2026)

GET settings дополнен mode(free/hobby), profiles (две маски/статус/дата проверки, без ключей), sameKey и централизованными limits. PUT key принимает необязательный mode (старый контракт сохраняет Free). Замена Hobby-ключа выключает Hobby до повторной проверки и подтверждения. POST check принимает {mode?} и требует полный credit-usage с действующим расчётным периодом. PUT `/api/firecrawl/mode` {mode,revision,confirmed:true для hobby}: admin/CAS/Origin, проверка Hobby key и включённого пакета5000; downgrade не требует сети и сбрасывает proxy=basic/enhancedAllowed=false. API не сообщает название подписки или настройки pay-as-you-go: пакет5000 — проверка технического доступа, пользователь дополнительно подтверждает Hobby/выключенный pay-as-you-go в кабинете.

Весь Scrape проходит через services/firecrawl-usage-policy.js, opaque server session фиксирует профиль/ключ до конца запроса. Raw transport закрыт для обработчиков. Перед каждой попыткой: актуальный профиль/CAS, global active<=1, sliding-minute10/100, живой GET credit-usage и реальный период, атомарный INSERT reservation в D1. Agent/JSON/Crawl/Map/неограниченные опции/auto/proxy-переходы запрещены. Free всегда basic. Обычный HTML Scrape резервирует верхнюю оценку4 (base1+возможная командная scan2+ZDR1); provider-routed LinkedIn/X/Twitter исключены. Фактический creditsUsed заменяет резерв; без точного значения или после неопределённого завершения остаётся верхняя оценка. После timeout/network незавершённый слот удерживается6минут, неопределённый расход до конца периода. Минутный лимит и собственный бюджет учитывают смену ключей/режимов; один и тот же ключ делит историю обоих профилей.

Учёт консервативно суммирует локальный полный расход (known/unknown/reserved) и верхнее наблюдавшееся usage провайдера. API не подтверждает, какие запросы уже включены в баланс; без такой гарантии нельзя освобождать локальный known расход. Это может учитывать подтверждённые кредиты дважды и останавливает сбор раньше полного пакета, защищая от задержки provider billing. Provider used — оценка planCredits−remainingCredits, не выдаётся за точный счёт при бонусах. Бонусный баланс выше planCredits, отсутствующий период/статистика или неподдерживаемый пакет блокируют Scrape. Новые запросы не переходят на Hobby/другой ключ при исчерпании. Paused preview сохраняет найденные пары и адрес необработанной страницы; очистка обычных previews не удаляет paused.

GET `/api/firecrawl/usage?mode=free|hobby` читает кэш и локальный журнал без обращения к Firecrawl; POST `/api/firecrawl/usage/refresh` `{mode?}` явно обновляет credit-usage. Freshness60секунд и действующий период обязательны; иначе budget:null и понятная причина, старые цифры скрываются. provider/local/reserved/estimated показаны раздельно. Даты периода берутся из API, сброса первого числа нет. Новый период допускает продолжение только после новой проверки.

GET `/api/firecrawl/pending?companyId=&sourceId=` возвращает до50 приостановленных результатов текущего admin/поколения. POST `/api/firecrawl/runs/:uuid/resume` `{}` продолжает с сохранённого необработанного URL, повторно проверяет источник/CAS и создаёт новую policy session с текущим режимом. Ранее обработанные страницы не загружаются. Paused preview/save не истекает через1час; обычный preview истекает. Источник должен остаться прежним. Restore меняет поколение и отменяет старые preview/запросы, но не откатывает операционный журнал расходов. Выключение Hobby не запускает автоматическое продолжение Free: это отдельное ручное действие. Синхронный Scrape не предоставляет приложению отмену уже отправленного provider job.

## Автоматическая классификация, этап А (15.10.2026)

Единая candidate projection дополнена classificationComplete/needsClassification. Accepted не скрывает недостающие роли: ожидающие выбираются по needsClassification, включая accepted с частичным match. Все reject требуются для rejected, no_active_roles не равен reject. Серверная выборка ограничена50 и поддерживает candidate/company/source/IDs, исключает imported. Статистика вычисляется с живыми активными ролями и хешами промтов/заголовков/model_key. Только внешние метаданные или крайние пробелы titleRaw не инвалидируют решения; изменение содержимого заголовка сохраняет историю и требует пересчёта. Эта выборка используется пакетной очередью и административным интерфейсом.

## Пакеты JEV (15.10.2026)

Admin-only GET `/api/classification/jobs` возвращает динамическую statistics, limits и20последних пакетов текущего поколения. POST `{requestId(UUID),candidateId?,companyId?,sourceId?,limit<=50,force?,confirmed?}` выбирает только missing решения (force требует confirmed), возвращает202/jobIds/queued/reused/execution. GET `/api/classification/jobs/:uuid` показывает durable progress, outcome/error каждого ID, duration/known sum/unknown cost count. POST `/:uuid/resume` `{}` повторно запускает незавершённый пакет; completed не пересчитывается. Повтор requestId с тем же snapshot возвращает тот же job, candidate lease/cache не допускают двойного inference.

Save Firecrawl после успешного staging upsert ставит именно сохранённые ID (до200) в пакеты по50. Preview/test не вызывает JEV. Queue failure не откатывает исходные данные. Saved response.classification показывает queued/reused/jobIds; ctx.waitUntil инициирует серверное выполнение, request.signal не передаётся в фон. Строго<=2title-classification inference глобально, job owner/generation/CAS и прежний candidate lease. Все роли title делятся одним Decisions запросом; retry до3 (250/1000/4000мс) толькоtemporary429/5xx/network, сохраняет успешные роли. Fatal key/credits/model errors останавливают новые запросы.

В Sites нет настроенного Queue binding. Используется fallback: один общий time slice24сек на фоновый запуск, request timeout<=20сек; невыполненный остаток остаётся paused/queued в D1 и продолжается вручную кнопкой/API. Вкладка для начатого slice не нужна. Автоматического бессрочного scheduler/обхода компаний нет; нельзя обещать завершение arbitrarily slow50в одном waitUntil. После прерывания candidate cache восстанавливает уже сохранённые решения; force baseline не повторяет уже обновлённую роль. Пакеты/слоты операционные, backup31 не меняется; restore делает старые jobs недоступными по generation. Расход хранится отдельно в candidate attempts/task=title_classification, usage.cost или null; незавершённое списание не превращается в0.

При timeout в фоновом job сохраняется technical attempt с null cost/model и отдельными error для запрошенных ролей; reject не подставляется. Неопределённая сетевая попытка удерживает общий слот до5минут, чтобы новую проверку нельзя было начать поверх внешнего незавершённого запроса. Imported исключены из missing selection/statistics и автоматически не классифицируются. Начавшиеся inference/audit metadata записываются в job до внешнего вызова.

## Полные тексты вакансий

GET /api/vacancy-details возвращает актуальную статистику, до50 подходящих записей и20 последних пакетов. GET /api/vacancy-details/:id — текущие JEV, исходный Markdown, структурированные данные, историю до100 записей каждого вида. POST /api/vacancy-details/jobs: {requestId:UUID,candidateId?:number,limit?:1..20,action?:pipeline|fetch|llm|refresh,confirmed?:boolean}. refresh требует одной candidateId и confirmed:true. GET jobs/:uuid читает пакет; POST jobs/:uuid/resume с {} ставит фоновое окно, POST jobs/:uuid/run с {} выполняет продолжение до120секунд. Завершённые этапы не повторяются. Все методы только администратору; мутации проверяют Origin. Ни URL, ни Markdown/JSON результата не принимаются от клиента.

Структурирование использует JSON Schema версии2: level — обязательная строка enum Middle/Senior/Lead/Head, null/произвольный текст отклоняется как invalid_output и не становится успешной новой обработкой. Остальные nullable поля сохраняют прежние правила. Оценка уровня допускает вывод по обязанностям согласно редактируемому промту; description_md должен сохранять исходные формулировки без пересказа. Исторические schema1 результаты и уже сохранённые карточки не меняются автоматически этим обновлением. Запроса отдельной модели для уровня нет.

## Безопасный импорт подготовленных вакансий

Только администратор; мутации проверяют Origin и ограниченный JSON. GET /api/vacancy-import возвращает до50 кандидатов и20 пакетов. POST /api/vacancy-import/roles/:candidateId с {} отдельно запускает/переиспользует выбор конечных ролей, может расходовать токены выбранной модели. POST /api/vacancy-import/preview: {candidateIds:[1..20ID],requestId:UUID,resolutions?:{ID:{existingVacancyId?:ID,createNew?:true}}}. Возвращает реальные планы/сравнения/причины и batch.id, без основных записей и вызовов LLM/Firecrawl. POST /api/vacancy-import/batches/:id/confirm с {confirmed:true} разрешает только просмотренный пакет; POST /run с {} продолжает до3 записей в следующем окне, GET batch читает progress. Все входы повторно проверяются в D1 batch; изменившийся reviewed plan требует нового предпросмотра. GET /api/vacancy-import/history/:candidateId — до100 операций. Дополнительные URL возвращаются в vacancy.publicationSources у GET /api/vacancies/:id.


## Диспетчер и очередь парсера

Admin/Origin/bounded JSON: GET /api/scheduler возвращает public settings (без cloudOwnerId), текущие counts,20 циклов/50 попыток, статистику и расходы. PUT /api/scheduler/settings {enabled,dayBudgetUsd,monthBudgetUsd,revision,priorities?,favoritesOnly?,sourceIds?,intervalDays?} CAS, контекст только текущего администратора. GET /api/scheduler/sources?page=&company= —30 существующих источников, personal favorite scope, период/последняя/следующая/ошибка. Индивидуальный PUT sources/:companyId/:sourceId отменён (410 global_schedule). Общие sourceIds — массив уникальных поддерживаемых типов, [] останавливает автоматическую обработку источников; intervalDays=1|3|7 применяется ко всем компаниям. favoritesOnly defaulttrue — избранные текущего администратора, false — все компании. sourceOptions в dashboard содержит названия/доступность/причины для чекбоксов. Старые settings без sourceIds читаются как объединение ранее включённых типов. Настройки области и бюджеты сохраняются с CAS; смена favorites синхронизирует редакции Firecrawl и scheduler. POST тотже/check {} ставит manual scan (не обходит favorites/FCenabled). GET queue?page=&status=&type=&companyId= —30 строк/total, без payload. POST jobs/:uuid/(retry|cancel) {} только незапущенные/ошибочные. POST run {requestId:UUID} вызывает bounded dispatcher без браузерного таймера. POST probe {} — отдельная diagnostic-only проверка даже при паузе, без внешних запросов и обработки очереди парсера.

POST /api/internal/scheduler/tick {requestId?,diagnostic?} не требует user session: обязательный отдельный Bearer SCHEDULER_TOKEN; секрет только в runtime. Default automatic request key — текущий час; повтор и parallel защищены. Diagnostic создаёт/читает одну постоянную безопасную запись.

POST /mcp поддерживает stateless JSON-RPC initialize/tools/list/tools/call. Discovery не содержит частных данных. scheduler_tick/status/probe требуют oai-authenticated-user-id, совпадающий с привязанным cloudOwnerId. Привязка POST /api/scheduler/cloud-owner {confirmed:true} требует admin session+Origin и проверенную identity header Sites; ID не принимается из тела. Service bypass Sites не создаёт эту identity и не заменяет авторизацию.

Диспетчер (16.10 вечер): ready queue исполняется до reconciliation. Подготовка пустой очереди ограничена12с/5источниками и резервом35с, классификационные kickoff jobs записываются пакетно с прежними version keys. Все6этапов автоматического pipeline не требуют дополнительных HTTP confirm. Confirm endpoints ручных пакетов импорта сохраняют прежний контракт и не являются зависимостью scheduler.

### Количество вакансий (16.10.2026)

Общий предел вакансий на компанию/агентство снят. GET `/api/vacancies?company=…` возвращает `limit: null`; Excel-результат сохраняет `limitSkipped: 0` для совместимости. Ограничение размера одного запроса/пакета и отдельный лимит откликов не изменены.

Scheduler/LLM error_code `timeout` означает прерывание по времени; DOMException23/23.0 больше не сохраняется как provider code. Очередь автоматически повторяет временный сбой с cooldown6мин до max_attempts (по умолчанию4); исчерпание остаётся failed. `time_slice` откладывает запуск до свежего окна без расхода попытки.

### Серии тиков и ручной поток (16.10.2026)

Историческое восстановление `apify-listing-replay` допускает known-cost succeeded или failed listing при прежних parent/source/key/favorite/generation guards. Для failed до каждого незавершённого slice выполняется GET существующего run: FAILED/TIMED-OUT/ABORTED, точное совпадение run ID/dataset ID/account (если userId предоставлен). Неверный/активный/неизвестный статус отклоняется без кандидатов. Ledger state/identity/key/cost-known защищены SQL guard. Report дополнен sourceRunState/providerStatus/status; failed status всегда partial, complete означает только чтение всего фиксированного dataset, coverage.hhComplete=false. Completed replay не делает новых provider GET. Сам ledger и parent не переписываются.

В dashboard/MCP массив attempts содержит candidate_id из связанного scheduler job (null для источника/пробы или отсутствующего job). Только для classify_title возвращается outcome.accepted; для корректного assign_roles — outcome.roleIds и вычисленный outcome.filtered (пустой список). Сырые result_json и произвольные поля провайдера не возвращаются; чтение ограничено50 попытками и2048 символами результата на попытку. Неполный/повреждённый результат не получает outcome. Это read-only диагностика, не восстановление leases или запуск этапов.

Успешный текущий выбор конечных ролей [] завершает автоматический конвейер без import/relevance и без повторного LLM. Он сохраняется до обычного изменения входных хешей/промта/справочника. Ошибочный ответ/неразрешённые ID не становятся успешным отказом; старые failed import receipts не переписываются. Ручной importer по-прежнему блокирует roles_unresolved.

`vacancy_role` поддерживает openrouterModel=jev через сохранённое поле vacancy_role_settings.openrouter_model (SQL0074). PUT prompt-models проверяет каталог Decisions и CAS, не выполняет платный запрос. Конечные роли: один Decisions payload, state={vacancy:{title,markdown}}, полные правила сохранённого промта входят в instructions каждого вопроса; независимые questions.leaf_<id> типа choice с match/reject и полным путём роли. Все ответы обязательны; чужой/пропущенный/неверный ответ, другая модель или >12 подтверждений отклоняются целиком. JSON role_ids формирует сервер, после прежней валидации. Полный текст не режется; консервативный byte bound с4096 служебных байтов ограничивает запрос текущим context_length каталога. Бюджет/двеLLM leases/поколение/ключ/таймаут прежние, перехода на Luna/free при ошибке нет. Кэш включает модель, промт, дерево, снимок и исходные решения; старые результаты не перезаписываются. MCP diagnostics.models показывает настройки четырёх этапов без промтов/ключей. Бэкап37 переносит выбор, v33–36 получают best_free при прежнем OpenRouter; ручной выбор OpenAI/текст сохраняется.

MCP `scheduler_status` дополнительно возвращает `diagnostics:{queue,imports,freshness,apifyDetails,apifyListings}`. Queue группирует задания текущего поколения по type/source_id/status; imports содержит10 последних action/status без тел вакансий. Freshness содержит publicationDates (count/max публикации, загрузки и создания по entity_type) и recentAdditions (последние7 суток добавлений по календарному дню Москвы, count/min/max публикации). Даты публикации не заменяются датами добавления; отсутствие свежей публикации в базе не доказывает её отсутствие у работодателя.

ApifyDetails проверяет до4 последних существующих detail run и ApifyListings до3 listing (последний, с наибольшей запрошенной страницей и последний failed), только GET после enabled/paidAllowed/key hash: providerRunId/datasetId, providerStatus/buildId, число items, имена полей, длины description_text/description_html, диагноз и ошибки валидации. Input.source содержит только тип HH URL, числовые vacancyId/employerId/page/perPage, enum orderBy и признак date filter, без полного URL и остальных query. Input.pageMatchesOperation сравнивает страницу listing с operation key, vacancyMatchesCandidate — ID detail с candidate; null означает отсутствие сравнимых данных. Флаги/лимиты INPUT проверяются по типу/границам. LogSummary дополнен до10 уникальных безопасных HH URL projections и счётчиками fallback/enrichment/missingDescription; совпадение слова в логе — признак, не доказательство успеха запроса. Терминальный failed listing также проверяется на доступный dataset и получает partial_listing_dataset, не признак полного сбора. При точном ID и активной записи отсутствие title/description даёт incomplete_record и fixed-name missingFields; несовпадение ID/архив — identity_or_archive, валидация импортируемых данных не ослабляется. Сырые log/INPUT, секреты и тексты не возвращаются. Actor не запускается, ledger не изменяется. HTTP dashboard не делает внешних запросов.

MCP `scheduler_tick` без параметров только подготавливает новую серию: status=prepared, receivedAt, series.index=0/maxTicks=5, continuationRequestId=cloud:<UUID>:1. Не создаёт run/attempt и не обрабатывает очередь. При паузе status=paused/continuation=null. Повтор потерянной подготовки безопасен: каждый ответ выдаёт новый ID до начала работы. Рабочий вызов передаёт исключительно известный continuationRequestId в requestId; потерянный рабочий ответ повторяется с тем же ID, по durable receipt. Серия не зависит от смены часа. Предыдущий тик должен завершиться в текущем поколении; восстановление базы инвалидирует продолжения. Максимум5 рабочих тиков/12мин от начала root, каждый до50 заданий/110с; одна подготовка плюс до5 рабочих вызовов. Старые явные series:<hour>:<index> совместимы с прежним часовым ограничением. Произвольный requestId выполняет одиночный manual tick без продолжения и больше не обновляет automatic signal. Старую неоднозначную историю не переписываем. Ответ рабочего тика содержит series.id/index/maxTicks и continuationStopReason (paused/run_active/run_interrupted/tick_limit/series_expired/no_ready_work, null если есть продолжение). Безопасный structured worker log scheduler_series содержит те же ID/index/reused/stopReason, без тел и ключей. Dashboard lastSignalAt ограничен текущим generation и означает последний automatic серверный тик, не гарантирует запуск внешней задачи; nextSignalAt — расчётный ориентир, не подтверждённое расписанием время.

POST `/api/scheduler/run` с Accept `application/x-ndjson` передаёт progress/done/error; done содержит run. JSON-ответ для старых клиентов сохранён. GET `/api/scheduler/run?requestId=UUID` читает только сохранённый цикл текущего поколения, admin-only, 404 для неизвестного UUID; повторный POST не выполняет. При обрыве потока UI до3 раз читает тот же receipt, сохраняя явные 4xx/отмену. Heartbeat8с, живой progress, прекращение внешнего запроса и сохранение прогресса при закрытии потока; waitUntil применяется для уборки после disconnect, а не многоминутного фонового выполнения. Доступ admin/Origin/UUID прежний.

Claim выбирает только помещающиеся по времени типы, не расходуя попытки длинных задач. Независимые LLM stages могут использовать два прежних слота одновременно; один candidate не захватывается параллельно. Бюджеты резервируются атомарно для каждого запроса. Firecrawl сохраняет concurrency1.

При безопасном импорте свободный LLM level вне справочника сохраняется в unmappedMetadata.level, основной level остаётся пустым. Допустимые названия сопоставляются без учёта регистра, неизвестный уровень не угадывается. Ручной vacancyInput остаётся строгим. SQL0065 восстанавливает только mapping_invalid по этому полю.


## Устойчивый облачный тик (17.10.2026)

MCP2026-07-28: `server/discover` публично возвращает supportedVersions (2026-07-28 и прежние2025/2024), capabilities.tools, resultType:complete, _meta serverInfo, ttlMs:300000/cacheScope:public. Современные tools/list имеют TTL/cacheScope, JSON/SSE tool results — complete/serverInfo. Прежний initialize доступен. Discovery не читает DB и не авторизует вызовы данных.

POST/mcp tools/call scheduler_tick и scheduler_probe при Accept:text/event-stream отвечают SSE: немедленный comment, heartbeat8с, notifications/progress только при params._meta.progressToken, окончательный event:message с исходным JSON-RPC id/result. Без SSE остаётся JSON. Проверка identity/owner выполняется до открытия потока. Disconnect отменяет provider signal и даёт waitUntil только для короткого сохранения очистки. Повтор requestId с просроченным run получает terminal interrupted receipt и допустимое продолжение в той же серии; живой владелец не перезапускается. Сохраняются максимум5/12мин, generation, job/LLM leases и неизвестные резервы.

Structured OpenRouter: task-specific model health в существующих timings по generation/key revision, cooldown15мин, latency после2successes, одна free schema модель за попытку и90с предел в scheduler. Семантический validator проверяет результат до отметки health success. Invalid_output structured этапа может быть повторён с max_attempts, но не становится успешным без прежней валидации. Непросроченные LLM leases и ценовые ограничения не снимаются. Форматы HTTP/SQL/backup не расширены.

Перед окончанием окна dispatcher оставляет15с после LLM и3с после прочего этапа на fenced D1 запись и итоговый receipt; фактический provider deadline — меньший из90с и остатка окна с этим резервом.


## Apify для hh.ru

Только admin, same-origin для мутаций, ограниченный JSON. `GET /api/apify/settings` возвращает маски и billing metadata, никогда токены. `PUT /api/apify/key`: profile free/starter, apiKey, revision; AES-GCM, сброс paidAllowed. `PUT /api/apify/settings`: enabled, paidAllowed, revision; первое включение Starter требует confirmed:true. Подписку этот метод не покупает. `POST /api/apify/check` с profile читает account и limits, не запускает Actor. `GET /api/apify/usage` возвращает локальные резервы и безопасную оценку остатка, время проверки и активные run IDs.

По умолчанию выключен до ввода токена, paidAllowed=false. При сборе выбирается настоящий Free с планом$0 и лимитом$5, затем только явно разрешённый Starter с заранее оформленным платным аккаунтом/включёнными$19. Перед стартом свежий account/limits, атомарный резерв$0.25 +$0.01 на чтение, maxTotalChargeUsd0.25, timeout180s, restartOnError=false. Остаток использует max(account usage, confirmed local charges) плюс unresolved/read reserves. Неизвестный POST не повторяется. Долгий Actor будет опрашиваться отдельными тиками. Живые существующие запуски сверены 18.10; неизвестные расходы/ACK не снимают резерв.


Существующие `/api/firecrawl/sources|test|runs/:uuid/(save|resume)` сохраняются для совместимости, но sourceId=hh маршрутизируется исключительно в Apify (method=apify). UUID preview создаётся до отправки Actor, scheduler использует стабильный UUID своего scan job. `provider:apify`, `apifyProfile`, `apifyRunId`, `paused.apify` описывают курсор source-page/dataset-offset; jobs только текущей порции, found накопительный. Перед resume непустую порцию нужно сохранить без saveErrors. Тексты публичных вакансий доступны только admin. При save применяется обычный candidate upsert (acquisition_method=site), valid raw snapshot с diagnostics.provider=apify/profile/runId, legacy firecrawl_mode хранит free/hobby, а денежный расход только в apify_runs. Не выдавайте legacy поле за расход Firecrawl. Источник целиком завершается только после последней порции. Автоматический scheduler использует прежние JEV/LLM/import и общее расписание, HH full-text никогда не идёт в Firecrawl.


Проверка дубликатов (22.10): перед сохранением импорт сверяется со всеми текущими основными вакансиями и сохранёнными публикациями. Сначала используются канонический URL, identity keys, связь candidate→main и внешние ID HH/Habr/rabota/getmatch. Для разных площадок одной организации и одного entity_type дополнительно сравниваются близкие заголовки и содержательные разделы обязанностей/требований: оформление, служебные даты и зарплатные вставки исключаются, проверяются общие слова и последовательности из трёх слов с консервативными порогами и достаточным разнообразием текста. Общего описания компании или одного заголовка недостаточно. Неизменяемое extracted_data прошлой публикации участвует в проверке после ручных правок основной карточки. Разные ID/URL одной площадки сохраняются как разные публикации; совпадения нескольких main дают duplicate_ambiguous и не создают третью карточку. Только title-only без подтверждения содержанием сохраняет прежнее поведение отдельной публикации. Единственное подтверждённое совпадение обновляет существующую main, добавляет источник и сохраняет HH-приоритет, ручные поля/роли, письма, отклики и персональные данные. deduplication.method (identity/content/explicit/new) и vacancyId записываются в план, результат и audit changes_json. Транзакция повторно проверяет snapshot/ключи под import:global lease. Старые основные карточки автоматически не сливаются. Форматы SQL0080/backup38 прежние.


Apify production safeguards: Cloudflare-compatible redirect:manual rejects3xx without following URLs or forwarding the token. DOM TimeoutError/AbortError is mapped to string timeout, never numeric legacy23. An Actor ending at the per-run monetary cap with fewer than a full100-item source page remains partial/error (apify_run_limit), even if Apify reports SUCCEEDED; it must not advance next_run_at as a complete source. HH IDs in valid vacancy URLs are stable across hh.ru subdomains and independently checked before text-based duplicate matching.

Модели парсера: vacancy_extraction по умолчанию provider=openai/openaiModel=gpt-6-luna/reasoningEffort=medium; vacancy_role — provider=openrouter/openrouterModel=jev (SQL0074). Сохранённый явный выбор администратора имеет приоритет. Для ролей JEV используется Decisions API; при ошибке ни JEV, ни выбранный OpenAI не заменяются другой моделью.

Восстановление зависимостей импорта19.10: перед выполнением import_vacancy сверяется nextCandidateStage и dedupe hash. Устаревшее задание возвращает {superseded:true,nextStage:type|null}, ставит текущий этап ровно один раз и не выполняет импорт со старым решением; пустой текущий выбор завершает конвейер. Изменение зависимостей в ходе проверки остаётся ограниченным retry pipeline_changed/preview_changed. В начале обычного тика, включая непустую очередь, до3 исторических failed import с roles_pending/missing_extraction/missing_markdown/not_accepted проверяются в пределах8с: только выбранные источники, разрешённые компании, ещё не импортированные кандидаты и отсутствие активного задания. Старый receipt не сбрасывается; payload.dependencyRecovery={jobId,type,at} отмечает созданное текущее задание. Failed модели/неоднозначные дубли не возобновляются этим механизмом. Диагностические/jobId/handlers запуски сверку не выполняют; без внешнего запроса на этапе подготовки.

Политика Apify19.10: dashboard/MCP apifyPolicy={priorityPenalty:50,maxStagesPerCycle:1,maxHhStagesPerCycle:5,pollDelayMs:60000}. maxStagesPerCycle теперь ограничивает попытку допуска нового Actor, а не любой HH GET. Штраф50 только HH scan_source; сохранённые priorities не переписываются. За тик до5 HH scan/fetch этапов суммарно, включая deferred/error, при общем лимите110с/50jobs; готовые dataset порции и публичный HH fulltext не расходуют отдельный лимит старта. SCHEDULER_APIFY_ADMISSION проверяет допуск до account refresh и резервирует его перед INSERT ledger; отказ не создаёт пустой Actor identity. Существующий operationKey читается без нового допуска/POST; lost ACK сохраняет uncertain без повторного POST. После исчерпания допуска claim разрешает только scan с ledger для точной текущей cursor page, а не любой прежней страницы. Новый fallback detail Actor тоже проверяет допуск. apify_cycle_limit остаётся pending/1с, apify_running — pending/60с (явная jobId проверка1с); apify_busy прекращает новые admission до конца тика. Политика не меняет бюджеты, платное разрешение, scope, generation/leases, единственное расписание и обычный приоритет downstream HH. Подготовка новых источников остаётся после готовой очереди.

Apify Proxy17.10: новые hhInput используют proxyConfiguration.useApifyProxy=true без apifyProxyGroups (стандартный proxy внутри Actor, доступен Free в общем лимите платформы). Поле listing proxy=apify-configured означает запрошенную конфигурацию; фактический INPUT уже начатого run остаётся прежним. Старые listing cursors дочитывают уже оплаченный run; новые страницы используют proxy. Detail operationKey имеет суффикс :proxy-v1 и не переиспользует старый SUCCEEDED без описания; новые старты bounded прежними бюджетами/cap/uncertain. Same-ID неполный ответ получает apify_incomplete; чужой/archived/невалидный URL остаётся apify_data.

Owner-only проверка через существующий MCP scheduler_tick: явный requestId `apify-proxy-check:<candidateId>:<UUID>`. Допустима только текущая стадия fetch_full_text отобранного HH кандидата в scope. Обычные generation/lease/billing/paidAllowed/лимит одного нового Actor сохраняются; jobId ограничивает claim единственным заданием, reconciliation/параллельные LLM/continuation отсутствуют. Запуск отмечается manual. Повтор того же requestId читает прежний receipt. Новый UUID может опросить ту же proxy операцию без нового Actor; только в таком диагностическом цикле apify_running переносит run_after на1с. Число новых стартов ограничено устойчивым operationKey, основной график/приоритеты не меняются.

Автоматический score_relevance (18.10): новый тип в очереди/попытках планировщика, default priority85. После успешного импорта и для прежних импортированных пропусков заполняет company_vacancy_user_state.relevance только ownerName и только при NULL, через выбранную JEV. Общие day/month budgets и fencing действуют; данные резюме/вакансии не входят в публичный payload очереди. Ручной POST /api/vacancies/:id/relevance сохраняет прежний контракт и разрешает явный пересчёт существующего значения. Старые настройки priorities дополняются новым default; частичный прежний объект сохраняет новые приоритеты.

### Диагностика Apify, 18.10.2026

Scheduler status возвращает до4 apifyDetails и1 apifyListings, safe rawSample/clean sample, dataset itemCount/cleanItemCount, build/стоимость/proxy usage. Status остаётся read-only. POST /api/apify/check перед account refresh выполняет bounded GET reconciliation до4 известных run текущего профиля и возвращает reconciliation; новые Actor не запускаются. Новые acquisition тоже сверяют до2 run. Unknown charge/ACK сохраняют резервы.

### Списки HH: исходные строки dataset

Рабочее чтение Apify: clean=false/skipHidden=true/skipEmpty=false, offset по исходным строкам. Порции остаются до10, metadata itemCount и стабильность modifiedAt проверяются перед окончанием; короткий ответ сам по себе не завершает список. Result: rawRows, quarantinedCount, emptyCount, bounded quarantine (до20 page/offset/code без исходных объектов), coverage.datasetComplete/hhComplete. Последнее не подтверждается одним Actor dataset. Invalid rows не импортируются, хорошие сохраняются. Ошибки/денежный лимит дают partial. Старые paused clean cursors переигрывают текущий dataset с0 при том же operation_key, upsert сохраняет идентичность.

### Публичный HH и безопасная сверка

HH public detail не зависит от enabled Apify; fallback/listing требуют Apify. Источник HH компании, scope, revision и lease сохраняются, смена source во время запроса блокирует snapshot. description_html используется только при пустом description_text через text-only преобразование; диагностика snapshot сохраняет provider/descriptionFormat/buildId при наличии.

MCP scheduler_tick requestId=apify-reconcile:<UUID> — отдельная maintenance операция: до4 GET известных ID, глобальная lease и durable receipt, trigger=probe, actorStarts=0, без claim других jobs/LLM/импорта/continuation. Повтор того же ID не вызывает сеть. Обычные series/автоматизация не изменены.

### Компоненты бюджета Apify, 18.10.2026

`/api/apify/usage` возвращает ownConfirmedUsd, unknownReserveUsd, unknownRuns, readReserveUsd, coverageStatus=estimated_overlap и estimatedUsedUsd=max(providerUsedUsd,ownConfirmedUsd)+unknownReserveUsd+readReserveUsd. remainingUsd=max(0,limitUsd−estimatedUsedUsd). providerUsedUsd учитывает весь аккаунт, localUpperUsd описывает только локальную сумму confirmed+unknown+read и не вычитается дополнительно. Это оценка перекрывающихся расходов, а не доказанный биллинговый cutoff: задержка стороннего расхода остаётся ограничением. UI и атомарный INSERT используют одинаковую формулу, account/period fencing, SQL high-water mark и резерв одного нового запуска. Старые/неизвестные запуски и потерянный ACK сохраняют резерв. Схема0075/backup37 без изменений.

Owner-only MCP `scheduler_tick` с requestId `apify-budget-check:<UUID>` выполняет только account/limits/monthly usage GET (до3 на профиль, только configured Free и отдельно разрешённый Starter). Возвращает budgetAudit с компонентами бюджета, billing cycle, итогом до/после скидки при наличии и до128 числовых service rows. Никаких daily/raw user payload/token, Actor/model/job/import/continuation. Отдельный глобальный lease/trigger probe/durable receipt; replay не делает запросов. Ошибки ключа/смены revision/period изолированы как code; monthly aggregate не считается доказательством покрытия отдельных run ID (`runCoverageAvailable:false`). Проверка обновляет безопасный account cache; полный monthly report возвращается в ответе, не хранится как доказательство или новый баланс.

SQL0073: provider high-water хранится также в операционной apify_account_usage, общей для профилей одного account/period. Обновление usage ledger и account cache атомарно под revision/generation fence; usage UI и admission читают сохранённый максимум. Restore очищает settings cache, но не этот ledger. Backup36 не меняет формат и не содержит операционной таблицы.

### Безопасное восстановление первичных записей

Внутренний upsertVacancyCandidate принимает третий серверный аргумент onlyNew. В одной batch транзакции идентифицируется запись и вставляется только отсутствующая; существующие поля, title revision, timestamps и aliases сохраняются. Обычный upsert прежний. Это не новое входное поле HTTP: клиентский JSON по-прежнему не принимает onlyNew. Режим предназначен для чтения старых оплаченных HH dataset без отката свежих/ручных данных.

### Восстановление одного оплаченного HH dataset (19.10.2026)

Owner-only MCP scheduler_tick принимает requestId `apify-listing-replay:inspect:<UUID>` для DB-only списка допустимых ledger IDs и `apify-listing-replay:<ledgerUUID>:<UUID>` для одной порции до20 raw строк. Общий scheduler lease/receipt, отдельный Firecrawl request lease и generation/source/settings/favorite guards. Replay допускает только текущий generation, succeeded или failed cost-known ledger (failed требует GET подтверждения терминального статуса), тот же key hash и разрешённый профиль; operation_key должен ссылаться на сохранившийся завершённый серверный discovery preview UUID того же owner/company/HH URL/revision. Пропавший/активный/старый parent, смена ключа/источника/выключение/restore не создают запасной Actor. HTTP resume и список paused previews исключают внутренний replay progress.

Читается только закреплённый dataset (GET metadata/items), clean=false/skipEmpty=false/skipHidden=true, до100 raw записей; metadata должна быть стабильна, modifiedAt известен. Другой itemCount/modifiedAt или короткая порция не означают завершение. В result_json сохраняется отдельный raw offset/карантин до20 позиций без текстов; недопустимая запись/работодатель/identity conflict не блокируют соседние корректные строки. Новые кандидаты создаются onlyNew со стабильным creation token; существующие поля/aliases/snapshots не меняются. По диапазону creation_token восстанавливается принадлежность новых строк после lost ACK; все созданные этим replay IDs идемпотентно получают обычные classify_title задания JEV, без сброса completed/failed/cancelled и без вызова модели внутри replay. Длинные LIKE patterns не используются (проверено native D1).

Report: ledgerId/companyId, rawRows/processed/found, createdCandidates/existingRows/createdCandidateIds, quarantinedCount/offset+code, complete, coverage.datasetComplete/hhComplete=false, providerCapReached, actorStarts=0/modelCalls=0. complete означает прочитан один dataset, не всю HH компанию. Деньги/ledger/success/next_run_at источника не меняются; новый Actor, другие задания и модели не выполняются. Для следующей порции нужен новый UUID с тем же ledgerId; replay того же UUID не делает сеть. Обычная автоматизация/приоритет/лимит1 нового Actor за тик сохранены; чтение dataset ограничено отдельно. Основная карточка появляется только после стандартной цепочки JEV→описание→Luna→роли→safe import→JEV relevance.

Прерывание replay: до чтения и перед enqueue проверяется AbortSignal. DOM TimeoutError/AbortError преобразуется общим operationErrorCode в строку timeout (не legacy numeric23/20); уже сохранённый raw cursor/creation lineage остаётся для следующего UUID. Предварительно отменённый вызов не читает Apify и не создаёт кандидатов.

### Повтор начала страницы HH не отбрасывает её остаток

Fingerprint первой порции (до10 ID/URL) теперь лишь устанавливает repeatedPrefix. Все raw строки уже оплаченного dataset дочитываются и сохраняются обычными порциями. После подтверждённого datasetEnd повтор префикса завершает источник частично с apify_pagination_uncertain, coverage.datasetComplete=true/hhComplete=false; следующая страница Actor не запускается. Совпадение10 строк не называется доказательством повтора всего списка. Даже целиком повторённый dataset проходит через обычное identity dedupe. Стоимость/cap/метаданные/карантин и save-before-resume прежние. Исправлен воспроизведённый live пропуск корректных вакансий после повторяющихся первых10.

onlyNew также защищает ранее созданную этим же replay запись: стабильный creationToken обозначает происхождение, а не разрешение обновить данные. UPDATE в этом режиме полностью выключен; aliases добавляются только если непосредственно предшествующий INSERT в той же batch создал кандидата (changes()>0). Повтор после ручной правки собственного нового кандидата сохраняет все его поля/даты/revision/aliases; inserted отражает фактическую вставку в этой попытке. Проверено SQLite и native D1.


С 20.10 HH Apify listing использует api_only=true: собирает сводные записи без медленного резервного обогащения каждой карточки. Detail сохраняет api_only=false, публичный exact-ID HTML читается первым. Старые operation IDs не перезапускаются, лимиты/постраничность и полный текст после JEV прежние. Контракт JEV конечных ролей v2 входит в input_hash: старый кэш не подменяет новый способ передачи правил.


MCP scheduler_status.diagnostics.hhContent возвращает до8 последних попыток загрузки HH с provider/snapshotProvider, статусом, длиной текста и ID кандидата. failedJobs показывает до12 последних ошибок HH/rabota с ID задания/кандидата, заголовком, URL и кодом. Это DB-only чтение без текстов и новых запросов провайдерам.

MCP scheduler_status.diagnostics.rabota возвращает bounded DB-only сводку: candidates (company/count/imported, до20 групп), scans (до6 последних, loadedPages/found/status/pausedCode/nextPageUrl/errorCode), imports (до10 карточек, ID/title/company/descriptionLength/relevance). Новых Firecrawl/Actor/JEV вызовов для этой сводки нет. Полные описания и секреты не возвращаются.

MCP scheduler_status.diagnostics.roleCatalog показывает до100 ролей с ID, stableKey, названием, parentId и usageCount. preservedAssignments содержит до30 оставшихся связей со служебными сохранёнными ролями. Эта часть диагностики только читает БД; исправление ошибочных стандартных сочетаний выполняется при инициализации, без новых провайдерских вызовов.

## Диагностика проблем парсера

`GET /api/scheduler/issues` доступен только администратору, читает локальную БД без запросов к провайдерам. Возвращает checkedAt, issues (id, priority1–3, status observed/verification/planned, description, nextAction, target, observedAt/reviewedAt, необязательные jobs), historicalFailed, sampleLimited и limit120. Наблюдаемые сигналы отделены от поддерживаемого в коде списка известных проверок/улучшений. Учитываются выбранные источники, избранное, агентский переключатель, включение источника и последующее восстановление; исторические failed не объявляются актуальными сбоями. При превышении выборки limit явно указывается sampleLimited.

`GET /api/scheduler/queue` дополнительно принимает agencyId и search (название компании/агентства, до200символов, регистронезависимо включая кириллицу, буквальный поиск без SQL wildcards). companyId и agencyId вместе недопустимы; прежние параметры совместимы. Чтение/поиск не меняют очередь. MCP scheduler_status дополнительно содержит issues с той же локальной проекцией; работа статуса и его ограничения доступа прежние.

GET `/api/scheduler/issues` (только администратор) дополнен activeCounts:{p1,p2} и history:{timezone,metric,days,observedDays,average:{p1,p2},points:[{day,p1,p2,observedAt}]}. metric=daily_peak_active, days=30, Europe/Moscow, включая сегодня. P1 — число наблюдаемых критичных сигналов; P2 — число затронутых проблемных заданий в актуальной области сбора после исключения ожидаемых ожиданий и восстановленных/замещённых записей. P2 считается по всей выборке, независимо от лимита120 карточек. verification/planned не входят. Пики сохраняются атомарно при чтении диагностики и до/после обычных тиков, без запросов к провайдерам. У дней без наблюдения значения null; среднее вычислено только по наблюдённым дням, включая неполный текущий. Повторные чтения не суммируют одну и ту же ошибку. Изменение области сбора может изменить активные значения; история отражает действовавшие настройки. История разделена по ownerName, generation-fenced запись, исключена из пользовательского backup/restore и сохраняется локально при восстановлении.

GET `/api/scheduler/funnel?sourceId=all&parser=all` — только администратор, локальные агрегаты за30 календарных дат MSK, включая сегодня. Источник: all/из VACANCY_SOURCE_META/unknown; parser: all/apify/firecrawl/http/unknown. Неизвестные фильтры и ключи дают400. Не зависит от нынешних favorites/sourceIds/agency-enabled: показывает сохранённую историю ранее полученных публикаций. Ручные и Excel первичные записи исключены. Единица счёта — уникальная первичная публикация источника, а не запрос/попытка/основная карточка. daily/totals/averages показывают первые подтверждённые успешные этапы по фактическим датам этапов (среднее totals/30). В один день может завершаться обработка более старой когорты. cohort — публикации, впервые полученные в окне, и достигнутые ими этапы/текущие rejected,error,waiting; published определяется подтверждённым импортом или существующей main-связью. created/linked делят подтверждённые аудитом опубликованные записи когорты по action=create; исторические main-связи без аудита в это деление не включаются; undatedPublished обозначает историческую связь без даты импорта, она не попадает в дневной ряд published. Источник и парсер первого получения фиксируются атомарно и неизменно в операционной parser_candidate_intakes по creation_token; поздний парсер полного текста не меняет атрибуцию. Для старых записей восстанавливаются только точные creation receipts в текущем поколении; остальные parser=unknown. stage fulltext может предшествовать accepted, если Apify уже вернул описание. Результат содержит только агрегаты и варианты фильтров, без исходных текстов и личных данных. Изменение промтов ничего не пересчитывает; прошлые успешные этапы подтверждаются журналами.

MCP scheduler_status также возвращает общий funnel с теми же агрегатами (all/all), без запуска парсеров или моделей.

## Конкурентные изменения отклика и вакансии (GO-102)

Go PUT отклика и PUT ручной вакансии ограниченно повторяют целую транзакцию при MariaDB1213: сервер уже выбрал жертву взаимной блокировки и откатил её. На новой попытке используется исходная revision и заново проверяется CAS; чужое изменение возвращает409. Максимум три попытки в исходном context. Lock timeout, network/неизвестный COMMIT и произвольная SQL ошибка не повторяются. SQL/HTTP proof с actual server victims ожидается в required disposable CI; полная GO-031 пока открыта до GO-103.
