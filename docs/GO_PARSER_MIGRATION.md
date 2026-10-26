# Перенос парсера: текущий порядок спринта 1.1

Поручение пользователя 26.10.2026: сначала доступные high/critical части парсера, затем medium, с сохранением prerequisites. GO-073…075 отложены без закрытия. После блока парсера приоритетны critical write gates GO-010/GO-012, далее GO-041→042 после необходимой схемы GO-016 и CRM-ядра. GO-032 средний.

GO-076 / #31 сохраняет 29458b2 с прежними task/channel результатами и main=b3d4820 без code diff. Checkpoint 534e8a9 прошёл public 40306369291 и MariaDB/16 browser/API/native 40306369523. Новый стек направлен в main; закрытые parent branches не считаются автоматически частью main.

## GO-077: synthetic legacy crypto

internal/legacycrypto не читает env/SQL и не подключён к HTTP/provider settings. Производный ключ — SHA-256 UTF-8 строки google-calendar: + SESSION_SECRET + provider suffix. Существующий алгоритм AES-256-GCM, nonce 12 bytes, tag 16 bytes, без AAD; envelope raw base64url(iv).raw_base64url(ciphertext+tag). Encrypt использует crypto/rand; deterministic nonce доступен только package-private test helper.

| Domain | Суффикс |
| --- | --- |
| Calendar | пустой |
| OpenAI | :openai:v1 |
| OpenRouter/JEV | :openrouter:v1 |
| Firecrawl free/hobby | :firecrawl:v1 |
| Apify free/starter | :apify:v1 |

scripts/check-legacy-crypto.mjs выполняет настоящие неизменные legacy helpers, фиксирует 15 явно synthetic vectors и проверяет namespace sites в provider sources. Fixture содержит только известные synthetic strings, не реальные API keys/tokens. Каждый deterministic test nonce различается. Go сравнивает decrypt и ciphertext/tag побайтно; отдельный legacyinterop target шифрует с реальным случайным nonce и передаёт 15 результатов JS decrypt через stdin. Child process ограничен 15 секундами; отсутствие Node — failure, не skip.

Локальная проверка: node scripts/check-legacy-crypto.mjs; go test -race -tags legacyinterop ./internal/legacycrypto -count=1. CI явно устанавливает Node 24 перед required Go→JS target и отдельно проверяет fixture source oracle в web job. Tampered nonce/ciphertext/tag, wrong domain/secret, malformed/oversized/noncanonical encoding не возвращают plaintext и дают только статические ошибки без входных данных. Нулевой Codec/пустая конфигурация тоже отказывают. Максимум plaintext 64 KiB, envelope 100 KiB; принимается только canonical unpadded URL-safe двухкомпонентный формат старого encrypt, без permissive alternate/trailing encodings.

Это проверка совместимости, не приёмка стратегии секретов: ADR-016 остаётся предложенным, полный GO-040 открыт. Без исходного session secret ciphertext нельзя восстановить; fallback на другой domain/secret или пустое значение отсутствует. Реальные encrypted settings/токены не читались и не переносились. Новый Go parser, background scheduler и платные провайдеры не включаются. Durable budgets/leases/import ещё требуют собственных SQL/crash/HTTP gates.


GO-077: [PR #32](https://github.com/meInteractive/it-map/pull/32), `75ba9d7`; [public/required interoperability](https://github.com/meInteractive/it-map/actions/runs/40307364593) и [MariaDB/16 browser/API/native](https://github.com/meInteractive/it-map/actions/runs/40307364181) успешны. Это приёмка synthetic codec, не подключение encrypted settings.

## GO-078: pure settings contract

ValidateSettingsUpdate разбирает только известные JSON fields, проверяет enabled/числовые бюджеты 0…10000/safe integer revision, поддерживаемые sourceIds без повторов, интервалы 1/3/7, bool области организаций и приоритеты восьми стадий 0…100. Неизменный validation/normalization блок saveSchedulerSettings выполняется до SQL с fixed synthetic previous state; 62 source vectors включают omitted/null/type/error messages/defaults/options. Partial priority updates сливаются с previous без мутации входного состояния. Source options и defaults тоже сравниваются с JS, unsupported источники не становятся поддержанными.

Возвращаются normalized next settings и отдельная expectedRevision для будущего SQL CAS. Отрицательная input revision остаётся safe integer по legacy input-контракту; future SQL CAS обязан вернуть conflict, она не превращается в разрешение записи. Owner берётся из caller context, поля ownerName/cloudOwnerId в JSON отвергаются; наружу pure model содержит только cloudConnected. Дополнительный JSON boundary отвергает duplicate keys/trailing/oversized/nonfinite JSON; это явное строгое ограничение ещё не подключённой Go-функции, без изменения HTTP API.

Отдельно открыты: admin/session/Origin API, SQL CAS/rollback/readback настроек, проверки реального source scope/ролей, dry-run, durable jobs/leases и запрет двойного parser. Наличие pure input validation не закрывает GO-021/GO-045.
