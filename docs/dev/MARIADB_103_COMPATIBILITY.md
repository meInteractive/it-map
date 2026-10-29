# GO-334: проверка MariaDB 10.3.32

**Вывод: текущая доверенная схема it-map несовместима с MariaDB 10.3.32. Проверенный минимум остаётся 10.11.** Это подтверждено реальным импортом неизменённой пустой схемы в одноразовый `mariadb:10.3.32`, а не сравнением номеров версий. Рабочий сервер пользователя, данные и DDL не затрагивались.

## Подтверждённый блокер схемы

В `db/testdata/empty-schema.sql` **23 уникальных индекса `USING HASH`** по полным длинным текстовым значениям. Первый — `agency_identity_agency_name_unique`, строка 942. MariaDB 10.3.32 возвращает:

```text
ERROR 1170 (42000) at line 942: BLOB/TEXT column 'agency_name' used in key specification without a key length
```

Это затрагивает не только имена компаний/агентств, но и URL вакансий, пользовательское состояние, активные задания, generation/dedupe/request/operation keys и источники вакансий. Поддержка уникальных индексов по полным BLOB/TEXT была добавлена в [MariaDB 10.4](https://mariadb.com/docs/release-notes/community-server/old-releases/10.4/what-is-mariadb-104); само наличие этой возможности в 10.4 не доказывает совместимость остальных частей проекта. Полностью принятый контур проекта — 10.11.

Уникальный префикс меняет контракт: разные длинные значения с одинаковым началом будут конфликтовать. Удаление UNIQUE допускает дубликаты и нарушает изоляцию/идемпотентность. Дополнительный digest-столбец или собственный реестр с триггерами потребовали бы отдельной версии схемы, проверки коллизий, конкуренции, FK и восстановления. Такие замены здесь не выполняются.

## Другие различия, выявленные аудитом

- Runtime поиска очереди использует `JSON_TABLE`, появившийся в [10.6](https://mariadb.com/docs/server/reference/sql-functions/special-functions/json-functions/json_table).
- Снимки CRM в интеграционных тестах используют `JSON_ARRAYAGG`, появившийся в [10.5](https://mariadb.com/docs/server/reference/sql-functions/aggregate-functions/json_arrayagg).
- `information_schema.STATISTICS.IGNORED` доступен с [10.6](https://mariadb.com/docs/server/ha-and-performance/optimization-and-tuning/optimization-and-indexes/ignored-indexes).
- До [10.8 направление DESC физически игнорируется в индексах](https://mariadb.com/docs/server/server-usage/tables/create-table), поэтому метаданные и планы отличаются.
- Database `COMMENT` / `SCHEMA_COMMENT` для маркера пустой CI-базы появился в [10.5](https://mariadb.com/docs/release-notes/community-server/old-releases/10.5/what-is-mariadb-105).

Пробные совместимые замены SQL/метаданных не решают блокер 23 уникальных индексов. Они отозваны до итогового принятия: минимум не понижается, доверенная схема, v39 и ограничения уникальности остаются прежними.

## Изменения кода и проверки

`storage.Open` после Ping читает `SELECT VERSION()` и применяет общую проверку `dbschema.CheckServerVersion` **до baseline и запуска HTTP**. Неизвестный сервер, MySQL и MariaDB ниже 10.11 возвращают `ErrUnsupportedServer`; пул закрывается. `schema-init` использует ту же проверку до GET_LOCK/Inspect/DDL/generation seed. Ошибка не содержит DSN, пароля или недоверенной строки версии.

`.github/workflows/mariadb-103-ci.yml` намеренно проверяет **отказ**, а не успешную работу на 10.3:

1. Точный сервер `10.3.32-MariaDB`, одноразовая БД и уникальный маркер запуска в отдельной БД, созданной CI root. У тестового пользователя к маркеру только SELECT; UPDATE обязан получить 1142.
2. Импорт неизменённой схемы обязан завершиться именно ERROR1170 на `agency_name`; любая другая причина или неожиданно успешный импорт проваливают тест.
3. Реальные `storage.Open` и `dbschema.Initialize` обязаны отказать по версии, а не скрыть проблему ошибкой неполной схемы. Количество таблиц/столбцов/индексов/триггеров не меняется, generation не создаётся. Точный required test без skips.

Прежний полный `.github/workflows/mariadb-staging-ci.yml` остаётся на 10.11: schema init/retry/ownership, 72 таблицы/606 столбцов/50 FK/22 триггера, CRM/vacancy CAS/rollback, backup v14–39/restore/generation/crash, parser/settings/history и HTTP/UI. Локально выполняются Go race/vet/build, Go1.23, web, docs/links, API audit/actionlint/history; positive MariaDB только в guarded disposable GitHub CI.

Первичный эксперимент: PR[#94](https://github.com/meInteractive/it-map/pull/94), commit `e7212cfb0f1ff3465f06046370b57aabcc71c274`, [run40858223594](https://github.com/meInteractive/it-map/actions/runs/40858223594), job123394524459 — точная10.3.32 и ERROR1170 при импорте. Итоговый regression и negative guard после исправления ещё ожидаются.

## Последствия для сервера

Размещение текущей схемы на установленной MariaDB10.3.32 блокируется. Рабочий путь для текущего проекта — подготовить MariaDB10.11+ на том же сервере с отдельным согласованным backup/restore-планом либо отдельно принять адаптацию схемы под10.3. Автоматического обновления, установки пакетов и преобразования данных нет. Остальная независимая миграция Go может продолжаться; наличие negative guard не закрывает приёмку реального сервера.

Дополнительно [сопровождение MariaDB10.3 завершилось 25 мая 2023](https://mariadb.org/about/); функциональная адаптация сама по себе не обеспечит исправления безопасности установленного пакета.
