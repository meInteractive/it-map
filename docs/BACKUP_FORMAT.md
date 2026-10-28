# Техническая спецификация бэкапа v39

Актуально на23.10.2026, документация224. Формат `moscow-it-employers-backup`; текущая версия39, полные v14–39, SQL0083/84миграции. Версия38 добавляет nullable agency_id к vacancy_candidates/vacancy_source_parsing и сохраняет agenciesEnabled в scheduler_json; ссылки компании/агентства взаимоисключающие, числовые ID из разных пространств не смешиваются. Старые v14–37 дополняются agency_id=null; отсутствующий agenciesEnabled читается как false. Временные jobs/discovery/leases не входят в storage и после restore отменяются прежним epoch. Версия37 добавила openrouter_model в vacancy_role_settings; для v33–36 остаётся прежний best_free с предупреждением.


## Изменения v39 и границы восстановления

`parser_error_daily` и `parser_candidate_intakes` теперь прикладные: сохраняются все дневные наблюдения P1/P2 и точное происхождение первичного получения вакансий. Это сохраняет историю/средние графика ошибок и фильтрацию воронки по парсеру; поздний провайдер полного текста не подменяет исходный парсер. Файл v39 требует оба раздела, включая пустые. Старые v14–38 принимаются с предупреждением: дневные наблюдения назначения сохраняются, provenance сохраняется только для совпавшего creation_token восстановленного кандидата; на пустой базе данных нет, парсер неизвестен.

JSON восстанавливает данные в совместимом приложении. Для восстановления всего сайта нужны также исходники и SQL-миграции из Git, конфигурация hosting/bindings, пароли и переменные среды (особенно SESSION_SECRET и OAuth), доступ к внешним сервисам, домен и облачные расписания Sites. Они не содержатся в JSON. Текущие связанные облачные расписания нужно создать или привязать отдельно; их количество не зашито в формат.

После импорта планировщик выключен, cloudOwnerId пуст, Apify и разрешение платного тарифа выключены; настройки выбора источников, периодов, ролей и сохранённые результаты этапов переносятся. Временная очередь, незавершённые сетевые запросы и блокировки не воспроизводятся. Журналы расходов назначения не откатываются. Нельзя обещать точное возобновление старой очереди или восстановление расходов/ошибок каждого задания на другом сервере. После проверки подключений включите нужные сервисы, привяжите облачный триггер и возобновите штатный планировщик. Его состояние и Apify-прочность намеренно отличаются от экспортированного файла; остальные прикладные значения должны совпасть.

## Состав файла

GET /api/backup читает все 52 прикладные таблицы: каталог, постоянные ID, общий и личный CRM, отклики/историю, задачи, вакансии/состояния, дерево ролей и связи, резюме, промты, выбор моделей, личные письма, контакты, каналы/источники, подключения календаря, OpenRouter/OpenAI и аудит. Все строки и столбцы читаются без пагинации, временных фильтров и сокращения текста. Удалённые до создания копии записи в неё не попадают.

| Раздел | Содержание |
| --- | --- |
| format, version, createdAt | Идентификатор формата, версия, дата создания |
| storage.tables | Таблицы, имена колонок, исходные значения строк, включая даты и JSON-строки |
| catalog | Удобное представление; при v16–33 исходный app_catalog.data_json первичен |
| manifest | Имена таблиц, наборы колонок, количества строк |
| integrity | SHA-256 от UTF-8 JSON.stringify(payload без integrity) |
| metadata.schema | SQLite DDL для анализа; загруженный SQL не выполняется |
| metadata.sequences | Верхние значения AUTOINCREMENT прикладных таблиц |
| metadata.migrations | Журналы d1_migrations, __drizzle_migrations, __appgarden_migrations, если есть |
| metadata.missingTables | Таблицы, ещё не созданные на источнике, с пустыми разделами |

