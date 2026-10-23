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
- `/api/vacancy-roles`, `/api/vacancy-roles/roots`, `/api/vacancy-roles/leaves`, `/api/vacancy-roles/{numeric-id}/descendants`
- `/api/vacancies/list?period=week|month|twoMonths|archive|all` (authenticated, read-only; absent period shows all)
- Legacy session endpoints `/auth`, `/logout`, `/api/session`

No source module writes to the database through these routes. Unsupported APIs remain behind authentication and respond as unmigrated; health readiness remains HTTP 503.

## Tests and limits

`go test -race ./...`, `go vet ./...` and `python3 scripts/check-public-repo.py` pass in the local environment. Read-only fixture tests also parsed the attached D1 snapshot's 229 companies, 31 agencies, and eight vacancy roles. The private snapshot was never committed to Git and temporary copies were removed after verification.

**Not yet verified:** MariaDB integration (server/driver unavailable locally); React build with downloaded npm dependencies; API compatibility against a running staging site; behavior under live concurrent writes; scheduling, parsing, backup/restore, Google Calendar, CRM writes, vacancy detail and editing. The vacancy listing is implemented but not yet proven against a live staging MariaDB.

The initial schema conversion did not preserve all original UNIQUE constraints. In particular, the identity name columns remain LONGTEXT without uniqueness. Existing identity mappings in the supplied snapshot are complete, but new company imports need an explicit, tested identity creation and uniqueness migration. See `docs/GO_CATALOG_PORT.md` and `db/identity-preflight.sql`.

## Next engineering milestones

1. Bring up an isolated MariaDB staging instance from the private dump and run read-only integration tests with the `mariadb` build tag.
2. Compare GET response payloads with GPT Sites for companies/agency/channel/role endpoints and verify Vite front-end build.
3. Implement remaining CRM reads/writes, vacancy detail, then safe vacancy mutations.
4. Migrate backups, integrations, parser and durable job scheduler. Full production promotion remains blocked until all acceptance checks pass.

## Vacancy listing checkpoint (new)

`internal/vacancies` implements a read-only projection matching `server/services/vacancy-projection.js` for the list endpoint. It supports Moscow calendar bounds, the new `all` filter, assigned leaf roles, Telegram sources, user-specific favorites/hidden/relevance and application statuses. The HTTP route does not accept writes or disclose SQL errors. The code deliberately does **not** run old lazy `prepareVacancyProjection` insert/backfill operations: any data absent from the staged MariaDB dump must be addressed by an explicit, independently validated migration.

To run the SQL integration test on an isolated, non-production MariaDB loaded with private data, install module dependencies and invoke `ITMAP_TEST_MARIADB_DSN=... go test -tags mariadb ./internal/vacancies -run TestMariaDBVacancyListReadOnlyIntegration`. The DSN belongs in your runtime environment, never GitHub or CI logs. Use a SELECT-only user.
