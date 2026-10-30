# GO-351: HTTP-настройки OpenAI

После родительских348/349 main получает `/api/openai`, `/api/openai/key` и
`/api/openai/refresh`: статус/каталог, шифрованное сохранение, явный refresh.
Routes защищены session/admin; unknown path404, wrong method405, чужой Origin403,
key body2048 bytes. Generation фиксируется до чтения body, SaveKey проверяет её
под lock; concurrent restore не может получить ключ старого запроса.

Неподтверждённые ciphertext не сбрасываются: GET возвращает configured=true,
пустой каталог и понятное предупреждение о повторном вводе; refresh возвращает
ошибку409. Ключ вводится только в password input UI; plaintext/envelope не входят
в ответ. Авторизационный SESSION_SECRET автоматически не меняется.

Клиент25s и six-hour key-bound cache уже проверены source/mocks. OpenAI env fallback
остаётся только при отсутствии stored key. Метрики letterTiming, profile-model
validator и `/test` ещё отдельные gates. `/test` намеренно503 с
backend_not_migrated до ledger/provenance интеграции; оплачиваемый POST этим PR
не активируется. OpenRouter HTTP и parser пока не перенесены.

Mock HTTP проверяет auth/admin/path/method/origin/body size, safe status и
capture generation до body. Required TestMariaDBOpenAISettingsHTTPDisposable
в existing guarded CI проверяет actual handler→service→AES-GCM→SQL,
отсутствие initialization на GET, denial сторонним ролям и restore во время body
с409 без bootstrap rows. Никаких real keys/provider calls/server data в тестах.
Положительный native HTTP gate должен подтвердиться на точном PR SHA.

Это изменение кода; на текущем сервере оно ещё не развёрнуто. Readiness503 с
business_api_not_migrated и запрет production не меняются. Общая миграция parser
и весь GO-036 не закрываются по этому HTTP subset.
