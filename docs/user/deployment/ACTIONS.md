# GO-057: включить автоматическую публикацию текущего Go

**Действующая схема ADR-028:** сейчас только main/staging на временном сервере; после спринта1.1 main и конкретная ветка1.2 работают на разных серверах с независимыми SQL-users/паролями/БД/env/deploy-доступами. Двухконтурные команды и общая SQL-учётная запись ниже сохранены как история прежней схемы, не выполнять их для текущего этапа. Актуальная установка — [SERVER.md](SERVER.md); автоматическую доставку не включать до main-only routing и проверки сети runner.

Реализация: `.github/workflows/publish.yml`, `deploy/publish_release.py` и [приёмник](RECEIVER.md). Основной автоматический вариант — **native без Docker**, Ubuntu 24.04/systemd. Dockerfile и ручной контейнерный вариант сохранены отдельно; этот workflow не управляет Docker.

## Перед включением

1. Объединить проверенные PR автоматики в main вручную, затем перенести контроллер в существующие **рабочие Go-ветки**, которые должны публиковаться. Новые ветки создавать от актуальной вершины: они наследуют workflow. Наличие publish.yml только в main не запускает workflow при push в старую ветку без этого файла. Исторические gpt-site/master с Worker не публикуются как Go; менять их ради деплоя не надо. Для непрерывного охвата вообще всех произвольных ref без workflow нужен отдельный repository webhook controller, этот вариант не реализован.
2. На разрешённом сервере выполнить SERVER.md и RECEIVER.md: отдельные runtime/SSH пользователи, root-owned приёмник, точные sudo/forced commands, env/schema/DB, TLS. Оба процесса пока `APP_ENV=staging`: имя main-домена не включает production. Фактическое подключение сервера/данных этой задачей не выполняется.
3. Установить unit template, выполнить `systemctl daemon-reload`, **enable без --now** для itmap@main/itmap@dev. Первая доставка создаст current и выполнит restart; запускать службу с отсутствующим current заранее не требуется. Убедиться, что env заполнены и MariaDB/schema готовы; иначе первая публикация будет отклонена health gate.
4. Создать GitHub Environments `itmap-main` и `itmap-dev`. main разрешает только branch main. dev разрешает доверенные рабочие ветки **и main**: delete-событие обрабатывается workflow из default main, но направляется в dev-приёмник по удалённому ref. Для полностью автоматического режима не ставить required reviewer перед каждой регистрацией; если он нужен по политике, регистрация ожидает ручного решения и режим уже не полностью автоматический.
5. В **каждом Environment** задать variables `DEPLOY_HOST`, `DEPLOY_USER`, необязательный `DEPLOY_PORT` (по умолчанию 22); secrets `DEPLOY_SSH_KEY` и `DEPLOY_KNOWN_HOSTS`. Разные deploy-user/key для main/dev. Файл known_hosts получить после независимой проверки fingerprint администратором, не доверять первому ssh-keyscan. Полные строки ключей не помещать в .env.example/Git. Чтобы адрес не отображался в публичных Actions logs, задайте DEPLOY_HOST как Environment secret: он имеет приоритет над одноимённой variable. Реальные домены в публичных файлах не нужны.
6. Repository variable `ITMAP_GOARCH`: `amd64` по умолчанию, либо `arm64` для сервера ARM64. Оба контура предполагаются на одном Linux-сервере одной архитектуры. Receiver проверит ELF против своей машины и не запустит другой platform.
7. Проверить SSH/firewall/место на диске и настройки доступа авторов веток. Затем установить **repository variable `ITMAP_DEPLOY_ENABLED=true`**. До этого publish workflow не подключается к серверу. Переменную не добавлять в Go `.env`: это управление Actions.

Здесь Environment — настройка GitHub, а `/etc/itmap/main.env`/dev.env — runtime конфигурация сервера. Пароли БД, SESSION_SECRET и аккаунтов Actions **не нужны**: они остаются root-only на сервере. CI использует только прежнюю disposable MariaDB и синтетические auth-значения.

## Первый запуск

После подготовки откройте Actions → **Publish native main and latest dev** → Run workflow. Выберите main или подготовленную Go-ветку. Это явный начальный выбор текущего состояния; не нужно изобретать историю push по датам коммитов. Дальше обычный push в подготовленную ветку автоматически регистрирует новый кандидат. Main и dev имеют разные службы/порты/DB_NAME/SESSION_SECRET/журналы; DB_USER/DB_PASSWORD общие по решению пользователя (ADR-019).

Manual Run workflow — новый запрос со своим run_number. **Re-run jobs** сохраняет старый run_number и не делает ветку последней заново. Не переименовывать/пересоздавать publish.yml или сбрасывать журнал незаметно: последовательность принадлежит одному контроллеру publish-v1. При восстановлении счётчика/сервера требуется операторская сверка текущего номера и журнала.

## Что выполняет workflow

