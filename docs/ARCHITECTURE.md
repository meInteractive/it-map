# Архитектура it-map: действующая и целевая

**Срез кода:** локальная `master`, последний кодовый коммит `89add93` (24.10.2026). Код Go частично реализован, но production-интеграция не подтверждена. Спринты: [../PLANS.md](../PLANS.md).

## 1. Старый GPT Sites (эталон поведения)

- `build/sites-worker.ts`, `server/routes/index.js`, `server/http.js`: обработка входящих запросов Cloudflare Worker; `server/auth/session.js`: сессии и роли.
- `server/repositories/`: SQL-слой D1/SQLite, включая компании, вакансии, отклики/receipt, задачи, парсерные кандидаты, состояние автоматизации, настройки моделей.
- `server/services/`: бизнес-логика, генерация писем, классификация и источники вакансий, маршрутизация операций, диагностика и планировщик.
- `server/integrations/`: Firecrawl, Apify, OpenAI, OpenRouter/JEV, Google Calendar и вспомогательные адаптеры.
- `server/backup/index.js`, `server/import-export/`: резервное копирование формата v39, Excel/JSON и прямой DB export (историческая функциональность).
- `db/schema.ts`, `docs/DATABASE.md`, `server/generated/backup-schema.json`: историческая схема D1. Рабочий GPT Sites **не обслуживается Go** и продолжает использовать собственную D1 и Site Automation.

## 2. Новый Go-монолит: реализованное в исходниках

| Каталог | Реально присутствует | Не следует считать выполненным |
|---|---|---|
| `cmd/itmap` | запуск HTTP, graceful shutdown, конфигурация и сборка зависимостей | успешное production-развёртывание |
| `internal/config` | переменные среды и запрет production до приёмки | работа с внешним хранилищем секретов |
| `internal/storage` | `database/sql`, DSN, пул, транзакции, baseline для 72 таблиц / 606 полей | проверенная работа с настоящей MariaDB; драйвер загружается с `-tags mariadb` |
| `internal/httpapi` | `net/http` ServeMux, middleware, условное подключение доступных маршрутов, статика SPA, `/health` | полное покрытие legacy API; `/health/ready` всегда 503 в текущей версии |
| `internal/auth` | signed cookie, логин, роль и изоляция запросов | проверенная браузерная сессия на целевом домене |
| `internal/catalog`, `crm`, `channels`, `roles` | чтение компаний/агентств, часть CRM, флаги, справочники каналов/ролей | все операции настройки каналов/компаний |
| `internal/vacancies` | списки, карточка, создание/изменение/удаление, роли и пользовательское состояние | реальные DB-тесты и неподдерживаемые сценарии агентств |
| `internal/applications` | чтение, статус/история, редактирование, удаление, idempotency-context/receipt, one-click создание | сквозная приёмка на MariaDB |
| `internal/tasks` | чтение и изменение завершённости | создание/перенос дат и полный Google Calendar sync |

В Go **нет** перенесённых модулей `parser`, `scheduler`, `backup`, `integrations` для полноценного функционала. Их JS-аналоги в `server/` не делают Go-перенос завершённым. Отсутствующие обработчики возвращают отказ, а не успешную пустую заглушку. HTTP основан на стандартном `net/http`, **Gin отсутствует**.

## 3. Фронтенд и API

`frontend/Application.tsx`, компоненты `frontend/`, общий `frontend/api/client.ts`; исходная Next/Vinext-структура `app/` остаётся справочной; новая сборка SPA предусмотрена через `web/`, `vite.web.config.ts` и раздачу Go `internal/httpapi/static.go`. Её фактическая сборка и проверка PWA/мобильных сценариев на staging пока не подтверждены.

Веб-интерфейс использует относительные HTTP-пути `/api/...` и сессии. Новый `internal/httpapi/handler.go` регистрирует маршруты только при переданных зависимостях: auth, каталог, вакансии, отклики, задачи и флаги. Полный исходный реестр: `docs/LEGACY_API_BASELINE.md`, `docs/LEGACY_API_PATHS.json`, `docs/API.md`. Дальнейшие маршруты обязаны сохранять контракт или изменяться согласованно.

## 4. Парсер и жизненный цикл вакансии

**Старый JS-процесс (есть в исходниках):** `server/services/scheduler-{dispatcher,pipeline,...}.js`, `server/repositories/{scheduler,parser-intakes,vacancy-candidates,...}.js`.

```
источник компании/канала
  → scan_source → classify_title (JEV)
  → fetch_full_text → extract_vacancy
  → assign_roles → import_vacancy
  → score_relevance → вакансия / аналитика
```

Источники: поддерживаемые текущими JS-модулями адаптеры сайтов/Firecrawl, hh.ru через Apify, специализированные адаптеры rabota.ru и другие; **не предполагать**, что все хранящиеся в каталоге источники реально поддерживаются конвейером. Есть дедупликация, raw/candidate stages, квоты провайдеров, ретраи, расходы и диагностика.

**План Go 1.1:** перенос существующей семантики стадий, durable state и lease в MariaDB. Планировщик станет локальным Go-процессом, вместо единственной часовой Site Automation GPT Sites. **Сейчас этого Go-конвейера нет.**

**План 1.3:** RabbitMQ доставляет `job_id` и метаданные поколения; MariaDB хранит состояние и transactional outbox. Не заменять SQL-истину сообщениями брокера.

## 5. ИИ и внешние интеграции

JS: `server/integrations/{openai,openrouter,jev,apify,firecrawl,google-calendar}.js`, `server/services/{cover-letter,classification-jobs,scheduler-relevance,...}.js`. JEV применяется к бинарной классификации заголовков и оценке релевантности; есть промты, режимы моделей, отдельная маршрутизация/бюджеты и учёт расходов. Go-клиенты ещё **не** портированы. Продолжать работу только через тестовые/подставные ответы, не запускать платные вызовы из PR.

Google Calendar: в JS имеются OAuth и синхронизация задач; в Go перенесено чтение `google_sync_status` и переключение завершённости задач без полного OAuth/sync. Ключи интеграций могут быть зашифрованы производными `SESSION_SECRET`; нельзя обещать чтение старых настроек без отдельной совместимой криптографии/повторной настройки.

## 6. Хранилища и границы данных

- GPT Sites: D1 SQLite, исходные операции продолжаются. Схема: [DATABASE.md](DATABASE.md).
- Go staging/будущий production: локальная MariaDB 10.11+, прямой преобразованный снимок D1 и минимальные последующие миграции: [MARIADB.md](MARIADB.md).
- Разрешать запись в обе БД одновременно **нельзя** без спроектированной репликации/согласования: snapshots не являются синхронизацией.
- Сервисные границы: HTTP отвечает за доступ и контракты; доменные `internal/*` за бизнес-инварианты; `internal/storage` за подключение; JavaScript `server/*` остаётся эталоном при переносе, не частью работающего Go-конвейера.

## 7. Проверки и реальные ограничения

В репозитории есть 52 файла `*_test.go`, `.github/workflows/go-ci.yml`, `db/*-preflight.sql` (read-only), `scripts/check-public-repo.py`, многочисленные JS-скрипты тестов. В текущем сеансе выполнены `go test ./...`, `go vet ./...`, `go build ./cmd/itmap` и статический контроль публичных исходников. Не выполнялись `go test -tags mariadb`, тесты с реальной MariaDB, Node/Vite-сборка, Go end-to-end парсера и staging-сценарии. Отдельный runbook для Go production пока отсутствует.
