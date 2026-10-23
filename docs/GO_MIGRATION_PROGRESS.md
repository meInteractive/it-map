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
