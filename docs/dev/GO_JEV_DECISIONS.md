# GO-356: каталог и транспорт JEV

JEV использует `typesafe/jev-1.13` из `models?output_modalities=decisions` с положительной ценой prompt. Автоматический запрос направляется только в `https://openrouter.ai/api/alpha/decisions`; chat completions и бесплатная подмена модели не используются.

GET `/api/openrouter/jev` доступен администратору и возвращает прежний source DTO. Выбор JEV в настройках промта проверяет каталог, не отправляя платный POST. Реальные API-ключи, сервер и данные при разработке не затронуты.

`RouterService.AutomaticDecisions` требует `Billing`, совместимый с существующим `parser.Attempt`: pricing, contextLength и полный payload передаются ledger до внешней работы. Повтор POST отсутствует. Generation/revision/context проверяются до резерва, непосредственно перед POST и после ответа. Неопределённые денежные результаты остаются в существующем conservative ledger. При 402 возвращается безопасная ошибка credits.

Actual JavaScript oracle проверяет успешный каталог и отказ для нулевой/отсутствующей цены. Mock tests проверяют точные endpoint, запрет replay, бюджет, смену ключа/restore перед POST и admin/read-only HTTP. SQL-схема не менялась, SQL gates предыдущего стека сохраняются.

Это первый слой JEV: типизированные relevance/title/leaf решения и подключение стадий парсера следуют отдельно. Автоматический worker и историческая очередь не запускаются. Live provider smoke не выполнялся. Деплой запрещён текущим поручением пользователя.