Пароли, исходники, переменные среды и SESSION_SECRET в бэкап данных не входят. Зашифрованные календарные токены, ключи OpenRouter, OpenAI и Firecrawl входят; на другой среде нужен прежний ключ шифрования или повторное подключение. Бэкап кода из Git — отдельная операция и не копия живой D1.

Девятнадцать операционных таблиц `app_runtime_generation`, `application_creation_receipts`, `cover_letter_generation_leases`, `vacancy_candidate_classification_leases`, `vacancy_discovery_runs`, `firecrawl_request_lease`, `firecrawl_usage_periods`, `firecrawl_usage_events`, `vacancy_classification_jobs`, `vacancy_classification_slots`, `vacancy_detail_jobs`, `vacancy_detail_leases`, `vacancy_import_batches`, `vacancy_import_write_context`, `scheduler_jobs`, `scheduler_runs`, `scheduler_attempts`, `apify_runs`, `apify_account_usage` не экспортируются в storage/manifest/sequences и не принимаются из файла. Их DDL может присутствовать только в metadata.schema. _cf_*/sqlite_* не переносятся как прикладные таблицы; sqlite_sequence обрабатывается отдельно. Неизвестная прикладная таблица/колонка вызывает отказ, а не тихий пропуск.

## Экспорт и проверка

Экспорт читает данные одной D1 batch-транзакцией и проверяет схему до/после чтения. Он не выполняет инициализацию, миграцию и нормализацию рабочей базы. Поле требует согласованного изменения схемы/формата и теста совместимости.

`POST /api/backup/validate` принимает непосредственно JSON-файл, не `{backup:...}`. Нужна административная сессия; присутствующий чужой Origin отклоняется. Проверяются версия, контрольная сумма, обязательные таблицы/столбцы, поля каждой строки, лимиты, каталог и FK. Предварительная проверка данные не меняет. Контрольная сумма выявляет изменение файла, но не подтверждает автора. Нельзя менять version вручную без проверенного преобразования.

Лимит: 25 МиБ, до 200000 строк и 52 таблиц. Оба POST проверяют фактические потоковые байты даже без Content-Length. Экспорт проверяет размер форматированного JSON и возвращает ошибку вместо обрезания. Для большей базы нужен отдельно организованный полный SQL-дамп.

## Транзакционное восстановление

`POST /api/backup` повторяет проверку, затем одним batch создаёт недостающую доверенную структуру, заменяет прикладные строки, восстанавливает последовательности, сверяет значения/количества/FK и задаёт новый UUID поколения базы. SQL берётся только из миграций репозитория. Даже изменение значения триггером вызывает откат. Ошибка любого этапа откатывает данные и поколение вместе; после commit дополнительной нормализации нет.

Операционные строки не заменяются. Новое поколение блокирует прежние AI-запросы, lease и контексты создания откликов, даже после идентичного restore. Неудачный restore поколение не меняет. HTTP-генерация и операции AI используют SQL guard того же поколения и owner fencing; запоздалый результат не записывается в восстановленную базу.

Журналы миграций назначения не заменяются журналами источника. На новой базе сначала примените исходную SQL-историю проекта, затем восстанавливайте файл. Restore может подготовить полностью пустую базу, но не помечает начальные миграции выполненными: после такого bootstrap нельзя повторять их вслепую.

## Совместимость версий

| Версия | Добавление / правило |
| --- | --- |
| 14–15 | Полные старые layouts с обязательными полями; нормализация каталога, без точного raw-каталога/счётчиков/современных метаданных; предупреждения |
| 16 | Точные SQL-значения, raw JSON, manifest/integrity/metadata и последовательности |
| 17 | vacancySources.dreamJob и hCareers |
| 18 | user_profiles — резюме и редакции |
| 19 | openrouter_settings и openrouter_ai_logs |
| 20 | system_prompts — общие промты |
| 21 | vacancy_cover_letters — личные письма |
| 22 | openrouter_model_metrics — время генерации моделей |
| 23 | prompt_model_settings — провайдер/модель/рассуждения каждого промта |
| 24 | openai_settings — зашифрованный административный ключ |
| 25 | vacancy_cover_letters.override_shared и revision |

