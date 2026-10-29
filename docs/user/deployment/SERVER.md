# Ubuntu 24.04: временный main без Docker

Действующая схема [ADR-028](../../dev/DECISIONS.md) заменяет прежние два контура
и общую SQL-учётную запись. Сейчас на временном сервере **только main**,
`APP_ENV=staging`. После приёмки спринта 1.1 main переезжает на основной сервер,
а на временном остаётся конкретная ветка спринта 1.2. Второй сервер, дата и ref
пока не определены. Не доставлять произвольные feature-ветки.

Это инструкция нового изолированного стенда, не production cutover. Реальные
данные/D1/старый Sites не переносить автоматически. Не удалять неизвестные
файлы/БД. Для фактической Ubuntu20.04 сначала читать [UBUNTU-20.md](UBUNTU-20.md).

## 1. Проверки и зависимости

До установки проверить `/etc/os-release`, `uname -m`, `ss -ltn`, `df -h`, службы,
пакеты MariaDB/MySQL и существующие базы. Используемая старая БД требует отдельного
плана обновления и восстановления. Для проекта выбрана **MariaDB10.11**; MySQL
не является заменой. `SELECT VERSION()` проверяет сервер, не только клиент.
10.11.14 удовлетворяет минимуму;10.11.19 в CI не означает новый минимальный patch.
[Причина отказа10.3](../../dev/MARIADB_103_COMPATIBILITY.md).

На разрешённой новой Ubuntu24.04:

```bash
sudo apt-get update
sudo apt-get install --yes ca-certificates curl openssl python3 nginx certbot mariadb-server
sudo mariadb -e 'SELECT VERSION(); SHOW DATABASES;'
sudo useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin itmap-main
sudo install -d -o root -g root -m 0755 /opt/itmap/main/releases
sudo install -d -o root -g root -m 0700 /etc/itmap
```

Не запускать useradd повторно без проверки назначения пользователя. Go/Node/npm
нужны на сборщике, не runtime. Не выполнять массовый upgrade/reboot/Docker install
как скрытую часть настройки. IPv4 достаточно; не создавать нерабочую AAAA.
NTP/UTC проверить через `timedatectl`. SSH сохраняет действующий VPN-доступ;
изменение firewall не должно блокировать администратора.

## 2. Независимая локальная MariaDB

SQL слушает только `127.0.0.1:3306`; HTTP Go только `127.0.0.1:18080`.
На Ubuntu bind-address обычно уже задан в `50-server.cnf`; отдельный файл
`/etc/mysql/mariadb.conf.d/60-itmap.cnf` может закрепить:

```ini
[mysqld]
bind-address=127.0.0.1
default_time_zone=+00:00
```

После проверки конфигурации перезапустить только новую разрешённую MariaDB и
подтвердить listener. Создание **новой отсутствующей** базы администратором:

```sql
CREATE DATABASE itmap_main CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin;
CREATE USER 'itmap_main_app'@'127.0.0.1' IDENTIFIED BY '<PRIVATE_RANDOM_PASSWORD>';
GRANT SELECT, INSERT, UPDATE, DELETE ON itmap_main.* TO 'itmap_main_app'@'127.0.0.1';
```

SQL выше — шаблон. Секрет генерировать приватно, не в argv/history/Git/логах.
Runtime не получает CREATE/ALTER/DROP/TRIGGER/GRANT. Для schema-init отдельная
локальная admin-учётная запись с правами **только этой БД**, без GRANT OPTION.
Не выдавать `%`/`*.*` ради исправления подключения. Проверять привилегии через
information_schema, не публиковать authentication strings/password hashes.

Создание базы не создаёт таблицы. До HTTP отдельно выполнить trusted CLI
из проверенного релиза с mariadb tag, передав admin DB_PASSWORD в окружении:

```bash
APP_ENV=staging ./itmap schema-init --database itmap_main --user SCHEMA_ADMIN_USER
```

Пароль не аргумент. [Поведение schema-init](../../dev/MARIADB.md): только trusted
missing objects в новой/pristine partial базе, неизвестная/заполненная partial
структура отвергается. DDL имеет отдельные commits; общего rollback нет.
Baseline:72 таблицы,606 столбцов,50FK,22 триггера. Не импортировать CI fixtures
как данные приложения. Повторная полная schema-init должна только проверить базу.

После разделения серверов development получает **собственные** физическую
MariaDB, SQL-user, базу и пароль. Одинаковый `DB_HOST=127.0.0.1` — разные серверы.
Не копировать main.env, не синхронизировать БД и не предоставлять development
доступ к стабильной базе. Backup/restore и финальная остановка записи при переносе
согласуются отдельно; не удалять старую main-базу или backup автоматически.

## 3. Runtime env и релиз

`/etc/itmap/main.env` — root:root0600, вне Git/релизов. Основные значения:

```text
APP_ENV=staging
HTTP_ADDR=127.0.0.1:18080
DB_ENABLED=true
AUTH_ENABLED=true
DB_HOST=127.0.0.1
DB_PORT=3306
DB_NAME=itmap_main
DB_USER=itmap_main_app
WEB_DIST_DIR=/opt/itmap/main/current/web
```

DB_PASSWORD, SESSION_SECRET (случайные минимум32байта), SITE_PASSWORD задаются
приватно. Настроенный SITE_PASSWORD включает eugene; пустые NASTYA_PASSWORD и
DEMO_PASSWORD выключают дополнительные аккаунты. Go не загружает .env: systemd
использует EnvironmentFile. См. [LOCAL.md](LOCAL.md) и [.env.example](../../../.env.example).

