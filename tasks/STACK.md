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