Отсутствовавшие в старой версии новые разделы сохраняются в базе назначения с предупреждением; на пустой базе остаются пустыми/default. Нельзя считать отсутствие обязательного раздела внутри новой версии признаком старого формата. v1–13 не поддерживаются без отдельно проверенного преобразования.

Для v21–24 проверяется полный старый либо полный актуальный layout личных писем. Если оба новых поля отсутствуют, после проверки override_shared/revision получают 0, сохраняя прежний приоритет общего письма; переданные оба поля сохраняются. Частично добавленный layout отклоняется. В v25 оба поля обязательны: override_shared 0/1, revision целое ≥0. В v14–20 текущие личные письма сохраняются с предупреждением. v16–33 сохраняют исходные бизнес-значения; совместимость структуры не означает воспроизведение журнала миграций источника.

Ограничение промтов задаёт схема 0079: cover_letter до 15000, relevance до 5000 Unicode-символов. Это не новый layout бэкапа. Копию с длинным промтом нельзя восстанавливать в приложение со старым CHECK. Необязательный `vacancySources.rabota` — строка URL; сохраняется и в catalog, и в raw JSON; отсутствие в старом файле допустимо.

## Реализация и проверки

### Состояние Go-переноса

GO-094 переносит текущий v39 в `internal/backup`: проверку состава/manifest/FK/sequences/catalog, SHA-256 по семантике `JSON.stringify`, входной byte cap и форматирование с итоговым лимитом 25 МиБ. `internal/v39/schema.json` — точная копия доверенной структуры старого приложения, без строк таблиц. 67 синтетических differential cases исполняют неизменённые JS digest/validator/request reader и сравнивают результат с Go. Векторы передаются через pipe; файлы пользовательских бэкапов не создаются.

GO-095 добавляет `GET /api/backup` для администратора: все прикладные строки читаются в одной read-only Repeatable Read транзакции, структура сверяется до/после, файл полностью проверяется до отправки. Required disposable SQL/HTTP CI подтверждает полноту и совместимость; текущий статус приёмки — в `tasks/SPRINT-01.md`. GO-098 добавил atomic v39 restore/generation, GO-099 подключает admin POST/validate (подробности и текущие проверки ниже). Signed v16–38 добавлены GO-100-B; v14–15/bootstrap остаются GO-100-C/D, полная GO-019 ещё открыта.

MariaDB `AUTO_INCREMENT` не входит в MVCC-снимок строк. Экспорт сохраняет безопасную верхнюю границу: `next_id-1`, прочитанную после строк, как минимум максимальный ID снимка. В ней могут оставаться промежутки от concurrent/rolled-back allocations; они не понижаются ради сходства со SQLite. `metadata.schema` содержит доверенный логический SQLite DDL из репозитория, а не физический MariaDB dump; фактические columns/engines и два известных invisible generated поля проверяются отдельно. DDL загруженного файла не исполняется. Отсутствующий сохранённый каталог получает source `normalizeCatalog(DATA)` только в файле, без вставки в базу. Исходное JS-поведение и его проверки описаны ниже.

GO-097 добавляет pure `PrepareV39Restore`: source scheduler/Apify safety transforms, warnings и parent-first table/role order проверены 28 сравнениями с неизменённым JS. GO-101 подготавливает counters в отдельной сессии: advisory restore owner, `LOCK TABLES … WRITE`, повторный current/MAX и только увеличение `AUTO_INCREMENT` до начала data TX. MariaDB-проверки подтверждают сохранение write fence после фактического ALTER, cleanup после cancel/fault и сохранность непустых данных/generation. Для этой стадии нужны SELECT/LOCK TABLES/ALTER на prepared schema; реальные права здесь не настраиваются. Ошибка может оставить безопасные ID gaps, но не изменённые прикладные строки. Atomic data replacement и HTTP v39 реализованы GO-098/099; текущая приёмка отражена ниже и в tasks.

