# GO-358: execution boundary AI-стадий

`AIStageExecutor.Execute` принимает уже полученную непрозрачную `JobLease`. Четыре типа задания сопоставляются с title_classification/vacancy_extraction/vacancy_role/relevance; неизвестные типы закрыты. Промт, модель, роли и cache загружает server-owned `AIStageStore` в owned SQL-транзакции, job.payload не трактуется как инструкция модели.

Оплачиваемый вызов получает существующий `Attempt`, а `CheckActive` проверяет generation/run/job и `Store.Revalidate` перед внешней работой и после ответа. Provider I/O выполняется вне SQL TX. Cache завершается без provider и резерва. Частичные title решения сохраняются, но задание остаётся failed. Неизвестный paid outcome сохраняет резерв, повтор Execute не создаёт новый execution handle.

`Attempt.FinishWithin` добавляет ограниченный SELECT/DML callback к прежней атомарной записи attempt+job. Store повторно проверяет provenance и сохраняет результат в этой же транзакции. При поздней ошибке Save откатываются и данные стадии, и завершение; уже выполненный оплачиваемый POST не повторяется. Scheduler receipt содержит только model/reused, без prompt/response.

Required disposable MariaDB gate `TestMariaDBParserAIExecutionDisposable` проверяет dispatch4, reserve-before-work, cache, budget denial, unknown outcome/replay, late Save rollback и изменение provenance. Применяется fixture store с synthetic результатом, не бизнес-loader реальных кандидатов. Это не приёмка полного pipeline: конкретные SQL stage loaders/cache/hashes/slots, enqueue-next, источники, import/relevance persistence, ticks/API подключаются следующими задачами. Main runtime этим PR не активируется; сервер, историческая очередь и реальные провайдеры не затрагиваются.
