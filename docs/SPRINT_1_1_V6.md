# Sprint 1.1 — V6: application mutation groundwork and company CRM flags

Date: 2026-10-23. This is an **incomplete development checkpoint**, not a production release. `gpt-site` and the existing D1 database have not been modified.

## Commits

| Commit | Implemented | Enabled HTTP route |
|---|---|---|
| `c3b2bc2` | `applications.Repository.Update`: revision checks, row locks, single-vacancy ownership checks, atomic history, strict input validation | **No**. Cannot expose until a complete v316 company response is implemented. |
| `0a6c9d2` | `applications.Repository.Delete`: authenticated-owner query, row lock, delete history and application in one transaction | **No**, same response compatibility requirement. |
| `ae6d776` | `crm.Repository.SetCompanyFlag`, route and handler tests | `PUT /api/crm/favorite?company=...`, `PUT /api/crm/hidden-company?company=...` |
| `48a6170` | DSN `clientFoundRows=true`: reliable matched row count for same-second/no-op updates | Internal storage setting |

## HTTP contracts implemented

These routes require an authenticated cookie session; browsers must use the same origin. Each body must be JSON containing exactly the named boolean property:

- `PUT /api/crm/favorite?company=<encoded-name>` with `{ "favorite": true }` or `false`.
- `PUT /api/crm/hidden-company?company=<encoded-name>` with `{ "hidden": true }` or `false`.

Response is the prior `CompanyState` projection: `status`, `comment`, `favorite`, `hidden`, `updatedAt`, `history`. A company not present in `app_catalog` returns 404. Existing duplicate `company_user_state` rows return HTTP 409, *without a write*, and require staging reconciliation. Global company CRM rows provide a serialized lock because the imported user-state table lacks the composite unique key. No duplicate rows are automatically deleted or merged. The production contract still needs live end-to-end verification.

## Application write preparation

- `Update` requires a 64-character SHA-256 revision derived from the same MariaDB `JSON_ARRAY` expression as the read route. Stale revisions fail without changing rows.
- Changing the linked vacancy requires that the vacancy belongs to the company, and it locks the vacancy row to serialize **Go** updates against the missing unique constraint. Writes from other services that ignore this lock are not protected.
- History insertion takes place in the same database transaction as the application update. Status-unmodified updates do not add spurious history.
- `Delete` removes history and the application in a single transaction. As with the existing Worker, it does not require a revision.
- The repository returns a company name, not a partial substitute for the full response. Do **not** enable application mutations yet: the Worker returns the complete `readCompanyApplications` payload, including full company vacancies and personal CRM flags, not only the updated application.

## Tests and staging gate

- Offline `go test -race ./...`, `go vet ./...` and public-source scan pass.
- An in-memory test SQL driver checks application UPDATE rollback, stale revision, link validation, status history, and DELETE transaction boundary. Tests do **not** validate MariaDB syntax or the converted data constraints.
- No staging MariaDB instance, usable `mysql` Go driver cache, `go.sum` or full Vite build was available here. The production build remains explicitly blocked.
- Before promoting: restore a private staging clone; run `db/crm-preflight.sql`, then `go mod tidy` and `go test -tags mariadb ./...`; run integration tests of NULL/UTF8/collation, optimistic revisions, duplicate rows, status history, and concurrent edits. Compare actual response shapes with the old front-end.

## Next implementation tasks

1. Port `readCompanyApplications` complete `{companyName,favorite,hidden,limit,records,vacancies}` response for GET and the mutation result paths, retaining role/Telegram sources and full descriptions; then wire PUT and DELETE application routes.
2. Port privileged manual vacancy create/edit/delete with provenance/revision safeguards and return formats.
3. Enable isolated MariaDB staging; repair missing uniqueness only after preflight and a repeatable migration; download and lock Go dependencies in a verified networked environment.
4. Migrate durable application creation requests/idempotency receipts, tasks/Google Calendar, backups, parser and scheduler. Production must remain disabled until the full sprint acceptance checklist passes.
