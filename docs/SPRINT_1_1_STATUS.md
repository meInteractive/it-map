# Sprint 1.1 — development progress (23 October 2026)

This repository is an incomplete, fail-closed Go migration of the original GPT Sites v316 project. It is **not approved for production**. The legacy `gpt-site` branch is a sanitized *reference*, not a source of operational database records.

## Newly completed code commits

| Commit | Scope | Offline verification |
| --- | --- | --- |
| `5b44e54` | 1.1.09: read-only MariaDB company/agency list/map/detail projections and personal flags | Go unit tests and private catalog JSON smoke test |
| `e0c4ef9` | 1.1.09: authenticated HTTP endpoints, status/error envelopes, legacy search alias | Go HTTP tests |
| `b07aa9b` | 1.1.14 (partial): independent Telegram channels GET; identity schema preflight | Go HTTP tests |
| `eb57098` | 1.1.11 (partial): vacancy role tree, root/leaf/descendant projections | Go unit/HTTP tests and private role-tree smoke test |

## Implemented GET endpoints

- `/api/companies/search?q=...`, `/api/agencies/search?q=...` (legacy alias)
- `/api/companies/list`, `/api/companies/map`, `/api/companies/detail?name=...`, `/api/companies/{numeric-id}`
- `/api/agencies/list`, `/api/agencies/map`, `/api/agencies/detail?name=...`, `/api/agencies/{numeric-id}`
- `/api/channels`
- `/api/tasks?company=...&vacancy=...&includeCompleted=1` (authenticated, user-scoped, read-only; task creation/editing not yet ported)
- `/api/vacancy-roles`, `/api/vacancy-roles/roots`, `/api/vacancy-roles/leaves`, `/api/vacancy-roles/{numeric-id}/descendants`
- `/api/vacancies/list?period=week|month|twoMonths|archive|all` (authenticated, read-only; absent period shows all)
- `/api/vacancies/{numeric-id}` (read-only drawer: description, effective letter, roles, sources)
- `PUT /api/vacancies/{numeric-id}/state` (personal favorite/hidden flags, transactional)
- Legacy session endpoints `/auth`, `/logout`, `/api/session`

Only the explicitly migrated vacancy flag PUT writes to the database; all migrated GET routes are read-only. Unsupported APIs remain behind authentication and respond as unmigrated; health readiness remains HTTP 503.

## Tests and limits

`go test -race ./...`, `go vet ./...` and `python3 scripts/check-public-repo.py` pass in the local environment. Read-only fixture tests also parsed the attached D1 snapshot's 229 companies, 31 agencies, and eight vacancy roles. The private snapshot was never committed to Git and temporary copies were removed after verification.

**Not yet verified:** MariaDB integration (server/driver unavailable locally); React build with downloaded npm dependencies; API compatibility against a running staging site; behavior under live concurrent writes; scheduling, parsing, backup/restore, Google Calendar, most CRM writes and vacancy editing. The vacancy listing is implemented but not yet proven against a live staging MariaDB.

The initial schema conversion did not preserve all original UNIQUE constraints. In particular, the identity name columns remain LONGTEXT without uniqueness. Existing identity mappings in the supplied snapshot are complete, but new company imports need an explicit, tested identity creation and uniqueness migration. See `docs/GO_CATALOG_PORT.md` and `db/identity-preflight.sql`.

## Next engineering milestones

1. Bring up an isolated MariaDB staging instance from the private dump and run read-only integration tests with the `mariadb` build tag.
2. Compare GET response payloads with GPT Sites for companies/agency/channel/role endpoints and verify Vite front-end build.
3. Implement remaining CRM reads/writes, vacancy detail, then safe vacancy mutations.
4. Migrate backups, integrations, parser and durable job scheduler. Full production promotion remains blocked until all acceptance checks pass.

## Vacancy listing checkpoint (new)

`internal/vacancies` implements a read-only projection matching `server/services/vacancy-projection.js` for the list endpoint. It supports Moscow calendar bounds, the new `all` filter, assigned leaf roles, Telegram sources, user-specific favorites/hidden/relevance and application statuses. The HTTP route does not accept writes or disclose SQL errors. The code deliberately does **not** run old lazy `prepareVacancyProjection` insert/backfill operations: any data absent from the staged MariaDB dump must be addressed by an explicit, independently validated migration.

To run the SQL integration test on an isolated, non-production MariaDB loaded with private data, install module dependencies and invoke `ITMAP_TEST_MARIADB_DSN=... go test -tags mariadb ./internal/vacancies -run TestMariaDBVacancyListReadOnlyIntegration`. The DSN belongs in your runtime environment, never GitHub or CI logs. Use a SELECT-only user.

## Next checkpoint: vacancy drawer and personal flags

`GET /api/vacancies/{id}` and `PUT /api/vacancies/{id}/state` are implemented, with handler/unit tests but **not yet confirmed against live MariaDB**. See `docs/GO_VACANCY_DETAIL_AND_FLAGS.md` and `db/vacancy-state-preflight.sql`. The personal flag writer serializes Go writes with a vacancy row lock and synchronizes pre-existing duplicate user-state rows; an eventual uniqueness/index migration is still needed. The Go editor remains disabled.

## Tasks checkpoint

`GET /api/tasks` now reads the signed-in user's task records, supports company/vacancy/completion filters, preserves calendar sync fields, and returns `{tasks,entities}`. Legacy `POST /api/tasks` and `PUT /api/tasks/{id}` are still unmigrated. A read-only MariaDB integration smoke test is provided under the `mariadb` build tag; live verification remains pending.

## V5 continuation (2026-10-23)

See `docs/SPRINT_1_1_V5.md` for four new implementation commits covering
application reads, task completion, company favorites/hidden lists and company
CRM state/history. The production readiness gate remains closed.

## V6 continuation (2026-10-23)

See [`SPRINT_1_1_V6.md`](SPRINT_1_1_V6.md). Go-only transactional
application editing/deletion repository methods have been implemented but NOT
exposed over HTTP yet, because the legacy API's full company response is not
ready. Authenticated `PUT /api/crm/favorite` and `PUT /api/crm/hidden-company`
are exposed and tested, with a duplicate-user-state fail-closed policy. The
Go driver DSN requests `clientFoundRows=true` for matched-row semantics.
All changes remain subject to staging MariaDB integration testing.

## V8 continuation (2026-10-23)

See [`SPRINT_1_1_V8.md`](SPRINT_1_1_V8.md). Admin-only `POST /api/vacancies`
now has transactional Go persistence and the legacy `{ok,vacancies}` response.
Manual creation checks catalog ownership, leaf roles, Telegram provenance and
user relevance, and uses a temporary MariaDB named lock while original
UNIQUE URL constraints remain missing. This is **not** global uniqueness against
parser writes and is not a substitute for a reviewed database migration.
PUT/DELETE vacancy editing, MariaDB-tagged tests and staging deployment remain
pending; `/health/ready` continues returning 503.

## V9 continuation (2026-10-24)

See [`SPRINT_1_1_V9.md`](SPRINT_1_1_V9.md). Admin-only `DELETE /api/vacancies/{id}` now implements transactional archive/preserved-CRM deletion for companies and Telegram channels. Agency deletion is explicitly blocked until salary projection is migrated. Tests use fake SQL; real MariaDB validation is pending. `PUT /api/vacancies/{id}` remains disabled.
