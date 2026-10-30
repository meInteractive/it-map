# GO-354: полный ручной запрос OpenAI/OpenRouter

Теперь `/api/openai/test` и `/api/openrouter/test` выполняют **явный POST
администратора**: ключ → каталог → payload → provider → проверенный ответ → metadata.
При разработке это доказано только mocks/source/native CI; live providers не
вызывались, реальные ключи и сервер не менялись. Ручной OpenAI test потенциально
платный при последующем использовании владельцем, как в исходном приложении.

Actual source oracle:8 `runOpenAI` frames и6 `runAI` OpenRouter frames над synthetic
DB/fetch. Сверяются payload и результат/ошибки. OpenAI: Responses API, system→developer,
max_output_tokens минимум32768, store:false, только подтверждённый reasoning effort,
completed/assistant text/refusal/model identity/date-snapshot checks. POST не
повторяется при потере ответа. OpenRouter: только ranked free models, max_price0
и require_parameters, reasoning compatibility fallback, серверный model fallback,
refusal/finish_reason/model/content checks. Не включать платную модель скрытым fallback.

`Generator.Automatic` требует Billing; `parser.Attempt` совместим с интерфейсом.
Metadata reserve/settle окружает transport, до отправки POST. Отказ бюджета не
отправляет provider request; unknown paid outcome не повторяется. Это точка
подключения будущей стадии, **не запущенный dispatcher**. Parser/queue/JEV и
structured health ordering/metrics остаются отдельными gates. Structured automatic
использование не объявляется готовым по ручному connection-test.

ManualTests получает сохранённый cover_letter config OpenAI и проверяет generation
после ответа. Router receipts/last_used обновляются атомарно под generation+revision
locks. Old response после restore/ротации ключа не пишет новую БД. Logs содержат
только model/task/result/error-code и source retention200; prompt/ответ/key не пишутся.
Source OpenAI manual test не создаёт отдельный log receipt. Ошибки сохраняют безопасные
коды/сообщения без сырых provider diagnostics.

Handler проверяет auth/admin/method/Origin до работы. Для test-only установлен
context335s/write deadline340s, обычные routes не меняются. Nginx-шаблон получил
отдельный test location с read timeout350s. **На существующем сервере необходимо
обновить этот location перед приёмкой долгих запросов**; сам PR конфигурацию сервера
не устанавливает. Общий лимит335s может остановить длинную последовательность
free-model fallback; нет автоматического повторения платного POST.

Required TestMariaDBAIManualTestDisposable в disposable CI: atomic receipt,
отсутствие content в logs, generation/revision changes during provider wait.
Mock HTTP подтверждает, что unauthorized/cross-origin/GET не запускают test.
Локальный tagged skip не positive SQL gate; требуется результат конкретного CI SHA.
Внешний live smoke/ключи/финальные тарифы при последующей приёмке отдельно.