Код: `server/backup/index.js`, константы `server/constants.js`, доверенная схема `scripts/backup-schema.mjs` → `server/generated/backup-schema.json`. Генератор исполняет SQL в изолированной SQLite в памяти, без рабочего D1 и seed-данных; нужен Node ≥24.

`test-backup-roundtrip.mjs` проверяет все прикладные таблицы, даты/длинные тексты, raw JSON/ID, совместимость, неполные файлы, checksum, ограничения/FK, авторизацию, потоковые лимиты и откат. `test-backup-miniflare.mjs` проверяет восстановление через локальный D1/workerd, включая оба раздела диагностики. `test-backup-diagnostics.mjs` отдельно сравнивает 30-дневные наблюдения/средние и воронку с пересечением источника/парсера до и после восстановления в пустую базу; проверяет совместимость v38 по immutable token и откат ошибочных наблюдений. `test-cover-letter.mjs`, `test-runtime-generation.mjs`, `test-ai-restore-barrier.mjs` покрывают новые поля и барьеры. Проверки используют синтетические данные, не полный снимок текущей рабочей базы.

```bash
node scripts/test-backup-roundtrip.mjs .tests/backup-fixture.json
node scripts/test-backup-miniflare.mjs .tests/backup-fixture.json
```

Второй тест требует runtime Miniflare/workerd. Для всех проверок используйте полный Git checkout, установленный lockfile и `npm run validate` после сборки. Не меняйте старые миграции или номера версий ради успешного импорта.

## Версия 26: модель OpenRouter для промта

`prompt_model_settings.openrouter_model` хранит `best_free` или `jev`, по умолчанию `best_free`. Выбор JEV разрешён только для relevance. Полный экспорт v30 включает столбец, миграция0051 добавляет его к существующим записям без изменения прочих настроек. При восстановлении v14–25 отсутствующий столбец заполняется `best_free`; показывается предупреждение. JEV при переносе не отключается.

`vacancy_roles` и `vacancy_role_assignments` появились в v27. Для v14–26 восстановление создаёт начальное дерево и переносит одиночное `company_vacancies.role`; неизвестные значения сохраняются отдельными листьями без смысловой классификации.

Формат28 добавляет vacancy_role_classification: role_id, markdown, revision, parsing_enabled, updated_at. При восстановлении старого файла текущие настройки сохраняются только для совпавших stable_key ролей и переназначаются на восстановленные ID. Не сопоставляются только по номеру ID.

Формат29 добавляет vacancy_candidates, vacancy_candidate_identities, vacancy_classification_attempts и vacancy_classifications. Владение обработкой vacancy_candidate_classification_leases операционное и не экспортируется. При restore v14–28 промежуточное хранилище становится пустым с явным предупреждением: старые файлы заменяют роли, компании и вакансии, поэтому сохранение старых FK по числу ID небезопасно.

Версия34 добавляет firecrawl_settings.favorites_only (0/1, default1). Полные v14–33 принимаются по их прежней схеме; при восстановлении фильтр включается. v34 сохраняет и выключенное значение.

Firecrawl в версии30: firecrawl_settings и vacancy_source_parsing входят в полный бэкап. vacancy_discovery_runs и firecrawl_request_lease операционные и исключены. Старый14–29 сохраняет текущий глобальный зашифрованный ключ; source-настройки очищаются, чтобы чужие восстановленные ID/URL не получали разрешение на запуск.

Версия31: режим Free по умолчанию, отдельный зашифрованный ключ Hobby и проверка доступа. Операционные firecrawl_usage_periods/events не входят в backup: restore не может откатить журнал расходов. Old14–30 переводит Firecrawl в Free, basic; Hobby требуется включить заново.

## Полные тексты (v32)

Пять новых прикладных таблиц хранят настройки структурирования, неизменяемые версии Markdown, части больших текстов, аудит загрузок и результаты извлечения. Восстановление v14–31 очищает эти пять таблиц; основные данные и Firecrawl сохраняют прежние правила совместимости. Задания и блокировки не входят в переносимые данные.

