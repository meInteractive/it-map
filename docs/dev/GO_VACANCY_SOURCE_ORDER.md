# GO-340: порядок Telegram-источников вакансии

Критическая часть GO-011, спринт1.1. База — принятая GO-339, checkpoint5064e26, source1026/native162 и полный CI успешны.

## Наблюдение и контракт

Неизменённые readVacancyProjectionRows и readVacancies на полной исходной SQLite3.53.3-схеме возвращают telegramSources по channel_id: используется индекс vacancy_telegram_sources_vacancy_channel_unique(vacancy_id,channel_id). Source SQL не содержит явного ORDER BY для этого массива; это зафиксированное поведение конкретной исходной схемы/индексов, а не обещание общего порядка любого SQL-плана.

В synthetic примере source IDs1,2,3 соответствуют channel IDs3,1,2. Все три исходных чтения — list/detail/company — возвращают каналы1,2,3; прежний Go ORDER BY source.id возвращал3,1,2. Первый элемент используется displayEntity/vacancyContext во [frontend model](../../frontend/features/vacancies/model.ts), поэтому расхождение влияет на видимый канал и контекст перехода.

## Исправление

[Общие функции](../../internal/vacancies/source_order.go) сортируют собственные копии компактного/полного DTO по ChannelID, сохраняя поля. BuildList использует компактную проекцию; CompanyVacancies — полную. DetailByID упорядочивает только публичный ответ после readDetailTx и вычисления revision. Общий readDetailTx, порядок provenance по source.id, optimistic-lock/CAS и fallback UPDATE не меняются. Пустой массив остаётся []. Различие source list/full DTO сохраняется: список содержит компактные поля и источник с пустым messageUrl, полная карточка/компания такой источник исключает.

## Проверки

[Oracle](../../scripts/check-vacancy-source-order.mjs) вызывает настоящие неизменённые source-функции на полной схеме SQLite3.53.3, Node24.19.0. Данные заранее подготовлены, total_changes() не меняется при чтении; сеть запрещена.57 наборов×3 представления=171 сравнение: пустые источники, один канал, обратный порядок, разреженные ID, изменение порядка INSERT, точные Unicode/NUL/optional fields, пустой messageUrl и случайные перестановки. До исправления воспроизведён ошибочный source.id порядок, после DTO совпадают. Проверяется неизменность входных provenance и revision при изменении только порядка показа.

[Native proof](../../internal/storage/vacancy_source_order_integration_test.go): девять именованных source-наборов×3 пользователя×list/detail/company=81 обязательный случай. Настоящие Repository reads через SELECT-only MariaDB10.11 сравниваются с заново вычисленными SQLite DTO; fingerprint всех72 таблиц неизменен после каждого чтения. Только положительные синтетические channel IDs. Fixture/cleanup — отдельный guarded writer только disposable GitHub CI. Локально положительная MariaDB не запускается. Workflow требует parent pass, ровно81 leaf pass, нулевые skips.

Local source/default/tagged race/vet/build, весь vacancy source regression, Go1.23, React,325 локальных ссылок, API audit/actionlint/history проходят. Native81 проходят без skips в [job123526700623](https://github.com/meInteractive/it-map/actions/runs/40896436757/job/123526700623). Полный CI успешен: [Public40896436711](https://github.com/meInteractive/it-map/actions/runs/40896436711), [MariaDB40896436757](https://github.com/meInteractive/it-map/actions/runs/40896436757), [negative10.3.32 40896436820](https://github.com/meInteractive/it-map/actions/runs/40896436820). Весь прежний regression также проходит. Код опубликован в [PR #100](https://github.com/meInteractive/it-map/pull/100),43fc14b745bb1a6762bcd99daef0d88f939057d5/tree74f462c9d8bf87cd02ef78a338c5ba3d8efe02b1.

Полная GO-011 остаётся открытой. Схема, source JS, запись источников, ревизии и реальные данные не меняются. Настройка сервера/SSH, провайдеры и деплой не выполняются.
