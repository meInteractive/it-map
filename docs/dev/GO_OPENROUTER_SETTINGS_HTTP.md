# GO-353: status/key/check/refresh/logs OpenRouter

Перенесены source публичная проекция сохранённого cache, metrics, active test
model и последние50 metadata logs; GET не создаёт singleton и не выполняет DDL.
Как и source, устаревший каталог автоматически обновляется при GET существующей
настройки; это **не SELECT-only endpoint** при истёкшем cache. Fresh/unconfigured
чтение не выполняет provider calls и не инициализирует хранилище.

`/key` сохраняет ciphertext с generation fence до body и запускает обновление
каталога. `/check` вызывает read-only provider key endpoint; `/refresh` запрашивает
models/benchmarks параллельно, затем сохраняет только отфильтрованный рейтинг.
Сеть вне SQL TX, cache/status update проверяют generation и revision под lock.
Если ключ изменился/выполнен restore во время сети, старый ответ не перезаписывает
новые настройки. Ошибка провайдера сохраняет прежний список, last_error содержит
только безопасное сообщение. Stored credentials не выдаются в public DTO/logs.

Session/admin/path/method/Origin guards и2048-byte body применяются до операций.
`/test` и `/jev` пока503: generation/JEV wiring отдельно. Переход от отсутствия
этих routes к готовому parser этим subset не заявляется. Тесты не используют
реальные ключи, провайдеры, существующую БД или рабочий сервер.

`check-openrouter-public.mjs` выполняет actual unchanged `orPublic` над synthetic DB
с запрещённым fetch. Native required TestMariaDBRouterSettingsDisposable сверяет
полный public DTO для4 source frames, metrics/null/active model, no-write fresh
read, generation/revision refusal и сохранение предыдущего cache. Local mock
refresh/error/stale and HTTP admin/method/origin/body tests проходят. Native SQL
положительный результат должен подтвердиться на точном CI SHA, local skip не gate.
