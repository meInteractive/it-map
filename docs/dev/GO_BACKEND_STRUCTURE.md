# Структура Go/React-кода IT Map (срез 19.11.2026)

**Точка сверки:** `main=2e670273`, после merge #496. Актуальное описание потоков и жизненного цикла — [ARCHITECTURE.md](ARCHITECTURE.md), проверка фактов по коду/CI — [GO-119](GO-119-ARCHITECTURE-EVIDENCE.md). Исторические handoff и план миграции не заменяют текущего `main`.

## С чего начинать

Обязательные инженерные правила и ограничения — [AGENTS.md](../../AGENTS.md), задачи и статусы — [tasks/](../../tasks/README.md), последний [handoff](handoff/README.md). Исторический GitHub `merged` у stacked PR может обозначать слияние в промежуточную ветку, не в `main`; проверяйте фактические файлы. Не изменяйте рабочую БД, платные операции и службы без отдельного разрешения владельца.

## Точки входа

```text
cmd/itmap/main.go               Go HTTP API + раздача React SPA
cmd/itmap-worker/main.go        отдельный постоянный Go Worker
cmd/itmap-recovery/main.go      инспекция / ограниченная сверка известных Apify операций
cmd/itmap-parser-staging/       изолированный тестовый запуск парсера
cmd/itmap-schema-audit/         отдельные операторские проверки схемы

internal/httpapi/               net/http ServeMux, middleware, /api/*, /health/*
internal/auth/                  сессии и роли
internal/config/                конфигурация Go API (не конфигурация Worker)
internal/workerconfig/          явные enable-флаги, интервалы и grace Worker
internal/workerengine/          engine, loopback monitor и read-only readers
internal/workerloop/            три последовательных профиля и bounded backoff
internal/parser/                SQL jobs/runs/attempts, HH, AI, import, recovery,
                                scheduler, расходы, диагностика, воронка
internal/consumercontrol/       отдельные guarded/consumer candidate-контракты;
                                не считать все подключёнными к runtime

internal/storage/               MariaDB, SQL и baseline/schema tests
internal/dbschema/              доверенная схема
internal/backup/, v39/          export/validate/restore
internal/catalog/, crm/         каталог и пользовательская CRM
internal/vacancies/             операции над вакансиями
internal/applications/          отклики, receipts, CAS
internal/tasks/, contacts/      задачи и контакты
internal/channels/, roles/      каналы и справочники
internal/profiles/              резюме/настройки
internal/classification/        классификация направлений и промтов
internal/coverletters/          генерация сопроводительных писем
internal/aiproviders/           OpenAI/OpenRouter и защищённые провайдеры
internal/apifyruns/             paid Actor ledger/reconcile и uncertainty
internal/integrations/
  googlecalendar/               OAuth/PKCE, ручной sync, стабильные Event ID
  hhpublic/                     публичный HH, read-only извлечение
internal/legacycrypto/          криптосовместимость прежних форматов

frontend/                       React, TypeScript, бизнес-экраны/карта/редакторы
frontend/api/client.ts          API client и cache invalidation
frontend/features/settings/     диагностика, JSON, настройки парсера
web/main.tsx                    точка входа React SPA
vite.web.config.ts              сборка клиентского кода
shared/                         общие контракты/фильтры
server/, build/, db/schema.ts   историческая JS/Cloudflare D1 база для parity,
                                не backend новой Go-программы
deploy/                         release receiver, systemd templates и runbooks
.github/workflows/              проверка, публикация API и Worker artifact
docs/, tasks/                   документация, утверждённые критерии и статусы
```

**Именование:** названия `workerengine`, `workerloop`, `parser.Worker` обозначают разные уровни **одного** фонового runtime. Профили `launch`, `reconcile`, `pipeline` работают поверх общей SQL-очереди, а не трёх отдельных брокеров. HTTP API не обрабатывает очередь непрерывно.

## Сборка и деплой

- Go использует `net/http`, `database/sql` и build tag `mariadb`; минимальный модульный Go — `go 1.23.0` по [go.mod](../../go.mod).
- [`publish.yml`](../../.github/workflows/publish.yml) собирает `./cmd/itmap` и Vite `dist/web` для разрешённого `main`; **не** собирает и не обновляет отдельно установленный Worker в release archive.
- [`worker-artifact.yml`](../../.github/workflows/worker-artifact.yml) собирает `./cmd/itmap-worker` и `./cmd/itmap-recovery` для Linux amd64/arm64, с `vcs.revision` и checksum; не включает автоустановку на сервер.
- [GO-447](../../tasks/GO-447-WORKER-DEPLOY-RESTART.md) — ещё открытая задача безопасной совместной доставки, grace/drain, restart и rollback. [`deploy/WORKER-PRODUCTION.md`](../../deploy/WORKER-PRODUCTION.md) содержит текущую процедуру и ограничения.
- Worker запускается с отдельными разрешающими флагами и проверкой схемы. Включение флага способно инициировать платный запрос, поэтому не является диагностикой. В API `APP_ENV=production` пока запрещён кодом конфигурации, а `/health/ready` намеренно сообщает 503.
- Команды проекта и тестирования: [локальная инструкция](../user/deployment/LOCAL.md). Для React нужны `npm run typecheck:web`, `npm run format:check`, `npm run build:web`; для Go — `go test -race ./...`, `go vet ./...` и disposable MariaDB gate при затронутой SQL-логике.

**Граница готовности:** наличие Go-модуля и успешный PR CI не доказывают, что тот же бинарник установлен, что legacy и новый consumer не сосуществуют, или что реальные OAuth/AI/Apify вызовы безопасно завершились. Проверки в `main` и на сервере документируются раздельно.
