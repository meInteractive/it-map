# it-map — перенос GPT Sites → Go / MariaDB

it-map — CRM и карта работодателей с вакансиями, откликами, заданиями и автоматическим поиском вакансий. В этом монорепозитории находятся **Go-бекенд в разработке**, React/TypeScript-фронтенд и очищенный исходный JavaScript GPT Sites как справочник. **Текущий Go-код не готов к production.** Работающий GPT Sites с D1 и Site Automation не изменён.

## Начать работу с Codex Cloud

1. Прочитать [AGENTS.md](AGENTS.md): обязательные ограничения, проверка и PR.
2. Открыть [PLANS.md](PLANS.md): общий план 1.1–1.4, cutover и rollback.
3. В [tasks/README.md](tasks/README.md) выбрать одну задачу и проверить зависимости. **Не менять `main` и `master` напрямую** и не выполнять production deployment/слияние автоматически.
4. Сверять изменения с [архитектурой](docs/ARCHITECTURE.md), [старой БД D1](docs/DATABASE.md) и [целевой MariaDB](docs/MARIADB.md). [Решения и открытые вопросы](docs/DECISIONS.md).

## Исходники

```text
cmd/itmap/main.go         точка входа Go
internal/                 Go: auth, storage, httpapi, catalog, vacancies, applications, tasks и др.
frontend/                 React + TypeScript
web/, vite.web.config.ts  клиентская Vite-сборка
server/, build/           очищенный исторический JS-код GPT Sites (для сверки)
db/schema.ts              старая D1-схема
internal/storage/baseline.json  ожидаемая схема MariaDB
docs/, tasks/             документация и список задач
```

Go пока использует **стандартный `net/http`**, а не Gin. Драйвер MariaDB подключается через build tag `mariadb`. Конфигурация Go: `.env.example` содержит **только имена переменных**, не секреты.

## Локальные проверки (без доступа к реальным данным)

```bash
go test ./...
go vet ./...
go build ./cmd/itmap
python3 scripts/check-public-repo.py
```

С доступными пакетами и безопасной базой staging дополнительно:

```bash
go test -race ./...
go test -tags mariadb ./...
go build -tags mariadb ./cmd/itmap
npm ci
npm run build:web
```

Статус проверки каждого из этих шагов **не подразумевается автоматически**. HTTP readiness в незавершённом приложении намеренно возвращает 503, а не ложное подтверждение готовности. Для MariaDB-интеграционных тестов требуется приватная тестовая база и явная безопасная конфигурация.

## Публикация и данные

Это потомок **очищенного исходного проекта**, а не точная копия работающей production-среды и не самостоятельный production-restore. `src/data.json` в исходной ветке — синтетический каталог; оригинальные SQL-миграции с данными, приватная история, собранный `dist/` и проектные hosting ID были исключены из публичного снимка. Полные SQL-дампы MariaDB/D1, JSON-backup v39, реальные резюме, контакты и секреты хранятся отдельно и **не должны попадать в GitHub**. `.gitignore` не стирает уже опубликованную историю: её следует проверять отдельно.

Исторические материалы: [старое API](docs/API.md), [формат backup v39](docs/BACKUP_FORMAT.md), [исходный публичный снимок](docs/MIGRATION_PUBLIC_REPOSITORY.md), [предыдущие Go-чекпойнты](docs/SPRINT_1_1_V12.md).
