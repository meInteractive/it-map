# it-map — sanitized GPT Sites legacy sources

This archive is intended as the `gpt-site` reference branch for a Go rewrite.
It is **not** an exact Git checkout and **not** a self-contained production restore.

* Frontend: `frontend/`, `app/`, `shared/`; backend: `server/`, `build/`.
* Legacy SQLite schema code: `db/schema.ts` and `server/generated/backup-schema.json`.
* `src/data.json` is a **synthetic demo catalog**. No real CRM/jobs are included.
* `drizzle/` SQL migrations (including historical vacancy imports and prompts),
  compiled `dist/`, hosting project ID and internal progress logs were excluded.
* The production MariaDB export and backup are stored separately and privately.
* The original legacy build/test scripts may need restoration of private legacy
  migration inputs; **do not treat build failure here as regression of the original**.

See `docs/MIGRATION_PUBLIC_REPOSITORY.md` for security assumptions and staging.
