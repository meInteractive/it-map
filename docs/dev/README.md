# Документация разработчика

Внутренние материалы разработки, переноса на Go и проверки совместимости.
Пользовательские руководства и инструкции установки находятся в [docs/user](../user/README.md).

Начало работы:

- [Правила работы](../../AGENTS.md), [текущий план](../../PLANS.md), [реестр задач](../../tasks/README.md) и [результаты проверок](../../tasks/STACK.md).
- [Архитектура](ARCHITECTURE.md), [структура Go](GO_BACKEND_STRUCTURE.md), [целевая MariaDB](MARIADB.md), [архитектурные решения](DECISIONS.md).
- [Поддержка документации](MAINTENANCE.md).

Контракты и проверка переноса:

- [Исходный эталон](SOURCE_REFERENCE.md), [базовый HTTP-контракт](LEGACY_API_BASELINE.md), [покрытие API](API_COVERAGE.md).
- [Каталог](CATALOG_READ_COMPATIBILITY.md), [вакансии](VACANCY_READ_COMPATIBILITY.md), [Unicode и сортировка](LEGACY_COLLATION_COMPATIBILITY.md).
- [Парсер](GO_PARSER_MIGRATION.md), [создание записей](GO_CREATION_CONTRACT.md), [receipts](GO_RECEIPT_WRITES.md), [задачи](GO_TASK_WRITES.md).
- [Проверки в браузере](BROWSER_SMOKE.md), [сборка React](GO_WEB_BUILD.md), [авторизация](GO_AUTH_PORT.md).

Файлы `SPRINT_1_1_*` — исторические отчёты; актуальные статусы берутся из реестра задач и текущего плана.
[Историческая инструкция GPT Sites](archive/MAINTENANCE-SITES.md) сохранена для справки.
