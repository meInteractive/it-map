# GO-334: совместимость с MariaDB 10.3.32

Пользователь 29.10.2026 уточнил фактическую версию сервера: `10.3.32-MariaDB`. Проверка относится к Go-коду и синтетической схеме, без подключения к этому серверу, реальных данных, DDL или обновления установленной БД. Текущая минимальная версия приложения и offline `schema-init` — 10.3.32; MySQL не поддерживается.

## Исправленные различия

- `JSON_TABLE` появился в [10.6](https://mariadb.com/docs/server/reference/sql-functions/special-functions/json-functions/json_table). Поиск очереди теперь использует `JSON_CONTAINS` строковых ID: сохраняются D1 Number/JSON1 INTEGER/REAL, округление больших ID и точное сравнение без преобразования MariaDB в double. Oracle расширен до 106 ответов, включая границы signed BIGINT.
- `JSON_ARRAYAGG` появился в [10.5](https://mariadb.com/docs/server/reference/sql-functions/aggregate-functions/json_arrayagg). Контроль сохранности CRM в тестах читает упорядоченные `JSON_ARRAY` по строкам и собирает снимок в Go. Лимит `GROUP_CONCAT` не используется.
- Поле `information_schema.STATISTICS.IGNORED` связано с [ignored indexes, начиная с 10.6](https://mariadb.com/docs/server/ha-and-performance/optimization-and-tuning/optimization-and-indexes/ignored-indexes). Старые серверы дают эквивалент `NO`; новые проверяют фактическое значение.
- До [10.8 направление DESC в индексах игнорируется](https://mariadb.com/docs/server/server-usage/tables/create-table). Только известные позиции DESC эталона нормализуются для старых версий. Уникальность, состав, порядок столбцов, префиксы, FK, CHECK и триггеры проверяются строго. На новых версиях неверное направление остаётся ошибкой. Возможен менее эффективный план; результат задаётся явным `ORDER BY`.
- Database `COMMENT` / `SCHEMA_COMMENT` появился в [10.5](https://mariadb.com/docs/release-notes/community-server/old-releases/10.5/what-is-mariadb-105). Защита пересоздания пустой disposable CI-базы перенесена в отдельную `itmap_ci_guard.ownership`, созданную CI root. Тестовый пользователь имеет там только SELECT; UUID текущего запуска проверяется до каждого DROP. Рабочая схема и backup v39 не расширяются.
- CI использует совместимые `MYSQL_*`, клиент `mysql` и TCP healthcheck через `mysqladmin`. Каждый job проверяет реальный `SELECT VERSION()`. Для 10.3.32 проверяется точный patch, для 10.11 допускается актуальный patch образа.

## Проверки

Полный одноразовый набор `.github/workflows/mariadb-staging-ci.yml` повторяется через `.github/workflows/mariadb-103-ci.yml` на `mariadb:10.3.32`; прежний контур остаётся на `mariadb:10.11`. В обоих обязательны прежние точные количества тестов и запрет пропусков. Включены schema init/retry/ownership, 72 таблицы/606 столбцов/50 FK/22 триггера, CRM и vacancy CAS/rollback, backup v14–39/restore/generation/crash, parser/settings/history и HTTP/UI.

Статус на момент реализации: локальные проверки выполняются, положительный результат двух MariaDB CI ещё ожидается. После завершения здесь фиксируются ссылки и точные вершины; наличие workflow само по себе совместимость не доказывает.

## Эксплуатация

Функциональная совместимость и сопровождение сервера — разные условия: [MariaDB 10.3 завершила сопровождение 25 мая 2023](https://mariadb.org/about/). Источник исправлений установленного пакета проверяется отдельно. Для новой установки предпочтительна поддерживаемая ветка; автоматического обновления существующего сервера нет. Конфигурация конкретного сервера, его права, данные, нагрузка и live deployment этой синтетической проверкой не принимаются.
