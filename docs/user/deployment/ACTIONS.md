# Автоматическая доставка только main: этап A

[ADR-028](../../dev/DECISIONS.md): сейчас один временный сервер, только main,
APP_ENV=staging. Workflow `.github/workflows/publish.yml` принимает push main
и ручной dispatch **из main**. Feature/tag/delete не выбирают dev и не получают
серверных секретов. Независимые PR/branch Go и MariaDB workflows продолжают
работать. Старый двухконтурный автоматический routing отменён.

## Подготовка

1. Вручную принять проверенный PR main-only controller. Не включать старую
   версию publish, доставляющую любую non-main ветку. Automatic merge запрещён.
2. Выполнить [SERVER.md](SERVER.md), установить [main receiver](RECEIVER.md).
   Только itmap@main, отдельный runtime, локальные DB/HTTP, env0600 и HTTPS.
3. Создать **только** GitHub Environment `itmap-main`, deployment branch policy
   только main. Required reviewer — по политике владельца; он может остановить
   автоматическую доставку до ручного утверждения.
4. В Environment установить DEPLOY_HOST, DEPLOY_USER=itmap-deploy-main,
   DEPLOY_PORT=22; secrets DEPLOY_SSH_KEY и DEPLOY_KNOWN_HOSTS. Для приватности
   DEPLOY_HOST можно хранить как secret, он имеет приоритет над variable.
   Использовать отдельный ограниченный deploy-key, не административный root-key.
   Проверенный host key передаётся приватно; StrictHostKeyChecking не отключать.
5. Repository variable ITMAP_GOARCH=arm64 для текущего ARM64-сервера. Сейчас
   цель одна; перед двумя серверами архитектуру выбирать отдельно для target,
   не переносить глобальное значение автоматически на будущий основной сервер.
6. **Проверить маршрут из Actions runner.** Tailscale в Codex не подключает
   GitHub-hosted runner к Tailnet. Текущий workflow сам VPN не поднимает.
   VPN Actions integration/соответствующий runner/публичный SSH-маршрут требуют
   отдельной настройки; успешный SSH из чата не является этим gate.
7. Проверить main branch policy, forced command, sudo, schema/health/SPA,
   свободное место и rollback. Только затем установить repository variable
   ITMAP_DEPLOY_ENABLED=true. До этого не запускать реальную автоматику.

DB_PASSWORD/SESSION_SECRET/SITE_PASSWORD остаются только на сервере, не в Actions.
GitHub variables/secrets требуют Settings→Environments/Secrets and variables→Actions.
Если API отвечает403, записать блокировку; не утверждать, что значения сохранены,
не просить владельца присылать ключи в чат. Защищённые настройки заполняет владелец
или интеграция с соответствующими разрешениями.

## Доставка и проверки

Actions → Publish native main → Run workflow → branch main. Sequence — run_number
того же publish workflow; rerun сохраняет прежний номер и не делает запрос новым.
Не переименовывать файл workflow, не сбрасывать журнал receiver/counters.

Context проверяет репозиторий/ref/SHA/sequence до регистрации. Register отправляет
кандидат через ограниченный SSH, checks и mariadb проверяют один SHA. Package
зависит от register+checks+mariadb, собирает статический Go с mariadb tag и web
одного SHA, manifest/digests и immutable artifact. Publish сверяет актуальный
main head, загружает архив и ждёт terminal status receiver, не только успех SSH.

Ожидаемый серверный gate: live200, ready503 **business_api_not_migrated**, SPA200.
DB unavailable, failed_rolled_back, rollback_failed, superseded и timeout не успех.
Фактические endpoint HTTPS/auth/browser проверяются отдельно. Откат кода не
откатывает БД и не импортирует dump. CI fixtures не данные рабочего стенда.

После спринта1.1 — отдельная подготовка основного сервера/согласованный перенос
main, смена его Environment target, независимая БД и отдельный точный ref1.2 для
временного сервера. Никакого выбора «последней любой non-main ветки».

## Подтверждения и ограничения

GO-345: unit/protocol suite27 и actionlint проверяют main-only context,
feature/manual/tag/delete refusal, package/client/receiver regression. Реальный
main forced-command status и wrong-slot refusal проверены при подготовке сервера.
Это не выполненный Actions→server gate, не live failed-release rollback и не
разрешение на production. Точные SHA/CI/PR — [STACK.md](../../../tasks/STACK.md).
