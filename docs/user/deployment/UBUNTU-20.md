# Ubuntu 20.04 LTS: отличия установки

Под «Ubuntu 20» здесь понимается **20.04 LTS (Focal)**. Это дополнение к [SERVER.md](SERVER.md), а не подтверждение запуска на реальном сервере. Каталоги main/dev, два порта, одна SQL-учётная запись, env, Nginx и доставка готового релиза остаются прежними. Версию и архитектуру сначала проверьте через `cat /etc/os-release` и `uname -m`.

## Сводка отличий

| Компонент | Ubuntu 24.04 | Ubuntu 20.04: что изменить |
| --- | --- | --- |
| Поддержка ОС | Стандартная LTS | Стандартная поддержка закончилась в мае 2025; проверить Ubuntu Pro/ESM и покрытие пакетов |
| MariaDB из штатного APT | 10.11 | Обычно 10.3; минимум приложения — 10.3.32, проверяйте установленный patch |
| systemd | 255 | 245; убрать только `ProtectProc=invisible` из устанавливаемой копии unit |
| Python по `/usr/bin/python3` | 3.12 | 3.8; приёмнику нужен отдельный поддерживаемый Python, например 3.12 |
| Nginx / OpenSSL | 1.24 / 3.x | Обычно 1.18 / 1.1.1; используемые директивы и TLS 1.2/1.3 доступны, нужны security updates |
| Go / Node / npm / Git | Сборка в CI | На runtime не устанавливать SDK: доставлять `CGO_ENABLED=0` бинарник и `web/` из одного SHA |

Версии — базовые для дистрибутивов, а не гарантия конкретного установленного patch. Сверьте `systemd --version`, `nginx -v`, `python3 --version`, `openssl version`, `apt-cache policy mariadb-server` и версию **самого сервера** MariaDB.

## Пакеты и MariaDB

Для нового изолированного Focal базовый набор, **без установки MariaDB из старого APT**:

```bash
sudo apt-get update
sudo apt-get install --yes ca-certificates curl openssl python3 nginx certbot
```

Штатный Python используется системными инструментами; не заменяйте `/usr/bin/python3`, его symlink или `update-alternatives` ради приложения. Go и SPA собираются существующим GitHub Actions на Ubuntu 24.04. Статическая Go-сборка `CGO_ENABLED=0` исключает зависимость бинарника от glibc нового runner; архитектура ELF должна совпадать с сервером. Linux/amd64 или arm64 и ядро дополнительно проверяются при приёмке.

**Уточнённая версия сервера — 10.3.32-MariaDB.** Минимум приложения и отдельной команды `itmap schema-init` — MariaDB 10.3.32. Совместимые SQL и проверка метаданных учитывают эту версию; полный синтетический CI запускается на точной 10.3.32 и на 10.11. Результаты и пределы проверки — [отчёт о совместимости](../../dev/MARIADB_103_COMPATIBILITY.md).

