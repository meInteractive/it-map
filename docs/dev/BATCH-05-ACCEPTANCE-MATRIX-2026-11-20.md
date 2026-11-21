# BATCH-05 — сверка критериев, React/Go/MariaDB

**Дата:** 20.11.2026. **База анализа:** `main` `2098f7653d1c047bf79ad478749ea85e0f3b7b09` (PR #512–516 уже merged). Приведённые здесь проверки исходников — инженерный аудит ChatGPT, **не независимая приёмка Codex**. Все `S11-B05-01…30` остаются `[ ]` / «частично реализовано — к проверке Codex».

## Доказательства и их границы

- **E** — [vacancy-editor.spec.ts](../../tests/browser/vacancy-editor.spec.ts): настоящее React-приложение и API client, все HTTP-ответы синтетические.
- **D** — [vacancy-drawer.spec.ts](../../tests/browser/vacancy-drawer.spec.ts): React browser, mock HTTP, включая новые сценарии потери ответа при DELETE (в PR с этой матрицей).
- **A** — [vacancy-applications.spec.ts](../../tests/browser/vacancy-applications.spec.ts): React browser, mock HTTP, включая GET перед повтором DELETE отклика.
- **VC** — [create SQL](../../internal/storage/vacancy_create_integration_test.go) и [create HTTP](../../internal/storage/vacancy_create_http_test.go): `TestMariaDBVacancyCreate(HTTP)?Disposable`.
- **VE** — [edit SQL](../../internal/storage/vacancy_edit_integration_test.go) и [edit HTTP](../../internal/storage/vacancy_edit_http_test.go): `TestMariaDBVacancyEdit(HTTP)?Disposable`.
- **VD** — [delete SQL](../../internal/storage/vacancy_delete_integration_test.go) и [delete HTTP](../../internal/storage/vacancy_delete_http_test.go): `TestMariaDBVacancyDelete(HTTP)?Disposable`.
- **VF** — [flags SQL](../../internal/storage/vacancy_flags_integration_test.go) и [flags HTTP](../../internal/storage/vacancy_flags_http_test.go): `TestMariaDBVacancy(Flags|FlagHTTP)Disposable`.
- **AH** — [application HTTP](../../internal/storage/application_http_integration_test.go): `TestMariaDBApplicationHTTPDisposable`. **AR** — [receipts](../../internal/storage/application_receipts_integration_test.go) и [faults](../../internal/storage/application_faults_integration_test.go); **AC** — [CAS](../../internal/storage/application_cas_integration_test.go); **AM** — [mixed races](../../internal/storage/application_mixed_integration_test.go).
- SQL/HTTP-тесты выполняются против disposable MariaDB и перечислены как обязательные targets в [.github/workflows/mariadb-staging-ci.yml](../../.github/workflows/mariadb-staging-ci.yml). Проверить `pass` и отсутствие `skip` **на последнем SHA каждого нового PR**. Сопоставление *раздельных* E/D/A и SQL/HTTP не подтверждает сквозной browser → реальный Go API → MariaDB round-trip.

## Матрица 30 критериев

| ID | React / HTTP fixture | Go / MariaDB | Неподтверждённое или блокирующее условие |
|---|---|---|---|
| **01** создание с ID | E 01/02 | VC create/read | Browser через реальный Go+MariaDB |
| **02** описание и роли после read | E 01/02 | VC read/roles | Совместный readback без HTTP mock |
| **03** неправильное обязательное поле без POST | E 03/04 | VC HTTP 400 invalid role | Проверить единый UI/server validation contract |
| **04** исправление ошибки сохраняет поля | E 03/04 | VC form persistence | Реальный create после клиентского исправления |
| **05** edit одной revision в списке и drawer | E 05 | VE CAS/readback | Сквозной cache/readback с настоящими ответами |
| **06** неверная revision сохраняет черновик | E 06/07, частично | VE stale 409 и race | Разрешение конфликта с реальным серверным readback |
| **07** отмена конфликта без записи | E 06/07, частично: отменяется выход из формы | VE не доказывает UI cancel | Отдельно проверить именно отмену разрешения конфликта |
| **08** 503 и GET revision перед PUT | E 08: до/после mock COMMIT | VE rollback/старая revision | Реальный fault после COMMIT + browser → Go |
| **09** смена компании-владельца | Нет выбора в E | VE owner move переносит отклики/задачи | **Нужно продуктовое решение:** UI, разрешённые владельцы, ссылки и статус истории |
| **10** non-admin read-only | E 10 | VC/VE HTTP 403 | Сквозной browser с авторизованными пользователями |
| **11** архив исчезает из *активного* списка | D 11/15 лишь архивирует и сохраняет строку | VD архивирует; Go List включает архив | **Нужно продуктовое решение:** отдельный доступ к архиву до фильтрации |
| **12** cancel удаления без DELETE | D 12/13 | Не требуется запись SQL | Проверить без повторного browser dialog в реальной сессии |
| **13** delete закрывает drawer и строку | D 12/13 | VD immediate hard delete | Сквозное обновление списка через Go |
| **14** 503 не лишает карточки и повтор безопасен | D 14, добавлены lost ACK archive/hard-delete | VD late fault/rollback и 404 после hard-delete | Реальный lost ACK при двухэтапном архивировании/удалении |
| **15** отклик доступен после удаления вакансии | D 11/15 показывает архивную связку | VD сохраняет CRM/FK/history, detach | HTTP GET самого отклика после hard-delete с двумя пользователями |
| **16** избранное переживает reload | D 16/18 | VF личный flag/reload | Сквозной durable browser readback |
| **17** флаги пользователей изолированы | Нет двух browser-контекстов в D | VF SQL/HTTP Demo/Nastya | Два независимых браузерных сеанса против Go |
| **18** ошибка личного флага не меняет revision | D 16/18 mock 503/revision | VF SQL/HTTP rollback + revision | Сквозная ошибка реального SQL и UI reread |
| **19** быстрые изменения двух флагов | D 19/20 только последовательные действия | VF concurrency разных соединений | Rapid clicks/response inversion в browser + reread |
| **20** снятие hidden возвращает строку | D 19/20 | VF HTTP list/detail | Browser filter/readback на настоящем Go |
| **21** отклик получает верный vacancy ID | A 21/23 | AH create readback | Сквозной real HTTP |
| **22** выбор вакансии в CRM сохраняется | A 22 (vacancy 902, отмена смены tab) | AH проверяет допустимость vacancy | Сквозной React → Go после переключений |
| **23** запись только в личном списке | A 21/23, mock Alice/Bob | AH личный GET/404 для второго | Два браузерных контекста + SQL |
| **24** lost ACK создания не дублирует | A 24 сохраняет key/context | AR durable receipt, AH реальный 503 после COMMIT | Объединить браузерный retry с настоящим Go |
| **25** comment edit не сбрасывает поля | A 25 | AH, AC comment-only CAS/history | Сквозной re-read всей формы |
| **26** stale edit не закрывает черновик | A 26/27 | AC 409, same-second CAS | Серверная конкуренция между двумя UI-сеансами |
| **27** разрешение конфликта обновляет историю | A 26/27: новая revision, **история не утверждается** | AC/AH атомарная история перехода статуса | Browser edit со сменой статуса + real readback history |
| **28** cancel удаления не тратит receipt | A 28/29: нет DELETE | AR durable create receipt | Уточнить семантику: create receipt — не токен однократного DELETE |
| **29** удаление очищает список и сохраняет предусмотренную историю | A 28/29 | AH: удаляет отклик **и его статусную историю**, result history=0 | Уточнить формулировку критерия по legacy; не требовать сохранение удаляемой истории |
| **30** lost ACK delete не воскрешает запись | A 30: GET 404/200 до retry | AH delete→404, AM replay/delete tombstone | Реальный lost ACK + два сеанса + browser reread |

## Найденный критический дефект и безопасное исправление

До PR с этой матрицей `frontend/features/vacancies/Vacancies.tsx` повторял `DELETE /api/vacancies/{id}` сразу после неизвестного ответа `503`. Для vacancy с CRM-связями первый DELETE **архивирует**, а второй **отсоединяет и окончательно удаляет**. Потеря ACK первого COMMIT превращала повтор «архивирования» в несанкционированный второй этап удаления.

Исправление в текущем PR: запоминать исходное `archived` только после попытки DELETE с неизвестным результатом; перед повторной отправкой выполнить **прямой GET с `cache: 'no-store'`**. Если GET возвращает 404 — подтвердить отсутствие, синхронизировать UI и **не** отправлять второй DELETE. Если `archived` изменилось — обновить кэши и потребовать *нового отдельного подтверждения*; если state не изменилось — разрешить повтор. При ошибке проверочного GET повторную запись блокировать. Существующий серверный двухэтапный контракт не меняется.

Эта защита создаёт новую browser GET-точку; вместе с компонентом обновляются API callsite inventory и синтетические Playwright-тесты. Программная работа остаётся «к проверке Codex» до полного CI на финальном SHA и независимого review.

## Приоритет следующей независимой проверки

1. Подтвердить безопасность потерянного ответа DELETE вакансии: archive COMMIT, hard-delete COMMIT, no COMMIT, failed reconciliation GET. Приоритет **P0**, поскольку прежнее повторное действие могло обойти отдельное подтверждение.
2. Вывести совместную E2E acceptance на disposable MariaDB через настоящий Go HTTP (минимум 01/05/14/15/17/24/27/30), с двумя пользователями и явно контролируемыми fault boundaries. Не выдавать раздельные browser/mock и SQL тесты за объединённый E2E.
3. **Не начинать** owner switch (09) и смену видимости архива (11) без продуктового решения. Отдельно уточнить удаление статусной истории отклика (29).
4. Выполнить независимый review Codex, сверить последний SHA и CI, затем менять отдельные статусы в `BATCH-05.md` по реально доказанным критериям. До того **все 30 `[ ]`**.

Production MariaDB, D1, worker, платные провайдеры и релиз не затронуты.
