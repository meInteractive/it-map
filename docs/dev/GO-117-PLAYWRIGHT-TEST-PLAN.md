# GO-117 — изоляция браузерных сценариев и проверка полноты Playwright

**Срез:** 09.11.2026, отдельный PR из `main` после GO-119. Эта часть задачи меняет только synthetic/browser-тесты, их fixture и CI guard, а **не** приложение, сервер или БД.

## Сценарии, ставшие независимыми

| Файл | Было | Стало | Почему |
| --- | --- | --- | --- |
| [drawer-focus.spec.ts](../../tests/browser/drawer-focus.spec.ts) | 1 тест «26–30» | 4 именованных проверки | Доступное имя/начальный фокус, Tab/Shift+Tab, Escape/очистка и повторное открытие независимо проверяются с нуля |
| [entity-navigation-keyboard.spec.ts](../../tests/browser/entity-navigation-keyboard.spec.ts) | 1 тест «1–5» | 3 именованных проверки | Зацикливание стрелок, Home/End/Escape/ArrowUp, отказ и подтверждение выбора |
| [filter-menu-keyboard.spec.ts](../../tests/browser/filter-menu-keyboard.spec.ts) | 1 тест «16–20» | 4 именованных проверки | Открытие/Escape/подписки, накопление флажков, повторное открытие/внешний клик, unmount/очистка |
| [navigation-dirty-recovery.spec.ts](../../tests/browser/navigation-dirty-recovery.spec.ts) | 1 связанный сценарий | **4 независимых теста**, каждый с собственным Workspace/редактором | Back/Forward, смена раздела, подтверждение закрытия и сохранение имеют отдельные результаты; зависимые шаги внутри каждого теста помечены `test.step` |

[Общая fixture](../../tests/browser/component-fixtures.ts) монтирует настоящие компоненты через esbuild в новый Page для каждого независимого теста; все сетевые запросы заблокированы. **Этап 2 (PR после #295):** для сложных переходов `navigation-dirty-recovery.spec.ts` добавлена отдельная fixture `workspace`: каждый тест начинает со свежего synthetic same-origin документа, новой модели вакансии, очереди ответов `window.confirm` и пустого счётчика PUT. HTTP-маршруты и данные остаются подменёнными внутри изолированного Chromium, **без изменения настоящего API или production CSP**. Внутри каждого теста зависимые реакции «отказ → подтверждение» сохранены, но независимые операции больше не скрывают друг друга ранним падением. Автоматическая проверка browser pageerror фиксирует **только имя исключения**, без message/stack, cookies, URL и payload. В [playwright.config.ts](../../playwright.config.ts) trace, video и screenshots отключены; desktop/mobile работают на синтетическом окружении.

## Строгая приёмка

[Репортёр](../../scripts/playwright-strict-reporter.mjs) на `onEnd` возвращает Playwright failure, если нет тестов, есть `skipped` или тесты без результата, либо получены не все успешные результаты. Он работает в основном браузерном конфиге и не скрывает исходные ошибки. [Отрицательные unit-тесты](../../scripts/test-playwright-strict-reporter.mjs) доказывают отказ для пустого, partially skipped, полностью skipped и failed прогона. В [go-ci](../../.github/workflows/go-ci.yml) этот тест запускается рядом с `npx playwright test --list`; в [MariaDB CI](../../.github/workflows/mariadb-staging-ci.yml) фактическая `npm run test:browser` проверяется этим репортёром на одноразовой CI БД и обеих browser projects.

**Как проверить:**

```bash
node scripts/test-playwright-strict-reporter.mjs
npx playwright test --config=playwright.config.ts --list
npx playwright test --config=playwright.config.ts --project=desktop tests/browser/drawer-focus.spec.ts tests/browser/entity-navigation-keyboard.spec.ts tests/browser/filter-menu-keyboard.spec.ts
npx playwright test --config=playwright.config.ts --project=mobile tests/browser/drawer-focus.spec.ts tests/browser/entity-navigation-keyboard.spec.ts tests/browser/filter-menu-keyboard.spec.ts
npx playwright test --config=playwright.config.ts --project=desktop tests/browser/navigation-dirty-recovery.spec.ts
npx playwright test --config=playwright.config.ts --project=mobile tests/browser/navigation-dirty-recovery.spec.ts
```

Последние две команды не требуют Go API/БД, но требуют `npm ci` и установленный Chromium. Полный CI прогоняется по обоим проектам с синтетической MariaDB только при наличии изолированной тестовой инфраструктуры.

**Граница задачи:** GO-117 в спринте 1.2 зависит от release gate спринта 1.1 и GO-113 (TypeScript); этот PR — самостоятельная безопасная часть, не закрывающая весь комплекс GO-117, пока нет приёмки оставшихся сложных сценариев, гонок и CI. Не менять production, не запускать внешних провайдеров. Не ослаблять унаследованные тесты, не добавлять `test.skip` ради зелёного CI.

**Результаты по этапу 1:** [PR #295](https://github.com/meInteractive/it-map/pull/295), merged в `main` 09.11.2026; три обязательных workflow прошли на SHA `33c21f204bc388753108feb2e8826a06ed9b58e7`, `60/60` desktop/mobile браузерных кейсов, `0 skipped`. **Этап 2:** после разделения dirty navigation общее количество ожидаемых тестов (если конфиги и остальные файлы не менялись) — `66` для двух проектов; это **расчёт**, не фактическая приёмка. Финальное число, точный HEAD SHA и результат CI подтверждаются только после PR.
