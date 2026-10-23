# Sprint 1.1 — V10: manual vacancy editing on Go

Date: 2026-10-24. **Development checkpoint only, not production-ready.**

## Implemented

- `PUT /api/vacancies/{id}` with admin-only session authorization, same-origin checks, strict JSON, request size bound and v316-compatible `{ok:true,vacancies:[...]}` result.
- An edit requires the **exact** 64-hex-character token returned by `GET /api/vacancies/{id}`. Both endpoints now share the same `readDetailTx` implementation for computing revision. `PUT` locks the parent vacancy row before recomputing that token in a MariaDB transaction. A stale token returns HTTP 409; missing/malformed returns HTTP 428; no writes occur on conflict.
- Shared and personal cover letters are handled separately. Unchanged effective letters are never promoted from one user's personal letter to shared company-wide content. Personal letter deletion is supported by blank subject and body. Pre-existing duplicate personal-letter rows cause a fail-closed conflict.
- Role assignment replacement, optional Telegram source edit, company/applications/tasks owner change and user-specific relevance are updated in one transaction. Existing extra Telegram sources are preserved if no source is explicitly edited. The personal favorite/hidden flags are not overwritten.
- URL conflicts between manual Go creation and editing use the same named lock. **This is temporary and does not serialize future parser writes.** A reviewed DB-level unique constraint remains required.
- Agency vacancy editing remains HTTP 503 because the old JavaScript version recomputed agency salary projections; that functionality has not been ported.

## Tests completed

- `go test -race ./...`, `go vet ./...`, source-file static scan.
- Unit tests cover update payload presence semantics, token format, old vs personal letter scopes, HTTP authorization/error mapping and CSRF checks.
- Fake SQL-driver tests cover exact GET→PUT revision, stale revisions with zero writes, atomic rollback on failed role insertion, personal letter persistence as a separate row, named lock release and one-connection pool handling.

## Required before enabling production

1. Restore the **private** MariaDB 10.11 dump into a disposable staging database and run `db/vacancy-update-preflight.sql`; review and resolve data anomalies without mutating the source dump.
2. Obtain pinned Go driver, verify `go.sum`, run all tests with `-tags mariadb`, then exercise a synthetic vacancy through GET → PUT → conflict → rollback → GET; verify other users' shared/personal letter isolation.
3. Ensure MariaDB timestamps, table constraints, Telegram links, optimistic locking and concurrent writes behave as tested. Extend tests to parser writes after migrating the parser.
4. Validate the full React edit workflow on staging; verify agency salary projections before lifting the agency guard.
5. Complete remaining API, backup/restore, scheduler and parsing work. `/health/ready` intentionally stays HTTP 503.

Production GPT Sites and the original dump were **not modified**. No secrets or operational data were added to Git.
