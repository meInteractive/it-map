# GO-349: ключи и model catalogs OpenAI/OpenRouter

Первый слой задачи — `internal/aiproviders`: существующие таблицы
openai_settings/openrouter_settings и AES-GCM domains OpenAI/OpenRouter из
legacycrypto. Клиентские HTTP routes/main ещё не подключены: это следующий gate
после положительной native SQL-проверки, не утверждение готовности UI/parser.

Read не создаёт singleton и не делает DDL. SaveKey получает generation,
захватывает generation row FOR UPDATE, проверяет значение и атомарно сохраняет
новый ciphertext/suffix с увеличением revision. Параллельные saves сериализуются;
ключ не пишется в открытом виде. generation должен быть захвачен HTTP до body.
Неизвестная конфигурация/SQL ошибки возвращают безопасный storage error.

Resolve предпочитает сохранённый ciphertext. Неверный secret/domain вызывает
явный ErrDecrypt, без удаления ciphertext и без тихого fallback на env. OpenAI
legacy env fallback работает только когда сохранённого ключа нет. OpenRouter
не получает env fallback. Реальные ключи/старый секрет из БД не читались.

Public Settings содержит configured, mask, revision и дату, без ciphertext или
plaintext. Поля secret/env key приватны; String/GoString и JSON тестируются на
отсутствие credential markers. ParseKey сохраняет исходные различия в extra
fields OpenAI/OpenRouter; malformed/null JSON отклоняется безопасно до SQL.

Pure catalogs сохраняют source text-model filtering, capability/pricing profiles,
requested/recommended defaults и OpenRouter eligibility/ranking. Embedded
model_profiles.json — публичные неизменённые source profiles/pricing, не реальные
настройки или утверждение актуальности провайдерского каталога. Доступность всегда
нужно сверять через API, цену неизвестной/dated модели не придумывать.
`check-ai-model-catalog.mjs` воспроизводит actual source pure exports без сети.

Обязательный TestMariaDBAIKeySettingsDisposable запускается только в guarded
GitHub CI на одноразовой itmap_ci: read without writes, encrypt/resolve/decrypt
failure, generation refusal, concurrent revisions и отсутствие новых таблиц.
Локальный tagged skip не положительный SQL gate. Рабочая импортированная БД
для тестов не используется. HTTP/API/source UI/generation body race и provider
refresh-cache ещё требуют отдельной реализации и приёмки.

Сервис OpenAI подготавливает six-hour cache, принудительный refresh и source warning
при отсутствии/невозможности расшифровки ключа. Cache привязан к SHA256 ключа;
возвращаются копии slices, чтобы caller не менял общий cache. Secret в cache-key
не хранится. Метрики letterTiming и полный публичный HTTP DTO ещё не подключены,
этот сервис не передаётся main/httpapi до завершения следующего gate.
