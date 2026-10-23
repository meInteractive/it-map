export const IMPORT_TABLE_NAMES=['vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations'];
export const ROLE_SETTINGS_SQL=`CREATE TABLE IF NOT EXISTS vacancy_role_settings (
 id INTEGER PRIMARY KEY CHECK(id=1),markdown TEXT NOT NULL CHECK(length(markdown)<=5000),prompt_revision INTEGER NOT NULL DEFAULT 0,
 provider TEXT NOT NULL DEFAULT 'openrouter' CHECK(provider IN ('openrouter','openai')),openai_model TEXT NOT NULL DEFAULT '',openrouter_model TEXT NOT NULL DEFAULT 'best_free' CHECK(openrouter_model IN ('best_free','jev')),reasoning_effort TEXT NOT NULL DEFAULT 'medium',model_revision INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;
export const VACANCY_IMPORT_SCHEMA=[ROLE_SETTINGS_SQL,
`CREATE TABLE IF NOT EXISTS vacancy_role_results (
 id TEXT PRIMARY KEY,candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,snapshot_id TEXT NOT NULL REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE,
 input_hash TEXT NOT NULL,tree_hash TEXT NOT NULL,prompt_version TEXT NOT NULL,config_hash TEXT NOT NULL,model_id TEXT,provider TEXT NOT NULL,role_ids TEXT,
 status TEXT NOT NULL CHECK(status IN ('success','error')),error_code TEXT,message TEXT NOT NULL DEFAULT '',processed_at TEXT NOT NULL,duration_ms INTEGER NOT NULL,input_tokens INTEGER,output_tokens INTEGER,cost_usd REAL
)`,
`CREATE INDEX IF NOT EXISTS role_result_cache_idx ON vacancy_role_results(candidate_id,input_hash,status,processed_at)`,
`CREATE TABLE IF NOT EXISTS vacancy_parser_fields (
 vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE,field TEXT NOT NULL,value_json TEXT NOT NULL,manual INTEGER NOT NULL DEFAULT 0 CHECK(manual IN (0,1)),candidate_id INTEGER REFERENCES vacancy_candidates(id) ON DELETE SET NULL,PRIMARY KEY(vacancy_id,field)
)`,
`CREATE TABLE IF NOT EXISTS vacancy_parser_roles (
 vacancy_id INTEGER NOT NULL,role_id INTEGER NOT NULL,candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,PRIMARY KEY(vacancy_id,role_id,candidate_id),FOREIGN KEY(vacancy_id,role_id) REFERENCES vacancy_role_assignments(vacancy_id,role_id) ON DELETE CASCADE
)`,
`CREATE TABLE IF NOT EXISTS vacancy_publication_sources (
 candidate_id INTEGER PRIMARY KEY REFERENCES vacancy_candidates(id) ON DELETE CASCADE,vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE,url TEXT NOT NULL,canonical_url TEXT NOT NULL,source_namespace TEXT,external_job_id TEXT,extraction_id TEXT REFERENCES vacancy_extractions(id) ON DELETE SET NULL,updated_at TEXT NOT NULL
)`,
`CREATE INDEX IF NOT EXISTS publication_source_url_idx ON vacancy_publication_sources(canonical_url)`,
`CREATE TABLE IF NOT EXISTS vacancy_publication_keys (
 identity_key TEXT PRIMARY KEY,vacancy_id INTEGER NOT NULL REFERENCES company_vacancies(id) ON DELETE CASCADE
)`,
`CREATE TABLE IF NOT EXISTS vacancy_import_operations (
 id TEXT PRIMARY KEY,candidate_id INTEGER REFERENCES vacancy_candidates(id) ON DELETE SET NULL,vacancy_id INTEGER REFERENCES company_vacancies(id) ON DELETE SET NULL,batch_id TEXT,action TEXT NOT NULL,status TEXT NOT NULL,processed_at TEXT NOT NULL,input_hash TEXT,changes_json TEXT NOT NULL,error_code TEXT,message TEXT NOT NULL DEFAULT ''
)`,
`CREATE INDEX IF NOT EXISTS vacancy_import_history_idx ON vacancy_import_operations(candidate_id,processed_at)`,
`CREATE TABLE IF NOT EXISTS vacancy_import_write_context (vacancy_id INTEGER PRIMARY KEY,generation TEXT NOT NULL,owner_token TEXT NOT NULL)`,
`CREATE TABLE IF NOT EXISTS vacancy_import_batches (id TEXT PRIMARY KEY,generation TEXT NOT NULL,request_key TEXT NOT NULL,items_json TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',created_at TEXT NOT NULL,UNIQUE(generation,request_key))`,
...['title','description_md','salary_from','salary_to','level','published_at'].map(field=>`CREATE TRIGGER IF NOT EXISTS parser_protect_${field} AFTER UPDATE OF ${field} ON company_vacancies WHEN OLD.${field} IS NOT NEW.${field} AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='${field}'; END`)
];
