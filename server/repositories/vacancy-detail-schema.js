export const DETAIL_TABLE_NAMES=['vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions'];
export const DETAIL_SETTINGS_SQL=`CREATE TABLE IF NOT EXISTS vacancy_detail_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), markdown TEXT NOT NULL CHECK(length(markdown)<=10000), prompt_revision INTEGER NOT NULL DEFAULT 0,
 provider TEXT NOT NULL DEFAULT 'openrouter' CHECK(provider IN ('openrouter','openai')), openai_model TEXT NOT NULL DEFAULT '', reasoning_effort TEXT NOT NULL DEFAULT 'medium',
 model_revision INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`;
export const VACANCY_DETAIL_SCHEMA=[DETAIL_SETTINGS_SQL,
`CREATE TABLE IF NOT EXISTS vacancy_raw_snapshots (
 id TEXT PRIMARY KEY, candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 source_url TEXT NOT NULL, final_url TEXT NOT NULL, markdown_raw TEXT, content_hash TEXT NOT NULL,
 fetched_at TEXT NOT NULL, firecrawl_mode TEXT NOT NULL CHECK(firecrawl_mode IN ('free','hobby')), firecrawl_credits REAL,
 content_status TEXT NOT NULL CHECK(content_status IN ('valid','invalid')), diagnostics TEXT NOT NULL, created_at TEXT NOT NULL
)`,
`CREATE INDEX IF NOT EXISTS raw_snapshot_candidate_idx ON vacancy_raw_snapshots(candidate_id,created_at)`,
`CREATE TABLE IF NOT EXISTS vacancy_raw_snapshot_parts (
 snapshot_id TEXT NOT NULL REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE, part_index INTEGER NOT NULL, markdown_raw TEXT NOT NULL,
 PRIMARY KEY(snapshot_id,part_index)
)`,
`CREATE TABLE IF NOT EXISTS vacancy_content_fetches (
 id TEXT PRIMARY KEY, candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 snapshot_id TEXT REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE, source_url TEXT NOT NULL, final_url TEXT,
 firecrawl_mode TEXT NOT NULL, credits REAL, started_at INTEGER NOT NULL, duration_ms INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('success','error')), error_code TEXT, message TEXT NOT NULL DEFAULT ''
)`,
`CREATE INDEX IF NOT EXISTS content_fetch_candidate_idx ON vacancy_content_fetches(candidate_id,started_at)`,
`CREATE TABLE IF NOT EXISTS vacancy_extractions (
 id TEXT PRIMARY KEY, candidate_id INTEGER NOT NULL REFERENCES vacancy_candidates(id) ON DELETE CASCADE,
 snapshot_id TEXT NOT NULL REFERENCES vacancy_raw_snapshots(id) ON DELETE CASCADE, provider TEXT NOT NULL, model_id TEXT,
 config_hash TEXT NOT NULL, prompt_version TEXT NOT NULL, schema_version INTEGER NOT NULL,
 extracted_data TEXT, processed_at TEXT NOT NULL, duration_ms INTEGER NOT NULL,
 input_tokens INTEGER, output_tokens INTEGER, cost_usd REAL,
 status TEXT NOT NULL CHECK(status IN ('success','error')), error_code TEXT, diagnostics TEXT NOT NULL DEFAULT '{}'
)`,
`CREATE INDEX IF NOT EXISTS extraction_current_idx ON vacancy_extractions(candidate_id,snapshot_id,config_hash,prompt_version,schema_version,status)`,
`CREATE TABLE IF NOT EXISTS vacancy_detail_jobs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, request_key TEXT NOT NULL, items_json TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued', owner_token TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, duration_ms INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 UNIQUE(generation,request_key)
)`,
`CREATE TABLE IF NOT EXISTS vacancy_detail_leases (
 lease_key TEXT PRIMARY KEY, generation TEXT NOT NULL, owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL
)`
];
