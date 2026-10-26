# Архитектура it-map: действующая и целевая

**Актуализация: 26.10.2026.** Go-реализация частичная; production-интеграция и полный перенос не приняты. Здесь описаны границы и фактически присутствующие модули. Проверки конкретных SHA и статусы — в [реестре задач](../tasks/README.md) и [контрольных точках](../tasks/STACK.md), общий план — в [PLANS.md](../PLANS.md). Расположение файлов и вход в разработку: [GO_BACKEND_STRUCTURE.md](GO_BACKEND_STRUCTURE.md).

## 1. Старый GPT Sites (эталон поведения)

- `build/sites-worker.ts`, `server/routes/index.js`, `server/http.js`: входящие запросы Cloudflare Worker; `server/auth/session.js`: сессии и роли.
- `server/repositories/`: SQL D1/SQLite, CRM, вакансии, отклики, задачи, кандидаты, настройки и состояние автоматизации.
- `server/services/`: бизнес-логика, классификация и извлечение, источники, диагностика и планировщик.
- `server/integrations/`: Firecrawl, Apify, OpenAI, OpenRouter/JEV, Google Calendar и вспомогательные адаптеры.
- `server/backup/index.js`, `server/import-export/`: исторические backup v39, Excel/JSON и DB export.
- `db/schema.ts`, [DATABASE.md](DATABASE.md), `server/generated/backup-schema.json`: историческая схема D1.

Работающий GPT Sites **не обслуживается Go** и продолжает использовать собственные D1 и Site Automation. Публичный снимок исходников не равен этой production-среде.

## 2. Новый Go-монолит: реализованное в исходниках

| Каталог | Присутствует | Граница приёмки |
| --- | --- | --- |
| `cmd/itmap` | HTTP, graceful shutdown и сборка зависимостей | запуск на реальном production не принят |
| `internal/config` | конфигурация процесса, проверка переменных, запрет production | внешнее хранилище секретов не подключено |
| `internal/storage` | `database/sql`, driver/pool, baseline 72 таблиц / 606 полей, SQL-интеграционные проверки | driver через `-tags mariadb`; synthetic CI не заменяет согласованный реальный снимок |
| `internal/httpapi` | `net/http` ServeMux, middleware, условная регистрация доступных маршрутов, SPA и health | полный legacy API ещё не перенесён; readiness остаётся закрытым |
| `internal/auth` | вход, signed cookie, роли и сессии | браузерные CI-сценарии не заменяют целевой домен/TLS |
| `internal/catalog`, `crm`, `channels`, `roles` | каталог, CRM-чтение/флаги, каналы и роли; pure channel input | CRUD каналов/контактов и полная MariaDB write-приёмка CRM открыты |
| `internal/vacancies` | чтение, создание/изменение/удаление, роли и пользовательское состояние | полный write/CAS/FK gate и операции агентств не приняты целиком |
| `internal/applications` | отклики, статусы/история, receipts, версии, one-click создание | SQL/HTTP/concurrency части проверены; межмодульные сценарии открыты |
| `internal/tasks` | создание/срок/завершение/reopen/фильтры без Calendar sync | поддерживаемый API проверен SQL/HTTP/desktop-mobile; полный Calendar sync открыт |
| `internal/parser` | pure settings/cost/периоды МСК и durable cycle owner/recovery | нет полного dispatcher, claim стадий и денежного ledger |
| `internal/legacycrypto` | synthetic AES-GCM compatibility JS↔Go | не перенос реальных секретов и не утверждение ADR-016 |
| `internal/integrations/apify` | изолированный HTTP/HH/run/dataset адаптер на mocks | не подключён к API, settings, оплате или фоновому worker |

Это модульный монолит с минимальными интерфейсами у потребителей. Частичные библиотеки парсера не означают работающий Go-конвейер; backup/restore и остальные provider clients также не приняты. Отсутствующие API возвращают отказ, а не успешную пустую заглушку. Gin не используется.

## 3. Фронтенд и API

React/TypeScript находится в `frontend/`, вход — `frontend/Application.tsx`, API-клиент — `frontend/api/client.ts`. Историческая `app/` остаётся справочной. Самостоятельная SPA собирается из `web/` через `vite.web.config.ts` командой `npm run build:web`; Node нужен при сборке, отдельный Node-сервер при работе Go не нужен. Go раздаёт результат через `internal/httpapi/static.go`.

