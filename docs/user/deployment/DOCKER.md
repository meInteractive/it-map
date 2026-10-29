# Запуск с Docker на Linux

Основной вариант — [systemd без Docker](SERVER.md). Реализованный publish.yml публикует native-релизы; контейнерный вариант в этом документе пока ручной, без автоматического переключения Docker из Actions. Docker здесь упаковывает Go и React; он не включает отсутствующие функции и не снимает запрет production. Kubernetes и Compose не требуются.

## Ограничение сети текущего Go

Go разрешает только loopback и для HTTP_ADDR, и для DB_HOST. Обычная bridge-сеть с `DB_HOST=mariadb` и `-p 8080:8080` **не подойдёт**: приложение откажется стартовать или порт останется внутри контейнера. Рабочий вариант на Linux — `--network host`, Go слушает `127.0.0.1:18080/18081`, Nginx и MariaDB доступны через host loopback. Параметр `-p` Go-контейнеру не нужен. Host networking уменьшает сетевую изоляцию; доверенные ветки и разные секреты всё равно обязательны. Docker Desktop/Windows/macOS этой инструкцией не проверяются.

## Собрать образ

[deploy/Dockerfile](../../../deploy/Dockerfile) — multistage: Node 24 собирает SPA, Go 1.27.3 собирает статический бинарник с `mariadb`, финальный scratch содержит только бинарник, web и CA bundle; uid/gid 65532. Go/Node/npm в runtime-образ не входят. [.dockerignore](../../../.dockerignore) разрешает только сборочные входы; `.git`, env, dist, node_modules и дампы передавать в context нельзя.

Из корня **проверенного** checkout:

```bash
release_sha=$(git rev-parse HEAD)
docker build -f deploy/Dockerfile -t "itmap:$release_sha" .
```

Образы builder заданы через `NODE_IMAGE`/`GO_IMAGE`; перед реальным выпуском проверьте их наличие и зафиксируйте проверенные digest, например через `--build-arg GO_IMAGE=golang@sha256:<DIGEST>`. В обновлении SDK сохраняйте минимум go.mod и прогоняйте CI. BuildKit может получить публичный CA bundle через `--secret id=proxy_ca,src=/path/to/combined-ca.crt` для среды с TLS proxy. TLS verification не отключайте, приватные env/ключи в build-args не передавайте. Если образ/registry недоступен, сборка должна завершиться ошибкой; не менять SDK на случайную версию.

## Запустить два контейнера

Подготовьте две БД с общей SQL-учётной записью/паролем и HTTPS по [SERVER.md](SERVER.md). Скопируйте `.env.example` в отдельные root-readable `/etc/itmap/main.env` и `/etc/itmap/dev.env`, задайте разные DB_NAME и общий DB_USER/DB_PASSWORD; SESSION_SECRET и пароли входа приложения заполните отдельно. Для Docker установите **`WEB_DIST_DIR=/app/web`**, а HTTP_ADDR — 127.0.0.1:18080 и 127.0.0.1:18081. Формат `--env-file`: `KEY=value`, без shell-кавычек и подстановок. Оба APP_ENV пока staging.

Пример dev после готовности БД:

```bash
docker run -d --name itmap-dev --restart unless-stopped \
  --network host --read-only --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 128 --memory 512m --cpus 1 \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --log-opt max-size=10m --log-opt max-file=3 \
  --env-file /etc/itmap/dev.env "itmap:$release_sha"
curl --fail http://127.0.0.1:18081/health/live
bash scripts/staging-smoke.sh http://127.0.0.1:18081
```

Main запускается отдельным именем/портом/env, после разрешения владельца. Не запускайте systemd Go и Docker Go одновременно на одном порте. Заданные CPU/RAM — стартовые ограничения небольшого тестового процесса, их уточняют измерением, не гарантируют ёмкость. Для внешних HTTPS-вызовов в особой proxy-среде нужен корректный CA bundle в runtime; не отключать проверку сертификата.

Логи: `docker logs --tail 50 itmap-dev`; завершение: `docker stop --time 20 itmap-dev`. Образ scratch не содержит shell/curl, проверку выполняйте с host, а не через docker exec sh. `/health/ready` пока 503 даже при подключённой MariaDB; Docker HEALTHCHECK на 200 readiness делать нельзя. По умолчанию Docker socket/root-доступ позволяют читать env контейнеров: выдавайте их только администраторам, не помещайте docker inspect с env в отчёты.

## Если MariaDB тоже в Docker

Предпочтительно оставить штатную MariaDB на том же сервере. Альтернатива: отдельный официальный `mariadb:10.11` с **постоянным volume**, портом **`127.0.0.1:3306:3306`** и секретом root из защищённого файла через поддерживаемый образом `MARIADB_ROOT_PASSWORD_FILE`. Не копировать пароль в CLI или образ. Go остаётся с host network и DB_HOST=127.0.0.1. Не запускайте такой контейнер, если 3306 уже занят штатной MariaDB.

Один контейнер БД может содержать две базы с общим SQL-пользователем, которому выданы права на обе; одноразовый init образа не является версионированной миграцией. Проверьте права local/TCP host-учётных записей с учётом NAT. Доступ Go не должен становиться root или глобальным `*.*`. Постоянный volume, защищённые backup/restore, обновление MariaDB и восстановление после reboot надо проверить отдельно. Не использовать `docker rm -v`, удаление volume или пересоздание базы как обычное обновление приложения.

## Обновление и откат

После CI Actions передаёт образ с неизменяемым SHA/digest и метаданные на сервер. Сервер сериализует выбор/запуск/проверку/откат одного контура, сохраняет предыдущий image digest и env вне образа. В текущей схеме один loopback-порт: остановка старого и запуск нового дают короткий простой; zero-downtime не заявлен. При ошибке запустите предыдущий образ с тем же env и совместимой БД. Смена image не откатывает SQL-данные. Политика выбора ветки и автоматические gates — [RELEASES.md](RELEASES.md).
