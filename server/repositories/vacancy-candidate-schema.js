import {PARSER_INTAKE_SQL} from './parser-intakes.js';
// Additive staging storage; no trigger or write targets the main vacancies.
export const VACANCY_CANDIDATE_SCHEMA = [
PARSER_INTAKE_SQL,
`CREATE TABLE IF NOT EXISTS vacancy_candidates (
 id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
 creation_token TEXT NOT NULL UNIQUE,
 company_id INTEGER REFERENCES company_identity(id) ON DELETE SET NULL,
 agency_id INTEGER REFERENCES agency_identity(id) ON DELETE SET NULL CHECK(agency_id IS NULL OR company_id IS NULL),
 company_name_raw TEXT NOT NULL DEFAULT '',
 title_raw TEXT NOT NULL CHECK(length(trim(title_raw)) BETWEEN 1 AND 500),
 title_hash TEXT NOT NULL CHECK(length(title_hash)=64),
 title_revision INTEGER NOT NULL DEFAULT 1 CHECK(title_revision>=1),
 vacancy_url TEXT, canonical_url TEXT, source_url TEXT, source_id TEXT,
 source_namespace TEXT, external_job_id TEXT, import_key TEXT,
 acquisition_method TEXT NOT NULL CHECK(acquisition_method IN ('site','firecrawl','excel','manual')),
 first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, title_updated_at TEXT,
 main_vacancy_id INTEGER REFERENCES company_vacancies(id) ON DELETE SET NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
)`,
`CREATE TABLE IF NOT EXISTS vacancy_candidate_identities (
 identity_key TEXT PRIMARY KEY NOT NULL,
 candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE
)`,
`CREATE INDEX IF NOT EXISTS candidate_identity_candidate_idx ON vacancy_candidate_identities(candidate_id)`,
`CREATE INDEX IF NOT EXISTS candidate_agency_seen_idx ON vacancy_candidates(agency_id,last_seen_at DESC,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_company_seen_idx ON vacancy_candidates(company_id,last_seen_at DESC,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_source_seen_idx ON vacancy_candidates(source_namespace,last_seen_at DESC,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_source_id_seen_idx ON vacancy_candidates(source_id,last_seen_at DESC,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_seen_idx ON vacancy_candidates(last_seen_at DESC,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_external_idx ON vacancy_candidates(source_namespace,external_job_id)`,
`CREATE INDEX IF NOT EXISTS candidate_canonical_idx ON vacancy_candidates(canonical_url)`,
`CREATE TABLE IF NOT EXISTS vacancy_classification_attempts (
 id TEXT PRIMARY KEY NOT NULL,
 candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 title_raw TEXT NOT NULL, title_hash TEXT NOT NULL CHECK(length(title_hash)=64),
 title_revision INTEGER NOT NULL,
 model_key TEXT NOT NULL, model_id TEXT,
 requested_roles_json TEXT NOT NULL CHECK(json_valid(requested_roles_json)),
 errors_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(errors_json)),
 outcome TEXT NOT NULL CHECK(outcome IN ('complete','partial','error')),
 classified_at TEXT NOT NULL,
 duration_ms INTEGER CHECK(duration_ms IS NULL OR duration_ms>=0),
 cost_usd REAL CHECK(cost_usd IS NULL OR cost_usd>=0)
)`,
`CREATE INDEX IF NOT EXISTS candidate_attempt_history_idx ON vacancy_classification_attempts(candidate_id,classified_at DESC,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_attempt_input_idx ON vacancy_classification_attempts(candidate_id,title_revision,title_hash,model_key)`,
`CREATE TABLE IF NOT EXISTS vacancy_classifications (
 id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
 candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 attempt_id TEXT NOT NULL REFERENCES vacancy_classification_attempts(id) ON DELETE CASCADE,
 root_role_id INTEGER NOT NULL REFERENCES vacancy_roles(id) ON DELETE RESTRICT,
 root_role_name TEXT NOT NULL,
 decision TEXT NOT NULL CHECK(decision IN ('match','reject')),
 model_key TEXT NOT NULL, model_id TEXT NOT NULL,
 prompt_version TEXT NOT NULL CHECK(length(prompt_version)=64),
 prompt_revision INTEGER NOT NULL,
 title_hash TEXT NOT NULL CHECK(length(title_hash)=64),
 title_revision INTEGER NOT NULL,
 classified_at TEXT NOT NULL,
 UNIQUE(attempt_id,root_role_id)
)`,
`CREATE INDEX IF NOT EXISTS candidate_classification_current_idx ON vacancy_classifications(candidate_id,root_role_id,title_revision,title_hash,prompt_version,model_key,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_classification_history_idx ON vacancy_classifications(candidate_id,id DESC)`,
`CREATE INDEX IF NOT EXISTS candidate_classification_role_idx ON vacancy_classifications(root_role_id,candidate_id)`,
`CREATE TABLE IF NOT EXISTS vacancy_candidate_classification_leases (
 candidate_id INTEGER PRIMARY KEY NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 generation TEXT NOT NULL, owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL CHECK(expires_at>0)
)`
];