Фронтенд использует относительные `/api/...` и сессии. `httpapi.Options` подключает только переданные зависимости. Исходные маршруты и JSON: [LEGACY_API_BASELINE.md](LEGACY_API_BASELINE.md), [LEGACY_API_PATHS.json](LEGACY_API_PATHS.json), [API.md](API.md). Контракты и права сохраняются при переносе. Для реализованных частей есть synthetic Go/MariaDB/API и desktop/mobile CI; полный внешний staging пока не принят.

## 4. Парсер и жизненный цикл вакансии

Существующий JS-процесс расположен в `server/services/scheduler-{dispatcher,pipeline,...}.js` и соответствующих repositories:

```text
источник → scan_source → classify_title
        → fetch_full_text → extract_vacancy
        → assign_roles → import_vacancy
        → score_relevance → вакансия / аналитика
```

Поддержка определяется конкретными JS-адаптерами, а не просто наличием источника в каталоге: сайты/Firecrawl, hh.ru через Apify, rabota.ru и другие подтверждённые случаи. Нельзя считать каждый сохранённый Telegram или иной URL поддержанным автоматическим сборщиком. Дедупликация, provenance, бюджеты, retry, generation и сохранённые состояния — часть совместимости.

Go 1.1 сохраняет состояние/leases в MariaDB без RabbitMQ. Уже проверенные codec/settings/budget/cycle-lease и mock HTTP части описаны в [GO_PARSER_MIGRATION.md](GO_PARSER_MIGRATION.md); они ещё не связаны в автоматический dispatcher. RabbitMQ и transactional outbox относятся к 1.3; MariaDB остаётся источником истины. Параллельное включение нового Go parser и отдельной Sites Automation не разрешено.

## 5. ИИ и внешние интеграции

JS-интеграции и их бизнес-процессы сохраняются в `server/integrations/` и `server/services/`. JEV используется для классификации/релевантности, есть отдельные промты, выбор моделей, лимиты и учёт расходов. Изолированные Go-части проверяются на синтетических данных/HTTP mocks; это не приёмка платных провайдеров и полного workflow.

Google Calendar OAuth/sync в Go не перенесены. При подключённом Calendar неподдерживаемые task create/reschedule отказывают, вместо скрытого рассогласования. Legacy keys используют производные `SESSION_SECRET`; synthetic crypto compatibility не гарантирует доступность прежнего секрета. Стратегия реальных ключей/staging остаётся открытой в [ADR-016](DECISIONS.md).

## 6. Хранилища и границы данных

- Действующая D1/SQLite и её историческая схема: [DATABASE.md](DATABASE.md).
- Целевая локальная MariaDB 10.11+, baseline и ограничения переноса: [MARIADB.md](MARIADB.md). Приложение не создаёт и не мигрирует схему при старте.
- Snapshot не является синхронизацией двух БД. Cutover, заморозка и восстановление требуют отдельной согласованной процедуры.
- HTTP отвечает за доступ/контракт, доменные `internal/*` — за бизнес-инварианты, `internal/storage` — за подключение. JS остаётся эталоном, не работающим Node-бекендом Go.
- Публичные исходники очищены от рабочих дампов/backup, резюме, контактов, секретов и hosting bindings. Состав и ограничения: [MIGRATION_PUBLIC_REPOSITORY.md](MIGRATION_PUBLIC_REPOSITORY.md).

## 7. Проверки и реальные ограничения

Набор проверок и команды собраны в [локальной инструкции](deployment/LOCAL.md), сервер/CI/CD/rollback — в [документации развёртывания](deployment/README.md). Результаты отдельных unit/contract/SQL/браузерных проверок фиксируются по SHA; наличие тестового файла не доказывает его выполнение.

`/health/live` подтверждает живой процесс. `/health/ready` **намеренно всегда возвращает 503**, включая доступную БД: причины различают отсутствие/недоступность MariaDB и `business_api_not_migrated`. Это release gate до общей приёмки, а не утверждение, что каждый реализованный API недоступен. `APP_ENV=production` запрещён конфигурацией. Реальная инфраструктура, полный parser/backup/restore и production-cutover не следуют автоматически из успешного synthetic CI.
