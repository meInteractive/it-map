# Перенос парсера: текущий порядок спринта 1.1

Общий порядок и приоритеты работы ведутся в [PLANS.md](../PLANS.md). Ниже — контракты, ограничения и доказательства конкретных порций парсера.

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


GO-078: [PR #33](https://github.com/meInteractive/it-map/pull/33), `14b26cd`; [public/62 settings vectors](https://github.com/meInteractive/it-map/actions/runs/40308221146), [MariaDB/16 browser/API/native](https://github.com/meInteractive/it-map/actions/runs/40308221140) успешны.

## GO-079: historical upper costs и МСК

UpperCostJSON выполняет арифметику на JSON request metadata и не запускает work. Сохраняются free===true, Apify cap 0.26, historical OpenAI tariff allowlist/alias (unknown snapshot не угадывается), обязательная positive safe max_output_tokens, UTF-8 JSON payload bytes +4096 +256×input length и общий context bound JEV/pricing/fixed fee/число questions+1. JSON размер учитывает UTF-16 string input length, Unicode/HTML/U+2028/U+2029 в ключах и значениях без неправильной замены literal backslash-u. Поддерживаются обычные числовые provider prices/string context; весь произвольный JS Number coercion не обещается.

77 cost vectors и 10 day/month period vectors выполняют неизменные schedulerUpperCost/schedulerPeriod. Go periods используют фиксированный MSK UTC+3, как JS, включая переход UTC-полуночи/месяца/года/високосного дня. Исторические тарифы из openai-pricing.js сверяются по всем моделям/alias, но не объявляются актуальными тарифами для новых paid calls. Перепроверка тарифа/доступности нужна до подключения реального клиента.

Выявлены два допуска Go ParseFloat: числовые строки 1_0/0.0_1 принимались, JS Number их отвергает. Новые oracle cases воспроизвели ошибку до исправления, теперь отказывают. Отдельный source case показывает arithmetic overflow Infinity даже при finite operands; Go дополнительно fail-closed, не передаёт nonfinite upper bound в будущий SQL/provider. Negative/nonfinite synthetic metadata тоже отказывает.

Это не денежный ledger, не durable reservation и не гарантия exactly-once оплаты: full GO-017/021/023 и scheduler/idempotency/crash recovery остаются открытыми. free flag — доверенная metadata будущего внутреннего caller, а не публичное право пользователя обойти бюджет. Не подключены API/settings, SQL reservation, model client или scheduler.


GO-079 / [#34](https://github.com/meInteractive/it-map/pull/34), `ebc7a1d`: [public](https://github.com/meInteractive/it-map/actions/runs/40309279822) и [MariaDB/16 browser/API/native](https://github.com/meInteractive/it-map/actions/runs/40309279795) успешны.

## GO-080: durable scheduler cycle lease

RunRepository.Begin сериализует Go cycle ownership на app_runtime_generation(id=1) FOR UPDATE и читает current generation/clock/settings в одной транзакции. Время для lease берётся из MariaDB, не доверяется клиенту. SHA256 generation:requestId совпадает с legacy candidateHash; повтор существующего requestId всегда Reused=true/Acquired=false, не продлевает lease и не запускает work. Disabled settings дают paused; explicit diagnostic допускается как в beginSchedulerRun. Поддерживаемые Go triggers manual/automatic/probe, обязательный requestId≤500 UTF-16, duration до5мин/default110сек+30сек slack — границы ещё не подключённой API-функции.

Активные running циклы проверяются через все generation; смена generation сама по себе не разрешает второй owner до expiry. AssertActive — snapshot-проверка, она не заменяет atomic fence будущих job/provider mutations. Finish сверяет generation/status/lease в транзакции, stale/expired owner не меняет terminal receipt или новый цикл. Generic SQL errors не отражают DSN/query/input.

Recovery переводит expired cycles/attempts в interrupted, не меняя reserved_usd/cost_usd/provider/result_json. Только expired jobs текущей generation возвращаются pending либо failed по max_attempts, unexpired/другие generation не затрагиваются. Recovery и новый INSERT выполняются атомарно. В SQL MAX переведён в GREATEST для MariaDB; таблицы/DDL не меняются.

Именованный TestMariaDBSchedulerRunsDisposable под schedulerwrites tag проверяет реальные blocked SQL connections, replay/disabled/diagnostic, generation restore fencing, recovery job states/reserved result, fault после реально выполненного INSERT и UPDATE recovery, а также SIGKILL отдельного Go worker после commit lease/reservation и восстановление новым SQL pool. Cleanup владеет четырьмя заранее пустыми таблицами scheduler_runs/jobs/attempts/firecrawl_settings, возвращает их пустыми и проверяет исходную generation. Только GitHub CI+parsed loopback DSN/current-run marker; четыре required отрицательных guards отказывают до соединения без skips. Локально positive SQL не выполняется.

Это один Go/MariaDB контур, не блокировка отдельной D1/Sites среды. Background worker не запускается. GO-045 остаётся открыт: отсутствуют job claim/owned mutation facade, durable monetary reservation/provider idempotency, полная crash/double-charge/staging приёмка. До активации parser отдельно требуется исключить одновременно работающий Sites scheduler.
