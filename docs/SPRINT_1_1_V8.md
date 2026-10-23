# Sprint 1.1 — V8: manually create a vacancy through Go

Date: 2026-10-23. **Development checkpoint, NOT a production release.**
No production GPT Sites, D1 or MariaDB was modified.

## Commit scope

- `e41c2ff` — atomic `Repository.CreateManual`, role selection and Telegram message validation; read-only database preflight and SQL transaction tests.
- `10e3e5d` — authenticated, admin-only `POST /api/vacancies`, v316 `{ok:true,vacancies:[...]}` response, HTTP security/error tests and server wiring.

## Compatibility behavior

The original React `VacancyEditor` sends numeric role IDs, `companyName`, `entityType`, salary bounds, publication dates, full Markdown, optional shared cover letter, Telegram source and personal relevance. These are accepted by the Go manual-creation API. Personal cover letters are not copied into shared fields. Role assignments use only leaf IDs. A legacy semicolon-separated role string is supported when `roleIds` is omitted.

The repository checks that a company/agency is present in the stored `app_catalog`, validates known Telegram channels and normalizes message URLs. It checks for existing vacancy URLs and Telegram messages before inserting. Vacancy, role links, source and user-specific relevance are committed atomically; failures roll back the transaction.

The MariaDB D1 conversion dropped UNIQUE indexes on long URL columns. A named MariaDB session lock serializes **Go manual vacancy POSTs only**. This does not make uniqueness global when other code (parser or legacy service) writes to the same DB. Before enabling automatic imports or mixed writers, review duplicate counts and enforce a durable uniqueness strategy (e.g. validated generated URL digest with collision-safe verification). Do not blindly add an index to production.

The existing response reads `CompanyVacancies` after commit. It contains user-scoped flags and letters but is limited to 100 linked vacancies, consistent with the current reader. We release the pinned lock/connection before that read, including when the connection pool has only one connection.

## Tests run in this checkpoint

- `go test -race ./...` passed.
- `go vet ./...` passed.
- Go server build without the external MariaDB build tag passed.
- `scripts/check-public-repo.py` passed (static known-pattern scan only).
- HTTP tests exercise unauthenticated, non-admin, cross-origin, invalid/oversized JSON and sanitized error handling.
- Fake-SQL transaction tests exercise duplicate URLs, unknown owners, role-assignment failure, commit/rollback and lock release. They **do not** validate MariaDB SQL syntax.

## Pending acceptance gates

1. Import the attached MariaDB SQL dump **privately** into an isolated staging server; validate `db/manual-vacancy-preflight.sql` counts and all schema prerequisites.
2. Download the pinned MariaDB driver, generate `go.sum`, build with `-tags mariadb` and run integration tests with a disposable user/database.
3. Compare v316 POST result with the existing React form in a running staging environment, including legacy catalog materialization and channel-only owners.
4. Add robust global URL/message uniqueness before enabling concurrent parser writers.
5. Implement and verify privileged PUT/DELETE `/api/vacancies/{id}` with optimistic revision and safe handling of linked applications/tasks. **These routes remain unported.**
6. Finish remaining CRUD, backup, AI services, parser/scheduler and full frontend tests. `/health/ready` intentionally remains HTTP 503.

Do not put live dumps, API secrets, resumes, generated letters or the full v39 backup into this public repository.
