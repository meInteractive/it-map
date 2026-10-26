# Контрольная точка последовательной миграции

Порядок пользователя от 25.10.2026: новые задачи выполняются последовательно по зависимостям, отдельными коммитами, ветками и stacked PR; не ждать приёмки или merge. Автоматический merge и продуктивные операции запрещены.

| Порция | Ветка | База | PR | Проверки / состояние |
| --- | --- | --- | --- | --- |
| GO-005: импорт/CI/фикстуры | codex/GO-005-cloud-onboarding | main | [#1](https://github.com/meInteractive/it-map/pull/1) | Объединён пользователем; проверки прошли |
| GO-005: блокировки | codex/GO-005-mariadb-locking | main | [#2](https://github.com/meInteractive/it-map/pull/2) | Объединён пользователем; проверки прошли |
| GO-006: браузер и вход | codex/GO-006-browser-smoke | main | [#3](https://github.com/meInteractive/it-map/pull/3) | Код 835edde и отчёт 92756a9: все 4 browser сценария и остальные CI прошли |
| GO-047 / GO-007: stacked workflow | codex/GO-007-stacked-workflow | codex/GO-006-browser-smoke | [#4](https://github.com/meInteractive/it-map/pull/4) | e3fcd23: все stacked CI прошли |
| GO-048 / GO-006: PWA cache | codex/GO-048-pwa-cache | codex/GO-007-stacked-workflow | [#5](https://github.com/meInteractive/it-map/pull/5) | 2727287: все CI и 6 browser-сценариев прошли |
| GO-049 / GO-006/008: повтор входа | codex/GO-049-login-retry | codex/GO-048-pwa-cache | [#6](https://github.com/meInteractive/it-map/pull/6) | 64f3caf: все CI и 8 browser-сценариев прошли |
| GO-050 / GO-001: source history | codex/GO-050-source-history | codex/GO-049-login-retry | [#7](https://github.com/meInteractive/it-map/pull/7) | adc6652: все CI, gpt-site=f51aacb, full-history gate/6 Git тестов |
| GO-051 / GO-006/008: read matrix | codex/GO-051-browser-read-matrix | codex/GO-050-source-history | [#8](https://github.com/meInteractive/it-map/pull/8) | 667d459: все CI и 14 browser-сценариев прошли; исправлена высота Vite #root |

## Завершение вчерашних порций и перенос стека (25.10.2026)

GO-050 и GO-051 проверены: PR #7/#8 объединены пользователем, финальный `cb7c7c1` прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40141225546) и [MariaDB/API/14 browser-сценариев](https://github.com/meInteractive/it-map/actions/runs/40141225541). Остатков реализации этих узких задач нет. Полные GO-005/GO-007 по-прежнему заблокированы отсутствием отдельно подготовленного staging/согласованного снимка; без них нельзя объявлять полную приёмку.

При проверке обнаружено: PR #3 был объединён в main раньше дочерних PR #4–8. Дочерние merge обновили уже закрытые родительские ветки, но не main (`3a01d40`). GO-052 сохраняет проверенную вершину `cb7c7c1`, присоединяет актуальный main без удаления кода и готовит отдельный PR в main. Никакого автоматического слияния. После объединения корневого PR оставшиеся открытые дочерние PR нужно перебазировать/перенаправить на новую актуальную базу до их merge; GitHub не переносит изменения из закрытой родительской ветки в main автоматически.

| Новая порция | Ветка | База | PR | Состояние |
| --- | --- | --- | --- | --- |
| GO-052: перенос закрытого стека | codex/GO-052-stack-checkpoint | main | [#9](https://github.com/meInteractive/it-map/pull/9) | CI прошли; пользователь объединил в main как 0974a97 (25.10.2026) |
| GO-053: запуск/свой сервер | codex/GO-053-launch-deploy-docs | main после merge PR #9 | [#10](https://github.com/meInteractive/it-map/pull/10) | 6527b69: все CI прошли; Docker/env/Nginx/systemd проверены без подключения сервера |

## Блокировки и границы

- Полная GO-005: пользователь 25.10.2026 подтвердил отсутствие подготовленного staging/согласованного снимка и поручил продолжать независимые задачи. Нет отдельно разрешённого согласованного снимка/тестовой БД. Синтетическая MariaDB в CI проверяет только код и fixture-контракт; реальную БД не подключать.
- Полная GO-007: внешний staging-домен, HTTPS/proxy и безопасная конфигурация ещё не предоставлены/не разрешены. Никакого реального deployment или DNS.
- Открытые ADR по миграционному инструменту, синхронизации D1, секретам и cutover не принимать самостоятельно.
- Не брать задачи, которым требуется заблокированная операция. Доступны независимые проверки GO-006/PWA и уже реализованной авторизации без внешних интеграций; они не закрывают родительские эпики целиком.
- После каждой порции здесь фиксировать фактический PR/base/SHA/результаты. При отказе API явно писать «PR не создан», сохраняя коммиты локально.


## Приоритеты пользователя от 25.10.2026

GO-054 обновляет статусы по доказательствам и разделяет каждую исходную задачу на критическую/среднюю/низкую части. Предыдущий порядок «ждать приёмки всего A» заменён приоритетом доступной бизнес-логики; staging/снимок остаются отдельными блокировками. Первой критической задачей стала завершённая GO-055: legacy JS → Go canonical create/intent hash, без SQL/провайдеров/вёрстки; её результат и следующий SQL-шаг отражены ниже. Native Actions→server код реализован последующими GO-056/057; внешний сервер пока не подключён.


| Продолжение | Ветка | База | PR | Проверки |
| --- | --- | --- | --- | --- |
| GO-054: статусы и 3 приоритета | codex/GO-054-sprint-priorities | codex/GO-053-launch-deploy-docs | [#11](https://github.com/meInteractive/it-map/pull/11) | edbc353: все обязательные CI успешны |
| GO-055: create contract | codex/GO-055-creation-contract | codex/GO-054-sprint-priorities | [#12](https://github.com/meInteractive/it-map/pull/12) | a9d735c: 43 JS→Go примера; оба обязательных CI и 14 browser успешны |


После пользовательского merge PR #9 база открытого PR #10 перенаправлена на main через GitHub API; PR #11 остаётся в ветку #10, #12 — в ветку #11. Код из вчерашних GO-050/051 теперь действительно достижим из main 0974a97. Автоматического merge агент не выполнял. Следующий критический результат — live receipt write/retry/conflict/rollback части GO-031, только в disposable CI с проверкой target и поколения; это ещё не выполненная задача.


## Автоматика публикации перед следующими бизнес-подзадачами

Пользователь поставил завершение кода GO-007 перед следующими критическими SQL-задачами. Предыдущие PR #10–12 объединены пользователем в main 59dd75b; GO-056 присоединяет этот main к проверенной вершине без переписывания истории.

| Задача | Ветка | База | PR | Проверки |
| --- | --- | --- | --- | --- |
| GO-056: receiver/recovery | codex/GO-056-release-receiver | main | [#13](https://github.com/meInteractive/it-map/pull/13) | 40f2be5: 16 изолированных тестов и все mandatory CI прошли |
| GO-057: Actions/native package/SSH | codex/GO-057-actions-publish | codex/GO-056-release-receiver | [#14](https://github.com/meInteractive/it-map/pull/14) | 1c2e0b7: [Go/web/security/actionlint/25 tests](https://github.com/meInteractive/it-map/actions/runs/40193567154) и [MariaDB/API/browser/native rollback](https://github.com/meInteractive/it-map/actions/runs/40193567153) успешны |

Внешний сервер/SSH-ключи/GitHub Environments пока не подключены; repo flag ITMAP_DEPLOY_ENABLED не включался. Это блокирует реальную инфраструктурную приёмку GO-007, но не написание/проверку автоматики. После неё — критическая receipt write часть GO-031. Для действующих старых Go-веток требуется перенести publish.yml перед автоматическим push; архивный Worker не деплоится как Go.


Уточнение конфигурации пользователя: две базы на одном MariaDB-сервере, общий DB_USER/DB_PASSWORD; два server env с разными DB_NAME. Выбор закреплён ADR-019 и в GO-057 документации. Это не меняет код workflow/Go и не включает реальный deploy. SESSION_SECRET/SSH/runtime учётные записи контуров раздельны.


## История продолжения после объединения PR #14 пользователем

PR #14 объединён пользователем в открытую родительскую ветку PR #13 (27d4da1); main пока 59dd75b. Последующее уточнение общей SQL-учётной записи 52e20b9 сохраняется отдельной GO-059/новым PR в открытую ветку #13. Тесты GO-058 (PR #15) направляются поверх этой новой doc-ветки, чтобы изменения не уходили в уже закрытый PR #14. Автоматического merge нет; код/root-server не меняются этим doc-этапом.


| Актуальное продолжение | Ветка | База | PR | Проверки |
| --- | --- | --- | --- | --- |
| GO-059: общий SQL profile/two env | codex/GO-059-shared-db-config | codex/GO-056-release-receiver | [#16](https://github.com/meInteractive/it-map/pull/16) | e4f7ce9: [public CI](https://github.com/meInteractive/it-map/actions/runs/40197308011) и [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40197308055) успешны; docs-only |
| GO-058: durable receipts | codex/GO-058-receipt-roundtrip | codex/GO-059-shared-db-config | [#15](https://github.com/meInteractive/it-map/pull/15) | ab38269: [public CI](https://github.com/meInteractive/it-map/actions/runs/40196634843) и [live receipt gate/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40196634823) успешны, ноль skips |

Критический GO-058 завершён в узком sequential create/retry/error/delete/limit контракте. Live SQL выполнялся только на disposable CI; API/схема/реальный сервер не менялись. Конкурентность/mid-write fault/crash/CAS остаются отдельной незавершённой частью GO-031. На этой контрольной точке PR #13 ещё был открыт и содержал объединённый пользователем #14; следующий порядок review был #13 → #16 → #15. Ни один PR агент не объединял.


Финальный повтор GO-058 после присоединения актуального parent registry/manifest: d5fa211 прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40197422949) и [MariaDB receipt/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40197422957). Открытые критерии — реальный SSH/systemd/TLS/сервер для GO-007 и concurrent/mid-write/CAS для GO-031; они не выдаются за завершённые.


## Актуальная база GO-058 после merge родительских PR

Пользователь объединил PR #13 (включая #14) и #16 в main; его проверенная при разрешении конфликта вершина — 1c426cd. Открытый PR #15 / codex/GO-058-receipt-roundtrip теперь направлен в main. Main присоединён обычным merge без rebase/force-push; конфликт касался только сохранённых выше записей журнала. Код receipt-теста, fixture guards и порядок CI не изменялись. SHA256SUMS пересчитан по итоговым файлам вместо выбора устаревшей стороны.

Исходные результаты GO-058 и выбранная пользователем конфигурация (два DB_NAME, общий SQL-user/password, отдельные server env) сохранены. Следующие обязательные проверки должны пройти для нового merge-коммита PR #15. Сам PR агент не объединял, production/server/DB вне CI не менялись.

## Продолжение 26.10.2026: main после PR #15

GitHub подтвердил пользовательский merge #15 как 0c473e0; #13/#14/#16 также присутствуют в истории. Открытых PR на начале работы нет, рабочее дерево чистое. Go/web/security main [40224752860](https://github.com/meInteractive/it-map/actions/runs/40224752860) успешен; native publication пропущена при выключенном флаге. Историческое описание открытого #15 выше сохранено как история.

GO-060 / codex/GO-060-receipt-concurrency начинается от этого main. Узкий результат: реальная конкуренция двух create транзакций, same-key replay/intent conflict/vacancy duplicate/10-limit; точные критерии в SPRINT-01.md. Mid-write faults/crash/update CAS остаются открытой частью GO-031. Live SQL разрешён только внутри existing disposable CI. Сетевой Git работает при предоставленной команде сетевой возможности; proxy/системные настройки не менялись. Внешний staging/снимок/реальный сервер по-прежнему не подключаются.

GO-060 / [PR #18](https://github.com/meInteractive/it-map/pull/18) в main: `660c3d1` прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40236682531) и [MariaDB concurrency/receipt/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40236682675). Код/API не потребовал исправлений. Автоматическая публикация осталась skipped. Следующая узкая часть GO-061 — mid-write rollback и fresh-connection retry после потери ответа; process crash/CAS/HTTP replay остаются открыты.


GO-061 / codex/GO-061-receipt-fault-recovery начинается от 8978512 (код GO-060 660c3d1 уже прошёл оба обязательных CI; parent 8978512 меняет только отчёт/manifest). Во время работы пользователь объединил #18 как main 20f9fc1 до добавления doc checkpoint 8978512. Main присоединён обычным merge; отчёт 8978512 сохранён. Новый PR направляется в main, а не в закрытую родительскую ветку. Проверяются mid-write rollback и fresh-connection retry после post-commit read failure, без production hooks/DDL/локальной DB. Process kill/неопределённый COMMIT и update/CAS остаются отдельными критериями.


GO-061 / [PR #19](https://github.com/meInteractive/it-map/pull/19) в main: `62b8b46` прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40237125022) и [MariaDB faults/receipts/concurrency/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40237125124), ноль skips. GO-062 / codex/GO-062-receipt-process-crash начинается от этого проверенного SHA; на старте #19 открыт, база нового stacked PR — ветка GO-061. Порция проверяет реальный kill/restart отдельного Go writer перед/после commit. Server crash/неопределённый COMMIT/CAS/HTTP replay остаются открытыми критериями GO-031.


GO-062 / [PR #20](https://github.com/meInteractive/it-map/pull/20) поверх #19: `96f13a0` прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40237455813) и [MariaDB kill/restart/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40237455777); все четыре receipt targets реально выполнены без skips. GO-063 / codex/GO-063-application-cas начинается от этого проверенного SHA и направляется поверх открытого #20. Узкий результат — revision/CAS одного application, same-second/no-op, атомарный rollback истории; GO-031 остаётся открыт для смешанных операций/controlled HTTP replay.


## Проверенный стек после GO-060…063, 26.10.2026

| Задача | Ветка | База / PR | Проверенный code SHA | Обязательные CI |
| --- | --- | --- | --- | --- |
| GO-060 concurrent create | codex/GO-060-receipt-concurrency | main / #18, объединён пользователем как 20f9fc1 | 660c3d1 | [public](https://github.com/meInteractive/it-map/actions/runs/40236682531), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40236682675) |
| GO-061 mid-write rollback/fresh connection | codex/GO-061-receipt-fault-recovery | main / #19 | 62b8b46 | [public](https://github.com/meInteractive/it-map/actions/runs/40237125022), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40237125124) |
| GO-062 writer kill/restart | codex/GO-062-receipt-process-crash | GO-061 / #20 | 96f13a0 | [public](https://github.com/meInteractive/it-map/actions/runs/40237455813), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40237455777) |
| GO-063 single-record revision/CAS/history | codex/GO-063-application-cas | GO-062 / #21 | 36b0133 | [public](https://github.com/meInteractive/it-map/actions/runs/40237800951), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40237800814) |

Все указанные code SHA прошли соответствующие обязательные CI. Пять receiptwrites targets теперь реально выполняются без skips; cleanup сохраняет прежний readonly API/14 browser/native smoke. Локально live SQL явно пропускался без DSN, а четыре negative guards отказали до соединения. Go 1.23.12 compile/tagged tests, Go 1.27.3 vet/race/сборки, source/history/schema/fixtures/actionlint/manifest проверены; frontend build выполнен на GO-060, последующие изменения только test-only Go/workflow/docs. Build artifact вынесен в work перед source gate; generated dist в Git не добавлялся.

Реальный сервер/SSH/systemd/TLS/согласованный снимок по-прежнему не подключены. Полные GO-005/007/013/031 остаются открыты. Следующая разрешённая узкая часть GO-031 — controlled authenticated HTTP create/replay/error/delete поверх disposable MariaDB, с отдельной фазой записи и восстановлением readonly fixtures; затем shortcut/смешанные операции и GO-032 по зависимостям. Новый номер выбирать после проверки актуального реестра/GitHub. Агент не выполнял merge PR, прямой push main/master, включение публикации, production/GPT Sites/D1 или реальную SQL-операцию вне CI.


## Восстановление закрытого стека и приоритет GO-032

GitHub повторно проверен 26.10.2026: #19 объединён в main 48fb03a, затем #21 — в #20 (b39e1a2), а #20 — в уже закрытую ветку #19 (7142c2c). Открытых PR нет; main пока не содержит GO-062/063. GO-064 / codex/GO-064-stack-and-priorities начинается от 7142c2c, присоединяет main обычным merge и возвращает весь результат отдельным PR в main. Diff internal/.github/go.mod/go.sum относительно проверенного 07e1ec0 пуст; история не переписывается.

По явному уточнению пользователя GO-032 имеет средний приоритет. Текущий спринт только 1.1; следующая критическая порция — GO-031 HTTP create/replay/error/delete, затем shortcut/смешанные операции, GO-033/034. Более ранние упоминания GO-032 в критической очереди заменены этим уточнением. Никакого автоматического merge, real server/DB/production/GPT Sites/D1 действий.


GO-064 / [PR #22](https://github.com/meInteractive/it-map/pull/22) в main: b21a051 прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40279232693) и [MariaDB/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40279232671). Проверенный стек GO-062/063 сохранён без code changes; GO-032 средний. GO-065 / codex/GO-065-application-http начинается от этой проверенной вершины: настоящий HTTPS login/router/Repository/SQL create/replay/update/error/delete и post-commit HTTP 503/retry. Полный GO-031 остаётся открытым для shortcut/смешанных конкурентных операций.


GO-065 / [PR #23](https://github.com/meInteractive/it-map/pull/23) поверх GO-064: b0c97af прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40279874891) и [MariaDB/HTTP/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40279874919). Шесть write targets прошли без skips. GO-066 / codex/GO-066-application-shortcut начинается от этого проверенного SHA: HTTP aliases/фиксированные поля/sticky receipts, claim edited application при 10-limit и concurrent ordinary create/shortcut. Channel-owned vacancies, mixed delete/update/owner changes не выдаются за выполненные критерии; GO-031 открыт. GO-032 имеет средний приоритет, следующая критическая очередь включает GO-033/034.


GO-066 / [PR #24](https://github.com/meInteractive/it-map/pull/24) поверх #23: 65a890a прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40280586960) и [MariaDB/shortcut/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40280586955); семь required targets без skips. GO-067 / codex/GO-067-application-mixed начинается от этой проверенной вершины: channel-owned shortcut вне каталога, update/delete и replay/delete на двух реальных SQL connections. Остаток GO-031 по смене vacancy/owner и отдельным restore сценариям не закрывается по этим тестам; GO-032 остаётся средним, GO-033/034 — следующие доступные критические бизнес-блоки спринта 1.1.


## Проверенный стек GO-064…067, только спринт 1.1

| Задача | Ветка / PR | Проверенный code SHA | Обязательные CI |
| --- | --- | --- | --- |
| GO-064 сохранение закрытого стека/средний GO-032 | codex/GO-064-stack-and-priorities / [#22](https://github.com/meInteractive/it-map/pull/22) в main | b21a051 | [public](https://github.com/meInteractive/it-map/actions/runs/40279232693), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40279232671) |
| GO-065 HTTPS create/replay/update/error/delete | codex/GO-065-application-http / [#23](https://github.com/meInteractive/it-map/pull/23) в #22 | b0c97af | [public](https://github.com/meInteractive/it-map/actions/runs/40279874891), [MariaDB/HTTP/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40279874919) |
| GO-066 shortcut/claim at limit/ordinary race | codex/GO-066-application-shortcut / [#24](https://github.com/meInteractive/it-map/pull/24) в #23 | 65a890a | [public](https://github.com/meInteractive/it-map/actions/runs/40280586960), [MariaDB/shortcut/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40280586955) |
| GO-067 channel shortcut/update-delete/replay-delete | codex/GO-067-application-mixed / [#25](https://github.com/meInteractive/it-map/pull/25) в #24 | 1ba76cb | [public](https://github.com/meInteractive/it-map/actions/runs/40281030793), [MariaDB/mixed/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40281030609) |

Все code SHA прошли оба обязательных CI, восемь required SQL targets реально выполняются без skips; затем прежние 14 browser/native smoke. Локальные live SQL skips без DSN явно отделены от CI; четыре negative guards отказали до подключения. Tagged/обычный vet/race/обе сборки/Go 1.23/source/history/schema/fixtures/actionlint/manifest проверены. Текущий финальный doc checkpoint только обновляет результаты/остатки и SHA256SUMS; повтор CI точной вершины требуется отдельно.

GO-032 — средний, остаётся открытым. Полный GO-031 открыт для межмодульной смены vacancy/owner и остальных операций GO-012/013; следующие независимые критические бизнес-блоки — GO-033, затем GO-034 в спринте 1.1. Полные GO-005/007/030 по-прежнему требуют отдельно разрешённых staging/снимка. Продуктивные данные/сервисы, локальная DB, paid providers, production/GPT Sites/D1/DNS, включение publication и автоматический merge не затрагивались.
