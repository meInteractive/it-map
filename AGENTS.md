# it-map migration instructions (public-safe)
This branch contains a *sanitized legacy reference*, not a deployable copy of production.
Read `docs/MIGRATION_PUBLIC_REPOSITORY.md` before porting functionality.
The active rewrite is in `master` / feature branches off master, not in this snapshot.
Do not run hosted Site Automations or connect to production data here.
Do not commit production dumps, backups, real resumes, recruiter contacts, tokens, passwords,
OAuth client credentials, encryption keys, or live API responses.
Original SQLite migrations/seed records, built dist, private operational notes, and
hosting identity were intentionally excluded. Use privately stored MariaDB DDL/data
for migration verification; never try to recreate production from this safe branch.
