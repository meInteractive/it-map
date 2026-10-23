# Sprint 1.1 — V7: company applications write API and manual vacancy input

Date: 2026-10-23. **Development checkpoint, NOT a production release.** No connection to live GPT Sites or production D1 was made. The historical `gpt-site` branch was not modified.

## Completed commits

| Commit | Scope | HTTP availability |
|---|---|---|
| `e86198d` | Complete company vacancy payload (roles, Telegram provenance, personal/shared letters, user flags), no GET side effects | Indirectly through company applications |
| `14dc4be` | Compose v316 `{companyName,favorite,hidden,limit,records,vacancies}` with a 10-application limit and max 100 company vacancies | `GET /api/applications?company=...` |
| `4a0e3fe` | Wire application update/delete with original result shape and conflict status | `PUT/DELETE /api/applications/{id}` |
| `2e0c085` | Authenticated HTTP tests and response/error behavior | Test coverage |
| `90a7b62` | Normalize manual vacancy form fields (dates, salaries, titles, URLs, roles, Telegram), UTF-16 character count | **Not exposed:** writer still not ported |

The complete company response is intentionally reconstructed only after the write commits. Repository writes return the owning company name; no partial `{ok:true}` response is issued to the legacy React UI. Writes are user-scoped, require the revision token on PUT and run in MariaDB transactions. Status-history updates are atomic. DELETE uses the previous Worker contract: no revision requirement.

## Test evidence in this checkpoint

- `go test -race ./...` passes without the optional external MariaDB driver.
- `go vet ./...` passes.
- HTTP tests cover signed-in/out behavior, non-same-origin mutations, invalid payload and IDs, optimistic conflicts, returned record arrays, error redaction and post-commit read failure.
- Go project structure and public-source scan remain clean.
- Added **opt-in**, read-only integration tests for `CompanyVacancies` and `CompanyApplications`; they require an isolated MariaDB clone. They have **not** been run.

## Explicit limitations / acceptance gate

1. The MariaDB driver dependency (`github.com/go-sql-driver/mysql@v1.9.3`) is pinned in `go.mod` but cannot be downloaded in this isolated environment, so there is **no** `go.sum`. A real MariaDB build, integration tests, SQL syntax/collation verification, and actual API parity against staging are still blocked.
2. `CompanyApplications` uses separate read-only transactions for catalog state, applications and vacancies, resembling the previous parallel reads; simultaneous external writes may yield a transiently mixed response. After queue migration, evaluate a single snapshot for all these projections.
3. The private staging DB must pass `db/crm-preflight.sql`, `db/vacancy-state-preflight.sql` and schema validation first. Imported tables missing unique constraints require a **reviewed** reconciliation migration, not a blind deduplication.
4. The manual vacancy validator only covers form constraints. No privileged `POST /api/vacancies`, `PUT/DELETE /api/vacancies/{id}`, role-assignment transaction, or audit trail is enabled yet.
5. Durable idempotent application creation remains unported; do not mistake the PUT and DELETE support for the entire application workflow.
6. Restore, import/export, parser, scheduling, Google Calendar, AI generation, secret setup, Vite build and full regression remain incomplete.
7. `/health/ready` intentionally answers 503 until all business APIs, parser, staging tests and production-readiness checks are accepted.

## Next work

1. Implement manual vacancy create/update/delete within MariaDB transactions with optimistic revision, role links, Telegram provenance and admin authorization; preserve the old full response shapes.
2. Implement application creation-context and idempotent receipts with restore-generation safeguards; verify duplicate requests and lost responses.
3. Complete task writes and Google Calendar synchronization.
4. Complete backup v39 restore and import/export, AI provider adapters and parser pipeline.
5. Restore sanitized staging MariaDB, download and verify module dependencies, lock `go.sum`, run full contract test suite and only then consider production cutover.

**Never upload the attached MariaDB dump or full user backup into public Git history.**
