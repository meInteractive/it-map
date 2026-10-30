# GO-348: HTTP-клиенты OpenAI/OpenRouter

`internal/integrations/aihttp` реализует изолированный transport. Только фиксированные
HTTPS API paths: OpenAI models/responses, OpenRouter models/ranked models/key/
benchmarks/chat completions. Ключ передаётся конструктору, env/БД не читаются;
String/GoString скрывают credentials. Redirects запрещены, TLS проверка штатная.

42 кадра actual `openaiFetch`/`orFetch` из неизменённого JS воспроизводятся через
`scripts/check-ai-provider-transport.mjs` с полностью заменённым global fetch.
Обязательная CI-проверка `TestUnchangedProviderTransportSource` сверяет
method/path/headers/error message/code/status и ProviderStatus; fixtures синтетические.

Таймауты:25s чтение,300s OpenAI POST,210s OpenRouter POST. Cancellation сохраняет
context identity. Клиент не повторяет запросы, GetBody=nil исключает replay POST.
Ошибка после отправки POST помечается IsUncertain: это консервативный сигнал для
будущего durable billing, не разрешение повторить POST или вернуть резерв.
Provider errors и network errors не содержат исходных messages/ключей/тел запросов.

Дополнительные ограничения безопасности: response8MiB, request2MiB, object JSON,
без перенаправления авторизации на чужой host. Исходный JS не ограничивал размер
ответа и мог пропустить некоторые scalar JSON: Go отклоняет их безопасной ошибкой.
Это не изменение модели данных/SQL/API публичного приложения.

Клиент пока не подключён к main/runtime, settings, paid requests или parser.
Следующий gate GO-349: model catalog/crypto/settings/API/native SQL; потом GO-350
связывает стадии с ledger. Транспорт сам не доказывает готовность parser.