- context валидирует собственный repo, branch ref, SHA, run_number/run_id. Для delete берёт payload ref, а не default github.ref; tag delete игнорируется.
- register сразу отправляет кандидат по ограниченному SSH. Checks не ждут доступности SSH: оба набора CI выполняются независимо, без серверных секретов. Ошибка регистрации всё равно блокирует package/publish.
- checks вызывает существующий public Go/web/source/history CI; mariadb вызывает полный schema → storage → fixtures → business/API/browser gate. Оба получили workflow_call; существующие PR-триггеры сохранены.
- package зависит от **успешных register + checks + mariadb**, собирает CGO_ENABLED=0/trimpath/mariadb бинарник и Vite SPA из точного github.sha. Packer проверяет HEAD/отсутствие tracked изменений, запрещает symlink/неполную сборку и создаёт детерминированный tar + manifest с SHA-256 каждого файла.
- publish в отдельном job/Environment скачивает immutable Actions artifact, дополнительно сверяет актуальный GitHub head ветки, доставляет в receiver и опрашивает его результат. Успех SSH upload ещё не означает успех деплоя. Только success своей identity/digest завершает job успешно; ошибки/rollback/superseded/timeout остаются ошибками.
- invalidate записывает удалённую ветку/tombstone, не останавливает работающий релиз и не отменяет чужого кандидата. Никакого автоматического выбора старой «зелёной» ветки.

Все gates относятся к одному SHA. Не используются continue-on-error, untrusted pull_request_target, SSH host-key bypass или передача паролей приложению в артефакте. Ключи присутствуют только в register/publish/invalidate и записываются клиентом во временные файлы 0600; stderr SSH не печатается. GH_TOKEN выдаётся самим GitHub с contents:read для сверки ref, personal access token не требуется.

Порядок кандидатов определяется последовательностью созданных push/dispatch запусков одного workflow; поздняя доставка старого run_number не перезапишет новый. Это не время автора коммита и не порядок завершения CI. Первая задержка планировщика Actions/регистрации возможна: журнал знает зарегистрированные запросы, не ещё не доставленные события. Новый неуспешный кандидат сохраняет уже работающий релиз; старый медленный job не может перезаписать более новый успешно принятый.

## Проверить результат и ошибки

В Actions должны успешно завершиться checks, mariadb, package, publish. Приёмник пишет root-only state.json и transient unit journal. Администратор может проверить/возобновить ранее поставленную операцию:

```bash
printf '{"operation":"status"}\n' | sudo /usr/bin/python3 -I /usr/local/libexec/itmap-receive.py --slot dev
sudo systemctl status itmap@dev --no-pager
sudo journalctl -u itmap@dev -n 50 --no-pager
```

Status может возобновить queued/interrupted worker. Не публиковать stdout приложения/заполненный env/SSH key или cookie в чат/PR. Проверить HTTPS/вход на своём домене отдельно: локальный worker проверяет Go/DB readiness и доступность SPA, не ACME/DNS внешнего клиента.

- `superseded`: другой push выбран позднее; не перезапускать старую ветку новым dispatch без намерения снова выбрать её.
- `branch_head_changed`: SHA ref изменился/force-push; доставлять новый проверенный кандидат.
- `ssh_request_failed`: проверить адрес/порт/user, forced command/sudo, host fingerprint/ключ и доступ сети. Не отключать StrictHostKeyChecking.
- `failed_rolled_back`: плохой релиз не остался активным; job правильно красный. `rollback_failed`: требуется администратор и проверка службы/БД/диска.
- `deployment_timeout_check_server_status`: исход не считать успешным; проверить durable state. Отмена runner/SSH не убивает detached systemd worker.
- `immutable_release_conflict`: повтор того же запроса доставляет другой tar (например, после изменения builder). Не заменять его молча. Проверить исходный artifact или после разбирательства сделать новый Run workflow с новым номером.
- Artifact хранится в Actions 7 дней. Re-run failed job использует имя artifact из outputs исходного package job; при истёкшем artifact нужен новый осознанный запуск, а не откат на случайный SHA.

## Что проверено и что остаётся

Unit/protocol tests включают реальные временные каталоги, межпроцессную блокировку, loopback HTTP, event/rerun/delete routing, строгие SSH options, детерминированную упаковку и сквозной client→receiver wire protocol с тестовым SSH transport. В mandatory MariaDB CI добавлен **native_smoke.py**: реальный собранный Go+SPA, SELECT-only одноразовая MariaDB, login/session и плохой ELF-релиз с реальным отказом exec → восстановление предыдущего Go процесса. Скрипт вне соответствующего CI target отказывает, локальную DB не создаёт.

Нет подключения реального сервера, проверки OpenSSH forced command/sudo/transient systemd на нём, reboot/DNS/ACME/HTTPS. После установки выполнить это на разрешённом тестовом сервере; GO-007 не закрывается целиком по unit/synthetic CI. Автоматического merge, production cutover, DDL/импорта данных, изменений GPT Sites/D1 нет. После готовности автоматики вернуться к критической бизнес-части GO-031.


Подтверждение кода `1c2e0b7`, [PR #14](https://github.com/meInteractive/it-map/pull/14): [Go/web/security/25 unit+protocol/actionlint](https://github.com/meInteractive/it-map/actions/runs/40193567154) и [MariaDB/API/14 browser/native publish+failed-exec rollback](https://github.com/meInteractive/it-map/actions/runs/40193567153) прошли. Отдельный Publish workflow был **skipped по выключенному ITMAP_DEPLOY_ENABLED**, не выполнял реальный SSH/deploy и не выдаётся за live-приёмку. Обязательные проверки и native smoke выполнены в независимом CI без пропусков.
