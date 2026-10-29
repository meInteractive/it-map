# Перенос парсера: текущий порядок спринта 1.1

Общий порядок и приоритеты работы ведутся в [PLANS.md](../../PLANS.md). Ниже — контракты, ограничения и доказательства конкретных порций парсера.

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


GO-080 / [#35](https://github.com/meInteractive/it-map/pull/35), `d48e4d7`: [public](https://github.com/meInteractive/it-map/actions/runs/40316420266) и [MariaDB/required cycle SQL+rollback+SIGKILL/recovery/16 browser/API/native](https://github.com/meInteractive/it-map/actions/runs/40316420306) успешны. Named SQL target и все шесть subcases фактически pass/no-skip. Test interceptor оборачивает prepared statements; поздняя ошибка следует после реально успешного mysql write.

## GO-081: Apify HTTP adapter на synthetic mocks

internal/integrations/apify использует фиксированный https://api.apify.com/v2 и заданный Actor shahidirfan~hh-ru-job-scraper. New(token) не читает env/SQL и не делает запросов. Start строит input из канонического public HTTPS HH source, one-page/100 list либо one detail, fixed proxy/cap0.25/timeout180/restartOnError=false/waitForFinish=0. Сохраняются query order/set/duplicates semantics, order_by и titleQuery NAME:(...) с UTF-16/control guards, очищается fragment. Общий WHATWG parser и произвольные JS coercions не обещаются: Go принимает typed options, nonnegative safe page/canonical UTF-8 URLs. Никакого нового API/startup wiring нет.

Run и DatasetItems/DatasetInfo выполняют только GET существующих ID; page cap10/limit1.8MB и metadata64KB, provider dataset ID/itemCount проверяются. Raw run metadata сохраняется без выдуманной стоимости; state соответствует legacy8 statuses. Optional modifiedAt ограничен поддерживаемым canonical RFC3339 input, полный Date.parse не переносится этой порцией. Успешный HTTP без допустимых данных даёт apify_data; 401/403/402/429/5xx, header timeout/body timeout/caller cancellation и bounded read повторяют проверенные legacy mappings. Redirect не следует, bearer token только в header, error/debug не отражают token/path/body. Client fmt/GoString редактирует credentials.

Любая ошибка после попытки POST помечается IsStartUncertain, сохраняет исходную typed error/cancel для caller и не вызывает automatic POST retry. Это не durable ledger: caller обязан до запуска получить подтверждённый SQL reservation/identity и после неопределённого результата сохранить uncertain. Эти стадии ещё не подключены. Ни один тест не обращается к api.apify.com: private test RoundTripper разрешает только provider origin, переписывает его на собственный httptest loopback и отказывает любому иному origin; JS oracle перехватывает fetch и блокирует live вызовы. Локальные socket tests требуют sandbox network grant, фактические запросы только loopback.

Source corpus: 36 HH input/36 HTTP/14 run-state/18 dataset vectors. Исходный checkedRun допускает missing/null/numeric ID из-за regexp coercion; fixture отдельно фиксирует legacyAccepted, Go отклоняет такой noncanonical provider reply до persistence. Canonical provider replies совпадают. Дополнительные cases воспроизвели до fix две ошибки Go: vacancy URL query Unicode/special encoding и text204 empty response; исправлены до PR. Fixtures содержат только synthetic HTTP payload/IDs, не реальные records/keys. GO-039/023/044 целиком не принимаются: настройки/credential wiring/durable budgets/source pipeline/staging не подключены.


GO-081 / [#36](https://github.com/meInteractive/it-map/pull/36), `93babc6`: [public/HTTP+HH source contracts](https://github.com/meInteractive/it-map/actions/runs/40320431546) и [MariaDB/scheduler/task/16 browser/API/native](https://github.com/meInteractive/it-map/actions/runs/40320431536) успешны. Новый Apify adapter принят в указанном mock scope, полные GO-039/023 и рабочий pipeline открыты.

## GO-322: stored scheduler settings без потери private state

Typed public Settings из078 не является storage document: пересериализация
только этого struct теряла бы unknown stored members и private cloudOwnerId.
Новый SchedulerSettingsView использует уже принятый ordered ECMAScript JSON
codec и trusted SchedulerDefaults/SupportedSources из v39.RestorePolicy.
Используются только исходные значения defaults; restoreScheduler не вызывается,
его намеренный reset enabled/cloud owner к обычному save не применяется.

Read повторяет schedulerSettings: defaults+saved, отдельный merge priorities,
legacy sourceIds fallback только при отсутствии own property (null сохраняется),
favoritesOnly из selected SQL flag. PublicJSON и generic MarshalJSON удаляют
cloudOwnerId и выводят cloudConnected по исходной truthiness. Этот bool означает
наличие сохранённого owner ID, не проверку живого облачного подключения.

После существующего parser.ValidateSettingsUpdate gate NextFromValidated
сохраняет private/unknown fields и сериализует source normalization. StorageJSON
отделён от public DTO; даже json.Marshal whole view/change безопасен для owner.
Возвращённые byte slices не меняют owned state, построение change не мутирует
старую view. FavoritesOnly не записывается в scheduler_json: отдельный SQL flag
и shared Firecrawl revision будут обновляться в durable write phase.

Required TestStoredSchedulerSettingsSource сравнивает2214 read/validated-save
frames с неизменёнными strict ESM function bodies. Source queries представлены
явными read sets, выполнение останавливается перед SQL UPDATE. Проверяются
fallback absent/null, typed valid updates через078, defaults/priorities,
private/unknown members, arrays/numeric keys, duplicate stored revision,
max safe revision, lone UTF16/Infinity и HTML JSON escaping. StorageJSON и
PublicJSON совпадают с source stringify; generic encoding сравнивается после
ECMAScript parse/stringify, поскольку Go JSON transport экранирует HTML.

Negative probe без owner redaction реально ломает source gate, затем исходник
восстановлен; unit проверяет immutability/byte ownership и generic serialization.
Это pure serialization proof. Никакие Repository/API/SQL/provider calls или
background parser не активируются. CAS по исходному JSON1 revision (в том числе
first duplicate против JSON.parse last duplicate), shared favorites revision,
generation/rollback/concurrent writers, SQL representability и API права
остаются следующими отдельными проверками GO-021.

GO-323 добавляет SettingsRepository без подключения HTTP или фонового исполнения.
Read использует SELECT-only repeatable-read snapshot: generation, сохранённый
JSON и legacy source scope читаются согласованно. Отсутствующий singleton даёт
defaults, GET не создаёт строки. Save сохраняет исходную generation до попыток,
блокирует generation и singleton, применяет GO-078 validation и GO-322 raw codec.
CAS повторяет SQLite JSON1: первая revision, числовое сравнение без приведения
строк, bool как 0/1; сериализация по-прежнему использует последнюю revision.
2324 сравнения проверяются непосредственно с pinned SQLite JSON1.

UPDATE меняет только scheduler_json и явно переданный favorites_only. Shared
Firecrawl revision увеличивается лишь при изменении этого флага; unrelated
provider fields не читаются в публичный DTO и не перезаписываются. Только
известный MariaDB deadlock1213 допускает ограниченную повторную транзакцию с
исходным CAS/generation. Неизвестный результат COMMIT возвращает ошибку без
повторного UPDATE; следующий read позволяет сверить фактически сохранённое.

В captured MariaDB schema DEFAULT scheduler_json содержит преобразованные
backticks вместо JSON quotes. Схема и metadata не меняются: первый Save явно
задаёт valid JSON revision0 внутри той же транзакции. Неуспешный initial save
откатывает эту строку. Это целевое atomic/read-only поведение; оно не доказывает
совместимость source lazy bootstrap, который может создать строку до ошибки.

Required disposable CI сравнивает42 перехода actual unchanged source SQLite
с MariaDB, включая полную строку и SELECT-only read. Ещё6 ownership и4 fault
cases проверяют двух писателей, shared revision, overflow/rollback, cancellation,
rotation generation, late SQL failure, actual unknown COMMIT и snapshot race.
Oracle использует подготовленную схему и не проверяет DDL/bootstrap. Положительная
SQL приёмка фиксируется отдельно после required CI; локально эти тесты не
подключаются к БД. Административный HTTP, commands/dry-run и полный pipeline
по-прежнему требуют отдельных задач GO-021/046.


GO-324 подключает только PUT /api/scheduler/settings к SettingsRepository в
cmd/itmap. Session → admin → storage → Origin проверяются до чтения тела.
Сохраняются source32KiB byte limit, отсутствие требования Content-Type,
BOM TextDecoder, error/code/status и auth-derived ownerName. Исходный роутер
читает JSON до method fallback: malformed/empty/non-object дают generic500,
oversize413 с code=storage, корректное тело неправильного метода —405.
Эти необычные ответы сохраняются сознательно; новый GET/settings не вводится.

Required native gate сравнивает70 ответов полного unchanged source router на
actual SQLite с Go handler/MariaDB: JSON/status, полная shared строка и generation.
Source schema заранее подготовлена, bootstrap/DDL не принимаются этим gate.
Отдельные4 target-only negative cases проверяют принятую GO-078 duplicate-input
policy и прежний Go Sec-Fetch-Site guard (same-site/cross-site):400/403 без записи.
Они не объявляются source parity: source JSON.parse допускает duplicate keys,
а Origin-only guard не проверяет Fetch Metadata. Как и прочие Go mutation routes,
host-preserving proxy boundary не доверяет X-Forwarded headers; внешний HTTPS
rehearsal остаётся отдельным gate GO-008/320.

enabled=true сохраняет конфигурацию и не запускает обработчик. Scheduler worker,
provider client, internal tick, cloud owner binding, run/probe и dashboard в этой
порции не подключаются; неперенесённые authenticated endpoints остаются503.
Полная GO-021 требует последующих dashboard/commands/dry-run/scope проверок.


GO-325 переносит только schedulerAttemptDetails для явного dashboard SELECT
read set. PrepareSchedulerAttemptJSON всегда исключает result_json; unknown SQL
columns отказывают до построения ответа. Наружу попадают лишь прежние selected
columns и ограниченный outcome: accepted:boolean, до12 положительных уникальных
safe integer roleIds/filtered, либо superseded:true с разрешённым nextStage/null.
Код переиспользует ordered ECMAScript codec, включая JSON.parse last duplicate,
IEEE numbers и Unicode. Некорректный/усечённый result_json просто не даёт outcome,
как в source; raw result, prompts и произвольный provider payload не возвращаются.

368 actual unchanged function frames и отрицательная проверка удаления redaction
проверяют pure projection; это не SQL/API gate. Выбор последних50 attempts,
LEFT JOIN, SQL substr2048 characters, isolation и generation будут проверены в
следующей dashboard порции326. Передавать SELECT * вместо reviewed columns
запрещено: safety allowlist намеренно уже общего object-rest source helper.


GO-326 подключает GET /api/scheduler к SELECT-only repeatable-read снимку.
Generation, settings, current-generation counts/20 runs, последние50 global
attempts с явным LEFT JOIN, statistics и расходы читаются согласованно. История
attempts и денежные агрегаты не ограничиваются текущим generation; перенос или
restore не скрывает прежние расходы. SQL substr2048 characters применяется до
GO-325 projection; result_json/private cloud owner в ответ не попадают.

Роутер сохраняет admin/session/storage права и отсутствие GET-body parsing;
неподдерживаемый метод проходит общий32KiB body gate перед405. Новые worker,
provider client, cloud binding, probe/run/queue/диагностика не подключаются.
Расписание и cloudStatus — проекция сохранённых сигналов, а не обещание живого
облачного подключения. Время берётся один раз на запрос: московские сутки/месяц,
75 минут и округление до часа проверяются по source Date.now fixtures.

SQLite SUM отличается от MariaDB: integer overflow, real/text affinity, числовой
префикс и компенсированная сумма влияют на статистику и финансовое представление.
Read-only расчёт переиспользует SQLite AtoF из принятого JSON1 gate и перенос
public-domain sumStep/Kahan–Babuška–Neumaier из SQLite3.53.3 src/func.c:
https://github.com/sqlite/sqlite/blob/version-3.53.3/src/func.c
260 actual SQLite sequences проверяют типы, NUL/whitespace, int64 boundaries,
переход к real после overflow, cancellation и Infinity/NaN. Денежная запись и
budget reservation GO-313 не меняются. Положительный reserve остаётся учтённым;
NULL cost использует reserve, NULL credits сохраняется какNULL.

Source SQL без полного ORDER BY не обещает порядок строк с одинаковым timestamp;
новый ID tie-break для history не вводится. Тест сравнивает полные записи внутри
одинакового timestamp как множество, проверяя DESC между группами; остальные
поля/массивы не переупорядочиваются. Pure sum proof использует явный
порядок прочитанных значений. Для расходов поток идёт по started_at/type, а группы
выдаются в исходном бинарном порядке type. Полный согласованный private snapshot
по-прежнему относится к GO-030, не подменяется этими синтетическими проверками.

Required native gate сравнивает36 состояний unchanged router/SQLite и144 Go→
MariaDB HTTP responses, включая20/50 limits, old-generation spend, first duplicate,
числовые строки/bool, большой integer/Infinity и compensated credits. Node SQLite
int64 явно преобразуется в JS Number, как read values D1; source код не меняется.
Driver-specific ERR_SQLITE_ERROR не принимается за переносимый API-код: integer
SUM overflow проверяется отдельным target/native refusal, а не подменённым JSON
source ответом. Дополнительные5 native cases проверяют missing singleton без
INSERT, actual concurrent generation snapshot, поздний SELECT fault без частичного
DTO, cancellation и integer overflow. Перед/после GET сравнивается содержимое
всех72 таблиц; reader также обязан получить1142 при пробе UPDATE с пустым WHERE.
Положительная SQL приёмка326 фиксируется после CI. Пока parent021 остаётся открыт.


## GO-328: поколение настроек до чтения запроса

Исходный `schedulerRoute` вызывает `candidateDb` до `readClassificationInput`.
В Go `PrepareSave` фиксирует поколение и authenticated owner до чтения body;
возвращаемая серверная операция не удерживает соединение или транзакцию.
Запись затем сверяет это поколение под прежней блокировкой, включая повторы
после deadlock. Клиент не передаёт generation.

Это не позволяет запросу, начатому до восстановления, сохранить настройки
в новом поколении при совпавшей revision. Ошибки разбора body и fallback метода
сохраняют source precedence. `check-scheduler-settings-body.mjs` проверяет восемь
сценариев на неизменённом source router/SQLite; native HTTP/body barrier,
сохранность 72 таблиц и pool из одного соединения проверяются только в guarded
MariaDB CI. Локальные unit/source/build проверки проходят; положительный
native proof до завершения CI не заявляется.


## GO-329: retry/cancel очереди

Перенесён существующий POST `/api/scheduler/jobs/:id/retry|cancel`: admin/session,
Origin и предел body сохраняют исходный порядок ошибок; generation фиксируется
до body. Под generation lock меняются только шесть исходных полей задания.
Payload, lease, completed_at и финансовый журнал сохраняются. Команда не запускает
worker и не создаёт платную попытку. Подробности — [JOBS.md](../../internal/parser/JOBS.md).

Source router/SQLite даёт 43 frames/45 HTTP responses, включая повтор в тот же
clock tick, текущие/старые поколения, статусы, права и body restore. Native CI
обязан сопоставить полные строки jobs/attempts и неизменность остальных 71 таблиц,
а также поздний UPDATE rollback, unknown COMMIT, restore и concurrent running.
До завершения CI положительная native-приёмка не заявляется.


## GO-330: чтение очереди

GET `/api/scheduler/queue` сохраняет source query validation, поиск по company/agency,
created_at DESC/id и страницы по 30 строк. URLSearchParams декодируется с WHATWG
replacement и последним значением дубликата. Сравнение имени использует принятые
UTF16/trim/lower/includes; locale SQL и LIKE не участвуют. Допуск owner ID через
JS Number отделён от SQLite INTEGER affinity: hex/binary/Unicode whitespace
могут быть допустимым фильтром с пустым результатом.

Owner search сохраняет D1 Number → JSON.stringify → json_each для больших ID:
в MariaDB используются два JSON binding и JSON_TABLE с точным DECIMAL, чтобы не
превысить число SQL parameters и не заменить сравнение целых float-сравнением.
JSON DTO проходит принятый source codec. Payload и невидимые служебные столбцы
не читаются. Поколение фиксируется до query validation/fallback метода, а
count/page/имена/title читаются в одном SELECT-only repeatable-read снимке,
по принятому в GO-326 правилу согласованного чтения.

104 unchanged source router/SQLite frames покрывают HTTP/фильтры/пагинацию/UTF16,
включая округление больших ID и разделение owner namespaces. Native CI требует
readonly account, неизменность 72 таблиц и четыре snapshot/fault/cancellation
сценария. Положительная native-приёмка до CI не заявляется.
