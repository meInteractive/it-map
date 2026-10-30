# GO-355: настройка модели OpenAI для промтов

Существующий profile-model API теперь получает OpenAIService как catalog validator:
текстовый model ID, подтверждённый effort, доступность через models и непустой
каталог. Неизвестные capabilities не придумываются: для новых моделей остаётся auto.
Provider errors возвращаются безопасно, без ключей или сырых diagnostics.
JEV validator по-прежнему503 до отдельного каталога/цен и протокола.

Захват generation выполняется до body и до потенциального provider GET; guarded
SaveModel сравнивает поколение под lock **до** bootstrap/revision/update.
Одновременно сохраняется прежний CAS по model revision. Это закрывает окно restore
при медленной загрузке body или проверке моделей. Запрос старого поколения получает
409/restored и не создаёт singleton/не меняет настройки новой БД.

Совместимый SaveModel сохранён для внутреннего storage API; actual runtime HTTP
использует дополнительный generation interface. Остальные prompt/resume contracts
не меняются. Repository не вызывает провайдеров внутри SQL-транзакции.

Mock model-choice tests проверяют unknown efforts/no network, catalog membership,
empty catalog409 и закрытый JEV. Required native TestMariaDBAIModelChoiceGenerationDisposable
проверяет restore during body/provider-validation и отсутствие bootstrap rows,
затем guarded valid save. Положительный native gate требуется на точном CI SHA;
локальный skip не acceptance. Source model/transport/generation parents продолжают
проверяться обязательным legacyinterop CI. Реальные ключи/данные/сервер не менялись.
