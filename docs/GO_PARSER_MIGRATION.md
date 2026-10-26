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
