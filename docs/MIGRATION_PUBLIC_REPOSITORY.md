# Public GitHub migration boundary — 2026-10-23

## Scope
This `gpt-site` snapshot is a **source reference** for Codex, derived from Sites v316.
It excludes user data, DB dump, full JSON backup, operational history, build artifacts,
original Site project binding, runtime queues, SQL import histories, and legacy worker template.

`src/data.json` is intentionally replaced by three synthetic employers with demo jobs.
Some old tests expect real named employers, 84 historical migrations or actual Git SHA;
they cannot be used as a complete regression baseline on the sanitized branch.
Use contract fixtures and new tests when implementing Go.

## Security boundary
Server-only secrets must come from runtime environment or a secret manager.
No production key/credential, API token, user resume, recruitment contact or dump may be
committed to *any branch or Git history*. Pushing a file then deleting it is not safe.
`.gitignore` prevents accidental adds but it is **not** a security scanner.
Before every push run `python3 scripts/check-public-repo.py` and ideally Gitleaks
against the full repository history from a private machine/CI.
No secret values are provided with this snapshot, including for deploy.

## How to use
1. Initialize a new empty public repository, without copying an old `.git` directory.
2. Import this archive as branch `gpt-site` (source reference, never production deploy).
3. Create branch `master` from it (or as an orphan root for a cleaner Go baseline).
4. Configure staging as isolated Go service with a separate MariaDB and test credentials.
5. Store actual MariaDB dump and backup **outside GitHub**, including releases and Actions logs.
6. Deploy only verified feature branches to test; promote reviewed commits to `master`.

## Limits of verification
Static scanning checks known key formats and suspicious filenames, not every possible
credential, licensed dataset, personal datum, or hidden Git history. Human review is still
required prior to making the repository public.
