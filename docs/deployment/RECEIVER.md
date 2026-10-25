# GO-056: серверный приёмник native-релиза

`deploy/receive_release.py` — root-owned программа Python 3.10+ для Ubuntu/systemd. Не HTTP-сервис. Отдельный SSH-ключ каждого контура имеет только forced command в этот приёмник. Бинарник исполняется существующей службой `itmap@main`/`itmap@dev` от разных непривилегированных runtime-пользователей; приёмник не читает DB_PASSWORD/SESSION_SECRET, не выполняет SQL и не меняет Nginx/DNS.

## Установка администратором на подготовленном сервере

Сначала выполнить подготовку из SERVER.md: MariaDB/schema, runtime-пользователи, `/etc/itmap/{main,dev}.env`, systemd template и HTTPS. Не запускать эти команды на чужой/production инфраструктуре без разрешения. Бинарник/SPA будет доставлять последующий workflow GO-057. На **новом** сервере:

```bash
sudo install -d -o root -g root -m 0755 /usr/local/libexec /opt/itmap/main/releases /opt/itmap/dev/releases
sudo install -d -o root -g root -m 0700 /var/lib/itmap-deploy/main /var/lib/itmap-deploy/dev
sudo install -o root -g root -m 0755 deploy/receive_release.py /usr/local/libexec/itmap-receive.py
```

Промежуточные `/opt/itmap`, `/opt/itmap/{main,dev}`, `/var/lib/itmap-deploy` также должны принадлежать root и быть недоступными для записи группе/остальным. Для runtime `/opt/itmap` и каталоги main/dev имеют mode 0755. Приёмник отклоняет symlink/writable/non-root владельцев в цепочке этих путей. Runtime-пользователи не должны иметь доступ к root-only журналу или запись в релизы/env. Файлы env остаются root:root 0600.

Создать **два** отдельных SSH-пользователя `itmap-deploy-main` и `itmap-deploy-dev` с заблокированными паролями. SSH требует работающий shell (например /bin/sh), но каждая разрешённая запись ключа должна быть ограничена. Для dev строка authorized_keys:

```text
restrict,command="sudo -n /usr/bin/python3 -I /usr/local/libexec/itmap-receive.py --slot dev" ssh-ed25519 <DEV_PUBLIC_KEY>
```

В отдельном `/etc/sudoers.d/itmap-deploy-dev`, проверенном `visudo -cf`, разрешить **точно**:

```text
itmap-deploy-dev ALL=(root) NOPASSWD: /usr/bin/python3 -I /usr/local/libexec/itmap-receive.py --slot dev
```

Для main отдельные пользователь/ключ/строки с main. Не использовать wildcard аргументов, общий root SSH, sudo sh или произвольный systemctl. Forced command игнорирует присланную SSH-команду, протокол поступает через stdin. Ключ dev не позволяет передать `--slot main` или `--apply`. Home/.ssh/authorized_keys не должны быть доступны runtime-пользователям; не выдавайте дополнительные интерактивные ключи этим deploy-аккаунтам. `-I` исключает подмену Python-модулей через PYTHONPATH/current directory. `restrict` запрещает PTY/forwarding/agent/X11. Key host SSH надо проверить независимо.

## Протокол

Одна UTF-8 JSON-строка до 8 KiB. Для register/upload/invalidate обязательны:

```json
{"operation":"register","repository":"meInteractive/it-map","workflow":"publish-v1","ref":"refs/heads/codex/example","sha":"1111111111111111111111111111111111111111","sequence":1,"run_id":1}
```

Это только синтетический пример, **не** команда начальной публикации. `sequence` — run_number одного контроллера; `run_id` — его GitHub run ID. Пространство publish-v1 постоянно. Переименование/потеря счётчика или журнала требуют осознанного восстановления оператором, а не сброса защиты автоматически. main принимает только refs/heads/main, dev — остальные ref. Старые/конфликтующие запросы отвергаются. Удаление ветки записывает tombstone и не оживляет предыдущего кандидата.

Upload передаёт ту же identity плюс size/digest SHA-256; после строки идут ровно size байт **несжатого tar**, затем EOF. Ограничения: 64 MiB tar, 128 MiB содержимого, 2000 записей, 32 MiB отдельного файла, 1 MiB manifest. Только `itmap`, `web/**`, `manifest.json`, regular files/directories; запрещены абсолютные/родительские пути, duplicate, symlink/hardlink/спецфайлы. Manifest содержит identity и SHA-256 каждого файла кроме себя. Бинарник должен быть ELF64 little-endian под amd64/arm64 текущего сервера. Отдельные package/client готовятся GO-057.

Ответы — JSON журнала без env/cookies/содержимого файлов. `{"operation":"status"}` читает результат и возобновляет queued/interrupted worker при необходимости. SSH upload подтверждает **постановку**, а не успешный deploy: клиент обязан опрашивать status до `success` для своей identity. `superseded`, ошибки очереди, rollback/отказ rollback — не успех.

## Переключение и восстановление

Артефакт поступает в root-only `/var/lib/itmap-deploy/<slot>`. `systemd-run` запускает отдельный transient unit `itmap-release-<slot>-<sequence>.service`; он переживает закрытие SSH. Worker блокирует контур через flock, проверяет кандидата/digest/manifest/ELF, распаковывает в новый каталог и атомарно переключает current. `previous` сохраняется/fsync до переключения; после перезапуска прерванный apply не примет недопроверенную новую версию за предыдущую.

Перезапуск только `itmap@<slot>.service`, ожидание liveness 200 и readiness **503 с business_api_not_migrated**. Ошибки подключения MariaDB не считаются успешным staging. Внешний HTTPS/логин и полноценный SQL parity проверяются отдельно. Тест readiness не снимает production gate.

При ошибке восстанавливается previous current и перезапускается служба; при первой неудачной установке процесс останавливается. `failed_rolled_back` остаётся failed; `rollback_failed` требует оператора. Автоматического отката БД, удаления релизов или повторного импорта нет. Повтор успешной доставки immutable archive не делает второй restart.

Состояние хранится атомарно в state.json, архивы и релизы сохраняются для диагностики/ручной политики retention. Не удалять журнал/sequence/текущий или previous релиз ради освобождения места; следить за диском. Transient worker ограничен 300 секундами; аварийное завершение оставляет восстанавливаемое applying, следующий status/повтор возобновляет работу. При отказе диска/повреждённом журнале публикация блокируется, state не сбрасывается.

## Проверки и границы

`python3 -m unittest discover -s deploy/tests -v`: временные каталоги, два физических процесса регистрации, реальные loopback HTTP-ответы health, stale/rerun/red candidate/deletion, tar negatives, нормальный/неуспешный rollback и recovery после переключения/до старта worker. Systemd-команды в этих тестах заменены проверяемым synthetic service adapter: это **не** live SSH/systemd/production acceptance. Mandatory CI выполняет suite. Реальный server/SSH/перезагрузку/TLS нужно проверить после подключения разрешённого сервера.