Бинарник и web собирать из одного точного проверенного main SHA:

```bash
npm ci
npm run build:web -- --outDir /tmp/itmap-web
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -mod=readonly -trimpath -tags mariadb -o /tmp/itmap ./cmd/itmap
```

Архитектуру сверять с сервером; будущий основной сервер может отличаться.
Go минимум1.23. Проверять SHA-256 и ELF. Разместить root-owned файлы в
`/opt/itmap/main/releases/<FULL_SHA>/{itmap,web/}`, без прав записи runtime.
Первый current — относительная ссылка `releases/<FULL_SHA>`; повторное обновление
выполнять с блокировкой и сохранённым previous, через [receiver](RECEIVER.md).

Установить [unit](../../../deploy/itmap@.service.example) в
`/etc/systemd/system/itmap@.service`, проверять **конкретный экземпляр** после
создания current (проверка template подставляет несуществующий `%i`):

```bash
sudo systemd-analyze verify itmap@main.service
sudo systemctl daemon-reload
sudo systemctl enable --now itmap@main
curl --fail http://127.0.0.1:18080/health/live
bash scripts/staging-smoke.sh http://127.0.0.1:18080
```

Runtime itmap-main без sudo/SQL-admin/записи в релиз. `/health/live=200`;
`/health/ready=503` с **business_api_not_migrated** — ожидаемый release gate.
`mariadb_not_connected`/`mariadb_unavailable` означают неисправный стенд.
Не менять readiness/APP_ENV guards ради зелёной проверки.

## 4. Nginx и HTTPS

Домен приложения и отдельный домен phpMyAdmin согласует владелец; не подставлять
старые WordPress-домены. [Шаблоны Nginx](../../../deploy/nginx.conf.example)
исторически содержат второй Go-контур: сейчас использовать только main upstream.
Второй согласованный домен обслуживает phpMyAdmin, **не dev Go**.

Для ACME временный HTTP server на оба домена: location
`^~ /.well-known/acme-challenge/` с root `/var/www/letsencrypt`; остальное до
готовности может отвечать503. Проверить nginx -t перед каждым reload.
Certbot webroot выпускает SAN для двух доменов; email владельца не придумывать.
Если email не задан, допустим `--register-unsafely-without-email`, уведомления
тогда нужно отдельно настроить. Подключить certificate/fullchain и privkey,
TLS1.2/1.3, HTTP→HTTPS308 и timer certbot. Deploy hook: nginx -t && reload nginx.
Обязательно `certbot renew --dry-run`.

Main Nginx proxy_pass на loopback18080, `Host $http_host`,
`X-Forwarded-Proto $scheme`, `X-Forwarded-For $remote_addr`, без общего cache.
Не отключать Origin/Secure cookie/TLS verification. Для прямого origin-теста
можно использовать curl --resolve с исходным доменом и доверенным сертификатом.
Это не замена внешней/browser-приёмки через Cloudflare.

При Cloudflare proxy использовать Full(strict); challenge403 может блокировать
HTTP-smoke/API. Владелец отдельно настраивает правила двух доменов или DNS only.
Не считать challenge ошибкой Go и не менять авторизацию приложения ради него.

## 5. phpMyAdmin

Устанавливается только по отдельному поручению, как в текущем стенде. На Ubuntu:
phpmyadmin + php-fpm/php-mysql/php-mbstring/php-xml/php-curl/php-zip/php-gd.
Пакет может создать собственную служебную phpmyadmin-базу; она отдельна от IT Map.
Nginx document root `/usr/share/phpmyadmin`, PHP через Unix FPM socket, только
существующие .php, запрет setup/libraries/templates/vendor/sql/dotfiles.

Выбор внешнего браузерного доступа или VPN согласуется с владельцем. Для внешнего
варианта дополнительный Nginx Basic Auth **поверх HTTPS**, отдельный пароль,
htpasswd root:www-data0640; SQL cookie auth затем требует другой пароль.
Приватные credentials не отправлять в Git/CI/чат. Root SQL через web запрещён,
AllowNoPassword=false, AllowArbitraryServer=false, host127.0.0.1,
only_db=itmap_main. Администратор этой БД не получает права на mysql/другие базы.
Не открывать SQL3306 наружу. Cloudflare Basic Auth не заменяет SQL-авторизацию.

## 6. Приёмка и эксплуатация

Проверить SHA/архитектуру/общий web SHA, непривилегированную службу, loopback SQL
и HTTP, единственный main, TLS/renewal, SPA/deep link, anonymous/invalid login,
Origin403/login/session/logout/relogin/cookie flags. [Browser expiry/три роли](AUTH-ACCEPTANCE.md)
проверяются отдельно при наличии аккаунтов. Не включать synthetic auth smoke
против реальных паролей; обычный staging-smoke остаётся read-only.

Локальный timer может проверять systemd, liveness, SQL ping, диск85% и TLS14дней.
Это не внешние уведомления или полноценный 5xx monitoring. Проверить nginx
logrotate, ограничить journald; RPO/RTO/backup/restore/retention согласует владелец.
В текущем поручении backup делает пользователь, автоматически не создавать.
Reboot и live failed-release rollback — отдельные проверяемые шаги; не выдавать
unit-тесты за выполненный server gate. Откат кода не откатывает БД.

[Ограниченный deploy-пользователь](RECEIVER.md), [порядок релизов](RELEASES.md),
[Actions](ACTIONS.md). Root admin-ключ не использовать как deploy-ключ. Codex VPN
не подключает Actions runner к Tailnet; маршрут и Environments проверяются отдельно.
