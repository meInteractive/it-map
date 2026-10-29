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


## Продолжение после пользовательского merge всех PR, кроме #24

GitHub проверен 26.10.2026: #22 объединён в main c00282a, #23 — в закрытую ветку #22 (08bf742), #25 — в открытую ветку #24 (a37a78c). Main пока не содержит GO-065…067. Открытый #24 перенаправляется из закрытого parent #23 в main; его актуальная вершина a37a78c содержит проверенный 3cfe83d. Main присоединён обычным merge, без rebase/force-push и без автоматического merge PR. Diff internal/.github/go.mod/go.sum относительно 3cfe83d пуст; обновляются только checkpoint/manifest. Новый CI требуется на точном SHA обновлённого #24.

Следующая независимая критическая работа спринта 1.1 — GO-033: SQL/API create/reschedule/completion задач без Calendar, права/даты/ошибки/rollback и сохранение исходных readonly fixtures. GO-032 остаётся средним. Новая ветка начинается от проверенной актуальной вершины #24; если пользователь объединит #24 — база актуализируется по фактическому main.


#24 перенаправлен в main через GitHub REST/CLI (коннектор отклоняет параметр base). b0023ab прошёл [Go/web/security](https://github.com/meInteractive/it-map/actions/runs/40285624819) и [MariaDB/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40285624924), #24 CLEAN. GO-068 / codex/GO-068-task-create-contract начинается от этой проверенной вершины: 31 JS-oracle case task create, исправление 10 воспроизведённых расхождений UTF-16/trim/Number.safeInteger. Следом — guarded SQL и HTTP части GO-033, без Calendar/live данных.


