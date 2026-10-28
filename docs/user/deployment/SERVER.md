# Ubuntu 24.04 / 20.04 LTS без Docker

Это подготовка **нового изолированного** сервера/контуров. Не применять команды установки, DDL, DNS и перезапуска к действующему сервису без разрешения владельца. Выбор main/dev не разрешает production: до приёмки оба процесса работают как staging. Код Go и его fail-closed проверки не менять ради запуска.

Для Ubuntu **20.04 LTS (Focal)** сначала выполните [отдельный порядок совместимости](UBUNTU-20.md). Основные команды ниже рассчитаны на 24.04; на Focal нельзя вслепую устанавливать штатную MariaDB или исходный unit с `ProtectProc`. Схема main/dev и переменные окружения общие.

## 1. Пакеты и доступ

Администратору нужны sudo, SSH, управление DNS и входящие TCP 80/443. SSH разрешайте только с административных адресов/VPN. Порты 18080/18081 и MariaDB не открывайте наружу. Если сервер доступен только по IPv4, не создавайте неработающую AAAA-запись. Настройте время/NTP.

На новом Ubuntu **24.04** (для 20.04 используйте перечень и ограничения из [UBUNTU-20.md](UBUNTU-20.md)):

```bash
sudo apt-get update
sudo apt-get install --yes git ca-certificates curl openssl python3 nginx certbot mariadb-server
```

Go 1.27.3 и Node.js 24/npm 11 установите из официальных дистрибутивов с проверкой опубликованных контрольных сумм; штатный `apt install golang nodejs` может дать другие версии. Сборку предпочтительно выполняет GitHub Actions: тогда эти SDK на runtime-сервере не нужны. Зафиксированные зависимости ставятся через `go mod download`/`npm ci`, не `npm install` с обновлением lockfile.

Создайте раздельных runtime-пользователей и root-owned каталоги:

```bash
sudo useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin itmap-main
sudo useradd --system --user-group --home-dir /nonexistent --shell /usr/sbin/nologin itmap-dev
sudo install -d -o root -g root -m 0755 /opt/itmap/main/releases /opt/itmap/dev/releases
sudo install -d -o root -g root -m 0700 /etc/itmap
```

Команды useradd выполняются один раз; при существующих пользователях сначала проверьте их назначение. Runtime-пользователям не нужны sudo, права на Git, SQL-admin или запись в бинарники/статические файлы. Загрузку релизов выполняет отдельный ограниченный deploy-пользователь; детали автоматики в [RELEASES.md](RELEASES.md).

## 2. MariaDB: две базы и общая SQL-учётная запись

Настройте MariaDB на `bind-address=127.0.0.1`, порт 3306 и проверьте `ss -ltn`: внешнего listener БД быть не должно. Рекомендуется UTC для сервера/DB-сессий; Go DSN использует UTC. Точный конфиг зависит от установленного пакета, на Ubuntu обычно `/etc/mysql/mariadb.conf.d/50-server.cnf`.

По выбору пользователя от 25.10.2026 используются две базы `itmap_main`/`itmap_dev` на одном сервере и **одна SQL-учётная запись `itmap_app` с общим паролем**. Пароль принадлежит user@host, а не отдельной базе. Создание баз/учётной записи выполняет администратор через защищённый SQL-сеанс. Пример ниже — **шаблон**, не готовые пароли; замените обозначения приватно и не сохраняйте заполненный SQL в Git/history:

```sql
CREATE DATABASE itmap_main CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin;
CREATE DATABASE itmap_dev CHARACTER SET utf8mb4 COLLATE utf8mb4_nopad_bin;
CREATE USER 'itmap_app'@'127.0.0.1' IDENTIFIED BY '<DB_PASSWORD>';
GRANT SELECT, INSERT, UPDATE, DELETE ON itmap_main.* TO 'itmap_app'@'127.0.0.1';
GRANT SELECT, INSERT, UPDATE, DELETE ON itmap_dev.* TO 'itmap_app'@'127.0.0.1';
```

Учитывайте настройки разрешения имён MariaDB: при необходимости используйте соответствующую **локальную** host-учётную запись и подтвердите `CURRENT_USER()`; не решайте ошибку выдачей прав на `%` или `*.*`. Для первой проверки чтения можно временно выдать только SELECT. Права CREATE/ALTER/DROP/TRIGGER/GRANT приложению не выдаются; миграции и импорт выполняет отдельный администратор. Шаблон не задаёт окончательную production-политику SQL-прав.

