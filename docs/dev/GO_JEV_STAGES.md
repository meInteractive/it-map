# GO-357: типизированные решения JEV

После GO-356 общий `Generator.Automatic` принимает JEV для relevance и vacancy_role, а `RouterService.ClassifyTitles` выполняет независимые бинарные вопросы ролей в одном оплачиваемом запросе. Все пути требуют billing и не повторяют POST при неизвестном исходе.

Сравнение с неизменёнными `server/integrations/jev.js` и `jev-leaf-roles.js`: 16 actual source frames на synthetic fetch/DB/ledger. Сравниваются полный payload, billing metadata, нормализованный результат и ошибки. Relevance использует тип score 0..9, округление в 0..100; title сохраняет decision:null/error для неполного ответа; leaf требует точный набор choice-ответов, максимум12 ролей, сортировку ID. Консервативный предел contextLength запрещает усечение оплачиваемых данных.

Метаданные результата пишутся в существующие openrouter_ai_logs и last_used_* под generation/revision locks, без prompt/ответа/сырой ошибки. Требуемый disposable CI test проверяет записи, partial-title failure и запрет stale metadata после restore/смены ключа; реальные SQL-данные не используются.

В AIResult возвращается нормализованный message/usage/model/fallback, timing и сырые answers провайдера не являются публичным контрактом нового внутреннего Go-интерфейса. HTTP DTO прежних routes не менялись. Автоматический parser worker, durable classification slots и SQL cache/provenance стадий подключаются отдельной GO-358; одних этих protocol adapters недостаточно для рабочего конвейера. Реальные вызовы и деплой не выполнялись.
