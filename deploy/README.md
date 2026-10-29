# GO-005 / GO-007: безопасная приёмка MariaDB и staging

Актуальная инструкция запуска на своём сервере: [docs/user/deployment/README.md](../docs/user/deployment/README.md). Там описаны Ubuntu 24.04 и условный вариант [Ubuntu 20.04 LTS](../docs/user/deployment/UBUNTU-20.md), два изолированных main/dev-контура, обязательные переменные, systemd/Nginx/TLS, Docker и требования к автоматическому GitHub Actions деплою. Этот исторический runbook ниже сохраняет этапы подготовки; старые сообщения о недоступном CI уточняются последующими отчётами.

Дополнительные шаблоны: `itmap@.service.example` (два native-процесса), `nginx-http.conf.example`/`nginx.conf.example` (ACME и HTTPS), `Dockerfile` (только сборка образа). Установка и включение native автоматики — [ACTIONS.md](../docs/user/deployment/ACTIONS.md) и [RECEIVER.md](../docs/user/deployment/RECEIVER.md); до ITMAP_DEPLOY_ENABLED=true и подготовки сервера публикация выключена. CI Go/web/MariaDB/API/browser уже подтверждён на синтетических данных; внешний staging и согласованный снимок всё ещё требуют подготовки.

**Только тестовый контур. Не развёртывать на production и не импортировать реальные данные без отдельного разрешения.**

## Что уже подготовлено

- `db/testdata/empty-schema.sql`: пустая схема 72 таблиц / 22 триггеров, без `INSERT` и данных пользователя, для disposable MariaDB 10.11 CI. **Не является источником данных для настоящего staging**.
- `.github/workflows/mariadb-staging-ci.yml`: запускает временную MariaDB 10.11, импортирует только пустую схему, проверяет в настоящей БД 50 FK и 22 триггера, `ValidateBaseline`, транзакционный `ROLLBACK` и `clientFoundRows`, затем поднимает Go-сервер и запускает HTTP-smoke. Тесты требуют явного DSN (не пропускаются в CI). Только синтетические записи, которые откатываются. Никаких внешних провайдеров и production credentials.
- `scripts/staging-smoke.sh`: проверяет `/health/live=200`, `/health/ready=503` с причиной `business_api_not_migrated`. HTTP 503 **обязателен** до полного cutover; простое включение `DB_ENABLED=true` не означает production-ready. Дополнительный режим `ITMAP_SMOKE_AUTH=1` проверяет анонимный доступ, логин и cookie-сессию **только через HTTP loopback** с синтетическими тестовыми паролями; не используйте его с боевыми учётными записями.
- `deploy/itmap-staging.service.example`: шаблон службы, без секретов и без автоматического запуска.

## Fail-closed запуск staging (v21)

`APP_ENV=staging` теперь **не запустится**, если `DB_ENABLED` или `AUTH_ENABLED` выключен. Значения разрешены только `true`/`false` (опечатки вроде `treu` вызывают ошибку). Это изменение не активирует базу или авторизацию автоматически и не является разрешением на production. Для локальной разработки `APP_ENV=development` оба флага могут быть `false`.

Проверка локально: `go test ./internal/config -count=1` и отрицательный запуск `APP_ENV=staging DB_ENABLED=false AUTH_ENABLED=true ... /opt/itmap/bin/itmap` (должен завершиться с кодом 1 до открытия порта).

## Порядок ручного staging (после согласования инфраструктуры)

1. Изолированная MariaDB 10.11: отдельная пустая база, отдельный пользователь, закрытый внешний порт; импорт **приватного** актуального прямого дампа только при отдельном разрешении. Сначала сохранить контрольные суммы и выполнить восстановление тестовой копии.
2. `go.sum` уже отслеживается в Git. На машине с доступом к Go Modules выполнить `go mod download`, `go mod verify`, `go mod tidy -diff` и `git diff --exit-code -- go.mod go.sum`. Проверки не должны изменять манифесты. Затем выполнить `go test -race ./...`, `go test -tags mariadb ./...`, `go build -tags mariadb -o /tmp/itmap-mariadb ./cmd/itmap` и `npm run build:web`. Запуск tagged-тестов без тестовых DSN пропускает SQL-интеграцию и не подтверждает готовность MariaDB. CI использует уже зафиксированные суммы; старый временный checksum artifact больше не создаётся.
3. Создать `/etc/itmap/staging.env` с доступом только у пользователя приложения/администратора и без секретов в публичном Git. Требуются `APP_ENV=staging`, `DB_ENABLED=true`, `AUTH_ENABLED=true`, `DB_HOST=127.0.0.1`, уникальная `DB_NAME`, а также `SESSION_SECRET`, `DB_PASSWORD` и пароли тестовых учётных записей. Не копировать боевые токены интеграций.
4. Запустить Go за HTTPS reverse proxy **только на тестовом домене**; `systemd` пока вручную. В отдельном окне: `bash scripts/staging-smoke.sh https://STAGING-HOST`. Smoke должен показывать, что БД доступна, но readiness для production ещё **закрыта**.
5. Проверить пользовательские сценарии на специально подготовленных тестовых данных, сравнить API старого JS и Go, а также сборку React/PWA/авторизацию на staging. Результаты занести в `tasks/SPRINT-01.md`. Никаких изменений в старом GPT Sites.

**Совместимость с драйвером:** DSN Go/CI должен указывать `collation=utf8mb4_bin` (поддерживается `go-sql-driver/mysql` v1.9.3). Столбцы MariaDB остаются в `utf8mb4_nopad_bin`, без изменений схемы. Интеграционный тест проверяет оба значения, см. `docs/dev/MARIADB.md` §10.

## Невыполненные acceptance gates

- Аудит облачной среды от 24.10.2026 подтвердил загрузку и проверку Go-модулей, неизменность `go.mod`/`go.sum`, обе Go-сборки и React-сборку. **Живые SQL/CI-тесты ещё не подтверждены**: GitHub API возвращает `Forbidden`, а локальная БД не запускалась — текущая задача разрешает синтетическую базу только внутри CI. Требуются успешный disposable MariaDB CI и отдельно согласованный внешний staging. Подробности и следующая задача: [аудит](../docs/dev/CLOUD_ONBOARDING_AUDIT.md).
- Пустая CI-схема подтверждает базовый SQL-контракт, но не проверяет перенос реальных записей, граничные случаи, миграции, все уникальные ограничения при конкурентной записи, и не заменяет тестовый импорт прямого дампа.
- Никакие операции создания тестового домена, настройки сервера, DNS, реального деплоя или production-переключения не выполнялись.

MariaDB10.3.32 несовместима с текущей доверенной схемой; runtime и schema-init отклоняют её до baseline/DDL. [Результат проверки](../docs/dev/MARIADB_103_COMPATIBILITY.md).
