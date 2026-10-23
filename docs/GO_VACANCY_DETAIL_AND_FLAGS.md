# Go vacancy detail and user-state migration — Sprint 1.1

Source reference: GPT Sites v316, `server/services/vacancy-projection.js`,
`server/services/vacancy-revision.js`, `server/repositories/vacancies.js`,
`server/routes/index.js`, `shared/contracts.ts`.

## Implemented API

- `GET /api/vacancies/{id}`: authenticated, read-only drawer, full Markdown,
  latest application, effective personal/shared cover letter, assigned roles,
  Telegram message links, publication sources, and compact employer directory.
  Response `{vacancy,entities}`. Returns 404 for a missing record, 400 for
  malformed IDs, 401 without a session; SQL errors do not leak DB details.
- `PUT /api/vacancies/{id}/state`: authenticated user-scoped update of EXACTLY
  ONE boolean `favorite` or `hidden`; returns `{ok:true,vacancyId,favorite,hidden}`.
  The request requires JSON, rejects suspicious cross-origin requests and
  limits the payload to 4 KiB. The write is transactional; the canonical
  vacancy row is locked before user-state read/insertion, preventing two new
  Go writers from both creating a row for the same vacancy and user.

## Deliberate differences and risks

1. Old v316 GET used SQLite-only expressions and could write to the database
   as a side effect. Go detail is read-only. Missing lazy data requires an
   explicit import/backfill step rather than silent GET-time changes.
2. The direct MariaDB dump is missing some legacy unique constraints. Duplicate
   user-state records are possible. This write path synchronizes flags across
   existing duplicates, but keeps relevance intact. For the long-term solution,
   inspect `db/vacancy-state-preflight.sql`, reconcile duplicates and migrate
   to a real composite uniqueness constraint (and suitable indexes). Never add
   a unique constraint blindly to a populated database.
3. The Go `revision` token is a SHA-256 digest of a deterministic Go snapshot,
   not the legacy SQLite `json_array` byte stream. The eventual Go `PUT`
   editor must compute/compare a compatible token **within its write transaction**;
   the two implementations must not mix revision tokens across deployments.
   The editor is NOT enabled yet and continues to report `backend_not_migrated`.
4. The Go role projection is read separately before the detail transaction.
   Staging parity tests should check concurrent role edits and decide whether
   to bring the role lookup into the same snapshot.
5. Some relational constraints/indexes were lost during the D1 conversion.
   Staging integration and source-vs-Go payload comparisons are required
   before production migration. The unit and HTTP tests are not proof that
   real MariaDB queries have run successfully.

## Required staging verification

- Restore the private snapshot into a disposable MariaDB instance outside Git.
- Run `db/vacancy-state-preflight.sql` with a read-only DB user.
- With a SELECT-only account, execute
  `ITMAP_TEST_MARIADB_DSN='...' go test -tags mariadb ./internal/vacancies -run 'TestMariaDBVacancy(List|Detail)ReadOnlyIntegration'`.
- Compare the old Sites and new Go responses for two users and known records,
  with care not to log any real private text or letters.
- Verify `PUT .../state` against an isolated **test** user/database, including
  rapid concurrent first writes. This write integration step is **not done**.
- Ensure the compiled production binary includes the MariaDB driver via
  `go build -tags mariadb`, but do not deploy until the full sprint is accepted.