Функциональная совместимость не продлевает поддержку сервера: [сопровождение ветки MariaDB 10.3 завершилось 25 мая 2023](https://mariadb.org/about/). Для существующей установки отдельно проверьте источник security updates пакета, включая покрытие поставщика ОС. Для новой установки предпочтительна поддерживаемая ветка MariaDB. По [политике MariaDB](https://mariadb.com/docs/release-notes/community-server/about/platform-deprecation-policy) выпуск Community-пакетов для Ubuntu 20.04 прекращён в мае 2025; наличие архивного пакета не означает получение исправлений. Не подменяйте `focal` на `jammy`/`noble` в APT. Обновление действующей БД требует отдельного backup/restore-плана и по этой инструкции не выполняется.

Для подготовленной MariaDB продолжить разделы 2–3 [SERVER.md](SERVER.md): loopback, две базы, права и schema preflight. Совместимая версия сервера не создаёт схему it-map. Для новой пустой базы предусмотрена отдельная `itmap schema-init` перед HTTP startup; права отдельного schema-admin описаны в SERVER.md. Автоматической миграции при запуске нет. На 10.3 направление `DESC` в индексе физически игнорируется сервером; явный `ORDER BY` сохраняет результат, план и скорость запросов могут отличаться.

## Systemd 245

Исходный `deploy/itmap@.service.example` содержит `ProtectProc=invisible`, добавленный в [systemd 247](https://www.freedesktop.org/software/systemd/man/247/systemd.exec.html#ProtectProc=). Для штатного systemd 245 установите адаптированную копию из checkout проверенного SHA:

```bash
sed '/^ProtectProc=/d' deploy/itmap@.service.example \
  | sudo tee /etc/systemd/system/itmap@.service >/dev/null
sudo chown root:root /etc/systemd/system/itmap@.service
sudo chmod 0644 /etc/systemd/system/itmap@.service
sudo systemd-analyze verify /etc/systemd/system/itmap@.service
```

Остальные ограничения (`NoNewPrivileges`, `PrivateTmp`, `ProtectSystem`, `ProtectHome`, `RestrictSUIDSGID`, пустой `CapabilityBoundingSet`) сохраняются. Без ProtectProc изоляция видимости чужих процессов через `/proc` слабее; отдельные runtime-пользователи и закрытые env остаются обязательны. Не отключать все sandbox-настройки ради предупреждения об одной директиве. Вариант с обновлённым systemd/kernel проверяется по фактическим версиям, а не по названию ОС.

Receiver использует `systemd-run --collect`, `Type=exec`, `RuntimeMaxSec`; эти параметры доступны в 245. Проверка parser unit на новой системе не заменяет запуск/reboot/rollback на целевом Focal.

## Python приёмника релизов

Код приёмника требует Python 3.10+, но для новой установки выбирайте поддерживаемую ветку, например **3.12 с актуальным security patch**. Стандартный Python 3.8 Focal не является заявленным runtime приёмника. Python 3.10 уже завершил upstream lifecycle в октябре 2026; см. [список поддерживаемых Python](https://www.python.org/downloads/).

Установите отдельный Python из проверенного источника в root-owned prefix, например `/opt/itmap-python`. Если собираете из официального source release, проверьте подпись/checksum, используйте отдельный prefix и `make altinstall` по [инструкции Python](https://docs.python.org/3.12/using/unix.html#building-python); не запускайте `make install` поверх системного Python. Администратор отвечает за последующие security patches этого отдельного runtime.

После установки и до настройки SSH:

```bash
/opt/itmap-python/bin/python3.12 --version
/opt/itmap-python/bin/python3.12 -I -c 'import ssl, tarfile, fcntl, urllib.request; print(ssl.OPENSSL_VERSION)'
/opt/itmap-python/bin/python3.12 -m unittest discover -s deploy/tests -v
```

Сами тесты создают синтетические временные каталоги/loopback HTTP и не выполняют deployment; выполнять их из проверенного checkout на изолированном стенде. Они не подтверждают TLS/systemd/MariaDB на реальном сервере.

В [RECEIVER.md](RECEIVER.md) заменить интерпретатор **и в forced command, и в sudoers**:

```text
restrict,command="sudo -n /opt/itmap-python/bin/python3.12 -I /usr/local/libexec/itmap-receive.py --slot dev" ssh-ed25519 <DEV_PUBLIC_KEY>
itmap-deploy-dev ALL=(root) NOPASSWD: /opt/itmap-python/bin/python3.12 -I /usr/local/libexec/itmap-receive.py --slot dev
```

Это две строки в разных файлах: первая в authorized_keys, вторая в `/etc/sudoers.d/itmap-deploy-dev`; проверить sudoers через `visudo -cf`. Для main — отдельные пользователь/ключ/slot. Вся цепочка каталогов Python и сам исполняемый файл принадлежат root без права записи deploy/runtime-пользователям. Transient worker сохраняет выбранный интерпретатор через `sys.executable`, а не возвращается к `/usr/bin/python3`.

## Nginx, TLS и приёмка

Используемые в [nginx.conf.example](../../../deploy/nginx.conf.example) директивы доступны в Nginx 1.18; `listen 443 ssl` сохраняется, новые директивы вида `http2 on` не нужны. OpenSSL 1.1.1 поддерживает TLS 1.3. Для пакетов Focal проверьте фактическое ESM/security-покрытие; возраст версии не заменяет проверку backport-исправлений. После установки выполнить `nginx -t`, выдачу сертификата, `certbot renew --dry-run` и проверку renew timer по [SERVER.md](SERVER.md); установку apt-версии и snap-версии Certbot не смешивать.

Текущий Nginx-шаблон ограничивает запрос 2 MiB: это относится и к Focal, и к 24.04. Код restore v39 допускает до 25 MiB, поэтому большие файлы потребуют отдельной рецензируемой настройки proxy; инструкция Ubuntu 20 сама по себе этот лимит не меняет.

Перед объявлением сервера готовым проверить оба контура, MariaDB baseline/FK, подпись cookie, HTTPS, работу приёмника с отдельным Python, запуск после reboot и rollback. В текущей среде живой Ubuntu 20.04, его package installation и systemd/TLS не проверялись. Статус ОС и ESM: [Canonical lifecycle](https://ubuntu.com/about/release-cycle).
