# GO-015 — Google Calendar: первый Go-контур и осторожный запуск

**Статус:** реализация синтетического контура в PR #496. Не подтверждены реальные OAuth/Calendar API, production OAuth-конфигурация и первый Google-аккаунт. По сообщению владельца от 18.11.2026, в старой Sites-версии календарь никогда реально не подключали — миграция токенов и исторических событий не требуется. Код всё равно сохраняет fail-closed защиту от случайно существующих записей.

## Что интегрируем

Текущая карточка **Задачи → Google Calendar** уже существует в React. Go API добавляет:

- `GET /api/google-calendar/status`: только для залогиненного пользователя; показывает configured/connected, email и два агрегата его задач, без токенов.
- `GET /api/google-calendar/connect`: требует основную сессию; выдаёт подписанный HMAC state с nonce, текущим пользователем и TTL, хранит его в защищённой HttpOnly `SameSite=Lax` cookie **только** на пути callback, и перенаправляет в Google OAuth с минимальными scope `calendar.events` и `userinfo.email`, `access_type=offline`, PKCE S256.
- `GET /api/google-calendar/callback`: основной cookie IT Map — `SameSite=Strict` и обычно не приходит по redirect от accounts.google.com. Callback **не принимает пользователя из URL**; личность берёт из проверенных одновременно state/HMAC и браузерной cookie. Меняет одноразовый code на токен в фиксированном endpoint, требует refresh token и verified account email, шифрует refresh AES-256-GCM (`legacycrypto.Calendar`) и сохраняет в существующую `google_calendar_user_connection` с ограничением по user.
- `POST /api/google-calendar/sync`: только ручной запуск из интерфейса, авторизованная same-origin команда. Получает access token через refresh, читает не более 100 задач текущего пользователя, синхронизирует отдельные non-completed задачи. Долговечный Google Event ID стабилен по user/task и не зависит от смены ключа шифрования. Для повторения сначала выполняет GET по этому же ID, проверяет `extendedProperties.private.source/taskKey`, при подтверждённом отсутствии делает INSERT, при корректном существующем событии — PATCH. Таймаут/lost ACK сохраняет ошибку и **не даёт права создать другой Event ID**. Одновременные запросы используют CAS по статусу `inflight`, а истёкший claim допускает безопасную проверку по тому же Event ID. Нет фоновой синхронизации и новых платных AI/Apify операций.
- `POST /api/google-calendar/disconnect`: удаляет **локальный refresh token** пользователя, не удаляя ранее созданные события Google и связи задач. Отзыв разрешений у Google можно дополнительно выполнить в настройках аккаунта Google; этот маршрут сам внешнюю revoke-команду не отправляет.

Создание задачи и перенос её срока при активном подключении теперь сохраняют `google_sync_status=pending`, а не возвращают прежнюю конфликтную ошибку. Синхронизация выполняется отдельно по кнопке, поэтому интерфейс явно показывает `Ожидает синхронизации` до подтверждения Google. Изменение срока при **отключённом**, но ранее связанном событии по-прежнему запрещено без операторской сверки.

## Настройка на сервере (операторский этап, **не выполняется этим PR**)

1. В контролируемом Google Cloud Project включить **Google Calendar API**. Настроить OAuth consent screen, область calendar.events + email, тестовую учётную запись и ограничения аудитории. По необходимости перейти из testing в production после требований Google.
2. Создать **OAuth client типа Web application**, указать разрешённый Redirect URI **в точности** `https://<публичный-домен>/api/google-calendar/callback`. Не использовать IP/HTTP, wildcard, query или произвольный Host.
3. Только во внешней секретной конфигурации Go API (не в GitHub/PR/логи/чат) установить `GOOGLE_CALENDAR_CLIENT_ID`, `GOOGLE_CALENDAR_CLIENT_SECRET`, `GOOGLE_CALENDAR_REDIRECT_URI` и ранее существующий `SESSION_SECRET`. Сервис по умолчанию `configured=false`, пока все поля и HTTPS callback невалидны.
4. После **отдельного разрешения на безопасный релиз** и read-only preflight в приложении проверить `configured=true, connected=false`. Пользователь лично нажимает **Подключить календарь**, выбирает учётную запись и явно даёт доступ. Затем создаёт одну тестовую задачу и нажимает **Синхронизировать задачи**. Проверить ровно один Event ID в своём Google Calendar и ссылку/статус в IT Map; ещё один sync не должен создать дубль. Затем проверить перенос срока и повтор. Не использовать настоящие токены в CI.
5. Для disconnect проверить отсутствие соединения в UI, но **не ожидать удаления созданных событий из самого Google Calendar**. Переподключение аккаунта с историческими связями и изменённым email — отдельный fail-closed сценарий.

## Границы и сознательно не решённые случаи

- **Нет** входящего импорта Google events, двусторонней синхронизации, автоматического периодического расписания, фоновой очереди и отслеживания удаления события в Google. Первый контур **однонаправленный и ручной**: IT Map → Google Calendar.
- При lost ACK после создания события локальная задача сохраняет `error` до следующего ручного sync; тот проверяет тот же Event ID. Синтетическая гарантия не является доказательством поведения Google в реальной учётной записи.
- Event ID с другой provenance / идентификатором не перезаписывается. Старые `google_event_id` иной схемы или reconnect после полного удаления локального подключения с оставшимися связанными событиями требуют явной сверки; код намеренно не «исправляет» ссылки задним числом.
- Для безопасности API не возвращает сырой ответ Google, access/refresh token, email чужого пользователя или текст задачи в диагностике. Данные задачи **передаются Google** только после личного согласия на OAuth и явного ручного sync.
- Таблица `google_calendar_user_connection` и поля `company_tasks.google_*` уже присутствуют в trusted baseline. **Без production DDL/миграции/удаления данных.**
- При ошибке сетевого запроса сервер не делает дополнительных POST для другой идентичности. По документации Google уникальность идентификатора события при insert — стратегия, но её реальный end-to-end результат следует проверить на тестовом аккаунте.

## Синтетическая приёмка

- `go test -race ./internal/integrations/googlecalendar ./internal/httpapi ./internal/tasks`: OAuth state, expiration, cookie binding, PKCE, refresh; отсутствие secret leak; event GET→POST→GET/patch, lost ACK, чужой event; пользовательские HTTP-маршруты и pending задачи.
- `TestMariaDBGoogleCalendarManualSyncDisposable`: отдельный required CI шаг на **одноразовой MariaDB** с mock Google HTTPS transport, проверяет первый lost ACK, отсутствие второго POST, сохранение Event ID, перевод времени через PATCH, разделение пользователей и disconnect.
- `npm run format:check`, `npm run typecheck`, `npm run build:web`; обязательные PR CI на exact SHA. Только после полного зелёного CI и ревью владелец сливает PR.

**Никакого реального Google OAuth или события на production в рамках PR.**
