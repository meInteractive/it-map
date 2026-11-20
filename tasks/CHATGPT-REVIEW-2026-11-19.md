# Аудит PR и задач ChatGPT за 05–19.11.2026

**Источник:** GitHub PR metadata, `AGENTS.md`, `tasks/SPRINT-01.md`, `tasks/SPRINT-01-PARSER-APIFY.md`, `tasks/SPRINT-02.md`, отдельные ТЗ `tasks/`, 26 датированных handoff за 07–18 ноября и записи в чате владельца. Контрольная точка `main`: `4c89ba3c4732486ed0910d9f322895356a33fa97`; **PR #496 смержен 19.11.2026, merge `2e670273`**. Текущий installed worker SHA не проверялся.

**Правило владельца:** готовый код ChatGPT без документированной проверки Codex — **«к проверке»**. Родительскую задачу нельзя закрывать, если остались внешние acceptance gates. Физические PR-метаданные не доказывают исполнителя: один GitHub-автор `meInteractive` и общий префикс `codex/`. Атрибуция «Чат / по переписке» — восстановлена из handoff и сообщений пользователя, а не из Git author. Для 05–06 и 11–16 ноября данных недостаточно, поэтому авторство помечено неизвестным; это не означает, что PR делал Codex.

## Основные находки и границы приёмки

