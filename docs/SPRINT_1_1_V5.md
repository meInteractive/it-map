# Sprint 1.1 — CRM continuation checkpoint (2026-10-23)

The source tree remains a **partial Go migration** of GPT Sites v316. It is not
production-ready. Production Sites and D1 are unchanged; the MariaDB dump and
JSON backup are NOT part of the public repository or its Git history.

## New commits after v4

- `09db3a4`: `PUT /api/tasks/{id}` with `{"completed":true|false}`. User-scoped
  transactional, idempotent, same-origin protected, strict body validation.
  Deadline rescheduling/creation remain disabled until Google Calendar sync is
  implemented. Calendar event IDs and task notes remain unchanged.
- `6ccfe20`: `GET /api/applications/{id}` and `GET /api/crm/applications`.
  User-scoped application records, recruiter contact data, status history,
  publication sources and SHA-256 revision derived from MariaDB JSON_ARRAY.
  Application mutation, idempotency receipts and other CRM routes still pending.
- `0f3a808`: `GET /api/crm/favorites`, `GET /api/crm/hidden-companies`.
- `c69d9d7`: `GET /api/crm?company=...` with private comment, flags, history;
  validates company against the catalog, does not create state on GET.

## Readiness and known risks

- Offline Go HTTP/unit/race/vet tests pass, but actual schema/driver queries
  MUST be verified on an isolated MariaDB staging instance.
- Missing `(user_name,company_name)` unique constraint in the converted
  `company_user_state` blocks safe global CRM writes until reconciliation.
  `db/crm-preflight.sql` is SELECT-only and reports aggregate anomalies.
- Go application fingerprints currently hash MariaDB's `JSON_ARRAY` string.
  Compare revision and concurrency behavior against the v316 SQLite format on
  staging before enabling PUT application writes.
- Neither React/Vite builds nor a complete running UI have been verified in
  this offline environment. `/health/ready` intentionally returns 503.
- Current Go CRM GET endpoints are not a substitute for full regression tests.

## Next commits

1. Write integration tests against an isolated, restored MariaDB clone,
   including user isolation, date/NULL handling, and app revision consistency.
2. Implement `GET /api/applications?company=...` while preserving the complete
   old vacancy payload rather than substituting an incomplete compact list.
3. Port admin-created/edited vacancies preserving source-provenance guards.
4. Add all application write operations with transactionally checked versions,
   ten-per-company limit and unique user-vacancy links.
5. Connect Google Calendar task changes; port backup/restore and background parser.

To run read-only staging checks (driver installed):

```bash
ITMAP_TEST_MARIADB_DSN='<staging SELECT-only DSN>' go test -tags mariadb ./internal/applications
```

Never place the actual DSN in GitHub or commit outputs containing private rows.