## Метаданные безопасного импорта (v33)

Всего49 прикладных и14 операционных таблиц. Семь новых переносимых таблиц: настройки/результаты конечных ролей, происхождение управляемых полей и автоматических ролей, публикации/устойчивые ключи и техническая история. vacancy_import_batches и vacancy_import_write_context операционные; первый хранит подтверждённый progress, второй существует только внутри write-транзакции. Операционные строки не импортируются, поколение меняется. При restore14–32 семь новых таблиц очищаются, старые непустые поля считаются неизвестного происхождения и защищены. При v32 прежние raw/extractions сохраняются. Флаги ручных правок v33 восстанавливаются точно. FK автоматических ролей к assignments каскадный: ручное переназначение снимает parser-ownership.

Версия35: scheduler_json в firecrawl_settings, auto_enabled/interval_days/next_run_at/last_attempt_at/last_result источников. При восстановлении старых14–34 defaults выключены. Любое восстановление приостанавливает диспетчер и сбрасывает привязку cloudOwnerId. Очередь scheduler_jobs, циклы scheduler_runs и журнал/резервы scheduler_attempts операционные, не экспортируются; поколение отменяет прежнюю очередь, журнал затрат не откатывается.

Настройки общего выбора источников (16.10.2026) хранятся внутри прежнего scheduler_json: sourceIds и intervalDays. Backup остаётся35, состав SQL-таблиц/столбцов прежний. При чтении/restore старых35 без sourceIds берутся типы ранее auto_enabled источников; до35 список пустой. Restore сохраняет выбор, но всегда выключает выполнение и сбрасывает облачную привязку.

SQL0062 снимает прежний предел количества вакансий на организацию, не меняя layout backup35. При восстановлении используется актуальный доверенный DDL без ограничивающего триггера; старый `company_vacancies_max_10` удаляется до загрузки строк. Старый backup не возвращает ограничение.

### GO-098: транзакционное ядро v39 на подготовленной MariaDB

`backup.Repository.RestoreV39` использует validated source plan и отдельный counter preflight, затем заменяет 52 прикладные таблицы в одной Repeatable Read TX. Первой блокируется generation, служебные receipt/spend не импортируются и сохраняются; старые classification leases удаляются исходным FK CASCADE. Все scalar значения проверяются на представимость до DDL, а восстановленные строки сравниваются как точные multisets до generation rotate/COMMIT; FK/CHECK и исходные триггеры остаются включены. Explicit zero IDs сохраняются через временный session SQL mode с возвратом настройки при Close. Новые таблицы/SQL из metadata не создаются.

Ядро принято на #53/de99bfe по отдельной disposable restore job и полному CI. Runtime POST подключён в GO-099; исторические версии/bootstrap остаются GO-100. После rollback допустимы только поднятые counter high-water/gaps, без частично восстановленных бизнес-данных или смены generation.

### GO-099: admin HTTP v39 и границы crash proof

`POST /api/backup/validate` строит проверенный план и читает подготовленную схему без DDL/ID/generation writes. `POST /api/backup` выполняет атомарный restore. Обе операции требуют подписанную admin session; cross-origin — 403, declared/actual stream свыше 25 MiB — 413, invalid input и SQL restore failure — 400 (как source, SQL details скрыты), отсутствие настроенного хранилища — 503. Ответы JSON/private/no-store; исходные summary/warnings сохранены. `/api/backup` GET продолжает безопасный export.

Процессный тест останавливает отдельный Go writer непосредственно перед COMMIT, принудительно завершает его SIGKILL и повторяет операцию новым процессом. Это проверка client process/socket loss, **не** аварии MariaDB, ОС или физического носителя. Конкурентные receipt/create и scheduler mutations проверяют существующий generation fence. GO-099 принят на #54/1b12301: Public40464855878 и MariaDB40464855875 pass. Отдельная disposable job проверила crash/fresh process, receipts/concurrency, admin HTTP и late-result SQL boundary без skips. На этапе GO-099 historical restore ещё не был принят; v16–38 добавлены GO-100-B ниже, v14–15/bootstrap остаются открытыми.

