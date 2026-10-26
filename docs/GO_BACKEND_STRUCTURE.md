# Go source layout (migration 1.1)

## Начать разработку

Обязательные правила и ограничения находятся в [AGENTS.md](../AGENTS.md), общий рабочий план — в [PLANS.md](../PLANS.md). Задачу, её зависимости и критерии выбирают в [реестре](../tasks/README.md); проверенные ветки, PR и результаты собраны в [STACK.md](../tasks/STACK.md). Отметка MERGED у дочернего PR сама по себе не означает, что его код уже есть в main.

Перед изменениями сверяют [архитектуру](ARCHITECTURE.md), [историческую D1](DATABASE.md), [целевую MariaDB](MARIADB.md) и [архитектурные решения](DECISIONS.md). Исходные API-контракты: [инвентарь](LEGACY_API_BASELINE.md) и [справочник](API.md). Ограничения публикации исходников описаны в [границе публичного снимка](MIGRATION_PUBLIC_REPOSITORY.md).

## Расположение исходников

```text
cmd/itmap/main.go                 точка входа Go и сборка зависимостей
internal/auth/                    сессии и роли
internal/config/                  конфигурация процесса
internal/httpapi/                 net/http handlers, middleware и SPA
internal/storage/                 MariaDB driver, baseline и SQL-проверки
internal/storage/baseline.json    ожидаемая схема MariaDB
internal/catalog/, crm/           каталог, CRM и пользовательские списки
internal/vacancies/               вакансии, роли и пользовательское состояние
internal/applications/            отклики, receipts, версии и история
internal/tasks/, channels/, roles/ задачи, каналы и справочники
internal/parser/                  отдельные settings/budget/cycle-lease контракты
internal/legacycrypto/            совместимость legacy AES-GCM
internal/integrations/apify/      HTTP-адаптер в отдельной ветке PR #36
frontend/                        React и TypeScript
web/, vite.web.config.ts         клиентская Vite-сборка
server/, build/                  очищенный исходный JavaScript для сверки
app/                             историческая Next/Vinext-структура
db/schema.ts                    историческая D1-схема
docs/, tasks/                   документация и критерии задач
```

Go-бекенд находится в `cmd/` и `internal/`, а не в `server/`. Для поиска файлов: `rg --files cmd internal -g '*.go'`. JavaScript сохраняется как эталон миграции; наличие JS-модуля не означает, что соответствующая функция уже подключена в Go. Модули `parser`, `legacycrypto` и Apify пока не составляют работающий полный Go-конвейер. Каталог Apify относится к [PR #36](https://github.com/meInteractive/it-map/pull/36), который пока не объединён в main; остальные перечисленные Go-модули присутствуют в текущей основной ветке.

HTTP использует стандартный `net/http`, не Gin; хранилище — `database/sql`. MariaDB-драйвер включается build tag `mariadb`. Приложение читает окружение процесса, но не загружает `.env` автоматически; [.env.example](../.env.example) содержит безопасный шаблон параметров, не реальные секреты.

## Сборка, запуск и проверки

Команды, переменные и назначение проверок перенесены в [локальную инструкцию](deployment/LOCAL.md). Сборка Go с MariaDB требует `-tags mariadb`, React — `npm run build:web`; старые Worker-команды не заменяют их. Запуск на собственном сервере и ограничения текущего приложения описаны в [развёртывании](deployment/README.md).

Работающий GPT Sites/D1/Site Automation остаётся отдельной системой. Не нужно включать исторический `server/` как Node-бекенд Go-сервиса или переносить в Git рабочие данные/ключи. Текущая Go-реализация не объявлена production-ready; статусы конкретных проверок и неперенесённых сценариев фиксируются в задачах, а не выводятся из наличия исходников.