| Область | Реализованная часть / PR | Как фиксировать |
|---|---|---|
| Диагностика GO-107, GO-108 | [#255](https://github.com/meInteractive/it-map/pull/255), [#257](https://github.com/meInteractive/it-map/pull/257); API и UI | Частично реализовано — к проверке; проверка historical 30 дней и live-когорт отдельно |
| S11-DIAG-01…05 / GO-439…444 | #259–269, #312, #314–316, #318, #320 | К проверке: кодовые результаты в main, acceptance по каждому ID в файлах |
| GO-433/434/436 | #272, #283–287, #289, #291; #475, #481 | Частично реализовано — к проверке; реальные суммы и причина исторических 61 не доказаны |
| GO-437/438 | [#271](https://github.com/meInteractive/it-map/pull/271), [#276](https://github.com/meInteractive/it-map/pull/276) | К проверке; не утверждать живую UI/DB приёмку |
| GO-018 | #277, #280–282, [#493](https://github.com/meInteractive/it-map/pull/493) | Письма/OpenAI частично реализованы; оценка релевантности и полная acceptance остаются |
| GO-113/114/115/117/119 | #294–298, #308, #313 | К проверке (выполненные части), дальнейший фронтенд/Playwright рефакторинг остаётся |
| GO-430/431/432 | #251, #252, #324–495 (большой поток GO-431) | К проверке код/интеграционные тесты; production worker и другие gates не закрыты |
| GO-448 | [#479](https://github.com/meInteractive/it-map/pull/479) | К проверке; код трипл-фильтра в main |
| GO-015 | [#496](https://github.com/meInteractive/it-map/pull/496) | Частично реализовано — к проверке Codex: PR смержен 19.11; 4 CI успешно на `51ffa71d`, нет live OAuth-acceptance |
| GO-447 | [#471](https://github.com/meInteractive/it-map/pull/471) | Постановка для Codex, а НЕ завершение автодеплоя Worker |
| Межкандидатная AI operation identity | #482 — design | Отдельная реализация/миграция под Issue #489; блокер GO-431 |
| HH legacy и диагностика | #292, #302, #309, #310, #487 и #320 | Read-only код/синтетические доказательства; историческая фактура и восстановление реального job отдельно |

## Непринятые или ошибочно отмеченные как «не начато»

- В основных реестрах GO-107, 108, 433, 434, 436, 437, 438, 018, 113, 114, 115, 117, 119 оставлены устаревшие сведения о «не начато», несмотря на PR. Изменения должны отмечать реализованные **части**, сохраняя `[ ]`, пока Codex/оператор не примет оставшиеся критерии.
- GO-015: конфликтный HTTP-тест на #496 ожидал `409`; новая семантика локального сохранения с `google_sync_status=pending` требует `201/200`. Исправление в PR #496 на `51ffa71d`; все четыре GitHub Actions успешны; внешняя синхронизация не проверена на реальном аккаунте.
- `GO-431` — многочисленные успешные synthetic CI и merge в GitHub **не** равны установленной версии отдельного `itmap-worker`; API-деплой не обновляет его. `GO-447` отдельно.
- `GO-433` — подтверждённая стоимость и `unknown`-резервы не эквивалентны; `GO-436` — candidate import не равен первой уникальной вакансии.
- Прежние handoff зафиксированы как исторические снимки, не исправлялись задним числом. Новое состояние закрепляется в основном реестре, файлах подзадач и handoff от 19.11.

## Сверенные handoff-файлы и отдельные подзадачи

Прочитаны **все 26 датированных handoff-файлов, находившихся в каталоге на 19.11 и относящихся к 07–18.11** (05–06.11 отдельных handoff нет), плюс [задачи ChatGPT 16.11](../docs/dev/handoff/CHATGPT-TASKS-2026-11-16.md):

- [07.11](../docs/dev/handoff/HANDOFF-2026-11-07.md): GO-430, HH-пилот, стоимость и таймер.
- [08.11](../docs/dev/handoff/HANDOFF-2026-11-08.md), [08.11 (02)](../docs/dev/handoff/HANDOFF-2026-11-08-02.md): JSON-диагностика, GO-018, GO-438/436/433.
- [09.11](../docs/dev/handoff/HANDOFF-2026-11-09.md): GO-433/434/436, архитектура, Playwright, ожидания HH.
- [10.11](../docs/dev/handoff/HANDOFF-2026-11-10.md), [10.11 (02)](../docs/dev/handoff/HANDOFF-2026-11-10-02.md): GO-113/114/115/117/119/303/304, HH-диагностика и задачи Codex.
- [11.11](../docs/dev/handoff/HANDOFF-2026-11-11.md): уточнения безопасного восстановления исторического HH.
- [12.11](../docs/dev/handoff/HANDOFF-2026-11-12.md), [12.11 (02)](../docs/dev/handoff/HANDOFF-2026-11-12-02.md), [12.11 (03)](../docs/dev/handoff/HANDOFF-2026-11-12-03.md): supervisor/lease и политика worker.
- [13.11](../docs/dev/handoff/HANDOFF-2026-11-13.md), [16.11](../docs/dev/handoff/HANDOFF-2026-11-16.md): consumer schema/recovery, worker safety, staging, предварительные read-only доказательства.
- [17.11](../docs/dev/handoff/HANDOFF-2026-11-17.md) и [02](../docs/dev/handoff/HANDOFF-2026-11-17-02.md), [03](../docs/dev/handoff/HANDOFF-2026-11-17-03.md), [04](../docs/dev/handoff/HANDOFF-2026-11-17-04.md), [05](../docs/dev/handoff/HANDOFF-2026-11-17-05.md), [06](../docs/dev/handoff/HANDOFF-2026-11-17-06.md), [07](../docs/dev/handoff/HANDOFF-2026-11-17-07.md), [08](../docs/dev/handoff/HANDOFF-2026-11-17-08.md), [09](../docs/dev/handoff/HANDOFF-2026-11-17-09.md), [10](../docs/dev/handoff/HANDOFF-2026-11-17-10.md), [11](../docs/dev/handoff/HANDOFF-2026-11-17-11.md): P0 AI, неизвестные расходы, восстановление, GO-448.
- [18.11](../docs/dev/handoff/HANDOFF-2026-11-18.md), [18.11 (02)](../docs/dev/handoff/HANDOFF-2026-11-18-02.md), [18.11 (03)](../docs/dev/handoff/HANDOFF-2026-11-18-03.md): финальная серия GO-431, GO-293/304 и GO-447.

Внутри `tasks/sprint-01-subtasks/` проверены S11-DIAG-01…11 и все BATCH-01…10. Для BATCH-01/02 есть только PR #193–198 **до** изучаемого периода; BATCH-03…10 не содержат отчётных PR для проверяемых пунктов. Поэтому чекбоксы BATCH не менялись: отсутствие нового PR не является доказательством выполнения или невыполнения старых ID.

## Полная инвентаризация PR

Проверены все `251` PR, созданные с 05.11 по 18.11 (включительно), №235–496 с пропусками GitHub Issues. Разбивка: **233** merge непосредственно в `main`, **15** merge в промежуточные ветки (потребуется проверка доставки), **0** открытых PR, **3** закрытых без merge (#300, #306, #307). **94** PR атрибутированы режиму ChatGPT по истории диалогов, **157** оставлены с неустановленным исполнителем. Статус указывается по merge target, поэтому `merged` в промежуточную ветку **не** означает присутствие в `main`. Для проверки обязательно сверять актуальное `main` и CI конкретного SHA.

| PR | Дата | Задача | GitHub | Исполнитель* | Предмет |
|---|---|---|---|---|---|
| [#235](https://github.com/meInteractive/it-map/pull/235) | 11-05 | GO-411 | Влит в main | Исполнитель не установлен | GO-411: Зафиксировать проверку HH в рабочем приложении |
| [#236](https://github.com/meInteractive/it-map/pull/236) | 11-05 | GO-419 | Влит в main | Исполнитель не установлен | GO-419: Продолжать уже запущенный HH Actor |
| [#237](https://github.com/meInteractive/it-map/pull/237) | 11-05 | GO-112 | Влит в main | Исполнитель не установлен | GO-112: Ограничить HH-поиск Москвой до запуска Apify |
| [#238](https://github.com/meInteractive/it-map/pull/238) | 11-05 | GO-418 | Влит в main | Исполнитель не установлен | GO-418: Пакетно обрабатывать этапы выбранного HH |
| [#239](https://github.com/meInteractive/it-map/pull/239) | 11-05 | GO-420 | Влит в main | Исполнитель не установлен | GO-420: Проверять город HH до AI-классификации |
| [#240](https://github.com/meInteractive/it-map/pull/240) | 11-05 | GO-421 | Влит в main | Исполнитель не установлен | GO-421: Проверять направления перед платным HH Actor |
| [#241](https://github.com/meInteractive/it-map/pull/241) | 11-05 | GO-411 | Влит в main | Исполнитель не установлен | GO-411: Зафиксировать проверку фильтра Москвы на HH |
| [#242](https://github.com/meInteractive/it-map/pull/242) | 11-05 | GO-422 | Влит в main | Исполнитель не установлен | GO-422: Обрабатывать релевантность выбранного HH |
| [#243](https://github.com/meInteractive/it-map/pull/243) | 11-05 | GO-422 | Влит в main | Исполнитель не установлен | GO-422: Зафиксировать проверку релевантности HH |
| [#244](https://github.com/meInteractive/it-map/pull/244) | 11-05 | GO-423 | Влит в main | Исполнитель не установлен | GO-423: Подготовить фильтр HH по профессиональным ролям |
| [#245](https://github.com/meInteractive/it-map/pull/245) | 11-05 | GO-427 | Влит в main | Исполнитель не установлен | GO-427: Добавить чтение справочника профессиональных ролей HH |
| [#246](https://github.com/meInteractive/it-map/pull/246) | 11-05 | GO-424 | Влит в main | Исполнитель не установлен | GO-424: Настраивать роли HH отдельно для каждого направления |
| [#247](https://github.com/meInteractive/it-map/pull/247) | 11-05 | GO-425 | Влит в main | Исполнитель не установлен | GO-425: Использовать роли HH при запуске парсера |
| [#248](https://github.com/meInteractive/it-map/pull/248) | 11-06 | GO-428 | Влит в main | Исполнитель не установлен | GO-428: Защитить общую очередь HH перед автозапуском |
| [#249](https://github.com/meInteractive/it-map/pull/249) | 11-06 | GO-429 | Влит в main | Исполнитель не установлен | GO-429: Подготовить ограниченный пилот расписания парсера |
| [#250](https://github.com/meInteractive/it-map/pull/250) | 11-06 | GO-430 | Влит в main | Исполнитель не установлен | Спринт 1.1: приоритетные GO-430/431 — разделение очередей и постоянный Go Worker |
| [#251](https://github.com/meInteractive/it-map/pull/251) | 11-06 | GO-432 | Влит в main | Исполнитель не установлен | GO-432: принимать вакансии HH из Apify без повторного фильтра дат |
| [#252](https://github.com/meInteractive/it-map/pull/252) | 11-06 | GO-430 | Влит в main | Исполнитель не установлен | GO-430: разделить HH-старты, reconcile Actor и обработку кандидатов |
| [#253](https://github.com/meInteractive/it-map/pull/253) | 11-07 | GO-433 | Влит в main | Чат / по переписке | GO-433: запланировать учёт себестоимости опубликованной вакансии (спринт 1.2) |
| [#254](https://github.com/meInteractive/it-map/pull/254) | 11-07 | GO-107 | Влит в main | Чат / по переписке | GO-107/108 в спринт 1.1 + единый датированный handoff-каталог |
| [#255](https://github.com/meInteractive/it-map/pull/255) | 11-07 | GO-107 | Влит в main | Чат / по переписке | GO-107: диагностика P1/P2, 30-дневная история и защищённый Go API |
| [#256](https://github.com/meInteractive/it-map/pull/256) | 11-07 | Документация/другое | Влит в main | Чат / по переписке | Спринт 1.1: JSON-диагностика парсера, каталог подзадач и справочник |
| [#257](https://github.com/meInteractive/it-map/pull/257) | 11-07 | GO-108 | Влит в main | Чат / по переписке | GO-108: Go/MariaDB воронка вакансий, источники и когорты |
| [#258](https://github.com/meInteractive/it-map/pull/258) | 11-07 | S11-DIAG-04 | Влит в main | Чат / по переписке | S11-DIAG-04: уточнить JSON на графике P1/P2 и методику диагностики |
| [#259](https://github.com/meInteractive/it-map/pull/259) | 11-07 | S11-DIAG-01 | Влит в main | Чат / по переписке | S11-DIAG-01: безопасная основа кнопки «Копировать JSON» |
| [#260](https://github.com/meInteractive/it-map/pull/260) | 11-07 | S11-DIAG-02 | Влит в main | Чат / по переписке | S11-DIAG-02: JSON-кнопки обзора, очереди, циклов и попыток |
| [#261](https://github.com/meInteractive/it-map/pull/261) | 11-07 | GO-434, GO-433 | Влит в main | Чат / по переписке | Спринт 1.1: аудит расходов GO-434 и перенос GO-433 для Codex |
| [#262](https://github.com/meInteractive/it-map/pull/262) | 11-07 | GO-435 | Влит в main | Чат / по переписке | Спринт 1.2: GO-435 персональная рекомендательная система вакансий |
| [#263](https://github.com/meInteractive/it-map/pull/263) | 11-07 | S11-DIAG-03 | Влит в main | Чат / по переписке | S11-DIAG-03 A: JSON бюджетов Apify и Firecrawl без внешних запросов |
| [#264](https://github.com/meInteractive/it-map/pull/264) | 11-07 | S11-DIAG-03 | Влит в main | Чат / по переписке | S11-DIAG-03 B: подтверждённые и неопределённые расходы JEV/LLM |
| [#265](https://github.com/meInteractive/it-map/pull/265) | 11-07 | S11-DIAG-03 | Влит в main | Чат / по переписке | S11-DIAG-03 C: последние 10 операций Apify из полного ledger |
| [#266](https://github.com/meInteractive/it-map/pull/266) | 11-07 | GO-436 | Влит в main | Чат / по переписке | Спринт 1.1: GO-436 проверить 61 «публикацию» на графике 07.11 |
| [#267](https://github.com/meInteractive/it-map/pull/267) | 11-07 | S11-DIAG-04 | Влит в main | Чат / по переписке | S11-DIAG-04 A: JSON графика ошибок и воронки вакансий |
| [#268](https://github.com/meInteractive/it-map/pull/268) | 11-08 | S11-DIAG-04 | Влит в main | Чат / по переписке | S11-DIAG-04 B: полная безопасная разбивка активных P2 |
| [#269](https://github.com/meInteractive/it-map/pull/269) | 11-08 | S11-DIAG-05 | Влит в main | Чат / по переписке | S11-DIAG-05: одна кнопка «Копировать всю диагностику» |
| [#270](https://github.com/meInteractive/it-map/pull/270) | 11-08 | GO-437 | Влит в main | Чат / по переписке | GO-437: запланировать раздел «Диагностика» для администратора |
| [#271](https://github.com/meInteractive/it-map/pull/271) | 11-08 | GO-437 | Влит в main | Чат / по переписке | GO-437: раздел «Диагностика» в руководстве администратора |
| [#272](https://github.com/meInteractive/it-map/pull/272) | 11-08 | GO-436 | Влит в main | Чат / по переписке | GO-436: воспроизвести расхождение 61 импорта и новых вакансий |
| [#274](https://github.com/meInteractive/it-map/pull/274) | 11-08 | GO-438 | Влит в main | Чат / по переписке | GO-438: добавить миграцию «Направления отбора» и handoff задач Codex |
| [#276](https://github.com/meInteractive/it-map/pull/276) | 11-08 | GO-438 | Влит в main | Чат / по переписке | GO-438: перенести настройки направлений отбора и промтов на Go |
| [#277](https://github.com/meInteractive/it-map/pull/277) | 11-08 | GO-018 | Влит в main | Чат / по переписке | GO-018: персональная генерация писем через безопасный OpenRouter на Go |
| [#278](https://github.com/meInteractive/it-map/pull/278) | 11-08 | GO-018, GO-436 | Влит в main | Чат / по переписке | Документация: handoff 08.11.2026 21:32 — письма GO-018 и статистика GO-436 |
| [#279](https://github.com/meInteractive/it-map/pull/279) | 11-08 | Документация/другое | Влит в main | Чат / по переписке | Документация: уточнить в handoff слияние PR #277 |
| [#280](https://github.com/meInteractive/it-map/pull/280) | 11-08 | GO-018 | Влит в main | Чат / по переписке | GO-018: не обрывать долгую генерацию писем по HTTP WriteTimeout |
| [#281](https://github.com/meInteractive/it-map/pull/281) | 11-08 | GO-018 | Влит в main | Чат / по переписке | GO-018: не выдавать старое письмо за успешную регенерацию при конфликте |
| [#282](https://github.com/meInteractive/it-map/pull/282) | 11-08 | GO-018 | Влит в main | Чат / по переписке | GO-018: безопасное ожидание письма при конкурентной генерации |
| [#283](https://github.com/meInteractive/it-map/pull/283) | 11-08 | GO-434 | Влит в main | Чат / по переписке | GO-434: аудит расходов и безопасная методика дедупликации операций |
| [#284](https://github.com/meInteractive/it-map/pull/284) | 11-09 | GO-433 | Влит в main | Чат / по переписке | GO-433: частичная себестоимость новой вакансии и статистика парсера |
| [#285](https://github.com/meInteractive/it-map/pull/285) | 11-09 | GO-433 | Влит в main | Чат / по переписке | GO-433: диагностика полноты связей ИИ-расходов с вакансиями |
| [#286](https://github.com/meInteractive/it-map/pull/286) | 11-09 | GO-433 | Влит в main | Чат / по переписке | GO-433: контракт доказуемой себестоимости когорт и план миграции |
| [#287](https://github.com/meInteractive/it-map/pull/287) | 11-09 | GO-436 | Влит в main | Чат / по переписке | GO-436: новые уникальные вакансии отдельно от импортов кандидатов |
| [#288](https://github.com/meInteractive/it-map/pull/288) | 11-09 | Документация/другое | Влит в main | Чат / по переписке | Документация: активный handoff и журнал решений от 09.11.2026 |
| [#289](https://github.com/meInteractive/it-map/pull/289) | 11-09 | GO-433 | Влит в main | Чат / по переписке | GO-433: защита интерфейса себестоимости от некорректных ответов API |
| [#291](https://github.com/meInteractive/it-map/pull/291) | 11-09 | GO-434 | Влит в main | Чат / по переписке | GO-434: read-only диагностика повторных операций парсера |
| [#292](https://github.com/meInteractive/it-map/pull/292) | 11-09 | Документация/другое | Влит в main | Чат / по переписке | #290: read-only диагностика сроков HH-сканирования и причин ожидания |
| [#294](https://github.com/meInteractive/it-map/pull/294) | 11-09 | GO-119 | Влит в main | Чат / по переписке | GO-119: актуализировать архитектуру по текущему Go-коду и проверенным PR/CI |
| [#295](https://github.com/meInteractive/it-map/pull/295) | 11-09 | GO-117 | Влит в main | Чат / по переписке | GO-117: независимые Playwright-сценарии и запрет ложного успеха при skipped |
| [#296](https://github.com/meInteractive/it-map/pull/296) | 11-09 | GO-117 | Влит в main | Чат / по переписке | GO-117: разделить dirty-navigation Playwright на независимые сценарии |
| [#297](https://github.com/meInteractive/it-map/pull/297) | 11-09 | GO-113 | Влит в main | Чат / по переписке | GO-113: обязательная строгая TypeScript-проверка React/Vite перед сборкой |
| [#298](https://github.com/meInteractive/it-map/pull/298) | 11-09 | GO-114 | Влит в main | Чат / по переписке | GO-114: единый Prettier для React/TypeScript и обязательный форматтер в CI |
| [#299](https://github.com/meInteractive/it-map/pull/299) | 11-10 | Документация/другое | Влит в main | Чат / по переписке | Документация: активный handoff IT Map на 10.11.2026 |
| [#300](https://github.com/meInteractive/it-map/pull/300) | 11-10 | GO-115 | Закрыт без merge | Чат / по переписке | GO-115: выделить правила инвалидирования API-кэша и защитить матричным тестом |
| [#301](https://github.com/meInteractive/it-map/pull/301) | 11-10 | Документация/другое | Влит в main | Чат / по переписке | Handoff: безопасная диагностика HH от 10 ноября и план расследования очереди |
| [#302](https://github.com/meInteractive/it-map/pull/302) | 11-10 | GO-290 | Влит в main | Чат / по переписке | GO-290: read-only диагностика причин ожидания HH-заданий |
| [#305](https://github.com/meInteractive/it-map/pull/305) | 11-10 | GO-303 | Влит в main | Чат / по переписке | GO-303: сократить интервал новых HH Actor до 15 минут с контролем бюджета |
| [#306](https://github.com/meInteractive/it-map/pull/306) | 11-10 | GO-303 | Закрыт без merge | Чат / по переписке | GO-303: read-only охват успешных HH-сканирований за 24/48 часов |
| [#307](https://github.com/meInteractive/it-map/pull/307) | 11-10 | GO-303 | Закрыт без merge | Чат / по переписке | GO-303: контроль успешных HH-сканирований за 24/48 часов |
| [#308](https://github.com/meInteractive/it-map/pull/308) | 11-10 | GO-115 | Влит в main | Чат / по переписке | GO-115: чистый PR правил инвалидации кэша с матричным тестом |
| [#309](https://github.com/meInteractive/it-map/pull/309) | 11-10 | GO-303 | Влит в main | Чат / по переписке | GO-303: read-only охват HH за 24/48 часов (чистая ветка) |
| [#310](https://github.com/meInteractive/it-map/pull/310) | 11-10 | GO-304 | Влит в main | Чат / по переписке | GO-304: объяснить расхождение новых импортов и недельного каталога |
| [#311](https://github.com/meInteractive/it-map/pull/311) | 11-10 | Документация/другое | Влит в main | Чат / по переписке | Handoff: передача разработки в новый чат 10.11, 16:43 МСК |
| [#312](https://github.com/meInteractive/it-map/pull/312) | 11-10 | GO-439 | Влит в main | Чат / по переписке | GO-439: добавить «Копировать JSON» в диагностику сроков HH |
| [#313](https://github.com/meInteractive/it-map/pull/313) | 11-10 | GO-117 | Влит в main | Чат / по переписке | GO-117: разделить независимые проверки Drawer и клавиатурной навигации |
| [#314](https://github.com/meInteractive/it-map/pull/314) | 11-10 | GO-440 | Влит в main | Чат / по переписке | GO-440: показать причины отсутствия успешного HH-обхода за 48 часов |
| [#315](https://github.com/meInteractive/it-map/pull/315) | 11-10 | GO-441 | Влит в main | Чат / по переписке | GO-441: не допускать HH-дочитывание без подтверждённого Apify Run ID |
| [#316](https://github.com/meInteractive/it-map/pull/316) | 11-10 | GO-442 | Влит в main | Чат / по переписке | GO-442: показать точную категорию блокировки HH-курсора |
| [#317](https://github.com/meInteractive/it-map/pull/317) | 11-10 | Документация/другое | Влит в main | Чат / по переписке | Handoff: дополнить активную передачу контекста итогами HH/Apify 10.11 |
| [#318](https://github.com/meInteractive/it-map/pull/318) | 11-10 | GO-443 | Влит в main | Чат / по переписке | GO-443: уточнить признаки устаревшего HH-курсора без запуска Actor |
| [#319](https://github.com/meInteractive/it-map/pull/319) | 11-10 | Документация/другое | Влит в main | Чат / по переписке | Handoff: безопасная проверка старого HH-курсора перед восстановлением |
| [#320](https://github.com/meInteractive/it-map/pull/320) | 11-10 | GO-444 | Влит в main | Чат / по переписке | GO-444: read-only признаки сохранённого HH Actor для legacy pending |
| [#321](https://github.com/meInteractive/it-map/pull/321) | 11-11 | GO-444 | Влит в main | Исполнитель не установлен | Handoff 11.11: Codex, отказ публикации GO-444 и очередь задач |
| [#324](https://github.com/meInteractive/it-map/pull/324) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: архитектура и план постоянного Go worker |
| [#325](https://github.com/meInteractive/it-map/pull/325) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: инвентаризация владения очередью |
| [#326](https://github.com/meInteractive/it-map/pull/326) | 11-11 | GO-431 | Влит в codex/GO-431-worker-inventory | Исполнитель не установлен | GO-431: выделить итерацию обработки задания |
| [#327](https://github.com/meInteractive/it-map/pull/327) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: перенести выделение итерации worker в main |
| [#328](https://github.com/meInteractive/it-map/pull/328) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: ограниченное ожидание рабочего цикла |
| [#329](https://github.com/meInteractive/it-map/pull/329) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: добавить разброс интервалов ожидания |
| [#330](https://github.com/meInteractive/it-map/pull/330) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверять готовность очереди без создания run |
| [#331](https://github.com/meInteractive/it-map/pull/331) | 11-11 | GO-431 | Влит в codex/GO-431-bounded-session | Исполнитель не установлен | GO-431: подготовить ограниченную pipeline-сессию |
| [#332](https://github.com/meInteractive/it-map/pull/332) | 11-11 | GO-431 | Влит в codex/GO-431-pipeline-main | Исполнитель не установлен | GO-431: восстанавливать истёкшие leases без пустого run |
| [#333](https://github.com/meInteractive/it-map/pull/333) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: пробуждение ожидающего worker |
| [#334](https://github.com/meInteractive/it-map/pull/334) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: перенести pipeline-сессию в main |
| [#335](https://github.com/meInteractive/it-map/pull/335) | 11-11 | GO-431 | Влит в codex/GO-431-expired-recovery | Исполнитель не установлен | GO-431: цикл обработки и восстановления pipeline |
| [#336](https://github.com/meInteractive/it-map/pull/336) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: состояние здоровья постоянного worker |
| [#337](https://github.com/meInteractive/it-map/pull/337) | 11-11 | GO-431 | Влит в codex/GO-431-pipeline-supervisor | Исполнитель не установлен | GO-431: ограничения запуска и безопасные ошибки worker |
| [#338](https://github.com/meInteractive/it-map/pull/338) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить pipeline supervisor и ограничения запуска |
| [#339](https://github.com/meInteractive/it-map/pull/339) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: контракт переключения consumer и защита backup |
| [#340](https://github.com/meInteractive/it-map/pull/340) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: правила допуска и переключения consumer |
| [#341](https://github.com/meInteractive/it-map/pull/341) | 11-11 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: транзакционная проверка consumer fence |
| [#342](https://github.com/meInteractive/it-map/pull/342) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: атомарный переход consumer в draining |
| [#343](https://github.com/meInteractive/it-map/pull/343) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: отменить срок пилота сигнализатора и worker |
| [#344](https://github.com/meInteractive/it-map/pull/344) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: убрать срок пилота из защиты consumer |
| [#345](https://github.com/meInteractive/it-map/pull/345) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: передать состояние worker и отмену срока пилота |
| [#346](https://github.com/meInteractive/it-map/pull/346) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: уточнить навык доступа к серверу через Tailscale |
| [#347](https://github.com/meInteractive/it-map/pull/347) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: продлевать lease только действующего владельца worker |
| [#348](https://github.com/meInteractive/it-map/pull/348) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: выделить транзакционную часть создания scheduler run |
| [#349](https://github.com/meInteractive/it-map/pull/349) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: описать миграцию и restore consumer control |
| [#350](https://github.com/meInteractive/it-map/pull/350) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: определить завершение работы при draining |
| [#351](https://github.com/meInteractive/it-map/pull/351) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверять consumer при завершении под блокировкой |
| [#352](https://github.com/meInteractive/it-map/pull/352) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: ограничить срок run сроком владельца |
| [#353](https://github.com/meInteractive/it-map/pull/353) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить защищённое создание scheduler run |
| [#354](https://github.com/meInteractive/it-map/pull/354) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: ограничить lease job сроком run |
| [#355](https://github.com/meInteractive/it-map/pull/355) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить связанный с consumer захват задания |
| [#356](https://github.com/meInteractive/it-map/pull/356) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: запретить выполнение незащищённого bound job |
| [#357](https://github.com/meInteractive/it-map/pull/357) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: безопасно вернуть незапущенный bound job |
| [#358](https://github.com/meInteractive/it-map/pull/358) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверять порядок lease при возврате задания |
| [#359](https://github.com/meInteractive/it-map/pull/359) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: выделить транзакционные блокировки bound job |
| [#360](https://github.com/meInteractive/it-map/pull/360) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить создание и безопасное прерывание bound attempt |
| [#361](https://github.com/meInteractive/it-map/pull/361) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить защищённое резервирование bound attempt |
| [#362](https://github.com/meInteractive/it-map/pull/362) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить гонки и потерю commit денежного резерва |
| [#363](https://github.com/meInteractive/it-map/pull/363) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: последовательный планировщик профилей worker |
| [#364](https://github.com/meInteractive/it-map/pull/364) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: ограничить шаги и учитывать исходы профилей |
| [#365](https://github.com/meInteractive/it-map/pull/365) | 11-12 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить изолированный supervisor pipeline |
| [#366](https://github.com/meInteractive/it-map/pull/366) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: передать состояние worker на 13 ноября |
| [#367](https://github.com/meInteractive/it-map/pull/367) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить метаданные и singleton consumer |
| [#368](https://github.com/meInteractive/it-map/pull/368) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверка candidate-схемы consumer под блокировкой |
| [#369](https://github.com/meInteractive/it-map/pull/369) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: диагностика baseline и защита пула соединений |
| [#370](https://github.com/meInteractive/it-map/pull/370) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: защитить блокировки и сессии schema-init и restore |
| [#371](https://github.com/meInteractive/it-map/pull/371) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: транзакционный сброс consumer при восстановлении |
| [#372](https://github.com/meInteractive/it-map/pull/372) | 11-13 | GO-431 | Влит в codex/GO-431-consumer-restore-reset | Исполнитель не установлен | GO-431: проверка экспорта v39 для candidate-схемы consumer |
| [#373](https://github.com/meInteractive/it-map/pull/373) | 11-13 | GO-431 | Влит в codex/GO-431-consumer-export-compat | Исполнитель не установлен | GO-431: сквозная проверка восстановления candidate consumer |
| [#374](https://github.com/meInteractive/it-map/pull/374) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: backup, restore и version marker схемы consumer |
| [#375](https://github.com/meInteractive/it-map/pull/375) | 11-13 | GO-431 | Влит в codex/GO-431-consumer-version-evidence | Исполнитель не установлен | GO-431: тестовая миграция consumer и аварийные состояния |
| [#376](https://github.com/meInteractive/it-map/pull/376) | 11-13 | GO-431 | Влит в codex/GO-431-consumer-migration-eval | Исполнитель не установлен | GO-431: сквозной versioned backup и restore consumer |
| [#377](https://github.com/meInteractive/it-map/pull/377) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: тестовая миграция и versioned backup/restore consumer |
| [#378](https://github.com/meInteractive/it-map/pull/378) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: стабильная identity HH и диагностика неопределённых расходов |
| [#379](https://github.com/meInteractive/it-map/pull/379) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: диагностика paid HH evidence под fence worker |
| [#380](https://github.com/meInteractive/it-map/pull/380) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: candidate-схема долговечной связи HH и attempt |
| [#381](https://github.com/meInteractive/it-map/pull/381) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: транзакционная привязка резерва HH к попытке worker |
| [#382](https://github.com/meInteractive/it-map/pull/382) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверка сохранённой HH-связи при recovery worker |
| [#383](https://github.com/meInteractive/it-map/pull/383) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: диагностика paid evidence прежних generation |
| [#384](https://github.com/meInteractive/it-map/pull/384) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: общий снимок paid evidence после смены generation |
| [#385](https://github.com/meInteractive/it-map/pull/385) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: audit durable HH attempt bindings |
| [#386](https://github.com/meInteractive/it-map/pull/386) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: обновить план перехода к одному worker |
| [#387](https://github.com/meInteractive/it-map/pull/387) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: непрерывная обработка на синтетических данных |
| [#388](https://github.com/meInteractive/it-map/pull/388) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить потерю ответа и SIGKILL worker |
| [#389](https://github.com/meInteractive/it-map/pull/389) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить reconcile Actor в непрерывном worker |
| [#390](https://github.com/meInteractive/it-map/pull/390) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: убрать пустые запуски профилей worker |
| [#391](https://github.com/meInteractive/it-map/pull/391) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: добавить отдельную staging-команду worker |
| [#392](https://github.com/meInteractive/it-map/pull/392) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: добавить локальную диагностику готовности worker |
| [#393](https://github.com/meInteractive/it-map/pull/393) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить бюджеты непрерывного worker |
| [#394](https://github.com/meInteractive/it-map/pull/394) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: безопасно возобновлять задания после нехватки AI-бюджета |
| [#395](https://github.com/meInteractive/it-map/pull/395) | 11-13 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить восстановление после потерянного COMMIT |
| [#396](https://github.com/meInteractive/it-map/pull/396) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: возобновлять relevance после пополнения бюджета |
| [#397](https://github.com/meInteractive/it-map/pull/397) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить конкуренцию двух непрерывных worker |
| [#398](https://github.com/meInteractive/it-map/pull/398) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: исправить диагностику отложенных попыток |
| [#399](https://github.com/meInteractive/it-map/pull/399) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить откат до COMMIT при нехватке бюджета |
| [#400](https://github.com/meInteractive/it-map/pull/400) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: Configure worker polling and bounded batch size |
| [#401](https://github.com/meInteractive/it-map/pull/401) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить остановку worker во время платного запроса |
| [#402](https://github.com/meInteractive/it-map/pull/402) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: ограниченное завершение активной работы при остановке worker |
| [#403](https://github.com/meInteractive/it-map/pull/403) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: уменьшить вытеснение старых заданий pipeline |
| [#404](https://github.com/meInteractive/it-map/pull/404) | 11-14 | GO-431 | Влит в codex/GO-431-graceful-drain | Исполнитель не установлен | GO-431: проверить платные операции при остановке worker |
| [#405](https://github.com/meInteractive/it-map/pull/405) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: перенести проверки платных границ остановки в main |
| [#406](https://github.com/meInteractive/it-map/pull/406) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить смену поколения во время платного запуска worker |
| [#407](https://github.com/meInteractive/it-map/pull/407) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: уменьшить стоимость опроса готовых заданий |
| [#408](https://github.com/meInteractive/it-map/pull/408) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: сократить чтение истории при выборе pipeline |
| [#409](https://github.com/meInteractive/it-map/pull/409) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: защитить платные запуски после смены поколения |
| [#410](https://github.com/meInteractive/it-map/pull/410) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: защитить AI-вызовы после смены поколения |
| [#411](https://github.com/meInteractive/it-map/pull/411) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить денежные журналы worker при восстановлении |
| [#412](https://github.com/meInteractive/it-map/pull/412) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: добавить безопасную диагностику очереди worker |
| [#413](https://github.com/meInteractive/it-map/pull/413) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: показать причины ожидания и исходы профилей worker |
| [#414](https://github.com/meInteractive/it-map/pull/414) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: различать занятый run и сбой профиля worker |
| [#415](https://github.com/meInteractive/it-map/pull/415) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: не создавать paused runs при выключенном scheduler |
| [#416](https://github.com/meInteractive/it-map/pull/416) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: сохранить исторический снимок при росте текущей очереди |
| [#417](https://github.com/meInteractive/it-map/pull/417) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: отклонять противоречивую незавершённую paid-попытку |
| [#418](https://github.com/meInteractive/it-map/pull/418) | 11-14 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: Prevent duplicate paid runs with unresolved current operations |
| [#419](https://github.com/meInteractive/it-map/pull/419) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверять идентификаторы сохранённого Actor перед восстановлением |
| [#420](https://github.com/meInteractive/it-map/pull/420) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: закрыть обход ограничения Actor в быстрых профилях worker |
| [#421](https://github.com/meInteractive/it-map/pull/421) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: общий cooldown Actor и возобновление полного текста |
| [#422](https://github.com/meInteractive/it-map/pull/422) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: завершать worker при смене поколения базы |
| [#423](https://github.com/meInteractive/it-map/pull/423) | 11-15 | GO-431 | Влит в codex/GO-431-worker-generation-exit | Исполнитель не установлен | GO-431: проверять идентификаторы Actor предыдущих поколений |
| [#424](https://github.com/meInteractive/it-map/pull/424) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: перенести проверку исторических Actor в main |
| [#425](https://github.com/meInteractive/it-map/pull/425) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: блокировать противоречивые исторические AI-попытки |
| [#426](https://github.com/meInteractive/it-map/pull/426) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: возобновлять fallback по окончании cooldown Actor |
| [#427](https://github.com/meInteractive/it-map/pull/427) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить управляемую staging-службу worker |
| [#428](https://github.com/meInteractive/it-map/pull/428) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить аварийную остановку после резерва Actor |
| [#429](https://github.com/meInteractive/it-map/pull/429) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверять identity Actor перед чтением Dataset |
| [#430](https://github.com/meInteractive/it-map/pull/430) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: возобновлять задания после ожидания Actor и бюджета |
| [#431](https://github.com/meInteractive/it-map/pull/431) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: повторять временный сбой HH без платного fallback |
| [#432](https://github.com/meInteractive/it-map/pull/432) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: повторять безопасные чтения Apify |
| [#433](https://github.com/meInteractive/it-map/pull/433) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: учитывать Retry-After публичного HH |
| [#434](https://github.com/meInteractive/it-map/pull/434) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: учитывать Retry-After при чтении Apify |
| [#435](https://github.com/meInteractive/it-map/pull/435) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить жизненный цикл команды worker без сети |
| [#436](https://github.com/meInteractive/it-map/pull/436) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: безопасно повторять HTTP 408 и 425 |
| [#437](https://github.com/meInteractive/it-map/pull/437) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: распределять повторы безопасных чтений провайдеров |
| [#438](https://github.com/meInteractive/it-map/pull/438) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: не запускать платный fallback на странице проверки HH |
| [#439](https://github.com/meInteractive/it-map/pull/439) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: увеличивать ожидание при повторных ошибках чтения |
| [#440](https://github.com/meInteractive/it-map/pull/440) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить сквозное восстановление после цепочки сбоев |
| [#441](https://github.com/meInteractive/it-map/pull/441) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: безопасно откладывать чтение Apify при бюджетной ошибке |
| [#442](https://github.com/meInteractive/it-map/pull/442) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: учитывать часы поставщика при Retry-After |
| [#443](https://github.com/meInteractive/it-map/pull/443) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: безопасно обрабатывать непрозрачный ответ HH 403 |
| [#444](https://github.com/meInteractive/it-map/pull/444) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: учитывать срок ожидания при бюджетной ошибке Apify |
| [#445](https://github.com/meInteractive/it-map/pull/445) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: собирать проверяемый CI-артефакт worker |
| [#446](https://github.com/meInteractive/it-map/pull/446) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: учитывать длительные шаги в проверке состояния worker |
| [#447](https://github.com/meInteractive/it-map/pull/447) | 11-15 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить отказ worker при занятом порте диагностики |
| [#448](https://github.com/meInteractive/it-map/pull/448) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: сборка worker для ARM64 и приоритетный план перехода |
| [#449](https://github.com/meInteractive/it-map/pull/449) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить эксплуатационный профиль и службу worker |
| [#450](https://github.com/meInteractive/it-map/pull/450) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверить блокировку и восстановление при неизвестной стоимости Actor |
| [#451](https://github.com/meInteractive/it-map/pull/451) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: проверять доказательства для сверки Actor после смены токена |
| [#452](https://github.com/meInteractive/it-map/pull/452) | 11-16 | GO-431 | Влит в codex/GO-431-recovery-evidence | Исполнитель не установлен | GO-431: читать известные операции через подтверждённый аккаунт |
| [#453](https://github.com/meInteractive/it-map/pull/453) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: повторяемая read-only сверка схемы перед переходом |
| [#454](https://github.com/meInteractive/it-map/pull/454) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: перенести безопасную сверку Actor в main |
| [#455](https://github.com/meInteractive/it-map/pull/455) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: транзакционно сверять стоимость известного Actor |
| [#456](https://github.com/meInteractive/it-map/pull/456) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: подготовить операторскую сверку известных Actor |
| [#457](https://github.com/meInteractive/it-map/pull/457) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: передача контекста перехода worker на 16 ноября |
| [#458](https://github.com/meInteractive/it-map/pull/458) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: сверка существующего Actor ключом другого аккаунта |
| [#459](https://github.com/meInteractive/it-map/pull/459) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: диагностика ожидания HH в worker |
| [#460](https://github.com/meInteractive/it-map/pull/460) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: показать резервы AI текущего поколения |
| [#461](https://github.com/meInteractive/it-map/pull/461) | 11-16 | GO-431 | Влит в main | Исполнитель не установлен | GO-431: диагностика фактической активности worker |
| [#462](https://github.com/meInteractive/it-map/pull/462) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: задачи устойчивости AI и актуальная передача |
| [#463](https://github.com/meInteractive/it-map/pull/463) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: защита платного повтора при нулевом резерве |
| [#464](https://github.com/meInteractive/it-map/pull/464) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: запас времени перед следующей стадией pipeline |
| [#465](https://github.com/meInteractive/it-map/pull/465) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: уточнить диагностику неверного описания |
| [#466](https://github.com/meInteractive/it-map/pull/466) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: не запускать первый job на исходе дедлайна |
| [#467](https://github.com/meInteractive/it-map/pull/467) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: оставить окно для AI cleanup и закрыть платный повтор после прерывания |
| [#468](https://github.com/meInteractive/it-map/pull/468) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: проверка восстановления AI после отмены и потери ACK |
| [#469](https://github.com/meInteractive/it-map/pull/469) | 11-17 | GO-431 | Влит в codex/GO-431-ai-fault-recovery-tests | Чат / по переписке | GO-431: неизвестный COMMIT AI settlement без повторного POST |
| [#470](https://github.com/meInteractive/it-map/pull/470) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: перенос проверки COMMIT в main, AI SIGKILL и generation fence |
| [#471](https://github.com/meInteractive/it-map/pull/471) | 11-17 | GO-447 | Влит в main | Чат / по переписке | GO-447: поставить в спринт 1.1 безопасный deploy/restart воркера для Codex |
| [#473](https://github.com/meInteractive/it-map/pull/473) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: SIGTERM после AI settlement с безопасным завершением в grace |
| [#474](https://github.com/meInteractive/it-map/pull/474) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: безопасные категории причин invalid_output для AI extraction |
| [#475](https://github.com/meInteractive/it-map/pull/475) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: разделить import receipts и новые вакансии в диагностике worker |
| [#477](https://github.com/meInteractive/it-map/pull/477) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: блокировать неопределённый AI-повтор между jobs одного кандидата |
| [#478](https://github.com/meInteractive/it-map/pull/478) | 11-17 | GO-431 | Влит в main | Чат / по переписке | GO-431: показывать неизвестные AI-исходы с нулевым резервом в recoveryz |
| [#479](https://github.com/meInteractive/it-map/pull/479) | 11-17 | GO-448 | Влит в main | Чат / по переписке | GO-448: отдельный фильтр Избранные / Не избранные / Все для списка и карты |
| [#480](https://github.com/meInteractive/it-map/pull/480) | 11-18 | Документация/другое | Влит в main | Чат / по переписке | docs(handoff): передача разработки IT Map в новый чат — 18.11.2026 |
| [#481](https://github.com/meInteractive/it-map/pull/481) | 11-18 | GO-304 | Влит в main | Чат / по переписке | GO-304: различать отсутствующую и некорректную разбивку дат публикаций |
| [#482](https://github.com/meInteractive/it-map/pull/482) | 11-18 | GO-431 | Влит в codex/GO-304-funnel-breakdown-visibility | Чат / по переписке | GO-431: дизайн durable AI operation identity для межкандидатных платных вызовов |
| [#483](https://github.com/meInteractive/it-map/pull/483) | 11-18 | GO-476 | Влит в codex/GO-431-durable-paid-identity-design | Чат / по переписке | GO-476: изолировать случаи загрязнённой SQL-сессии в тестах restore |
| [#484](https://github.com/meInteractive/it-map/pull/484) | 11-18 | GO-431, GO-476 | Влит в main | Чат / по переписке | GO-431/GO-476: доставить в main изменения ранее смерженных stacked PR |
| [#485](https://github.com/meInteractive/it-map/pull/485) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431: сквозная проверка ready/queue/activity/recovery без ложной публикации |
| [#486](https://github.com/meInteractive/it-map/pull/486) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431: безопасно различать неподтверждённый источник при AI invalid_output |
| [#487](https://github.com/meInteractive/it-map/pull/487) | 11-18 | GO-293 | Влит в main | Чат / по переписке | GO-293: доказать календарные пробелы HH 7/3 дней и подготовить read-only аудит |
| [#488](https://github.com/meInteractive/it-map/pull/488) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431: безопасно фиксировать late invalid_output после AI settlement без повторного POST |
| [#490](https://github.com/meInteractive/it-map/pull/490) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431 P0: повторная проверка shutdown и бюджета времени после claim |
| [#491](https://github.com/meInteractive/it-map/pull/491) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431 P0: не считать уже отменённый до резерва AI вызов платным unknown |
| [#492](https://github.com/meInteractive/it-map/pull/492) | 11-18 | GO-431 | Влит в main | Чат / по переписке | Docs: handoff 18.11 (03) после GO-431 P0 и merge #491 |
| [#493](https://github.com/meInteractive/it-map/pull/493) | 11-18 | GO-018 | Влит в main | Чат / по переписке | GO-018: восстановить платные сопроводительные через OpenAI без автосписаний |
| [#494](https://github.com/meInteractive/it-map/pull/494) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431 P0: запрет платного POST после отмены на границе reserve COMMIT |
| [#495](https://github.com/meInteractive/it-map/pull/495) | 11-18 | GO-431 | Влит в main | Чат / по переписке | GO-431 P0: остановить новый платный AI dispatch при SIGTERM без обрыва in-flight |
| [#496](https://github.com/meInteractive/it-map/pull/496) | 11-18 | GO-015 | Влит в main | Чат / по переписке | GO-015: Google Calendar OAuth и безопасная синхронизация задач в Go |

*«Чат / по переписке» — высокая уверенность для групп с прямой историей бесед 07–10 и 17–18 ноября. Более тонкая атрибуция конкретного коммита и ревью Codex требует независимой проверки в истории tools/PR. Остальные не назначать автоматически ни ChatGPT, ни Codex.

## Рекомендованный порядок ревью Codex
1. Закрыть подтверждённые технические части S11-DIAG-01…11 и GO-437/438/448 по документированным CI и синтетическим/браузерным критериям, затем явно отметить принятые подпункты `[x]`.
2. Проверить GO-113/114/115/117/119, GO-107/108 и GO-018 без live платных вызовов; частично принятые части не закрывают родительскую задачу.
3. Аудировать GO-431 денежные границы, evidence и recovery, сохраняя все unknown/reserved данные; отделить release GO-447, migration #489 и живую операторскую приёмку от уже смерженного Go-кода.
4. GO-433/434/436 и HH историческую полноту проверять read-only на отдельно разрешённых обезличенных данных; не подменять факты synthetic тестами.


## Дополнение к аудиту от 20.11.2026: результаты двух последних чатов

Это **дополнение** к историческому аудиту 251 PR за 05–18.11, не пересчёт той выборки. Контрольный `main` после merge #505 владельцем 20.11: `52ae69e4dfa9ceecebf03d3c638bcd744069ad75`. Задачи ChatGPT ниже не прошли независимую проверку Codex; **merge + CI не означают приёмку**.

| PR | Результат и статус | Проверки / доставка |
|---|---|---|
| [#496](https://github.com/meInteractive/it-map/pull/496) | GO-015 Google OAuth/manual sync, **частично реализовано — к проверке**; live Google acceptance нет | `51ffa71d`, 4/4 CI; в main |
| [#497](https://github.com/meInteractive/it-map/pull/497) | GO-119 аудит PR и статусов, **к проверке** | `a1ef31572`, 4/4 CI; в main |
| [#498](https://github.com/meInteractive/it-map/pull/498) | GO-119 архитектура Go API + Worker, **частично реализовано — к проверке** | `ead909a46`, 4/4 CI; в main |
| [#499](https://github.com/meInteractive/it-map/pull/499) | Альтернатива архитектурного обновления, **не реализовано в main** | Закрыт без merge; использовать #498 |
| [#500](https://github.com/meInteractive/it-map/pull/500) | GO-390/BATCH-03/A: 01–10, **к проверке** | `b71fe8a50`, 5/5 CI, Playwright **90/90**, main |
| [#501](https://github.com/meInteractive/it-map/pull/501) | GO-010/BATCH-03/B: CRM статусы/комментарии/история, **к проверке** | `ec42a6757`, 4/4 CI, 98/98; merge только в ветку #500, в main доставлен через #505 |
| [#502](https://github.com/meInteractive/it-map/pull/502) | GO-032/BATCH-03/C: read-only min/max, **частично реализовано — к проверке**; 22–26,29–30 не реализованы | `89d64e07b`, 4/4 CI; в main |
| [#503](https://github.com/meInteractive/it-map/pull/503) | Дополнительный React test после снятия конфликтов с #500, **к проверке** | `35ea6c82f`, 4/4 CI, Playwright **90/90**; в main |
| [#504](https://github.com/meInteractive/it-map/pull/504) | Guard несохранённых изменений отклика, **к проверке** | `060d24afd`, 4/4 CI; в main |
| [#505](https://github.com/meInteractive/it-map/pull/505) | Прямой перенос CRM-кода из #501 в main после missed stacked merge, **к проверке** | `804f7b0ba`, 5/5 CI, Playwright **98/98**, 0 skipped; merge 20.11 |

**Отладка CI 19.11:** S11-B03-09/10 первоначально падал в mobile, так как при смене synthetic пользователя карточка не открывалась (`dialogCount=0`) после лишнего нажатия на уже активную кнопку «Список». Исправлена навигация теста, полный Playwright в `MariaDB isolated staging gate` прошёл на финальных SHA. **Разница workflow:** зелёный специализированный company-flags Go workflow сам по себе не подтверждал полный Playwright.

**Незавершённая приёмка:** GO-010/390/119/015, GO-032 transactional CRUD, реальный Google OAuth, deployed Worker SHA/GO-447, durable AI identity #489, исторические HH/GO-436 когорты, GO-433/434 неизвестные расходы. Индивидуальные ID и точные границы в [BATCH-03](sprint-01-subtasks/BATCH-03.md). Следующая передача [20 ноября](../docs/dev/handoff/HANDOFF-2026-11-20.md).