Создание DATABASE не создаёт таблицы. Go ожидает baseline72 таблицы/606 столбцов, InnoDB,50 FK и22 триггера. GO-310 закрепляет доверенный DDL v1 и полный metadata contract; GO-311 добавляет отдельную команду **до запуска HTTP** (SQL gate принят на PR #71: fresh/partial schema, actual CLI, source restore, fault/retry):

```bash
# Из каталога распакованного проверенного релиза (-tags mariadb), current ещё не нужен.
# DB_PASSWORD передаётся приватно через окружение административного процесса.
./itmap schema-init --database itmap_dev --user SCHEMA_ADMIN_USER
```

Выполнять с остановленными main/dev-процессами, затрагивающими эту базу, от отдельной согласованной SQL-учётной записи администратора схемы. Ей нужны права на проверку/создание структуры и INSERT начального поколения; права runtime `itmap_app` этой командой не расширяются. Для restore отдельно применяются требования counter preflight из [BACKUP_FORMAT.md](../BACKUP_FORMAT.md). Не подставляйте административный пароль в runtime env или аргументы команды. Действующий запрет APP_ENV=production сохранён; для разрешённого изолированного контура используется staging.

Команда создаёт только missing trusted objects в новой или нетронутой частично созданной базе. Неизвестная/изменённая структура, существующие строки или ранее выделенные ID в partial schema вызывают отказ до DDL. Полная готовая база с поколением проверяется без изменения данных/счётчиков/поколения. DDL-шаги коммитятся MariaDB отдельно: после прерывания повторная команда заново сверяет структуру и продолжает только безопасный остаток; общего rollback DDL нет. После полной проверки структуры создаётся начальное поколение, затем запускают HTTP и отдельно восстанавливают файл через admin API.

Это не migration framework и не разрешение автоматически изменять реальные базы. `db/testdata/empty-schema.sql`, `smoke-catalog.sql`, `read-models.sql` остаются одноразовыми CI-фикстурами; в production их не импортируют. Реальные schema/data/deploy операции агентом не выполнялись. Статус и ограничения — [MARIADB.md](../../dev/MARIADB.md), [BACKUP_FORMAT.md](../BACKUP_FORMAT.md).

## 3. Конфигурация main и dev

Из корневого `.env.example` создайте `/etc/itmap/main.env` и `/etc/itmap/dev.env`, root:root, mode 0600. Не включайте их в каталог релизов. Systemd читает файлы от имени менеджера и передаёт значения процессам; runtime-пользователю читать файл не требуется.

| Переменная | main-контур | dev-контур |
| --- | --- | --- |
| APP_ENV | staging до приёмки | staging |
| HTTP_ADDR | 127.0.0.1:18080 | 127.0.0.1:18081 |
| DB_ENABLED / AUTH_ENABLED | true / true | true / true |
| DB_HOST / DB_PORT | 127.0.0.1 / 3306 | 127.0.0.1 / 3306 |
| DB_NAME | itmap_main | itmap_dev |
| DB_USER | itmap_app | itmap_app (тот же пользователь) |
| DB_PASSWORD | общий секрет БД | то же значение |
| SESSION_SECRET / SITE_PASSWORD | свои секреты main | другие секреты dev |
| NASTYA_PASSWORD / DEMO_PASSWORD | заполнить при необходимости | тестовые отдельные пароли при необходимости |
| WEB_DIST_DIR | /opt/itmap/main/current/web | /opt/itmap/dev/current/web |

DB_USER/DB_PASSWORD общие по принятому решению. SESSION_SECRET и пароли входа приложения задавайте отдельно для каждого контура. Cookie Go имеет Secure/HttpOnly/SameSite=Strict и не задаёт Domain: домены получают независимые cookie. Поддомены одного сайта всё равно не являются полноценной защитной границей от недоверенного кода: dev-контур допускает только доверенные ветки/авторов, пользователи ОС раздельны; SQL-учётная запись имеет права на обе базы. Это разделение данных по DB_NAME, а не изоляция SQL-привилегий. Для полностью недоверенных экспериментов нужен отдельный сервер/контур, а не общий production-хост.

Один `.env.example` не означает одну БД для всего сервера: он описывает один Go-процесс. Копируйте его дважды; `systemd` выбирает `/etc/itmap/%i.env` по имени экземпляра службы. DB_HOST/DB_PORT/DB_USER/DB_PASSWORD в файлах совпадают, **DB_NAME обязательно различается**. На сервере DB_ENABLED и AUTH_ENABLED должны быть true в обоих файлах; false в шаблоне — безопасный локальный default. В GitHub Environments находятся SSH-параметры доставки, а настройки БД остаются на сервере. Не добавлять DB_MAIN_NAME/DB_DEV_NAME: текущий Go такие переменные не читает.

## 4. Артефакт и systemd

Релиз — согласованная пара `itmap` + каталог `web/` из **одного SHA**. Соберите по [LOCAL.md](LOCAL.md), создайте на сборочной машине staging-каталог вне checkout и поместите в него бинарник и содержимое dist/web. Запишите SHA и SHA-256 артефакта. Для автоматики эту работу выполняет Actions после CI.

Пример установки уже полученного проверенного релиза администратором (здесь `release_sha` — полный 40-символьный SHA; `release_dir` содержит только бинарник и web):

```bash
release_sha=<ПРОВЕРЕННЫЙ_ПОЛНЫЙ_SHA>
release_dir=<АБСОЛЮТНЫЙ_ПУТЬ_К_ПРОВЕРЕННОМУ_АРТЕФАКТУ>
sudo install -d -m 0755 "/opt/itmap/dev/releases/$release_sha"
sudo install -m 0755 "$release_dir/itmap" "/opt/itmap/dev/releases/$release_sha/itmap"
sudo cp -a "$release_dir/web" "/opt/itmap/dev/releases/$release_sha/web"
sudo chown -R root:root "/opt/itmap/dev/releases/$release_sha"
sudo chmod -R a+rX "/opt/itmap/dev/releases/$release_sha"
sudo ln -s "releases/$release_sha" /opt/itmap/dev/current.next
sudo mv -Tf /opt/itmap/dev/current.next /opt/itmap/dev/current
```

Это пример **первой** установки dev. Перед повторным обновлением сохраните `readlink /opt/itmap/dev/current`, проверьте отсутствие старого current.next и используйте блокировку всего переключения/проверки/отката. Автоматизация должна выполнять этот алгоритм централизованно, не параллельными SSH-командами.

Установите [deploy/itmap@.service.example](../../../deploy/itmap@.service.example) как `/etc/systemd/system/itmap@.service`, проверьте и запустите **только после** готовности DB/env/release:

```bash
sudo systemd-analyze verify /etc/systemd/system/itmap@.service
sudo systemctl daemon-reload
sudo systemctl enable --now itmap@dev
sudo systemctl status itmap@dev --no-pager
sudo journalctl -u itmap@dev -n 50 --no-pager
curl --fail http://127.0.0.1:18081/health/live
bash scripts/staging-smoke.sh http://127.0.0.1:18081
```

Main устанавливается аналогично в свой каталог/службу/порт, когда отдельно разрешён его запуск. Service-файл не включает миграций, фоновых JS-заданий или записи в БД при pre-start. `systemctl enable` обеспечивает старт после перезагрузки; перезагрузку проверьте отдельно на тестовом сервере. Graceful shutdown Go ждёт до 15 секунд, unit оставляет 20 секунд перед принудительной остановкой.

`/health/live` должен быть 200. `/health/ready` пока обязан быть 503 с `business_api_not_migrated`, когда MariaDB подключена. `mariadb_not_connected`/`mariadb_unavailable` — не успешная проверка staging. Не используйте текущий `/health/ready=200` как условие запуска: такого ответа пока нет. Smoke проверяет текущий ограниченный контракт, а не снимает release gate.

## 5. HTTPS через Nginx

Создайте DNS A/при необходимости AAAA для двух собственных доменов, направленных на сервер. На **новом** сервере установите HTTP-шаблон [nginx-http.conf.example](../../../deploy/nginx-http.conf.example) в `/etc/nginx/sites-available/itmap`, замените оба имени и включите сайт symlink в sites-enabled. Если default-site мешает, отключите только стандартный default на новом сервере, не чужой действующий сайт. Создайте `/var/www/letsencrypt`, проверьте `sudo nginx -t`, затем reload.

Получите один SAN-сертификат для обоих доменов (заменить имена и email):

```bash
sudo certbot certonly --webroot -w /var/www/letsencrypt \
  --cert-name app.example.org \
  -d app.example.org -d app-dev.example.org \
  --email <EMAIL_АДМИНИСТРАТОРА> --agree-tos
```

Замените HTTP-only конфигурацию на [nginx.conf.example](../../../deploy/nginx.conf.example) с реальными именами/путями сертификата. Оба TLS-блока в примере используют этот SAN-сертификат. `sudo nginx -t` должен пройти перед reload. Для продления настройте deploy hook `systemctl reload nginx` в `/etc/letsencrypt/renewal-hooks/deploy/`, проверьте `sudo certbot renew --dry-run` и timer Certbot. Сам сертификат, ключ и env-файлы остаются вне Git.

Proxy должен сохранять исходный `Host`, включая порт при нестандартном внешнем порте: `/auth` сравнивает Origin с Host. Не подменяйте Host на 127.0.0.1 и не включайте общий cache для HTML/API/auth. TLS завершается на Nginx, Go остаётся на HTTP loopback. Шаблон ограничивает тело 2 MiB для текущего ограниченного режима; будущий импорт/restore больших файлов потребует отдельной настройки и реализации API. Не обещайте его работу сейчас.

## 6. Приёмка сервера

Проверьте оба домена: HTTPS/цепочку сертификата, правильный контур, вход/неверный пароль/выход трёх нужных аккаунтов, cookie, глубокую ссылку, компании/вакансии/задачи и изоляцию данных. Не публикуйте HAR, cookies, скриншоты или ответы с личными данными. `bash scripts/staging-smoke.sh https://app-dev.example.org` выполняет read-only health-проверку; режим ITMAP_SMOKE_AUTH разрешён только loopback и **синтетическим CI**-учётным данным, не реальным паролям сервера.

Настройте мониторинг процесса, liveness, свободного места, MariaDB, сроков TLS, ошибок 5xx и ротацию журналов. Резервные копии и восстановление проверяются отдельно; Go backup ещё не заменяет штатный защищённый DB backup. Не удаляйте старый релиз, пока не проверены новый запуск и путь отката.
