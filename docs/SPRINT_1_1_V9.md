# Sprint 1.1 — V9: safe deletion of vacancies

Date: 2026-10-24. **Development checkpoint, not a production release.**

## Code shipped

- `DELETE /api/vacancies/{id}` available for signed-in admins, guarded by same-origin and fetch-site checks, with strict positive-integer IDs.
- `internal/vacancies.DeleteManual` reproduces the two-stage v316 behavior for company and Telegram-channel vacancies. Row lock (`FOR UPDATE`) + MariaDB transaction ensure that related CRM references are not lost in concurrent writes:
  - Active vacancy with existing applications or tasks → mark `archived=1` and preserve record.
  - Unlinked vacancy, or a vacancy already archived → clear application/task references without deleting their history, then delete the vacancy, with all steps in one transaction.
- Legacy hard-delete response preserves `preservedApplication` and `preservedTask` boolean keys, including `false` values.
- SQL or integrity failures roll back and return a sanitized HTTP 503 rather than pretending success.
- **Agency vacancies are deliberately blocked with 503**: the D1 implementation recalculates agency salary ranges inside `app_catalog` on deletion. That MariaDB JSON projection has not yet been ported. Enabling agency deletion before doing so would corrupt the UI's salary projections.

## What was checked

- `go test -race ./...` and `go vet ./...` locally.
- Fake-SQL tests: archive on application or task, hard delete with/without links, missing record, rollback on failed deletion and failed detachment, guarded agency case.
- HTTP tests: unauthenticated/regular-user/cross-origin rejection, invalid IDs, success and sanitized database errors.
- Dump DDL review: linked `company_applications` and `company_tasks` have `ON DELETE SET NULL`; role assignments, Telegram sources, personal state and letters have `ON DELETE CASCADE`. `db/vacancy-delete-preflight.sql` validates critical FK actions and orphan counts on a private staging copy.

## Pending production acceptance

1. Import the privately held dump into isolated MariaDB 10.11 and run read/write integration tests on **synthetic** vacancies in a disposable database.
2. Verify trigger behavior, linked parser provenance and full React workflow for archive → delete.
3. Port agency salary recalculation transactionally before lifting the agency deletion guard.
4. Obtain the pinned Go MariaDB driver and commit verified `go.sum` (module fetch is blocked by DNS here).
5. Implement `PUT /api/vacancies/{id}` using **the exact token from** `GET /api/vacancies/{id}`, checked inside a transaction; preserve personal/shared letters, role links, Telegram sources and linked CRM.
6. Migrate remaining APIs, parser, queue and backups. `/health/ready` remains HTTP 503 until acceptance.

No private SQL dumps, actual backup, secrets, resumes or operational state should enter the public Git repository.
