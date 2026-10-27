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
