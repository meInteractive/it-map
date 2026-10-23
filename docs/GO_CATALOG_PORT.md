# Sprint 1.1.09 — Go compact catalog and identity migration notes

The Go router implements authenticated GET for `/api/companies/list`, `/map`, `/detail?name=`, `/:id` and corresponding `/api/agencies/*` routes. The `agencies/search` alias preserves v316's company-only name search. The HTTP payload uses `companies` and `company` envelopes, respectively. Company and agency records are read from `app_catalog.data_json` with user-specific state joined from MariaDB. No real data is committed.

## Important compatibility notes

- A single read-only REPEATABLE READ transaction collects identity rows, favorite/hidden flags, vacancy existence, application status, and initialized-vacancy flags. This avoids GET-time writes from the old Worker and keeps a consistent snapshot.
- **Identity gaps:** the initial conversion to MariaDB currently has `company_identity.company_name` and `agency_identity.agency_name` as `LONGTEXT` **without UNIQUE constraints**. Existing IDs are preserved. New catalog entities that have no identity record will temporarily expose `id:null`; the name-based detail URL remains available. Do not run the old `INSERT OR IGNORE` pattern in MariaDB. Before enabling imports, add an idempotent identity creation operation after a reviewed, tested uniqueness migration.
- The current stored catalog already has the ranked entities, normalized salaries, and review fields. The Go projection uses these stored values rather than silently recalculating rankings. Changes to the catalog's normalization, fallback sources, or rating algorithm are **not ported yet** and must be implemented before enabling full catalog writes.
- The original `readCompactEntities` implicitly initialized embedded legacy `jobs`; the Go GET does not. `hasVacancies` still reflects embedded jobs for uninitialized companies, but the initial migration must perform the one-time materialization before vacancies are written or edited.
- A detail read intentionally omits embedded `jobs` and includes user flags and `vacancyCount`. It does not include applications or contacts.
- Independent Telegram channels: only authenticated GET `/api/channels` is migrated. POST, PUT, and DELETE remain closed; never interpret the 503 responses as successful mutations.
- Production readiness `/health/ready` still returns 503. The parser and other APIs must be migrated before cutover.

## Staging preflight

Run `db/identity-preflight.sql` against the **staging MariaDB**, not production, and check that the row counts and distinct-name counts agree. Also verify all names fit within the intended key sizes before applying any uniqueness changes. No SQL in this document modifies rows.

MariaDB integration tests, true frontend smoke tests, data migration and unique index migration remain open. The repository's offline Go unit tests only validate projection rules and HTTP routes, not live database behavior.
