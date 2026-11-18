# GO-431 — безопасная диагностика `invalid_output` при структурировании вакансий

**Область:** Go parser, этап `extract_vacancy`; чтение сохранённых ошибок `vacancy_extractions.diagnostics` оператором. Реальные запросы к моделям, production-данные и повторные платные POST не нужны для проверки.

## Что есть в коде

- `internal/parser/extraction_validation.go`: проверки JSON, схемы, полей, текста и подтверждения значений источником. Строгая проверка до принятия ответа может закончиться `invalid_output` даже при синтаксически корректном JSON.
- `internal/parser/extraction_description_diagnostic.go`: `extractionFailureDiagnostic` сохраняет **только** фиксированные `outputIssue` и при проверке `description_md` — `descriptionIssue`. Произвольные сообщения провайдера, поля вакансий, response body, URL и промпты в diagnostic не копируются.
- `internal/parser/ai_stages.go`: `aiStageErrorCode` сохраняет ранее действовавший верхнеуровневый код. `invalid_output` не превращается в `success`, схема не ослабляется, автоматический повтор платного запроса не добавляется.

## Ограниченные категории `outputIssue`

| Код | Подтверждаемая граница |
| --- | --- |
| `json_invalid` | JSON синтаксически неприемлем или содержит дополнительные значения |
| `schema_shape` | JSON не соответствует требуемой структуре |
| `field_invalid` | Известное обязательное поле не прошло проверку типа/enum/значения |
| `content_insufficient` | Содержательного описания недостаточно |
| `salary_bounds` | Нижняя граница зарплаты больше верхней |
| `salary_unverified` | Число зарплаты не подтверждено исходным текстом |
| `publication_date` | Дата не соответствует допустимому формату или календарю |
| `source_unverified` | Значение `work_format`, `salary_currency`, `salary_tax_type`, `salary_period` либо дата/валюта не подтверждены источником: только **точные строки локального валидатора** |
| `provider_response` | У адаптера отсутствовал полный структурированный результат; конкретное содержимое provider response неизвестно |

При `description_md` дополнительные фиксированные `descriptionIssue` — `wrong_type`, `empty`, `null_byte`, `too_long`. Отсутствие `outputIssue` означает, что ограниченная классификация не имеет достоверной причины: **не** подставлять `other=provider_error` с сырыми деталями и не трактовать `null` как успешную обработку.

`source_unverified` вводится для уже существующего валидатора без изменения решения модели, текста, схемы, баланса или порядка этапов. Не распознавать произвольные поля через префикс строки; строго ограничить допустимые сообщения.

## Денежная неопределённость и граница доказательства

- `costEstimated:false` обозначает **отсутствие оценки стоимости в диагностике**, а не `cost_usd=0`, бесплатный запрос или состоявшийся расчёт.
- После возможной отправки запроса таймаут, `context.Canceled`, потерянный ACK, `ErrMoneyUncertain`, неопределённый COMMIT и неполный envelope **не разрешают** новый платный POST. Сопоставлять с durable `scheduler_attempts` и фактическими provider receipts только по разрешённой read-only методике.
- Категории появляются в сохранённом `SaveFailure` **только если транзакция записи ошибки действительно завершилась**. Отсутствующая диагностическая строка не доказывает отсутствие вызова или расходов.
- **GO-431 late Save:** если `ExtractionStageStore.Save` вернул заранее распознанный `invalid_output` с HTTP-статусом 422 **из callback** `FinishWithin`, первая транзакция однозначно откатывается без COMMIT. После этого `AIStageExecutor` завершает **тот же attempt**, записывая ограниченную ошибку `vacancy_extractions`, failed job и существующие денежные evidence одной новой транзакцией. Нового AI HTTP POST или второго `Attempt.Run` **нет**. Известный settlement сохраняется известным; неизвестный резерв — неизвестным.
- Только подтверждённый `invalid_output` на этапе `extract_vacancy` допускает такой fallback. Для SQL ошибок, потери lease, смены provenance, ошибок `FinishWithin` вне callback и неопределённого COMMIT путь сохраняет консервативный прежний режим: **не** выполнять второй finish на основании недостоверного исхода. Если сама запись `SaveFailure` не завершилась, attempt остаётся незавершённым и требует безопасного разбора, а не retry провайдера.

## Синтетические проверки

`go test ./internal/parser -run 'TestExtractionOutputIssue|TestGO431Extraction|TestDescriptionFailureCategory' -count=1`

- Исходный контрактный набор `internal/parser/testdata/extraction-contract.json`: не менее 20 **реальных** отказов нормализатора, включая `source_unverified` в строгом режиме, JSON/enum/зарплату/дату, без утечки source/response.
- Таблица всех шести разрешённых формулировок `source_unverified`, нераспознанное приватное поле исключается.
- `context.Canceled`, `DeadlineExceeded`, `ErrMoneyUncertain` и произвольное сообщение провайдера: отсутствие ложной `outputIssue` и якобы известной стоимости.
- Все required CI на точном head SHA, без `SKIP`. Набор не обращается к MariaDB и платным провайдерам.

**Приёмка GO-431 late Save:** в `internal/storage/extraction_stage_test.go` независимый синтетический адаптер намеренно пропускает `req.ValidateContent` и передаёт отклоняемый ответ в `Save` **после** фактического `Billing.Run`. Проверяются два исхода стоимости (известная / неизвестная), единичный вызов, failed job/attempt, одна `vacancy_extractions` error с безопасной категорией, отсутствие success/handoff, освобождение leases. Отдельный существующий `late_save_rollback` оставляет SQL-ошибку в fail-closed состоянии. Для запуска: `go test -tags 'mariadb schedulerwrites schedulerbilling' ./internal/storage -run 'TestMariaDBExtractionStageSourceDisposable|TestMariaDBParserAIExecutionDisposable' -count=1` на **одноразовой CI DB**; не использовать production.
