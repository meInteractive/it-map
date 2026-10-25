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
| GO-052: перенос закрытого стека | codex/GO-052-stack-checkpoint | main | [#9](https://github.com/meInteractive/it-map/pull/9) | 8dc1b0d: обязательные CI прошли; merge агентом не выполнялся |
| GO-053: запуск/свой сервер | codex/GO-053-launch-deploy-docs | codex/GO-052-stack-checkpoint | После создания | Документация/шаблоны, без подключения сервера |

## Блокировки и границы

- Полная GO-005: пользователь 25.10.2026 подтвердил отсутствие подготовленного staging/согласованного снимка и поручил продолжать независимые задачи. Нет отдельно разрешённого согласованного снимка/тестовой БД. Синтетическая MariaDB в CI проверяет только код и fixture-контракт; реальную БД не подключать.
- Полная GO-007: внешний staging-домен, HTTPS/proxy и безопасная конфигурация ещё не предоставлены/не разрешены. Никакого реального deployment или DNS.
- Открытые ADR по миграционному инструменту, синхронизации D1, секретам и cutover не принимать самостоятельно.
- Не брать задачи, которым требуется заблокированная операция. Доступны независимые проверки GO-006/PWA и уже реализованной авторизации без внешних интеграций; они не закрывают родительские эпики целиком.
- После каждой порции здесь фиксировать фактический PR/base/SHA/результаты. При отказе API явно писать «PR не создан», сохраняя коммиты локально.
