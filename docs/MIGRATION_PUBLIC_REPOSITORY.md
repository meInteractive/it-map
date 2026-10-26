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


## Использование в текущем репозитории

Очищенный исторический `gpt-site` — эталон исходников, не точная копия работающей среды и не самостоятельный production-restore. Рабочие SQL-миграции с данными, приватная история, собранный `dist/` и project hosting ID не входят в публичный снимок. `src/data.json` заменён синтетическим каталогом; его наличие не означает доступ к рабочей D1.

Реальные MariaDB/D1 dumps, JSON-backup v39, резюме, контакты и ключи хранятся вне GitHub. Их нельзя добавлять также в ветки, PR, releases или Actions logs. `.gitignore` не стирает уже опубликованную историю: для доступной истории отдельно выполняется `python3 scripts/check-git-history.py`; source gate и history audit проверяют известные правила, не обещая универсальный поиск всех приватных данных.

Раздел How to use выше сохраняет историческую процедуру первичного импорта архива. Текущая разработка ведётся через feature branches/PR в main по [AGENTS.md](../AGENTS.md); рабочий порядок — в [PLANS.md](../PLANS.md), результаты и актуальная база стека — в [tasks/STACK.md](../tasks/STACK.md). Настройка шаблонов [развёртывания](deployment/README.md) сама по себе не выполняет deployment.

Исторические и форматные справочники: [старое API](API.md), [backup v39](BACKUP_FORMAT.md), [очищенный source reference](SOURCE_REFERENCE.md), [чекпойнт V12](SPRINT_1_1_V12.md). V12 отражает прежний этап, а не текущую приёмку Go; текущие статусы находятся в [реестре задач](../tasks/README.md).