`storage.WithinGenerationTx` переносит SQL boundary legacy `generationBoundDb.batch`: expected generation фиксируется до внешней работы, после её завершения generation lock и все statements результата выполняются в одной TX. Запоздавший результат отвергается до callback; callback сохраняет ответственность за права/CAS/input/lease guards. Внутри callback нельзя обращаться к провайдеру. Для будущих AI HTTP handlers ErrGenerationChanged соответствует source409/code `restored`; AI/provider orchestration этим barrier proof не принимается. Required source oracle исполняет unchanged wrapper на SQLite :memory:, а disposable SQL gates проверяют stale/fresh/rollback/concurrent restore с синтетическим результатом. HTTP restore ограничен 45 секундами до общего server write deadline.

### GO-100-A: проверка исторических signed formats

`ValidateModernBackup` проверяет форматы v16–39 без доступа к БД: состав таблиц и столбцов зависит от версии, включая optional старые cover-letter/prompt-model layouts, ранние Firecrawl/scheduler поля и поздний agency FK. Manifest, integrity, scalar/row limits, typed FK и raw catalog проверяются с исходной семантикой. Required unchanged JS oracle содержит 416 synthetic vectors для всех 24 версий; #56/code92fff43 прошёл Public40469083705 и MariaDB40469083582, включая весь прежний v39/SQL/API/browser regression.

Это отдельная pure-валидация, ещё не план historical restore: она не добавляет отсутствующие разделы/defaults, не читает preserve rows и не пишет SQL. `ValidateV39` и export сохраняют v39 контракт. Runtime validate/restore расширены до v16–39 в GO-100-B ниже. C (v14–15) и D (trusted bootstrap) остаются открытыми; полная GO-042 не принята.

### GO-100-B-P: исторический план без SQL

`PrepareModernRestore(data, current, now)` переносит source plan v16–39. Отсутствующие в старом файле профили/настройки/промты/история сохраняются по исходным version gates; staging/import/detail storage очищается, role classification remap использует stable key, intake history — immutable candidate token. Старое одиночное поле роли преобразуется с исходными Unicode/whitespace/multi-selection/ambiguity правилами. Scheduler/Apify safety и warnings сохранены.

Публичная source policy хранит шаблоны стандартных ролей, warnings и Unicode metadata Node24; никаких пользовательских строк/ключей. Required oracle исполняет unchanged prepareBackupRestore/restoreBackup на metadata spies: 216 случаев для всех 24 версий, реальный SQL не выполняется. Входные bytes/current snapshot остаются caller-owned и не меняются. Сам pure helper не выполняет SQL: caller обязан заново получить preserve rows под data TX locks; stale pre-validation snapshot использовать нельзя. Runtime GO-100-B-S принят ниже; полный GO-100 остаётся открытым.

### GO-100-B-S: prepared-schema restore signed v16–39

Generic `Repository.ValidateRestore` / `Restore` подключены к прежним admin URL и принимают signed v16–39 на подготовленной схеме с initialized generation id1. Ответ restore сохраняет sourceVersion входного файла и version39 текущего экспорта; авторизация, origin, body cap и private/no-store не меняются. Strict Go V39 wrappers сохранены. Предварительная validation использует read-only согласованный snapshot без counter/DDL/generation writes.

При записи предварительный plan использует только incoming/generated IDs; поднятие AUTO_INCREMENT завершается до data TX. Финальный plan строится заново после generation FOR UPDATE из актуальных source-preserve таблиц, прочитанных в постоянном parent-first порядке через SELECT FOR UPDATE. Row/gap locks удерживаются до replacement/verification/generation rotate/COMMIT; изменения между validate и restore не подменяются старой копией. Остальные operational receipts/spend не импортируются. DDL внутри data TX нет.