GO-068 / [#26](https://github.com/meInteractive/it-map/pull/26): published d5e820d/tree bc334320 совпадает с локально проверенным draft, [public](https://github.com/meInteractive/it-map/actions/runs/40287308900) и [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40287308885) прошли. Git/gh write начал возвращать 401, рабочий GitHub-коннектор использован для blob/tree/commit/branch; локальный draft сохранён отдельной local-only веткой, опубликованная история не переписывается. GO-069 / codex/GO-069-task-sql-writes начинается от этого проверенного SHA: guarded SQL round-trip/Calendar guards/реальный rollback task writes, отдельный tag/required gate.


GO-069 / [#27](https://github.com/meInteractive/it-map/pull/27): c9f89ec прошёл [public](https://github.com/meInteractive/it-map/actions/runs/40288051112) и [MariaDB/task writes/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40288051119), именованный task target без skips. GO-070 / codex/GO-070-task-http начинается от этой проверенной вершины: real HTTP/SQL round-trip/ошибки/Calendar guard и concurrent completion/reschedule. Дополнительно устранены воспроизведённые +24:00/+00:60/comma-fraction расхождения; JS fixture расширен 31→34. Последующая UI write приёмка — отдельная порция GO-033.


GO-070 / [#28](https://github.com/meInteractive/it-map/pull/28): a9f7f95 прошёл [public/34 JS cases](https://github.com/meInteractive/it-map/actions/runs/40289210806) и [MariaDB/task HTTP+SQL/concurrency/API/14 browser/native](https://github.com/meInteractive/it-map/actions/runs/40289210834), required targets без skips. GO-071 / codex/GO-071-task-browser начинается от этой проверенной вершины: настоящий writable Go/SPA/MariaDB server под guard, два UI проекта create/reload/reschedule/complete/reopen/isolation. Cleanup до прежнего readonly smoke; write test не добавляется в старую readonly browser матрицу.


## Проверенная GO-033 после GO-068…071, спринт 1.1

| Подзадача | PR / code SHA | Обязательные CI |
| --- | --- | --- |
| GO-068 JS create normalization | [#26](https://github.com/meInteractive/it-map/pull/26) / d5e820d | [public](https://github.com/meInteractive/it-map/actions/runs/40287308900), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40287308885) |
| GO-069 task SQL/Calendar guard/rollback | [#27](https://github.com/meInteractive/it-map/pull/27) / c9f89ec | [public](https://github.com/meInteractive/it-map/actions/runs/40288051112), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40288051119) |
| GO-070 HTTP/dates/concurrent field edits | [#28](https://github.com/meInteractive/it-map/pull/28) / a9f7f95 | [public](https://github.com/meInteractive/it-map/actions/runs/40289210806), [MariaDB/API/browser/native](https://github.com/meInteractive/it-map/actions/runs/40289210834) |
| GO-071 desktop/mobile task UI writes | [#29](https://github.com/meInteractive/it-map/pull/29) / 179bce8 | [public](https://github.com/meInteractive/it-map/actions/runs/40291235900), [MariaDB/2 task browser + 14 old readonly/native](https://github.com/meInteractive/it-map/actions/runs/40291236012) |

Все code SHA прошли оба required CI; два task SQL/HTTP targets и browser Go parent фактически pass/no-skip, child PW expected=2/skipped=0/unexpected=0/flaky=0. Старые восемь receipt targets/14 readonly/API/native smoke сохранены и прошли после cleanup. Локально vet/race/build/tagged/Go 1.23/oracles/source/history/schema/fixtures/actionlint/manifest и negative guards успешны; live SQL/UI локально не запускались. Новые browser/config TS файлы отдельно проверены, полный tsc ограничен старым missing hosting import. Реальные env/tokens/SQL/private snapshots не использовались.

GO-033 закрыт только в поддерживаемом task API без Calendar sync. Parent GO-014 остаётся открытым для GO-034; GO-015/035/040 — Calendar/crypto, GO-005/007 — внешний staging/снимок. GO-032 средний. Следующая критическая задача спринта 1.1 — GO-034 (каналы/контакты), по legacy auth/SQL/reference contract. Финальный doc checkpoint только актуализирует эти доказательства и SHA256SUMS; CI точного SHA требуется отдельно. Автоматического merge PR/production/D1/Sites/DNS/deploy/paid calls нет.


## GO-072: начало каналов/контактов после проверенной GO-033

Финальный GO-071 / #29 `1fa08ba` прошёл [public](https://github.com/meInteractive/it-map/actions/runs/40291660817) и [MariaDB/task SQL+HTTP/2 task browser + прежние 14/API/native](https://github.com/meInteractive/it-map/actions/runs/40291660811). Новая ветка codex/GO-072-channel-input начинается от этой проверенной вершины. До реализации GO-034 разделён на неизменяемые GO-072 input contract → GO-073 channel CRUD → GO-074 contacts CRUD → GO-075 disposable acceptance; полный GO-034 остаётся открыт. Только спринт 1.1, GO-032 средний.

GO-072 добавляет pure Go normalize/validation и source-backed JS corpus: 55 input + 24 username vectors. Нет SQL writes или новых HTTP routes; existing GET и vacancy message validation не меняются. Scope typed UI/RFC3339, без обещания полного WHATWG/Date.parse/coercion parity. Следующая бизнес-часть — GO-073, с обязательной защитой ссылок вакансий и admin auth. Real DB/providers/инфраструктура/merge/deploy не используются.


GO-072 подтверждён: [PR #30](https://github.com/meInteractive/it-map/pull/30), code `b89ee88` / tree `dc9b0864dbe8430134b632da66f08a63f649f4a2`; [public/55+24 channel vectors](https://github.com/meInteractive/it-map/actions/runs/40292417590) и [MariaDB/task SQL+HTTP/2 task browser + 14 readonly/API/native](https://github.com/meInteractive/it-map/actions/runs/40292417772) успешны. Узкая GO-072 принята; GO-034 и GO-073…075 открыты. Следующая конкретная задача — GO-073 admin channel mutations/связанные источники вакансий; legacy SQL/auth/FK контракт уже прочитан. Важно включить защиту sole-source delete и сериализацию duplicate/name checks; не считать нормализацию завершённым CRUD. Финальный doc checkpoint не меняет runtime/test/workflow код относительно проверенного b89ee88; обязательный CI точного head требуется отдельно.

Открытый стек для последовательного review/merge пользователем: #24 → main, #26 → ветка #24, #27 → #26, #28 → #27, #29 → #28, #30 → #29. #25 уже объединён пользователем в ветку #24, его GO-067 содержится в #24. Ни один PR автоматически не объединён.


## Переключение на парсер, 26.10.2026

Пользователь запросил parser high→medium в спринте 1.1; GO-073…075 сейчас отложены. Финальный 29458b2 / #30 прошёл оба CI (40292857092/40292857030); PR #30 объединён в закрытый parent, main=b3d4820 содержит только #24. GO-076 сохраняет всю проверенную вершину 29458b2 и main через merge без изменений кода; tree 88e3fc37fc801d03e8028a0759e61155ac621180 совпадает до doc checkpoint. Новый стек начинается PR в main. Следующие pure critical-порции GO-077 crypto compatibility, GO-078 settings guards, GO-079 budgets/time; большие SQL/recovery/import и medium clients по зависимостям. ADR-016 остаётся предложенным, real secrets/providers не подключаются.


GO-077 реализует только canonical legacy crypto compatibility. Пять namespaces включают Calendar и ключи парсера OpenAI/OpenRouter-JEV/Firecrawl/Apify; free/hobby и free/starter используют старые общие provider domains. 15 synthetic JS fixtures проверяют Go decrypt и deterministic ciphertext/tag; отдельный legacyinterop build tag отправляет 15 случайно зашифрованных Go значений настоящему JS decrypt. Никаких provider HTTP/secret reads/новых API. Строгий Go codec отвергает malformed/trailing/padded/noncanonical envelopes и значения свыше 64 KiB; это границы pure-порции, а не изменение подключённого API. ADR-016 остаётся предложенным.


GO-077 / #32 `75ba9d7` подтверждён public40307364593 и MariaDB/16 browser/API/native40307364181. GO-078 в отдельной ветке codex/GO-078-parser-settings переносит pure validation/normalization из настоящего saveSchedulerSettings до SQL: 62 vectors/defaults/options и strict boundary checks, без runtime/API/jobs. GO-021/045 открыты. Следующая critical порция GO-079 upper-cost/time, затем отдельные durable SQL gates до любого paid клиента.


GO-078 / #33 `14b26cd` прошёл public40308221146 и MariaDB/16 browser/API/native40308221140; pure settings portion принята. GO-079 (codex/GO-079-parser-budget) переносит historical upper-cost/MSK periods и не выполняет work/reservation. 77 cost/10 period vectors, все historical tariffs; два numeric-string mismatch воспроизведены до fix, overflow дополнительно отказывает. Полные GO-017/021/023/045 и parser не закрываются. Следующая high часть требует durable SQL leases/recovery до включения клиентов; затем подключение конкретных готовых стадий.


GO-079 / #34 `ebc7a1d` прошёл public40309279822 и MariaDB/16browser/API/native40309279795. GO-080 в codex/GO-080-scheduler-run-lease добавляет durable global cycle owner/replay/expiry/generation fencing и conservative recovery на существующих таблицах. Новые source-run identity fixture и отдельный required schedulerwrites SQL/rollback/crash target; до положительного CI узкая GO-080 открыта. Полные GO-045/027 не закрываются. Общие рабочие очереди ведутся только в PLANS.md; здесь фиксируются проверенные branches/PR/SHA/results.


Перед PR GO-080 обнаружен очередной merge родителей пользователем: main=4b0bbb9 содержит #31, а #32…34 объединены в уже закрытые ветки. База GO-080 сохраняет весь проверенный ebc7a1d и новый main обычным merge; code tree 38cbe04d14eaf529fb400f0332139b659599c6bf до GO-080 изменений идентичен ebc7a1d. Новый PR GO-080 направляется прямо в main и включает ранее проверенные GO-077…079 как inherited prerequisites; closed parent branch не используется целью PR.


GO-080 / #35 `d48e4d7` / tree c9c9577ba0984539940098f3568673412304f531 прошёл public40316420266 и MariaDB40316420306: scheduler target6 subcases/no-skip, затем прежние task writes/UI/readonly/API/native. GO-081 начинается от этой проверенной вершины; отдельный Apify adapter проверяется только intercepted JS/loopback httptest, core runtime не включается.


Для GO-081 учтён merge #35 пользователем: main=7d426a0. Ветка codex/GO-081-apify-transport основана на проверенном d48e4d7 и присоединяет main без code diff; tree c9c9577ba0984539940098f3568673412304f531 до adapter changes совпал с d48e4d7. PR направляется в main и содержит только новую adapter-порцию плюс её docs/statuses, без восстановления старых закрытых parent branches.


GO-081 / #36 `93babc6` / tree7f91908e3dbd9eb04e2dcc20c6a3504f6ccac99a прошёл public40320431546 и MariaDB40320431536. GO-082 (codex/GO-082-project-readme) основан на этой проверенной вершине и меняет только README/docs/registry/checkpoint/manifest: авторский README, developer/source guide, команды локальных проверок, architecture status и public snapshot boundary. Общие приоритеты остаются в PLANS.md.


GO-082 завершён как документальная порция: [PR #37](https://github.com/meInteractive/it-map/pull/37), README code `fd5ca50` / tree57e6de64a928513194bdfdbd0620acdae7ba4818. 9 локальных ссылок README и 67 ссылок проверенных документов существуют и соответствуют назначению; UTF-8 bytes совпали с вложением после CRLF→LF. Полезная техническая информация перенесена в существующие разделы docs, readiness503 сохранён по коду. Source/history/manifest/diff validation успешны, runtime/test/workflow код не изменён. Финальный checkpoint меняет только отметку/доказательства задачи и SHA256SUMS; статусы автоматических CI доступны в PR, ожидание не считается pass.


После merge #36 пользователем main=9059c75 включает #37 и новый README; Git blob a31b71bcd3ed456f6a90f2221ed25b1f37eea627 совпал с принятым. Дублирующий GO-083 отменён без PR; 6753cff не является базой нового кода. Новая GO-084 начинается от актуального origin/main=9059c75, только CRM flag write gate. Source tree main совпадает с ранее проверенным e36c609; общий порядок продолжения фиксируется в PLANS.md.


GO-084 / [PR #38](https://github.com/meInteractive/it-map/pull/38), code9986429/tree65470cdeed6d77d535bc079429befb5ae5da7cf4: public40335262404 и MariaDB40335262393 успешны, CRM SQL/HTTP targets без skips и прежние browser/API smoke pass. Первый run40334908089 выявил конкурентную SQL-ошибку flags; no-op UPSERT заменил INSERT IGNORE и повторный gate подтвердил оба независимых flags. Checkpoint меняет только доказательства/статус и manifest; GO-010 целиком открыт.


GO-085 начата от GO-084 checkpoint08c3969 с проверенным runtime9986429; отдельная codex/GO-085-company-crm-save направляется в codex/GO-084-crm-flag-writes. Перенос PUT /api/crm / atomic state-history и required disposable gates; до положительного CI задача открыта.


GO-085 / [PR #39](https://github.com/meInteractive/it-map/pull/39), code8d9d16b/tree1fa5df8a468f8bbe64f20aca301f96defd7fa664: public40336444772 и MariaDB40336444745 успешны. CRMSaveDisposable/CRMSaveHTTPDisposable ran/pass без skips; все предыдущие SQL/14 browser/API smoke pass. Финальная отметка касается только статуса/доказательств/manifest, runtime не меняется.


GO-086 начата от GO-085 checkpointa24700a с проверенным runtime8d9d16b; codex/GO-086-vacancy-flags → codex/GO-085-company-crm-save. Принимается только personal flag SQL/HTTP часть GO-012, create/edit/delete пока открыты.


GO-086 / [PR #40](https://github.com/meInteractive/it-map/pull/40), coded2f9e44/tree878fa419f66c5203f5c44092adb6b52b11e27caa: public40338099465 и MariaDB40338099284 успешны. VacancyFlagsDisposable/VacancyFlagHTTPDisposable ran/pass без skips; прежние SQL/14 browser/API smoke также pass. Финальный checkpoint меняет только статусы/план/доказательства и manifest. GO-012 create/edit/delete остаются открыты.


Проверка после пользовательского merge #38/#39/#40: mainc9d6c57 содержит только #38; #39 merged848914e в прежнюю GO-084, #40 merged6b7d47f в прежнюю GO-085. GO-087 / [PR #41](https://github.com/meInteractive/it-map/pull/41) направлен непосредственно в main, mergebfe1280 сохраняет родителей mainc9d6c57 и #40/2ff18e7. Treee8686f8c5352b904d5aed93230972da140b422f9 совпадает с проверенным #40, runtime не меняется.


GO-088 начата от GO-087/#41 checkpoint9093c48: отдельная codex/GO-088-vacancy-create-gate направляется в codex/GO-087-deliver-merged-stack. Критерии create/edit/delete разделены GO-088/089/090 до реализации. Добавлены creation fixture и SQL/HTTP gates; положительная SQL-приёмка ожидает CI.


GO-087/#41 на9093c48 прошёл public40369680449/MariaDB40369680396, пользователь merged в maind2bfef8. Ancestry2ff18e7 и пустой diff9093c48→main подтвердили доставку #39/#40. Открытый GO-088/#42 сразу перенаправлен в main.

GO-088 / #42, codeb34918d/treef8086c3ced9776c4eeb83eabbe07134ffa6eba37: public40371043954 и MariaDB40371043986 pass, два required create targets без skips и все старые SQL/browser/API smoke pass. Пять late faults, два duplicate races, named-lock release/max1 pool и admin API приняты; missing channel owner404 исправлен после первого SQL/HTTP run.

GO-089 начата от проверенного b34918d, codex/GO-089-vacancy-edit-gate → codex/GO-088-vacancy-create-gate; SQL/HTTP edit/CAS/owner move gates ожидают положительный CI.


GO-089 / #43, codef06076b/tree09539438b1f95fc830b24de185ccfcb45f5f7683: public40373140791 и MariaDB40373143125 pass, оба required edit/CAS SQL/HTTP targets без skips и все прежние SQL/browser/API smoke pass. Первый run40372641813 воспроизвёл missing FK parent fresh owner; transactional UPSERT и late rollback подтверждены повторно.

GO-090 начата от проверенного f06076b, codex/GO-090-vacancy-delete-gate → codex/GO-089-vacancy-edit-gate. Только synthetic archive/delete/FK/CRM retention; общий fixture/shared helpers повторно проверяются create/edit gates.


GO-090 / #44, code0b4b3aa/tree692116d1adb1c8e9e15ff603d15a6354712aa6de: public40374264948 и MariaDB40374264558 pass, оба delete SQL/HTTP targets без skips и весь прежний SQL/browser/API suite успешен. Четыре late faults восстанавливают архив, refs и FK children после actual DELETE; fresh pool retry сохраняет applications/tasks/history.

Пользователь merged #42 в mainc8e79b9, затем #43 в прежнюю GO-088/77c8ee6. Открытый #44 сразу перенаправлен в main, содержит GO-089/090; diffb34918d→mainc8e79b9 пуст, поэтому проверенное runtime дерево остаётся прежним. Финальный checkpoint меняет только task status/plan/evidence/manifest. Требуется пользовательский merge #44 для доставки edit/delete в main; автоматический merge не выполняется.


После merge #44 maine75b7df содержит checkpoint7060858 и весь принятый GO-089/090 runtime; ancestry и пустой tree diff подтверждены. GO-091 начинается от актуального origin/main=e75b7df, codex/GO-091-profile-contracts → main. Порция содержит source-backed контракты и schema/default metadata; нового SQL/HTTP wiring нет.


GO-091 / #45, code35f6f74/tree0dae62b48a2c6b892d004598b34e0fdc05c00eef: public40385568261 и MariaDB40385568172 pass, включая source oracle/Go contracts и все прежние SQL/browser/API smoke. GO-092 начинается от проверенного 35f6f74: codex/GO-092-resume-writes → codex/GO-091-profile-contracts; новый resume SQL/HTTP gate ожидает positive CI.

GO-092 принят: [#46](https://github.com/meInteractive/it-map/pull/46), code51d48ee/tree77adc705d67c937d47b0ed2f3f1ae2b215074c77; public40389209915 и MariaDB40389209981 успешны. Required resume SQL/HTTP gates zero skips: defaults/no GET writes, Unicode CAS, два first writers/editors, late rollback/retry и signed-owner isolation. GO-093 строится от этой проверенной вершины; без нового shared-settings SQL/HTTP proof prerequisite GO-016 не считать принятым.

GO-093 начата от проверенного 51d48ee: codex/GO-093-shared-profile-settings → codex/GO-092-resume-writes. 24 legacy SQL transitions исполняются на SQLite :memory:, defaults/bootstrap/revisions сравниваются в required MariaDB gate. Shared SQL/HTTP/admin/catalog-mock proof ожидает CI; внешних providers нет.

GO-093 / #47, code5b9e891/tree150e79df198495a849ad657179caf19261f68ff9: public40392117857 и MariaDB40392117847 pass, shared SQL/HTTP zero skips и все прежние SQL/browser/API smoke успешны. Critical schema/data prerequisite GO-016 принят узко, без UI/provider catalog wiring. GO-094 starts codex/GO-094-v39-format-contracts → codex/GO-093-shared-profile-settings от проверенного 5b9e891; pure v39 contracts перед SQL export GO-095.

GO-094 / #48, codeac0fd60/tree65d138f020379a5d4de7bcaa4ad7ab7ad56868dd: public40394902190 и MariaDB40394901964 pass; required 67-case JS↔Go v39 oracle/race и весь прежний SQL/API/browser suite успешны. GO-094 принят как pure contract, GO-041/095 export и GO-042 restore открыты. Следующий checkpoint меняет только docs/status/manifest, runtime source остаётся ac0fd60. Стек открытых #45→46→47→48; автоматического merge нет, после merge родителя перенаправить открытого потомка в main и проверить доставку всей ancestry.


## Проверка пользовательского merge #45–48 и GO-096

PR #45–48 имеют MERGED, но их базы различались: #45→main, #46→GO-091, #47→GO-092, #48→GO-093. Фактический origin/main=1b137ea4e6797475ade21054c3dbc78d71503774/tree0dae62b48a2c6b892d004598b34e0fdc05c00eef содержит только GO-091; tree diff с 35f6f74 пуст. Проверка merge-base подтверждает отсутствие 51d48ee/5b9e891/f8c0edc в ancestry main. Исходный полный checkpoint f8c0edc прошёл public40395542183 и MariaDB40395542226, включая required v39 interop и все SQL/API/browser gates.

GO-096: codex/GO-096-stack-checkpoint → main. Commit b03434ee34cce4feb9b8e1621da1263d8f7b0787 имеет родителей f8c0edc и main1b137ea, tree679373460f7f21e73d93bcbd50757ffe3c58e3ae точно совпадает с f8c0edc. Runtime source не изменён; добавляются только task/plan/checkpoint/manifest. Этот PR должен доставить GO-092/093/094 в main; MERGED старых дочерних PR этого не сделал. Автоматическое слияние и запись в main не выполняются.

GO-096 / #49 принят на checkpoint7628adf: public40397110316 и MariaDB40397110258 pass. Пользователь объединил его в main293f9b702aba823dedbc02240ffb73713a51766d; ancestry и точное равенство дерева подтверждены. GO-095: codex/GO-095-v39-sql-export → main от этой актуальной проверенной вершины; SQL/admin HTTP export ожидает required disposable CI.

GO-095 / #50, code8b2fe066df36e9de8f5e3e5df60c5903b3ca0c4b/tree45babe9b1875b5105fffda88f8edabe09dfcf8ea: public40407984745 и MariaDB40407985424 pass, оба required export SQL/HTTP zero skips и весь прежний SQL/API/browser suite pass. Reader имеет только SELECT; independent comparison всех application scalars и unchanged legacy validator подтвердили файл. Приняты default catalog/migrations, schema rejection/concurrent DDL, RR snapshot/concurrent insert и non-MVCC rollback high-water. GO-041 принят как export v39; GO-019/042 restore открыты. Следующий checkpoint меняет только docs/status/manifest, runtime source сохраняется 8b2fe06.

После merge #50 origin/main0b5dfd8095469c95016e07d850d08ddc14446f1d содержит checkpoint886ca6f; ancestry и точное равенство tree подтверждены. Public40408615728 и MariaDB40408615753 pass. GO-097: codex/GO-097-v39-restore-plan → main, pure source-backed v39 plan без SQL/DDL/HTTP.

GO-097 / #51, coded6d7847/treef570947ea108392a5e9a2ef20c3910abbb25ad3b: public40412684211 pass, required 28-case unchanged JS restore oracle успешен; shared MariaDB40412684193 пока выполняет browser setup. GO-101 начинается от локально/публично проверенного d6d7847: codex/GO-101-restore-counter-preflight → codex/GO-097-v39-restore-plan. Counter-only proof выполняется в отдельной disposable job; API/data restore не подключён.

GO-097 принят по #51/d6d7847: public40412684211 и MariaDB40412684193 pass. GO-101 / #52, code609eef4/tree52bf54707338ef0eb7fa9f37af54432b7019e438: public40414471324 pass, отдельная restore-counters job121893184366/run40414471382 pass, семь SQL cases zero skips, общий browser pipeline ещё выполняется. Добавлена проверка nonempty business rows перед success/late faults; следующий CI ожидается. #52 перенаправляется в main с сохранением ancestry #51, чтобы batch merge не оставил код в закрытом parent branch.

GO-101 / #52 принят на code8a3f334abed1d8706386a15096beebf3f3bf3894/tree335cbc2bbb2e69de1f290373c6092b07498136cc: public40415955237 и MariaDB40415955239 pass; counter job121898215347 — семь live cases zero skips, включая nonempty data success/late faults, весь SQL/API/browser suite успешен. #51 и #52 оба имеют base main; #52 ancestry содержит d6d7847. Финальный checkpoint меняет только docs/status/manifest, runtime и tests остаются 8a3f334. GO-098/099/100 и полная GO-042 открыты.

GO-098: ветка `codex/GO-098-v39-transactional-restore` от проверенного main0319c24 после пользовательского merge #52. Prepared-schema restore + generation data TX, source49-row oracle, отдельная disposable MariaDB job. Локальные race/unit/tagged/vet/build/web проходят; положительные SQL проверки только в CI, PR/результаты будут зафиксированы после публикации.

GO-098 / [#53](https://github.com/meInteractive/it-map/pull/53): de99bfe/tree3fc9a98c610e6d84b8a6fb8d188e7fcd68d3ef15, Public40462975460 и MariaDB40462975508 полностью pass. GO-099 `codex/GO-099-v39-restore-http-crash` начинается от этой проверенной вершины; отдельная disposable crash/concurrency/HTTP job, PR и SQL proof ожидают публикации.

| Текущая порция | Ветка | База | PR | Proof |
| --- | --- | --- | --- | --- |
| GO-098 atomic v39/generation | codex/GO-098-v39-transactional-restore | main | [#53](https://github.com/meInteractive/it-map/pull/53) | de99bfe: Public40462975460/MariaDB40462975508 pass |
| GO-099 crash/HTTP/late-result fence; включает #53 | codex/GO-099-v39-restore-http-crash | main | [#54](https://github.com/meInteractive/it-map/pull/54) | 1b12301/treef1dbd7388f3ceaa8afb7fc1e83afdcda4d918d1b: Public40464855878/MariaDB40464855875 pass; required crash/concurrency/HTTP/late-result job122057707630 zero skips |

#54 направлен прямо в main и включает #53: одного merge #54 достаточно для доставки обеих порций. Финишный checkpoint меняет только docs/status/manifest; runtime/tests совпадают с 1b12301. GO-100 historical/bootstrap и полный GO-042 остаются открытыми. Полная AI/provider orchestration не принимается по одному generation boundary.

Уточнение GO-053 по поручению пользователя: `codex/GO-053-ubuntu-20-deploy-docs`, база main15c2ac8. Добавлен условный Focal runbook и исправлен выбор Python transient worker на выбранный sys.executable. Проверка на реальном Ubuntu20/MariaDB/systemd не выполнялась; CI/unit и источники фиксируются в PR.

GO-100-A: `codex/GO-100-historical-validation` от проверенного main15c2ac8, затем ff опубликованного GO-053 Ubuntu20/#55/5a7aba1. Required source416-case v16–39 validator и strict v39 regression реализованы без SQL/runtime расширения. Сохранена отдельная docs-порция #55; её ancestry входит в будущий PR GO-100-A.

| Продолжение | Ветка | База | PR | Proof |
| --- | --- | --- | --- | --- |
| GO-053 Ubuntu20 | codex/GO-053-ubuntu-20-deploy-docs | main | [#55](https://github.com/meInteractive/it-map/pull/55) | 5a7aba1: Public40468557418/MariaDB40468557597 pass; 26 receiver tests, docs links/unit syntax |
| GO-100-A validation v16–38; включает #55 | codex/GO-100-historical-validation | main | [#56](https://github.com/meInteractive/it-map/pull/56) | 92fff43/treeed1a42514117d9f03f4cad02f10c3bf80908deee: Public40469083705/MariaDB40469083582 pass; required416-case source oracle и весь старый SQL/API/browser regression |

#56 напрямую в main и включает #55: достаточно merge #56 для доставки обеих порций. Финишный checkpoint меняет только docs/status/manifest; code/tests совпадают с проверенным 92fff43. GO-100-B/C/D и полная GO-042 остаются открытыми. Реальный Ubuntu20 не проверен.

GO-100-B-P: `codex/GO-100-b-historical-plan`, база проверенный mainaf78f20 после пользовательского merge #55/#56. Pure source plan/Unicode policy и 216-case oracle без SQL; existing416/67/28 и local checks pass, PR/CI ожидают публикации.

GO-100-B-P / [#57](https://github.com/meInteractive/it-map/pull/57), a73f05e/treebbeff7db9db42a01f7abfb7325193cc175a7a38b: Public40476626861/MariaDB40476626918 fully pass. GO-100-B-S `codex/GO-100-b-historical-sql` начинается от этой проверенной вершины: generic SQL/HTTP v16–39, required separate historical job; live positive gates ожидаются.

| Продолжение | Ветка | База | PR | Proof |
| --- | --- | --- | --- | --- |
| GO-100-B-P pure plan | codex/GO-100-b-historical-plan | main | [#57](https://github.com/meInteractive/it-map/pull/57) | a73f05e: Public40476626861/MariaDB40476626918 pass; source216/416/67/28 + прежний SQL/API/browser |
| GO-100-B-S SQL/HTTP; включает #57 | codex/GO-100-b-historical-sql | main | [#58](https://github.com/meInteractive/it-map/pull/58) | 5b91bc5/treea31507461cb47ade37bb80908d6bd3a3c094b36e: Public40479135218/MariaDB40479135520 pass; required historical job122104725668 zero skips |

#58 прямо в main и включает #57: одного merge #58 достаточно для доставки GO-100-B. Финишный checkpoint меняет только docs/status/manifest; runtime/tests совпадают с проверенным5b91bc5. C v14–15 и D bootstrap остаются открытыми. Реальных данных/DB/providers/deploy не было.


Проверено после пользовательского merge #58: main5efe1cb содержит db2d3f4, tree76537012cc9f2399f69d6749950816bddf74f002 совпадает; финальные Public40480722656/MariaDB40480722680 success. GO-100-C-V / codex/GO-100-c-v-legacy-validation → main: pure unsigned v14–15 validation и required unchanged JS differential; runtime/sql/counters не расширяются. C до реализации разделён на C-V → C-P → C-S, родитель открыт. Local/CI proof будет добавлен по фактическому SHA.

GO-100-C-V / [#59](https://github.com/meInteractive/it-map/pull/59), code67104989929d9b2f0705e2a65b331c0d4710c60d/treed03b051ed713d7e0207f55614b35d4aacac4bd2a: Public40482915844 и MariaDB40482915965 fully pass. Required1468 legacy vectors (469 accepted) и весь source67/416/216/28/SQL/API/browser suite успешны. Финальный checkpoint меняет только PLANS/tasks/docs/manifest, runtime/tests совпадают с 6710498. GO-100-C-P source boundary исследован: company rating/sort отличается от agency normalization/order; implementation остаётся открытой вместе с C-S/D и полной GO-042. PR #59 → main, после пользовательского merge проверить фактические ancestry/tree.

GO-100-C-P / codex/GO-100-c-p-legacy-plan начинается от проверенного mainde22204 после #59. Самостоятельная порция: entity field normalization + required705 unchanged JS vectors, без locale sorting/полного legacy plan/runtime writes. Полная C-P остаётся открытой; source-compatible Russian collation — оставшийся prerequisite C-S/D. Node и x/text в runtime не добавляются. Proof будет записан по фактическому CI SHA.

GO-100-C-P/fields / [#60](https://github.com/meInteractive/it-map/pull/60), code4ce126bd683a58dac3f0e58bcc411ba22547075e/treeba7791349cf3cf75b2de0e4741a16407515dfd7a: Public40507165590/MariaDB40507165592 success. Required705-case exact JS serialization/ownership, старые source67/416/216/28/1468 и все disposable SQL/API/browser/counter/crash/historical restore gates pass. Checkpoint меняет только план/tasks/docs/manifest, runtime/tests остаются4ce126b. Полная C-P открыта до source-compatible locale order и legacy plan; runtime всё ещё16–39. PR60 → main, после user merge сверить ancestry/tree.

GO-100-C-P / codex/GO-100-c-p-locale-plan начинается от принятого main35dba9f/tree6affb28a4b8d0229b5faef1899a367f4eb4eeb41 после #60 (ancestry/tree проверены). Docs-only предложение Unicode tie-order: фактические8/418748 source differences, без runtime comparator. Вопрос передан на решение пользователя по AGENTS; пока доступна независимая critical GO-073. Не считать прототип принятым C-P.

GO-073 / codex/GO-073-channel-crud: база docs-only e0cc834 (#61), runtime база verified main35dba9f. Самостоятельный channel SQL/HTTP CRUD, Node SQLite :memory: oracle и отдельный disposable CI. Полная GO-034/075 остаётся открытой; PR и positive CI proof будут записаны после фактической проверки.

GO-073 / [#62](https://github.com/meInteractive/it-map/pull/62), code d457a59f8b9685c571e2d375b513cbe9c2f7e3a8/tree52497e6fe0ce92df8a3780c013a6db0e190710f1: Public40526046586/MariaDB40526046600 pass, source + три required channel targets без skips и весь regression успешен. Checkpoint меняет только docs/status/manifest. GO-074 стартует от проверенного #62, GO-034/075 ещё открыты.

GO-074 / codex/GO-074-hiring-contacts от #62/6c731c8 (runtime d457a59 принят двумя CI). Source297 validation +21 CRUD/read cases, owner-slot transactional SQL/HTTP и отдельный disposable contact-writes job. Positive proof/PR пока ожидаются; GO-075 остаётся отдельно.

GO-075 / codex/GO-075-channel-contact-gate от #63/f911ef9: совместный source SQL/HTTP gate и реальные vacancy editor/channel delete races. Семь required родителей без skips должны завершиться до mariadb-schema/API/browser/native job; PR/positive proof ожидаются.

GO-074/#63/f911ef9: Public40528713883/MariaDB40528713300 fully pass. GO-075/#64/3e53862: Public40529879529/MariaDB40529879394 fully pass; joint seven parents без skips и весь старый regression. Checkpoint меняет только docs/status/manifest. Leaf #64 →main включает ancestry #61/#62/#63; одного пользовательского merge64 достаточно. Docs-only #61 по-прежнему не разрешает runtime comparator. GO-031 продолжается независимой software порцией.

GO-102 / codex/GO-102-application-vacancy-races от финального #64/ccd7237 (runtime source3e53862 accepted двумя CI). Полная GO-031 до реализации разделена на102 Update/owner-move deadlock/CAS recovery →103 mixed create/delete/restore. Повтор только для actual MariaDB1213; required SQL/HTTP targets должны доказать настоящий server-selected victim, оба порядка, CAS/history/owner/FK. Positive proof ожидается.

GO-102/#65, code1c10e7b/treecfcc17a95c91a13aec0c86c9f280389e01a5a456: Public40583144187/MariaDB40583144491 fully pass, required actual SQL/HTTP deadlocks в обоих порядках без skips и весь regression успешен. Checkpoint только docs/status/manifest. Leaf65 →main включает64 и предшествующую ancestry; одного merge65 достаточно. Следующая GO-103; полная031 пока открыта.

Фактическая доставка после пользовательских merge: main ea2c20f/tree4d28dd1 совпадает с финальным64/ccd7237. PR65 merge21:11UTC прошёл в ветку GO-075 после merge64 и не доставил runtime1c10e7b в main. Создаётся отдельный GO-102 delivery PR той же feature ветки →main; runtime/test/workflow дерево совпадает с проверенным code1c10, дополнены только checkpoint/docs/manifest. Не переоткрывать пользовательский merged PR и не выполнять auto merge.

GO-102 delivery [#66](https://github.com/meInteractive/it-map/pull/66) →main, head690de6d/tree37ae579f92422ae217b13cfee7eac42c1f9dc04a; runtime/test/workflow exact1c10e7b, дополнены только docs/status/manifest. GO-103 / codex/GO-103-application-link-gates начинается от690de6d с пятью required mixed SQL/HTTP targets. Его leaf PR сразу вmain, ancestry сохраняет GO-102; положительный proof ещё ожидается.

GO-103 / [#67](https://github.com/meInteractive/it-map/pull/67), code93834b8625d1819ca5614869ad663a6161e9b789/tree0585fe2068b9b785ccaf3dbc8697bf5e705953e2: Public40597722262/MariaDB40597721853 fully pass. Required application-link-acceptance job122517783731 выполнила все пять родителей без skips; source, старые SQL/restore/channel/contact/deadlock/API/browser/native gates успешны. Runtime unchanged. Counter preflight wait наблюдается до data TX, тестовые actors join до cleanup. GO-103/031 и critical application SQL часть GO-013 приняты; UI остаётся отдельно. Последующие docs содержат ADR020…024 и явное delegated Unicode decision; checkpoint меняет docs/status/manifest. #67 →main, следующий C-P от принятой вершины без ожидания merge. Delivery #66 уже смержен пользователем в main по GitHub collection.

### GO-100-C-P — canonical comparator и полный pure legacy plan

- Ветка codex/GO-100-c-p-canonical-plan от #67/95712d9, leaf delivery →main. Unicode выбор уже принят ADR-023; approval/merge не ожидается для реализации.
- Own generator/публичная packed Unicode17/ICU78.3 table/лицензия; required479072 сравнения (8 accepted canonical differences) и170 unchanged52-table legacy plans. Runtime Repository принимает только signed16–39; SQL C-S/bootstrap D остаются открытыми. Локальные проверки/CI и PR будут добавлены после подтверждения.
- Локально pass: полный backup source/race suite (включая прежние1468/705/216/416/67 contracts и новые170 plans),479072 collation comparisons/8 declared exceptions, generator byte reproduction, обычный и mariadb-tagged race/vet, обе Go-сборки, Go1.23.12, web build/source/history/schema/fixtures/actionlint/local links/manifest. Live MariaDB локально не запускалась; C-P не выполняет SQL. PR/CI пока ожидаются; `[x]` не поставлен.

### GO-100-C-P принят / GO-100-C-S начинается

- #68 code97bad5d288761c9091f506c659d4dca0082422ba/treedf54b2d0359eb101abc75a8b86ee7746000f522f: Public40651564976/MariaDB40651565061 fully pass. Required generator/corpus479072/source170 plans и весь прежний regression успешны. #67 уже в main23f9512/tree3fc1a4, точное финальное дерево95712d9.
- Ветка codex/GO-100-c-s-legacy-restore от принятой #68 без ожидания merge; leaf delivery →main. Scope: unsigned generic intake, per-file preserve read-set до mutating pure plan, locked calendar/audit и separate six-parent historical/unsigned CI. SQL bootstrap D не включён. Positive MariaDB локально не запускать; приёмка C-S ожидается.
- Локально C-S pass: ordinary/tagged race/vet, обе сборки/Go1.23.12,6 source SQLite :memory:→Go plans и все прежние source cases. Старый runtime-negative guard1468 был обновлён под разрешённый generic legacy intake и повторно прошёл; strict Modern/V39 guards сохранены. Web/source/history/schema/fixture/actionlint/links и4 unsafe-target pre-connect refusals pass. Новые actual MariaDB subtests локально явно skipped, positive proof ожидается только в CI.

### GO-100-C-S принят / GO-310 начинается

- #69 code229717503bb957a706abe6744416481ed4e41ebc/tree9cd475bc6cd4375633f9c86766d7a8c0396b6c97: Public40653587489/MariaDB40653587455 fully pass. Historical job122699204697 выполнил все6 signed/unsigned required parents без skips; old API/browser/native regression pass.
- GO-100-D до реализации разделена на GO-310 trusted217-action DDL/full metadata preflight → GO-311 schema-init/disposable failure+retry. Ветка codex/GO-310-trusted-schema от принятой #69, без ожидания merge. Чужая или populated partial schema не дополняется, реальные DB/DDL/deploy не разрешены.
- GO-310 промежуточный capture:217-action DDL manifest привязан к reviewed schema-only source; Inspect/Preflight не исполняют DDL. До server-normalized metadata policy fail-closed с ErrUnverified. Required guarded read-only CI capture должен дать структуру72/606/50FK/22triggers, затем metadata встраивается и полный preflight/CI принимаются отдельным checkpoint того же PR. Новый runtime bootstrap пока не реализуется.
- Capture checkpoint: local race/unit/vet/combined-tag vet, обе сборки/Go1.23.12/source/schema/actionlint и4 pre-connect guards pass. Metadata ещё null, Preflight возвращает ErrUnverified; production/runtime DDL отсутствует. Новые SQL tests локально явно skipped. Capture — незавершённая часть GO-310, требуется получить и сверить server metadata, затем meaningful partial/full preflight tests и полный CI.
- Capture job122706734570 на2a014e6 прошёл:72/606/269 index-column rows/117 CHECK/51 FK-column rows (50 constraints)/22 triggers. Публичные metadata извлечены из26 ordered test-output chunks и встроены; preflight tests проверяют изменённые поля, missing columns/CHECK/PRIMARY, torn composite indexes/FK и additive retry plans. Приёмка310 требует повторного CI exact match с embedded metadata.

### GO-310 принят / GO-311 начинается

- #70 code05b8a588719faa36988019343e9aa89ca723c152/tree0ce30544ec3a579116d7fb1417a32a654671a247: Public40657232349/MariaDB40657232302 fully pass, required122711129066 exact full metadata match и весь старый regression. PR переведён из draft в ready, автоматического merge нет.
- GO-311 ветка codex/GO-311-schema-init от принятой #70. Только explicit offline command перед HTTP startup, same DB-scoped restore lock, trusted missing DDL, отказ для populated partial schema. DDL statement commits и generation seed отделены от последующего restore data TX. Positive tests только отдельная blank disposable DB с workflow nonce в SCHEMATA comment, без реальных подключений.
- GO-311 local pass: ordinary/tagged race/vet, обе сборки/Go1.23.12, CLI/env production guard preserved,3 unchanged SQLite :memory: fresh-schema restore versions14/16/39, schema/source/history/actionlint/links/manifest и4 unsafe-target pre-connect refusals. Positive schema init/CLI/DDL retry только в новом blank disposable CI, локально явно skipped. Unknown generation COMMIT проверяется только повторной inspection, без автоматического повторения INSERT. Реальные DB/keys/providers/deploy не использовались.

### GO-311/D/042 приняты / GO-312 начинается

- #71 codebb2455a026f493460f3edfd33f62cc3dba8d7252/treeee44ccf27eab3f385b12ce22c69ad66526f16454: Public40661335744/MariaDB40661335605 fully pass. Schema job122724824015:4 required parents no skips, actual CLI/source fresh14/16/39/round-trip/pristine refusal/late DDL+seed faults/unknown COMMIT+retry/ownership; весь старый regression pass. D/100/042 и critical019 restore закрыты, UI/real-data отдельны. #68/#69 уже доставлены пользователем в main55e4d80 по fresh PR collection, #70/#71 open.
- Остаток045 до реализации разделён312 job claim/fenced mutation →313 budget ledger/joint crash gate; ветка codex/GO-312-scheduler-job-lease от принятой #71. Pipeline/provider activation и реальные secrets не разрешены.

Проверено слияние #70/#71: фактический main5f76bd79b31d1be94b51bf413d3651bf35b1c5e0, treeee44ccf27eab3f385b12ce22c69ad66526f16454 точно совпадает с принятой вершиной #71/bb2455a. Ветка GO-312 начинается от этой проверенной published вершины; повторное подтверждение merge не требуется.

GO-312 до SQL приёмки: source142 selection/finish fixtures и23044 JSON scalar vectors,1001 BIGINT score comparisons локально pass; gofmt/vet/race/tagged/builds/web/source/history/schema/actionlint и четыре negative pre-connect guards обоих SQL parents выполнены. Новые SQL parents пока не приняты: required source full-row differential и ownership/two-connection/late-fault/SIGKILL recovery будут выполнены только в guarded disposable GitHub CI.

Первый SQL прогон #72/795bfea выявил clock skew фикстуры (+1ms future boundary стал due за время seed), production code не менялся. Source SQL сравнение теперь фиксирует timestamp только своей одно­соединительной CI-session; отдельный ownership/crash parent сохраняет actual clocks и два соединения. Required scheduler-job-acceptance вынесен в самостоятельную disposable job перед общим API/browser gate, чтобы независимая проверка не ждала historical restore. Приёмка до нового полного CI остаётся открытой.

GO-312 second CI: independent scheduler job dd4b6c8/122759863733 подтвердил все ownership/concurrency/rollback/SIGKILL subcases. Source fixture с lone UTF16 не представима в native MariaDB JSON CHECK; исходный codec проверяется отдельно (143 source fixtures), SQL сравнивает142 представимых fixtures и отдельно доказывает constraint refusal/no rows и rollback Finish override без потери lease. Нативный CHECK и production code не меняются. Полная приёмка ожидает новый CI.

GO-312/#72/a2683babbdbe2666a3be218f83e06656b6ab36d4/treed76ed8edf0bce85c169586b54961239d30f660f2: required scheduler-job-acceptance122762089873 pass —143 source subcases (142 representable+1 native refusal),5 ownership/rollback/SIGKILL cases, zero skips. Public40672364577/MariaDB40672364682 full regression ещё выполняются. GO-313 начинает изолированную ветку от этой published вершины; готовность #72 будет отмечена только после полного CI.

GO-312/#72/a2683ba принята полностью: Public40672364577/MariaDB40672364682 success, required122762089873 parents/subcases без skips, весь old SQL/API/browser/restore/bootstrap regression pass. PR переведён ready, auto merge не выполнялся. Следующая GO-313 ветка сохраняет эту ancestry. Оставшиеся critical leaves после312: GO-030/040 (внешние блокировки) и GO-313; parent045 ещё открыта.

GO-313 implementation: opaque one-attempt-per-lease, global MSK day/month reserve/known-vs-unknown settle и joint attempt+job receipt, без schema/provider activation. Source38 dispatcher+billing vectors локально pass. New required SQL source/ownership parents добавлены в scheduler-job-acceptance после312; tests охватывают actual two-connection ceiling, generation rotation, lease deadline, late fault/joint rollback, unknown actual COMMIT и SIGKILL/conservative recovery. Положительные SQL проверки до CI не объявляются успешными.

Проверено слияние #72: main9e6b42ce9e738fc52e3245332f2c4f6560d4db4f, treed76ed8edf0bce85c169586b54961239d30f660f2 точно совпадает с принятой a2683ba. GO-313/#73/bb4be571749a5f01134afb33a12c6505450d688c/tree2c64e8d9dc52ca678469ffa8ba3ff7ba88790257 принят: Public40691942853/MariaDB40691943019 success; required122828698691 —38 source/10 financial ownership и старые143/5 job cases без skips, весь old regression pass. PR ready, auto merge не выполнялся. Durable-core045 завершён, parent027/pipeline046 не закрыты.

После313 начата GO-314 API coverage audit от этой verified published вершины: инвентарь текущего React transport и evidence/gaps review matrix, без runtime изменений. Исправлен смысл прежнего счётчика:030/040 — две внешние leaf-блокировки прежней очереди, но не весь critical остаток спринта. Открытые API/read/authorization/import/pipeline/release части остаются в parent задачах и входят в последующий рабочий план.

GO-314 audit implementation:61 current frontend/web TSX/TS,129 direct transport/form/link sites (mutate aliases включены),7 URL bindings и1 download forwarding;26 reviewed families с конкретными Go/test/required SQL refs и открытыми gaps. Script не обращается к сети/SQL. Проверены stale method snapshot, missing test symbol и router hash drift — каждый отказал. Исторический server inventory не выдаётся за полный method-aware golden; API runtime/Go code diff относительно #73/bb4be57 пуст. Следующий code scope — source catalog read parity GO-009 по выявленному gap, отдельная задача до реализации.

GO-315 начинается от published #74/e8694d85f515936924788b4a39e54f4619f56a49. Public40696413474, включая новый AST audit, полностью pass; MariaDB40696413830 проверяет неизменный runtime. Catalog gap до code изменений разделён на315 pure projections →316 pure search →317 SQL/API activation. Причина — source normalizeCatalog на каждом GET против raw Go parse; нужны omitted/null/state/default/order proofs. Прежний readonly identity invariant сохраняется.

GO-314/#74/e8694d85f515936924788b4a39e54f4619f56a49/treecdb9c5856122d4625e306d84b09fc0f7cba26d53 принята: Public40696413474/MariaDB40696413830 fully pass, AST gate и весь old runtime regression успешны. Узкий audit завершён, Go009/011/002 read/full golden части открыты.

GO-315 implementation:1156 actual unchanged readCompactEntities/readCompanyDetail fixtures с явными query-result stubs passed. Это pure field projection, не SQL/bootstrap acceptance. Object builders отложили stringify: source job.title1e400 truthy, прежний entity JSON view превращал его в null до read projection. Existing entity/unsigned plan+SQL plan/stored-source gates прошли -race; runtime catalog Repository не переключён. Следующие316 search и317 actual readonly SQL/API остаются открытыми.

GO-315 review correction: при отказе от раннего stringify требуется сохранить не только Infinity, но и numeric OwnPropertyKeys после embedded ratingInputs spread. Добавлены immutable container ordering и источник embedded numeric-spread;1180 projection frames pass, отрицательный запуск без ordering fix действительно падает на новом fixture. Old entity/unsigned plan+SQL plan/stored-source gates вновь прошли -race. Исправление публикуется в315 до возврата к сохранённой316; исходные stashes пользователя не удалялись.

GO-316 продолжается от исправленной published #75/76bd7d9f998a6c6bb8c8827657bf685aede40079; Public40704508982/MariaDB40704508987 выполняются. Изначальные2447 source search frames проходили и на5f5171a; после rebase они повторяются на own-key ordering fix. Pure search использует тот же ordered catalog object, source Unicode17 lower/UTF16 includes и JavaScript coercions.2447 unchanged search fixtures locally value/error match; KMP не допускает half-unit byte matches и квадратичного поиска. Runtime catalog/search Repository до317 не переключается.

GO-316 expanded source corpus:2551 frames после отдельного UTF16 boundary case (Samaritan U0800 + ASCII и NUL query). Проверяется отличие unit matching от возможного ложного byte-substring совпадения. Search и1180 projection frames повторяются на76bd7d9 ordering fix; прежний316 stash сохранён как техническая страховка, чужие stashes не менялись.

GO-316 checks:2551 search+1180 projection fixtures passed -race on76bd7d9 foundation. Negative probe with byte-substring implementation failed exactly on UTF16 boundary, then original source restored. gofmt/vet/default race/tagged/builds/Go1.23/API-audit/source gates pass; schema/DB/provider runtime не меняется.

GO-315/#75/76bd7d9f998a6c6bb8c8827657bf685aede40079/tree545b08b6295f51899665d002f758ba93a575b78a принята: Public40704508982/MariaDB40704508987 fully success,1180 source frames+old regressions pass; PR ready, auto merge не выполнялся. GO-316 построена от этой исправленной canonical published вершины; сохранённая working copy восстановлена, единственный docs append конфликт разрешён с сохранением обоих task records.

GO-316/#76 принята: code0876f4da5c7c611a99fbc16c78fa47b819c8436b/tree9e3f0d7b12f72b29bd786f8296743dd421402f44, Public40708192883/MariaDB40708192913 fully pass;2551 search+1180 projection source frames, old regression pass. GO-317 начинается от этой canonical вершины. Fresh PR collection подтвердил пользовательские merge #73/#74; #75/#76 пока open.

GO-317 implementation: runtime normalized RawMessage DTO/search, WHATWG query/UTF16 cap/JS numeric ID fallback и method-aware session guard. Source router/actual SQLite oracle даёт2592 responses/12 frames, source total_changes неизменен; required catalog-read-acceptance добавлен как prerequisite основного MariaDB/API/browser job. Тесты actual SELECT-only/full72-table immutability/error/missing-ID/duplicate/concurrent snapshot пока ожидают positive CI. Локально vet/default+tagged race/builds/Go1.23/web/API audit/source/history/actionlint/manifest pass,4 unsafe target/nonce pre-connect отказа pass; local MariaDB tests compile/skip, не positive proof. Initial POST contract test до исправления действительно дал503 вместо source405.

GO-317 initial33072978120eddc1c9b8148ae1caf5dec4700cec/treea327407753910b5448359f3b4d9705c96a59fe54, PR #77: required catalog job122896849879 подтвердил все2592 actual SQLite→readonly MariaDB responses/72-table immutability и concurrent snapshot. Общий job не принят: два negative fixtures ошибочно ожидали вставку дубликатов, уже запрещённую UNIQUE HASH indexes baseline. Fixture исправляется на обязательный1062, неизменные строки и успешный последующий read; схема не меняется. Старый комментарий об отсутствии name uniqueness исправлен.

GO-318 начата от исправленной317/#77/40a026f5eb32f4d6c1e3d4c31123c73ee2076f4a/treec3a5bce39bac859bceddaebd32749ee0a13a79d1. Required catalog job122898776673 pass:2592 source HTTP responses/12 frames, all72-table immutability/SELECT-only/UNIQUE1062/missing-ID/private errors и actual snapshot concurrency; zero skips. Полный CI317 ещё выполняется.

GO-318: actual unchanged shared/publication-period.ts corpus27375 frames до correction дал569 расхождений, после correction pass-race. Runtime Go predicate теперь отвергает Date.UTC years0–99 и использует exact ECMAScript whitespace (FEFF включён,0085 нет). All10k four-digit years Jan1/Feb29, calendar/leap/month clamps,13 Moscow day/time change samples, archive/all/fallback tested. Ordinary race/vet/tagged/builds/Go1.23/API-audit/source/history/actionlint pass. Ни SQL, ни legacy/frontend source не менялись; full GO-011 отдельно.

GO-317/#77/40a026f5eb32f4d6c1e3d4c31123c73ee2076f4a/treec3a5bce39bac859bceddaebd32749ee0a13a79d1 полностью принята: Public40711808887/MariaDB40711808823 fully success. Required122898776673:3 parents,12 source frames/2592 HTTP responses, native UNIQUE1062/SELECT-only1142/whole72-table immutability/missing-ID/private failures/concurrent snapshot, zero skips; старый API/browser/restore/bootstrap regression pass. GO-318/#78/e6499be5a0d86a069ccbaf5386937e61516d57ec/tree6025110e88f00212359b9cdd3403c19ee45e7c71 проходит Public40713039740/MariaDB40713039811. Независимая319 начинается от этой canonical вершины.

GO-319 implementation: unchanged readVacancyRoles164 explicit read-set frames, no SQL/bootstrap proof. Initial129 mismatches (128 sibling permutations + SQL child-count preservation); после cached canonical Russian keys/sortOrder/ID tie и сохранения SQL counters exact DTO match-race.32 forests/arbitrary flat order и old corrupt graph tests pass. Keys reuse accepted ADR-023; new Unicode decision не требуется. SQL/API role/source joins и fullGO011 отдельно.

GO-319 local verification:164 source frames -race, ordinary/tagged race/vet/builds/Go1.23/API audit/history/source/actionlint pass. Existing HTTP role fixture initially omitted SQL childCount and returned an impossible read set; fixture now supplies the actual parent count, expected leaves contract unchanged. No role SQL query or HTTP route change.

GO-318/#78/e6499be5a0d86a069ccbaf5386937e61516d57ec/tree6025110e88f00212359b9cdd3403c19ee45e7c71 принята: Public40713039740/MariaDB40713039811 fully success,27375 source frames и old regression pass. GO-319/#79/edf0956ee0e47a6412ed6646815ec27483d8a18b/tree5f3392da73469babfdced35bde2c07bcfc1e861c принята: Public40714714847/MariaDB40714714685 fully success,164 source frames и full SQL/API/browser/restore/bootstrap regression pass. GO-320 medium GO008 начинает пользовательскую новую очередь от этой canonical вершины; external live gate не закрывается. Mainbcfe8dd25ea0ffeaf4f08cf5945ebcad5a365bce/tree545b08b6295f51899665d002f758ba93a575b78a точно содержит merged #75; #76–79 в свежей collection ещё open.

GO-320 local proof: TestAuthHTTPSProxySessionFlow/3 accounts pass-race/Go1.23, trusted certificate/virtual public example.com with strict2-port loopback dialer, actual TLS→HTTP proxy, cookie/roles/crossOrigin/tamper/signed expiry/relogin/logout. Начальный loopback-URL negative test обнаружил не defect приложения, а Secure-cookie localhost exception в новом Go jar; public hostname с проверяемым SAN устраняет ложную предпосылку и проходит обе Go версии. Ordinary/tagged race/vet/builds/actionlint/API audit/source/history/doc links pass. Runtime/proxy config/SQL не менялись; внешний staging medium parent008 открыт.

GO-320 опубликована5dba544df565ba5da68b4b3ebdd9106e426e9e41/tree4cb3b1d04096d305f55333fbe64b9437ac9ae233, CI выполняется; новый child321 начинает independent medium019 от canonical вершины. Runtime320 unchanged, local actual TLS/race/Go1.23 proof принят; external live auth gate открыт.

GO-321 implementation: отдельный fresh-schema/nonce/loopback/expected72-table fixture guard, actual writable Go/SPA/backup/core и6 desktop/mobile Playwright scenarios. Две подтверждённые restore операции обязаны rotate generation, validate/denied POST preserve; UI cancel не отправляет restore, original export снимается до synthetic favorite mutation. Invalid/oversize/shape/role cases и reload проверяются без mocks/external traffic/artifact upload. Локальный native parent compile/skip, typed TS/discovery6 — не positive acceptance; required backup-browser-acceptance добавлен перед main SQL/API/browser gate. Frontend/SQL engine не менялись.

GO-320/#80/5dba544df565ba5da68b4b3ebdd9106e426e9e41/tree4cb3b1d04096d305f55333fbe64b9437ac9ae233 принята: Public40743050421/MariaDB40743050396 fully success, new3-role TLS protocol proof и весь old regression pass. Parent008 external live medium gate остаётся открытым.

GO-321 local checks: TS strict check/selective config и6 Playwright test discovery pass (не browser execution), default/tagged race/vet/builds/Go1.23 compile и web build pass, source/schema/smoke/actionlint/API audit/history pass.4 remote/root/schema/missing nonce pre-connect refusals pass. Positive MariaDB/browser только CI.

GO-321 initialeaeb8708f80084da6ef90bf4c8a4db10cf7eab61/treeb467536d87d0081c0dfacf21829e6b391c022d1a, PR #81: first required browser job123016494194 не принят — fixture нажимал Settings menu trigger, но не выбирал General.2 nonadmin scenarios прошли HTTP403;4 admin scenarios timeout до backup controls. Исправляется только тестовая навигация, добавляются явные settings/general/data-admin assertions, product не меняется.

GO-322 pure settings начинается от321 fix7935f728d2c7742f087b57d786f6bcd092ae034b/tree19b7df1faea738f47e3791c0e02b13eb57aed337, product runtime321 unchanged; UI321 repeat CI выполняется. Новая задача до code: reusable ordered stored settings/read/serialized validated update, existing078 validation и trusted v39 source defaults, no SQL/API/provider activation. Durable setting CAS и commands отдельно.

GO-321/#81/7935f728d2c7742f087b57d786f6bcd092ae034b/tree19b7df1faea738f47e3791c0e02b13eb57aed337 полностью принята: Public40746549514/MariaDB40746548655 fully success. Required123021742469 —6 desktop/mobile actual UI+SQL,2 confirmed restores, validate/denied generation unchanged, successful generation rotations, zero skips. Old schema/business/restore/bootstrap/task/browser regression pass. Medium019 functional clause закрыта, real rehearsal/cutover отдельно.

GO-322 implementation:2214 unchanged strict source function-body read/validated-save frames pass-race, existing078 validation reused; defaults from trusted v39.RestorePolicy, no restore safety reset. Storage/private state preserved, PublicJSON and automatic MarshalJSON redact owner; byte ownership/old-view immutability unit pass. Negative redaction removal caused source mismatches and original file restored. Default/tagged race/vet/builds/Go1.23/source078/API-audit/history/source/actionlint pass. No Runtime/SQL/API activation. Source first-vs-last duplicate revision/CAS/shared favorites SQL proof remains next phase.

GO-322 опубликована4ac60e3df561826b42dd0d3f8a857bde154593df/tree607fad7ad7b3721e4e64b2f8c6cbe883495b98a7, PR #82, CI выполняется. GO-323 начинает source SQLite/MariaDB settings CAS отдельной веткой от этой canonical вершины;322 локально source2214/ownership/negative/race checks pass. Native schema DEFAULT quote corruption замечена read-only, SQL phase обязан явно задавать valid business JSON, без schema changes.

GO-322/#82/4ac60e3df561826b42dd0d3f8a857bde154593df/tree607fad7ad7b3721e4e64b2f8c6cbe883495b98a7 принята: Public40749561114/MariaDB40749561105 fully success, source2214/private redaction/old regression pass; PR ready. GO-323 реализует отдельный SQL слой: readonly snapshot, original CAS/generation, shared favorites/Firecrawl revision, unknown COMMIT refusal.2324 actual JSON1 comparisons локально pass, source42 full-row transitions и native42/6/4 scenarios требуют positive disposable CI. API/commands/providers не активируются.

Проверено пользовательское слияние #79–82: mainc506b2b0fca99d648defd7457259498ea1960879/tree607fad7ad7b3721e4e64b2f8c6cbe883495b98a7 совпадает с принятой GO-322; ancestry включает4ac60e3. GO-323 продолжает эту foundation. Local default/tagged race/vet/build/Go1.23, source2214+CAS2324, four unsafe pre-connect refusals, schema/source/history/API audit/actionlint/manifest pass; positive SQL пока ожидается.

GO-323 initiale62dc022/PR83: required123062240687 подтвердил42 source full-row и6 ownership,3/4 fault cases. Late-fault case не внедрил ошибку: driver ExecContext вернул ErrSkip, actual SQL прошёл через prepared statement. Test wrapper теперь перехватывает ConnPrepareContext/StmtExecContext, считает только actual executions, требует fired=true. Product SQL неизменён; повторная positive CI обязательна.

GO-324 выделена до HTTP code: узкий PUT settings с source guard/error/body контрактом и native SQL evidence.323 published e62dc0225053f6152035cee89624b391646ecd2a, PR83 проходит CI. Dashboard/read projection, commands/dry-run/source scope — отдельные порции после324; не добавлять выдуманный GET/settings или success для неготового pipeline.

GO-324 implementation: source full router/SQLite70 responses, native status/JSON/full-row comparison +4 target-only duplicate/Sec-Fetch guards, no worker/provider wiring. Read body byte32KiB/BOM/errors and session/admin/storage/Origin precedence preserved; source malformed body500 and wrong method after valid body405 сознательно сохранены. Local default/tagged race/vet/build/Go1.23/web/source/history/API audit/actionlint pass; required native HTTP pending.323 required123063684381 passed42/6/4 with zero skips; full Public40758482439/MariaDB40758482914 ещё выполняются.

GO-325 выделена до dashboard code: pure safe attempt projection из source schedulerAttemptDetails и выбранных SQL columns, затем326 actual readonly dashboard/GET.325 не считает mocks/explicit read sets SQL acceptance.324 published57445b133015f3eb2053e7871973a87a9d72ae8c/PR84: required123066912323 подтвердил70 full-source HTTP responses+4 target-only guards,42/6/4 previous SQL cases, zero skips; full CI выполняется.323/#83/6609d2d полностью принята: Public40758482439/MariaDB40758482914 success, PR ready.

GO-325 implementation:368 unchanged schedulerAttemptDetails frames pass-race, bounded outcomes/last duplicate/safe roleIds/nextStage/truncated JSON, target allowlist rejects unreviewed SQL columns. Negative result_json redaction removal действительно вызывает mismatch; исходник восстановлен. No SQL/API activation, actual SELECT limits/join/snapshot относятся к326.

GO-111: по поручению пользователя во второй спринт добавлено планирование перевода старых комментариев, низкий приоритет; AGENTS требует русский язык для новых комментариев. Старый код не переводился, реализация111 не начата. Это отдельная docs-порция без runtime изменений.

GO-324/#84/57445b133015f3eb2053e7871973a87a9d72ae8c/tree10e8dc011c98cd23d53936f5cfcf555df4d4de9a принята: Public40759399990/MariaDB40759399920 success; required123066912323 HTTP70+4/SQL42+6+4, zero skips и full old regression pass. PR ready. Fresh collection подтверждает пользовательский merge83. GO-325/#85/881e8abb327b8096e715472fd1f42b92cb7d522a source368 проверяется CI.326 выделена до SQL/API implementation: readonly dashboard с реальным source SQL oracle, без workers/providers/commands.

GO-325/#85/881e8abb327b8096e715472fd1f42b92cb7d522a/tree0d3d30a4eca1a2c231d21ab96653dcc314e64ea2 принята: Public40760822967/MariaDB40760822814 success, source368 и old regression pass; PR ready. GO-111 policy planning отдельно #86/611d6dd359bdae3625757d3799c39c1c066865a8, старые комментарии не переводились.326 продолжает от этой published вершины, working stash сохранён. Dashboard source36/144 + pureSUM260 и5 native cases реализованы; positive native CI пока ожидается. Scope — только readonly API, без pipeline/commands/providers.

GO-326 local verification: default/tagged race/vet/build/Go1.23, sourceSUM260, source36/144, API audit/source/history/schema/smoke/actionlint/manifest/web build и4 unsafe target/nonce pre-connect отказа pass. Local native tests только compile/skip. Native gate обязан подтвердить36 dashboard frames/144 HTTP+5 snapshot/fault cases и все old settings SQL/API gates; schema/provider/worker не менялись. Финансовое представление использует source compensated SUM, запись/резервирование313 неизменны.

GO-326 initialafc81f3/PR87: required123087499911 подтвердил все5 snapshot/fault случаев и old settings gates; source comparison не принят:10 fixtures содержали timestamp ties в history, где исходный ORDER BY не задаёт порядок. Comparator теперь проверяет DESC между timestamp groups и полные записи внутри ties как множество по ID; runtime tie-break не вводится. Другие DTO поля сравниваются строго; новая диагностика сообщает лишь имена различающихся полей. Нужен повтор positive CI.

GO-327 выделена до переноса файлов: docs/dev и docs/user с актуализацией ссылок, генераторов и UI документации. Ветка от published326 fix499044787650f7bb6101da6ac15360ea5ccd10cb; положительная приёмка326 ещё ожидается. Новые комментарии только на русском.

GO-326/#87/499044787650f7bb6101da6ac15360ea5ccd10cb/tree353083e4b10b4a6f76173e90b8c52d5936f74734 принята: Public40767577819/MariaDB40767577833 success, source36/144+5 snapshots и old regression pass; PR ready. GO-327: moved docs dev/user, обновлены relative/root/codegen/CI paths;293 local links и8 app source HTTP/HTML/download sections pass, negative broken-file/unregistered-section probes реально fail и восстановлены. Старый maintenance сохранён dev/archive, действующие dev/user инструкции разделены; отсутствовавший changelog восстановлен из committed generated. Frozen v39 schema fallback заменяет отсутствующий drizzle для генерации, backup-schema.json/v39 schema byte-unchanged. Go runtime diff относительно326 пуст.


GO-327/#88/22a407e5d4b4ecb7cd33e2761fb5045c82014c78/tree f2d96e2d90dcecf8b26cea5f3e9d7a8aa4122957 принята: Public40779666736/MariaDB40779666781 success. Проверено пользовательское слияние #87/#88: main3a99919a656e283d710e2d45aef2e5dbeefd2f25 имеет точно то же дерево. GO-328 выделена до реализации: source schedulerRoute фиксирует candidateDb generation до readClassificationInput, текущий Go Save после body; требуется source/native regression с восстановленной прежней revision. Ветка codex/GO-328-settings-body-generation от проверенного main; только текущая GO-021, новые комментарии на русском.


GO-328 local proof: source8 подтвердил precedence restored/body/method. Регрессия до исправления получила200 вместо409 и413 вместо500; после PrepareSave→opaque bound operation default/tagged race/vet/build/Go1.23/web/docs/API audit/actionlint успешны. Native8+2 compile/skip локально, required CI parent/count checks добавлены без допуска skips; positive MariaDB ожидается. Схема/provider execution не меняются. После приёмки328 продолжить текущую GO-021 командами retry/cancel, без запуска worker.


GO-328/#89/5f780e77bf99d64424ebe510621d2a12945aff46/tree d001767dcad0504291e7d63262b19bb620a6c80a принята: Public40831785848/MariaDB40831785899 success, все3+13 jobs, required8 body-generation+2 capture/cancellation cases без skips и old regression pass. PR ready; automatic merge не выполнялся. GO-329 продолжает от этой проверенной вершины.


GO-329 implementation: source43/45 HTTP, native source43+fault4 required counts без skips. Generation фиксируется до body, SQL clock как core, conditional UPDATE только6 полей, matched rows обеспечены existing clientFoundRows=true; финансовый журнал/lease/payload не меняются. Local default/tagged race/vet/build/Go1.23/web/source/docs/API audit/actionlint/history pass. Positive MariaDB ожидается; полная GO-021 остаётся открытой до remaining commands/read/stage gates.


GO-329/#90/c64e5933ae5688c1ef73dd89a821cf798de92ffa/tree9eb0fba7c7994a51b7a8a198c47a495a6c8e4e1f принята: Public40833105767/MariaDB40833105821 success, source43/45 HTTP+fault4 без skips, весь old regression pass. Пользователь поручил продолжать автономно, кроме критических ситуаций. GO-330 заранее выделена: readonly queue, поиск/пагинация/query parity; ветка от принятой329, без ожидания merge. Общий порядок GO-021 →010→012→013→017 сохраняется.


GO-330 local proof:104 unchanged queue router/SQLite frames, WHATWG last-query decoding, JS Number vs SQL affinity, UTF16 trim/lower/search и большие owner ID. Readonly RR count/page/имена/title, payload/service columns исключены; native readonly104+snapshot/fault4 required без skips, positive CI ожидается. Default/tagged race/vet/build/Go1.23/web/docs/API audit/actionlint/history pass. Далее независимое readonly чтение отдельного scheduler run; команды запуска/провайдеры не разрешаются этой порцией.


GO-330 опубликована PR #91/e857a674d0c6dc20c8e6b62c75c09fbc64d4506e, CI выполняется. GO-331 выделена до реализации: только GET отдельного сохранённого run, source requestId использует первое значение query-дубликата, в отличие от queue Object.fromEntries(last). POST /run и execution не включать. Ветка codex/GO-331-scheduler-run-read от опубликованной330; родительскую приёмку проверить до ready.


GO-330/#91/e857a674d0c6dc20c8e6b62c75c09fbc64d4506e/tree950f183e1af594b4983a09d67aecb75ed8241860 принята: Public40838213182/MariaDB40838213389 success, source104+snapshot/fault4 zero skips (job123327959102), весь regression pass. PR ready, automatic merge не выполнялся. GO-331 продолжает от этой проверенной вершины.


GO-331 local proof:32 source run HTTP/SQLite и first requestId interop проходят, GET-only factory/read-only RR/hash/DTO реализованы. Native32+fault5 required, включая POST remains503/no-storage. Default/tagged race/vet/build/Go1.23/web/docs/API audit/actionlint/history pass; positive MariaDB ожидается. При исследовании следующего /issues установлено: source GET пишет parser_error_history, поэтому его нельзя объявлять SELECT-only. Следующий prerequisite выделять как отдельную проверяемую write порцию истории, без реальных ключей/провайдеров.


GO-331 опубликована PR #92/b1d2e982f44d03ec6fc45a275fc96a604b1470c8, CI выполняется. GO-332 заранее выделена как prerequisite /issues: дневные пики/наблюдения на существующей parser_error_daily (уже baseline72 и exported v39), без новой схемы и standalone HTTP.30 дней — окно чтения, без удаления старой истории. Ветка от published331; продолжать автономно в текущей GO-021, providers/deploy/real data не включать.


GO-331/#92/b1d2e982f44d03ec6fc45a275fc96a604b1470c8/treee1fc1098caed3b19458a6b929d05233b8d52dd49 принята: Public40839580472/MariaDB40839580437 success, source32+fault5 zero skips, весь regression pass; POST remains503/no-storage подтверждён. PR ready, automatic merge не выполнялся. GO-332 продолжает от проверенной вершины.


GO-332 local proof: source19/calendars/Number storage interop pass; native source19+ownership3+fault4 compile/skip локально, required CI без skips. Owner/day upsert сохраняет independent MAX peaks/MIN-MAX timestamps; readonly30-day gaps/averages, old rows retained. Schema/React/HTTP activation не менялись. Default/tagged race/vet/build/Go1.23/web/docs/API audit/actionlint/history pass. Далее source issue scope/grouping и GET /issues integration отдельными порциями; нельзя подменять SQLite JSON1/date/BINARY predicates SQL-collation approximation.


GO-334 выделена до реализации по уточнению пользователя10.3.32-MariaDB. Ветка codex/GO-334-mariadb-103-compat от принятой332/283c9a1; GO-333 local work сохранена отдельно и проверена без потери. Выявлены runtime JSON_TABLE, schema-init floor10.11, test JSON_ARRAYAGG, metadata IGNORED/DESC и database COMMENT CI guard; positive10.3.32 proof ещё не получен.

GO-334: предварительный локальный эксперимент SQL/metadata прошёл проверки; положительную совместимость10.3.32 это не доказывает и итоговый code path его не включает. Проверено main5e2df036e987dff625e166c81961de4b3a10d1d8/tree51e010285020db014045e04c6497f96ef42a3994: пользователь слил #93, дерево равно базе ветки283c9a1. GO-333 сохранена отдельно до завершения совместимости.

GO-334: первичный реальный10.3.32/run40858223594/job123394524459 показал ERROR1170 на agency_identity_agency_name_unique, первом из23 уникальных LONGTEXT HASH-индексов. Сохранить10.11+; пробное понижение floor/SQL/metadata обходы отозваны, т.к. не решают schema blocker. Реализация меняется на ранний общий version guard и required negative10.3.32 плюс прежний полный10.11 regression. Deployment на установленную10.3.32 блокируется, без обновления сервера/данных; независимая текущая GO-021 и очередь не блокируются. GO-333 сохранена отдельно, продолжить после GO-334.

GO-334 итоговая реализация: local default/mariadb race, vet, build, Go1.23, web/docs313 links, API audit, actionlint и history pass. Native negative10.3.32 локально compile/skip; требуется реальный required CI без skips. Минимум10.11 сохранён; задача — доказанная диагностика и безопасный ранний отказ, а не поддержка10.3.32.

GO-334 принята как проверка/ранний отказ: PR #94, code commit973d60bc7a352ad27651d7a08acfa9d86b893faf/tree87167a4770870ce2bdc0aa5c9d5a729472105be1: Public40859355281 (3 jobs), полный MariaDB10.11.19/run40859355125 (13 jobs), negative exact10.3.32/run40859355106/job123398331854 (1 job) — success. Нулевые skips в required native gates. Negative guard доказывает несовместимость и ранний отказ, не поддержку10.3.32. Исходная схема/v39/23 UNIQUE сохранены. Адаптация схемы под10.3 или обновление реального сервера не выполнялись. Следующая независимая задача — сохранённая GO-333; исправить путь policy artifact до публикации, общий порядок GO-021→010→012→013→017 прежний.

GO-336 выделена по возобновлённому critical поручению: русский порядок assigned roles GO-011; отдельная ветка codex/GO-336-vacancy-role-order от принятой334/1fbd4d1. Source167 до исправления воспроизводит byte-order ошибку; после exact DTO/owned input pass. Native9 readonly/user/72-table proof required, positive CI ожидается. GO-335 local docs2878f59 и GO-333 backup сохранены отдельно; server setup на паузе.

GO-336/#95/9ed4ce1b96a403a9f831ac19d45c79850e492d51/tree8770bae50e9b27a21ad8b53ecc15d658d1a24d15 принята: Public40872977177/MariaDB40872977198/negative10.3.32 40872977191 success, source167/native9 job123444507918 без skips. PR ready, automatic merge нет. GO-337 реализует следующий critical read gap: нормализованный entity directory, source180 regression подтверждён до/после; native30 required, positive CI ожидается.

GO-337 принята: PR #96, code539f8e729c79ef81328e9107d366cc6c2ad38e1d/tree8fbe299ae291b53d9d92ce062e8340d5171f42ac: Public40875362030/MariaDB40875361741/negative10.3.32 40875361912 success. Source180 и required native30 (job123452669327) без skips, весь прежний regression pass. Следующая critical GO-338 — source NOCASE порядок строк с raw SQL NULL/empty dates, предварительное отличие SQLite уже воспроизведено. Полная GO-011 остаётся открытой; средние333/335 сохранены отдельно. Порядок merge: #95 → #96 с проверкой/retarget base на main после родителя; automatic merge запрещён.

Проверка пользовательского merge #95/#96: main b3fa1c60/tree8770bae5 имеет336, но337 осталась в старой feature-ветке. Recovery #97 codex/GO-337-main-delivery→main использует точную79851d7/treea4dccbe2, все final CI40876765998/40876765908/40876765945 success.338 ветка codex/GO-338-vacancy-row-order от79851d7, следующий leaf→main сохраняет ancestry. Source630 после meaningful pre-fix regression pass; native126 required, positive CI ожидается. Сервер не настраивается.

GO-338 принята: PR #98, code91208039ef1b66a2d2f39a0e5104bde9082bc896/tree41fa1e6d55faf22ae15524739204616d96544604: Public40884468004/MariaDB40884468494/negative10.3.32 40884468012 success. Required source105×6=630 full DTO comparisons и native126 (job123484362799) без skips; весь прежний regression pass. Пользователь слил recovery #97; main cd69e9b5181ab48a18b405521357c9147b84e263/treea4dccbe26e58ce93a20fa7c1b7332a8f41f080c8 точно совпадает с принятой337. Следующая critical339 — source порядок applicationSummary по первому появлению имён до period, defect воспроизведён на actual readCompactVacancyList без GET writes. GO-011 целиком остаётся открытой; real server/SSH не трогать, medium333/335 сохранены отдельно.

GO-339 codex/GO-339-application-summary-order начинается от проверенной338/274c326: main ce475316/tree739564af точно совпадает, final40886430903/40886430924/40886431100 success. Source57×3×6=1026 meaningful regression→pass, required native162 и72-table read-only proof ждут CI. Delivery→main с сохранением ancestry; automatic merge запрещён. Сервер/SSH на паузе.

GO-339 принята: PR #99, codefc4f0b6ea0e79b39219316311f6f285bc6747971/treec2ab5d13b4d0b3a7c6ac70600a3fb84719b9a534: Public40894290467/MariaDB40894290169/negative10.3.32 40894290637 success. Required source1026 и native162 (job123519067043) без skips; весь прежний regression pass. Local default/tagged race/vet/build/Go1.23/web/docs320 links/API audit/actionlint/history pass. Следующая critical340 — source Telegram display order, actual57×3-view corpus подготовлен вне репозитория; provenance/CAS порядок сохраняется. Delivery→main, без automatic merge.

GO-340 codex/GO-340-telegram-source-order от принятой339/5064e26: source171 до/после regression pass, native81 required в disposable CI. Detail DTO сортируется после revision только публичным wrapper; shared readDetailTx и mutation fallback не меняются. Следующий leaf→main сохраняет ancestry, без automatic merge.

GO-340 принята: PR #100, code43fc14b745bb1a6762bcd99daef0d88f939057d5/tree74f462c9d8bf87cd02ef78a338c5ba3d8efe02b1: Public40896436711/MariaDB40896436757/negative10.3.32 40896436820 success. Required source171 и native81 (job123526700623) без skips; весь прежний regression pass. Local default/tagged race/vet/build/Go1.23/web/docs325 links/API audit/actionlint/history pass.339 принята отдельно в #99, leaf100→main включает обе порции с ancestry; automatic merge нет. Следующая critical341 — GET роли/descendant negative IDs, source/Go расхождения подтверждены, код ещё не изменён. Сервер/SSH остаются на паузе;333/335 сохранены отдельно.

GO-341 codex/GO-341-role-read-contract от проверенной340/c0403e6; main9e4eff7/treeb2e1db97 совпадает, final40897942653/40897942787/40897942689 pass. Source166 actual router/SQLite meaningful negative regression→pass; required native166 tree/descendants/IDs/access/noDB/72-table immutability ждёт CI. Bounded GET paths, без mutations/общей method policy; следующий leaf→main сохраняет ancestry.

GO-341 принята: PR #101, code66e63c58877957c17874247d98a3e17a53aff946/treef901b03d762b4817cf76a7e40439958fcb2887a7: Public40903045398/MariaDB40903045429/negative10.3.32 40903045292 success. Required source166 и native166 (job123550239718) без skips; весь прежний regression pass. Local default/tagged race/vet/build/Go1.23/web/docs333 links/API audit/actionlint/history pass. Следующая critical342 — period query/semicolon и auth/path/DB/period precedence компактного списка; source176 corpus подготовлен вне репозитория, implementation ещё не начата. Delivery→main с ancestry, automatic merge нет.

GO-342 codex/GO-342-vacancy-list-query от принятой341/1c4531f: source176 HTTP status/query dispatch до/после pass. Required native176 — actual handler+SELECT-only Repository с test clock, полные DTO на двух fresh/archive строках,3-user flags/general summary,72-table immutability; positive CI ожидается. Следующий leaf→main сохраняет ancestry, automatic merge нет.

GO-342 принята: PR #102, codeb231685943e582020e1fa4d020e41b7e059f9cea/tree7921e6c5e60e651107eeaf9bbc0a449491d2d050: Public40905143239/MariaDB40905142983/negative10.3.32 40905143180 success. Required source176 и native176 (job123557744142) без skips; весь прежний regression pass. Local default/tagged race/vet/build/Go1.23/web/docs340 links/API audit/actionlint/history/source341+342 pass. Leaf102→main сохраняет ancestry принятой341/#101, automatic merge нет. Следующий critical343 выделен по actual source/Go GET detail negative ID probe; runtime код ещё не менялся.


GO-344 (30.10.2026): ветка codex/GO-344-main-staging-server от свежего origin/main cabe5ff892c208499aefc0e69383110604a581d4. Сервер получает именно этот main SHA, без feature-кода. Сборка Go1.23.12 ARM64/mariadb и npm ci/build:web pass; server baseline/origin TLS/auth/renewal pass. Внешний Cloudflare403 и GitHub variables API403 не считаются успехом; automation не включена. PR/commit и проверки документации фиксируются после завершения порции.

GO-344: коммит7790d82, [PR#103](https://github.com/meInteractive/it-map/pull/103)→main, без merge. Source/history и88 Markdown links pass. Поздняя внешняя проверка из Codex: live200,ready503 business_api_not_migrated,SPA200,pma401;403 с IP сервера не переносится на все клиенты.

GO-345: ветка codex/GO-345-main-only-deploy от7790d82/#103. Main-only push/dispatch controller, delete/feature/tag отказ; независимые CI workflows не менялись.27 deploy protocol tests pass (loopback требует обычной escalation), actionlint pass; внешний read-only staging-smoke проверяется отдельно. GitHub Actions variables API403; settings/VPN runner/реальная автоматическая доставка пока не настроены.
