# Миграция IT Map: актуальный срез и архив исходной итерации

> **Обновлено 09.11.2026 по коду main SHA [7fa1047](https://github.com/meInteractive/it-map/commit/7fa1047860df0a92165726c438418f77a4b43909).** Полная оценка по фактическому коду, PR/SHA/tests и границам — [GO-119: доказательная матрица](GO-119-ARCHITECTURE-EVIDENCE.md). Это **не** акт приёмки production и не обновление реальных данных.

## Срез 09.11.2026

| Область | Наблюдаемое состояние | Доказательство / ограничение |
| --- | --- | --- |
| Go HTTP / React / MariaDB | Маршруты каталога, вакансий, CRM, профиля и админ-инструментов подключены; база и SPA проверяются в CI | [HTTP-маршрутизатор](../../internal/httpapi/handler.go), [стадии CI](../../.github/workflows/go-ci.yml); несовместимые/неперенесённые legacy API и cutover остаются открытыми |
| HH → Apify → AI → импорт | Реализованы Go Worker.Cycle, стадии, курсоры и планирование HH, управляемое внешним сигналом | [worker.go](../../internal/parser/worker.go), [scheduled_sources.go](../../internal/parser/scheduled_sources.go), [historical live preflight](PARSER_LIVE_PREFLIGHT.md); Go Worker как постоянный самостоятельный процесс не включён |
| Статистика публикаций | Две самостоятельные метрики: успешные импорты кандидатов и новые уникальные вакансии | [PR #287](https://github.com/meInteractive/it-map/pull/287), [funnel.go](../../internal/parser/funnel.go); не путать с внешней датой публикации HH |
| Стоимость и аудит | Read-only частичная себестоимость, lineage-диагностика и признаки повторных расходов | [PR #284](https://github.com/meInteractive/it-map/pull/284)–[#286](https://github.com/meInteractive/it-map/pull/286), [#289](https://github.com/meInteractive/it-map/pull/289), [#291](https://github.com/meInteractive/it-map/pull/291); полной доказуемой когортной стоимости нет |
| Расписание HH | Read-only диагностика источников, сроков и причин ожидания | [PR #292](https://github.com/meInteractive/it-map/pull/292); наблюдение реального поступления вакансий после перехода на ежедневный интервал подтверждено владельцем, но исторические пробелы — отдельная [issue #293](https://github.com/meInteractive/it-map/issues/293) |
| Роли/письма | Admin-настройки классификации и Go-генерация писем с безопасным повтором | [PR #276](https://github.com/meInteractive/it-map/pull/276), [#277](https://github.com/meInteractive/it-map/pull/277), [#280](https://github.com/meInteractive/it-map/pull/280)–[#282](https://github.com/meInteractive/it-map/pull/282); платные провайдеры и live/browser не считаются принятыми |
| Доставка / данные | MariaDB baseline и восстановление проверяются на одноразовой CI БД; release gate запрещает ложную готовность | [ARCHITECTURE.md](ARCHITECTURE.md), [MARIADB.md](MARIADB.md); полный D1→MariaDB cutover/rollback на реальных данных не принят |

**Точная степень проверки:** PR #292 на head `b8235484c3e3cb24d26588bf770719400fd5c0a3` прошёл все три обязательных GitHub workflows (17/17 check runs), как и PR #289 и #291 на их собственных HEAD. Это не свидетельство того, что все поздние изменения были вручную приняты на внешнем staging, и не независимое ревью production.

**Что не следует делать без согласования:** миграции и реальный DDL, изменение данных или таймеров на рабочем сервере, paid provider calls, запуск второго worker, переназначение бюджетов, dogruzka пропущенных HH-публикаций. Владелец передал будущие изменения схемы Codex при отдельном разрешении.

## Архив: стартовая контрольная точка (историческая, больше не текущий статус)

Пункты ниже сохранены **дословно как архив ранней итерации**. Не переносить из них утверждения «нет работающего парсера», «нельзя собрать SPA» или «не проверена MariaDB» в описание состояния 09.11 без повторной сверки.

# Sprint 1.1 — факт выполнения

- [x] 1.1.01: очищенная публичная `gpt-site` с отдельным коммитом; полный DB dump/backup не добавлялись.
- [~] 1.1.02: статический маршрутный инвентарь и инварианты API; полная эталонная контрактная регрессия требует отдельного тестового окружения.
- [x] 1.1.03: стартовый Go HTTP-сервер, graceful shutdown, health и запрет ложного успеха неготового API; проверено локально.
- [x] 1.1.04: конфигурация рантайма, ограничение localhost и запрет production для незавершённого сервиса. Секреты будут подключены только одновременно с реальной авторизацией/интеграциями.
- [~] 1.1.05: MariaDB DSN/pool, transaction helper and strict schema baseline for 72 tables/606 columns; no live DB driver/network integration test yet.
- [~] 1.1.06: isolated Vite build configuration, Go SPA static handler and API-route isolation; npm build blocked by unavailable registry cache.
- [~] 1.1.07: basic public safety/Go/Web CI; production deployment remains disabled.
- [~] 1.1.08: legacy form login, HMAC cookie and role-aware session endpoint ported and unit tested; browser/proxy tests pending.
- [~] 1.1.09: first read-only authenticated MariaDB catalog search endpoint ported with unit/HTTP tests; list/map/detail and DB execution remain open.
- [ ] 1.1.10–1.1.28: not yet accepted.

Пока `/health/ready` и неперенесённые legacy API возвращают 503 (либо 401 без сессии при включённой авторизации). Это **не готовая миграция** и такой бекенд нельзя переключать на рабочий домен.

В стартовой итерации используется стандартный `net/http` (ServeMux), а не Gin. Он даёт типизированный маршрутизатор без внешних зависимостей. Если понадобится Gin, решение необходимо принять **до начала массового переноса HTTP-обработчиков**.
