# Локальный запуск и конфигурация

## Получить код и собрать

Нужны Git, Go, Node.js/npm и интернет к GitHub, Go Modules и npm registry; версии — в [оглавлении](README.md). Выбирайте проверенный коммит, содержащий последние исправления: отметка MERGED у дочернего PR сама по себе не доказывает, что его код есть в main (см. [ветки](RELEASES.md)).

```bash
git clone https://github.com/meInteractive/it-map.git
cd it-map
git switch --detach <ПРОВЕРЕННЫЙ_ПОЛНЫЙ_SHA>
go version
node --version
npm --version
go mod download
go mod verify
npm ci
npm run build:web
CGO_ENABLED=0 go build -mod=readonly -tags mariadb -o /tmp/itmap ./cmd/itmap
```

`<...>` в командах — место для подстановки, не готовый shell-синтаксис. Сборка создаёт `/tmp/itmap` и `dist/web/`; они не коммитятся. `-tags mariadb` обязателен для запуска с БД. Обычная Go-сборка без тега подходит только для режима без MariaDB. CGO_ENABLED=0 исключает зависимость артефакта от версии glibc сборочной машины (важно для доставки с нового Actions runner на Ubuntu 20.04 или 24.04). Архитектура бинарника должна совпадать с сервером; для Linux/amd64 можно явно задать `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`, для ARM64 — `GOARCH=arm64`.

## Шаблон .env

Отслеживаемый шаблон находится в корне: [.env.example](../../../.env.example). **Go читает окружение процесса, но не загружает .env автоматически.** dotenv-библиотека не требуется. В development можно загрузить свой доверенный shell-файл перед стартом; на сервере файл читает systemd, в Docker — `--env-file`.

```bash
umask 077
cp .env.example .env
chmod 600 .env
# Отредактировать .env своим редактором, затем в bash:
set -a
. ./.env
set +a
export WEB_DIST_DIR="$PWD/dist/web"
/tmp/itmap
```

По умолчанию шаблон выключает DB/Auth: это локальная оболочка на `127.0.0.1:8080`, `/health/live` отвечает 200, `/health/ready` — 503 с `mariadb_not_connected`. Чтобы проверить вход, включите `AUTH_ENABLED=true`, заполните `SESSION_SECRET` и `SITE_PASSWORD` в своём файле. Секрет подписи должен содержать минимум 32 байта. В отдельном терминале `openssl rand -hex 32` генерирует значение для **одного** секрета; не публикуйте вывод. Для main/dev используйте разные SESSION_SECRET и пароли входа приложения; DB_USER/DB_PASSWORD по решению пользователя общие, а DB_NAME отличается. Настроенный `SITE_PASSWORD` включает администратора `eugene`; `NASTYA_PASSWORD` и `DEMO_PASSWORD` включают соответствующих пользователей. Пустой пароль оставляет дополнительный аккаунт выключенным.

Для локального входа используйте Chromium и `http://127.0.0.1:8080`: Chromium считает loopback доверенным для Secure cookie. Для внешнего доступа нужен HTTPS. Страница должна открываться через тот же origin, куда отправляется `/auth`.

Файл, загружаемый через `.`, исполняется shell: используйте только собственный проверенный файл. Для переносимости между shell/systemd/Docker удобно генерировать секреты в hex и писать `KEY=value` без пробелов/кавычек/подстановок. Если существующее значение содержит `$`, пробелы, кавычки или `#`, правила экранирования у этих загрузчиков различаются: не копируйте один формат вслепую. Docker `--env-file` передаёт кавычки как часть значения. Не вводите реальные пароли в командную строку, Git, CI-логи или `set -x`.

## Переменные текущего Go-процесса

| Имя | Назначение и обязательность |
| --- | --- |
| `APP_ENV` | development/test или staging. production отклоняется до приёмки |
| `HTTP_ADDR` | только loopback: `127.0.0.1:8080`, на сервере порты 18080/18081 |
| `DB_ENABLED` | строго true/false; для staging обязательно true |
| `DB_HOST`, `DB_PORT` | только 127.0.0.1/localhost; порт 3306 по умолчанию |
| `DB_NAME`, `DB_USER`, `DB_PASSWORD` | база конкретного процесса/SQL-пользователь/пароль; обязательны при включённой БД, пароль не пустой; main/dev имеют разные DB_NAME и общий DB_USER/DB_PASSWORD |
| `AUTH_ENABLED` | строго true/false; для staging обязательно true |
| `SESSION_SECRET` | секрет HMAC, минимум 32 байта при включённой авторизации |
| `SITE_PASSWORD` | обязательный пароль администратора eugene при включённой авторизации |
| `NASTYA_PASSWORD`, `DEMO_PASSWORD` | необязательные пароли дополнительных аккаунтов |
| `WEB_DIST_DIR` | абсолютный путь к **только** собранному dist/web; без сборки HTML отдаёт 503 |