Расширенный план (входные и сохраняемые строки) ограничен 200000 строками и 25 MiB row JSON; слишком большой combined plan отвергается без частичного восстановления, rollback может оставить безопасные counter gaps. Это дополнительная явная граница безопасного Go restore, а не обещание импорта произвольного объёма current state. Все physical values/FK проверяются до COMMIT.

B-S принят на #58/code5b91bc5: Public40479135218 и MariaDB40479135520 полностью pass. Required historical job122104725668 выполнила каждую версию16–38, source SQL comparison/round-trip/counters, late faults/retry, оба порядка profile save/restore и actual signed admin HTTP; zero skips, весь прежний regression успешен. Настоящая MariaDB локально не запускалась, source oracle использует только SQLite :memory:. V14–15 и schema bootstrap остаются C/D, полный GO-100/042 не принят.

### GO-100-C-V: pure validation unsigned v14–15

`ValidateLegacyBackup` проверяет отдельный старый контракт: обязательные таблицы/столбцы, catalog AI/reviews/agency ratings, точный scalar layout, typed FK и лимиты строк/входных bytes. v14 допускает отсутствие AI-полей компании; v15 требует оба поля. Длины текста считаются в UTF16, числовые оценки агентств используют исходные JS coercion и half-step tolerance; normalizing/recalculation здесь нет. Входные scalars и catalog остаются неизменными.

Старые версии не гарантировали integrity, manifest, исходную дату и counters. Неизвестные legacy metadata не исполняются и не переносятся в доверенные поля плана; они не становятся signed v16 через подмену версии. Required unchanged JS oracle сравнивает 1468 синтетических случаев (469 приняты), включая все обязательные столбцы/typed FK, optional audit, UTF16 и source diagnostics. Native TypeError текста движка не фиксируется, отказ всё равно должен быть безопасным 400.

Это pure validator, без SQL и без расширения runtime validate/restore v16–39. C-P должен отдельно перенести source normalizeCatalog/plan, omitted calendar/audit preserve и truthful warnings; C-S — доказать запись на disposable MariaDB. Ни GO-100-C, ни полная GO-042 по одному валидатору не принимаются.

C-V принята на #59/code6710498: Public40482915844/MariaDB40482915965 полностью pass, required legacy1468 и весь прежний source/SQL/API/browser regression успешны. Только pure validation; source catalog normalization/legacy plan и SQL приёмка остаются C-P/C-S.

### GO-100-C-P: нормализация полей до полного legacy plan

Private entity helpers переносят company rating/WLB/salary/default sources и agency choices/score/revenue/reviews/UTF16 guide slicing. Исходный ordered input сохраняется до арифметики: overflow Number, отсутствующее поле, NULL и пустая строка не взаимозаменяемы. Source Math.round, object spread, точные UTF16 names/default lookup и inherited plain-object application defaults проверяются отдельно. Public catalog policy содержит только уже опубликованные options/default URLs и synthetic reference companies из src/data.json; source gate проверяет drift.

Required unchanged JS oracle сравнивает 705 случаев (700 приняты), по одной сущности, включая source отказ при throwing coercion. Company rank передаётся helper явно; сортировка и выбор rank этим oracle **не проверяются**. Внешний probe x/text collate v0.23.0 не совпал с Node24 localeCompare(ru), библиотека в проект не добавлена. Нет нового runtime/SQL/API или Node runtime dependency. Совместимая сортировка полного каталога и legacy restore plan/preserve/warnings остаются открытой частью C-P; C-S/D и весь GO-042 не принимаются по entity-only proof.

Entity fields приняты на #60/code4ce126b: Public40507165590 и MariaDB40507165592 полностью pass, required705 и весь прежний regression успешны. Source сравнения выполнены на Node24.19/ICU78.3/CLDR48/Unicode17. Порядок компаний/rank и full legacy plan остаются непроверенными и не подключены к runtime.

### GO-100-C-P/C-S: unsigned v14–15 и prepared schema

