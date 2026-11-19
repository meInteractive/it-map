# GO-447 — доставка и безопасный перезапуск Go Worker вместе с релизом main

**Сверка 19.11.2026: постановка для Codex, НЕ «к проверке» как выполненная реализация.** [PR #471](https://github.com/meInteractive/it-map/pull/471) добавляет план, но безопасная доставка API+Worker одним SHA, drain/restart/rollback и проверка реальной службы ещё не реализованы/не приняты. Отдельный Issue #472; изменения production запрещены без явного решения владельца.


- **Спринт:** 1.1.
- **Приоритет:** высокий операционный; реализация — после стабилизации критических инвариантов GO-431. Подготовку кода и синтетических тестов можно вести независимо.
- **Исполнитель:** Codex Cloud, отдельные PR, без автоматического merge. [GitHub Issue #472](https://github.com/meInteractive/it-map/issues/472).
- **Статус:** [ ] задача поставлена 17.11.2026; реализация и server rollout НЕ выполнялись в рамках постановки.
- **Связи:** GO-431 (единственный consumer, leases, платные POST, graceful shutdown), GO-345/GO-056/GO-057 (main-only native publish, receiver, atomic current/previous, rollback), GO-007 (реальный deployment gate).
- **Контекст:** на момент постановки `.github/workflows/publish.yml` собирает `./cmd/itmap` и web, а `deploy/receive_release.py` перезапускает лишь `itmap@main.service`. Воркер — отдельный `cmd/itmap-worker`, шаблон `deploy/itmap-worker.service.example` (`ExecStart=/opt/itmap/worker/current/itmap-worker`). Поэтому обычный деплой main и restart API **не доставляют новый воркер**; отдельный `systemctl restart itmap-worker.service` без замены бинарника также не применяет код.

## Задача

Сделать так, чтобы при подтверждённом деплое `main` новая версия отдельного `itmap-worker` доставлялась **из того же проверенного commit SHA**, безопасно активировалась и проверялась; пользователь не должен вручную пересобирать бинарник или помнить отдельную команду restart.

### Этап 1 — дизайн и тестируемая реализация в Codex (без live-переключения)

1. Проверить реальные `publish.yml`, упаковщик `deploy/publish_release.py`, `deploy/receive_release.py`, systemd unit и существующие protocol/rollback тесты; оформить краткий план атомарной доставки двух бинарников (API и worker). Не предполагать, что существующий пакет уже содержит `itmap-worker`.
2. Собирать `cmd/itmap-worker` для архитектуры целевого сервера (включая ARM64), с нужными build tags; проверять `go.mod`, исходный commit, ELF architecture, digest/manifest и совместимость runtime-конфигурации. **Оба бинарника должны происходить из одного SHA**; артефакт нельзя менять после проверки.
3. Расширить формат релиза/receiver по обратно совместимому, fail-closed протоколу: доставлять worker в отдельный root-controlled versioned release path с `current/previous`; использовать существующие ограниченный deploy identity, slot locking, sequence, digest и защиту от повторного/устаревшего release. Не выдавать произвольный shell/systemctl через SSH, не включать root-доступ runtime-пользователю.
4. Перед переключением worker **прекращать новые claims** и выполнять graceful drain текущего шага. Учитывать `ShutdownGrace` и `TimeoutStopSec`, наличие активного run/job, возможного POST и неопределённого monetary outcome. Если невозможно безопасно доказать quiescence/ownership, **отложить активацию или завершить deploy с явным блокирующим статусом**, а не посылать SIGKILL ради успешной публикации. Не запускать второй consumer.
5. После безопасной остановки атомарно переключить бинарник воркера, запустить его systemd unit ровно один раз и проверить **запущенную версию/SHA**, `active`/loopback `healthz` и `readyz`, а также доступность и состояние API. `active` или `ready` сами по себе не доказывают успешную обработку вакансий; дополнительно использовать обезличенные read-only данные о jobs/leases/неизвестных расходах.
6. Определить согласованную транзакционную семантику релиза API+worker: если второй компонент не запущен/не здоров, не сообщать `success`; восстановить подходящие **предыдущие бинарники** без отката/исправления БД и без возобновления старого parser consumer. Зафиксировать состояния `deferred`, `failed_rolled_back`, `rollback_failed` и recovery после interruption.
7. Сохранить старую main-only политику: никакая feature/PR/tag ветка не публикуется на сервер; repeat publish того же sequence/SHA не делает лишний restart. Новая логика должна быть отключена до отдельного подтверждения операционного включения на реальном сервере.
8. Обновить инструкции `docs/user/deployment/`, `deploy/WORKER-PRODUCTION.md`, политику доставки и операторский runbook: как проверить установленный SHA обоих процессов и что делать при активных платных операциях, блокировке release и частичном откате.

### Этап 2 — отдельная эксплуатационная приёмка владельцем (не входит в разрешение на разработку)

- Подтвердить актуальную backup/restore, версию live worker, совместимость generation и схемы, отсутствие второго consumer и активных платных вызовов; не сбрасывать `reserved_usd` и не выдумывать `cost_usd=0`.
- Только после разрешения владельца выполнить первый контролируемый server rollout с read-only проверкой и возможностью ручной остановки. **Ни Codex, ни этот PR не должны самостоятельно рестартовать live worker, выполнять deploy или обращаться к платным провайдерам.**

## Обязательные критерии приёмки

- [ ] Build/package содержит API и worker **одного exact SHA**, архитектура/digest/manifest сверены; неподдерживаемый или несовпадающий бинарник отвергается до изменения runtime.
- [ ] Синтетические протокольные тесты доказали: обычный релиз, repeat/no extra restart, stale/foreign/ref refusal, active paid attempt → defer/abort, safe drained restart, failed worker ready → rollback, неудачный rollback → blocking status, процесс прерван до/после переключения → recovery; текущие GO-056/057 тесты не сломаны.
- [ ] Изолированная MariaDB/synthetic worker проверяет lease/attempt reserve/unknown outcome и запрет второго платного POST после shutdown/restart; нет production SQL/DDL и реальных запросов AI/Apify.
- [ ] Требуемые Go vet/race, Python receiver/protocol, security, actionlint и existing required MariaDB checks выполнены **на последнем SHA PR** без `SKIP` для новых обязательных сценариев.
- [ ] Сервис worker не активируется на сервере без отдельного решения владельца. Проверка реального server SHA и обоих systemd units документируется отдельным результатом; синтетический CI не выдаётся за live acceptance.
- [ ] Инструкция для владельца понятна: после принятого deploy **не требуется ручной restart worker**, а отказ/отложенная активация показаны явно и без ложного `success`.

## Точки входа в код

`.github/workflows/publish.yml`, `.github/workflows/worker-build.yml` (если существует — проверить), `deploy/publish_release.py`, `deploy/receive_release.py`, `deploy/tests/`, `deploy/itmap-worker.service.example`, `docs/user/deployment/{ACTIONS,RECEIVER,SERVER}.md`, `internal/workerengine`, `internal/workerloop`, `internal/parser/{worker,attempts,worker_recovery}.go`.

**Ограничения:** никаких изменений production-БД, реальных job/ledger, секретов, server env, прав пользователей, публичных IP/Tailscale-адресов и запуска платных интеграций из тестов или PR. Если safe drain невозможно обеспечить существующим протоколом, сначала отдельный design/PR с фиксированными критериями; fail closed.