Имя БД ограничено ASCII-буквами, цифрами и `_`; не используйте DSN/URL вместо отдельных переменных. Приложение само задаёт `clientFoundRows`, UTF-8 и таймауты драйвера. Не задавайте production DSN интеграционным тестам.

В шаблоне отдельно перечислены `OPENAI_API_KEY`, `OPENROUTER_API_KEY`, `APIFY_API_TOKEN`, `FIRECRAWL_API_KEY`, `GOOGLE_CALENDAR_CLIENT_ID/SECRET`: это **исторические имена JS-интеграций**, текущий Go их не использует и они не нужны для запуска. Их заполнение не включает отсутствующие Go-функции. `ITMAP_TEST_*`, `ITMAP_SMOKE_*`, `ITMAP_BROWSER_*` — параметры проверок, не обязательные runtime-секреты; disposable SQL-тесты запускаются только в соответствующем синтетическом CI.

## Локальные проверки исходников

Базовые проверки Go и публичного состава репозитория не требуют рабочих данных:

```bash
go test ./...
go vet ./...
go build -o /tmp/itmap ./cmd/itmap
python3 scripts/check-public-repo.py
python3 scripts/check-git-history.py
```

Для race/tagged-проверок и React-сборки нужны установленные зависимости. `npm ci` использует отслеживаемый lockfile:

```bash
go test -race ./...
go test -tags mariadb ./...
go build -tags mariadb -o /tmp/itmap-mariadb ./cmd/itmap
npm ci
npm run build:web
```

В ограниченном sandbox HTTP mock-тестам может понадобиться разрешение на loopback sockets; это не разрешение обращаться к реальным провайдерам. Положительные write/rollback/crash SQL-проверки выполняются только в guarded disposable GitHub CI. Локальные unit-тесты и пропущенные без DSN интеграционные тесты не означают успешный MariaDB gate.

Успех каждого шага подтверждается отдельно. `/health/ready` остаётся 503 даже при доступной MariaDB (`business_api_not_migrated`) до полной приёмки; `/health/live` проверяет только процесс. Требования к публичным данным: [граница снимка](../../dev/MIGRATION_PUBLIC_REPOSITORY.md). После `build:web` учтите ограничение source gate на `dist/`, описанное ниже.

## С MariaDB и проверки

Для DB-режима нужны заранее созданная изолированная MariaDB не ниже 10.3.32 со схемой baseline и настроенные DB/Auth. Подробности — [сервер](SERVER.md) и [MARIADB.md](../../dev/MARIADB.md). До этого запуск с `DB_ENABLED=true` завершится ошибкой, а не создаст недостающие таблицы. Локальную реальную БД и дампы эта инструкция автоматически не разворачивает.

```bash
go vet ./...
go test -race ./...
go test -tags mariadb ./...
go mod tidy -diff
git diff --exit-code -- go.mod go.sum
curl --fail http://127.0.0.1:8080/health/live
```

Без специальных DSN tagged-тесты явно пропускают live SQL: это не успешная интеграционная проверка. Полная синтетическая MariaDB/API/browser проверка выполняется в Actions. Публичный source gate запрещает dist в checkout: перед `python3 scripts/check-public-repo.py` перенесите локальную сборку за пределы checkout и при необходимости обновите WEB_DIST_DIR. Не ослабляйте gate ради generated-файлов.

## Типичные ошибки

- `mysql driver unavailable` — пересобрать с `-tags mariadb`.
- Ошибка подключения/baseline — проверить локальный порт, права, DB_NAME и **согласованную** схему, не импортировать случайный дамп.
- Пустой HTML/503 — проверить абсолютный WEB_DIST_DIR и наличие index.html/assets; не указывать корень репозитория.
- 403 на входе — сохранить оригинальный Host через proxy и открыть форму на том же домене; не отключать Origin-проверку.
- Cookie не сохраняется — проверить HTTPS внешнего домена и настройки браузера; Secure не выключать.
- `/health/ready=503` — ожидаемо до завершения миграции; после подключения БД причина должна быть `business_api_not_migrated`.