C-P принята на [#68](https://github.com/meInteractive/it-map/pull/68), code97bad5d; Public40651564976/MariaDB40651565061 fully pass. Канонический русский порядок [ADR-023](DECISIONS.md), stable ties, score/rank и поля каталога проверены479072 сравнениями/170 source plans. Оригинальные parsed overflow numbers сохраняются до арифметики/truthiness; текст названий не нормализуется для хранения.

Реализация C-S расширяет generic `PrepareRestore`/`Repository.ValidateRestore`/`Restore` до v14–39. Strict `ValidateModernBackup`/`PrepareModernRestore` и V39 wrappers сохраняют прежние границы. v14–15 не имеют гарантии integrity: их SQL/schema/migrations/sequences из файла игнорируются. Current/MAX high-water назначения сохраняется; uploaded sequence не резервирует ID. Непредставимые SQL scalar/некорректная нормализация отвергаются до подключения/counter DDL.

Для legacy calendar сохраняется всегда; audit — только при отсутствии раздела, явно пустой audit очищает строки. Набор destination reads фиксируется по входному файлу до pure planning; final rows читаются заново под generation и `FOR UPDATE` locks до замены. `/validate` использует read-only snapshot и не резервирует counters/generation. Все normalization/clock/defaults завершаются до COMMIT.

C-S принята на #69/code2297175: Public40653587489/MariaDB40653587455 fully pass. Required job122699204697 выполнил все6 signed/unsigned parents без skips. Проверены unchanged source SQLite :memory: эталон, round-trip v39, rollback/retry и оба порядка concurrent calendar/audit writers. Trusted schema bootstrap D и полная GO-042 не приняты. Реальные backups/DB/keys/providers не используются.

### GO-100-D / GO-310…311: trusted bootstrap отдельно от restore

GO-310/#70 принят на05b8a588: Public40657232349/MariaDB40657232302 fully pass;217 trusted DDL actions и exact full metadata contract72/606/269 index rows/117 CHECK/50 FK/22 triggers. SQL не берётся из загружаемого файла.

GO-311 реализует отдельный offline `itmap schema-init` до HTTP startup. Missing trusted objects создаются только в pristine empty/empty-partial DB под общим restore ownership; изменения и чужие объекты, строки и прошлые ID allocations вызывают отказ. Полная база с текущим поколением проверяется без мутаций. MariaDB DDL commits отделены от generation seed TX и последующего restore data TX; failure не обещает общего DDL rollback. Повтор инспектирует фактический результат даже при unknown prior commit. Начальное поколение генерируется до своей TX, после exact schema check; post-commit normalization нет. Actual bootstrap/CLI/source restore/fault/retry proof принят на PR #71/bb2455a: Public40661335744/MariaDB40661335605; четыре required parents без skips, только blank disposable CI.

## Функциональная приёмка интерфейса восстановления (GO-321)

Критический SQL/export/restore/generation/rollback/crash core принят ранее.
Средняя часть019 проверяет существующие ExportCard/BackupRestore через
настоящий браузер, Go router и отдельную disposable MariaDB: скачать v39,
изменить synthetic favorite после снимка, выбрать скачанный файл, получить
validation preview, отменить confirmation без restore POST, подтвердить restore
и после reload увидеть восстановленные сохранённые данные.

Required backup-browser-acceptance job запускает6 desktop/mobile сценариев:
round-trip, malformed/oversize/невалидная структура и пользовательские роли.
Каждый успешный restore меняет generation; validate и отказ прав его сохраняют.
Проверяется ровно2 подтверждённых SQL restore и0 skips. Настоящие API responses
не подменяются. Все browser requests ограничены loopback origin; trace/video/
screenshots выключены, скачанные/загруженные synthetic backup остаются только
во временном browser workspace и не публикуются. Живые базы/backup/ключи не
используются; реальный операторский rehearsal/cutover остаётся отдельным.

Локально допускаются compile/typecheck/discovery и guard refusals. Positive
SQL/browser proof выполняется только в guarded GitHub CI; до него321 не принята.
