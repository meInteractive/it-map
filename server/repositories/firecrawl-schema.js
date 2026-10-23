// Persistent settings reuse company identities and existing source keys.
export const FIRECRAWL_SCHEMA = [
`CREATE TABLE IF NOT EXISTS firecrawl_settings (
 id INTEGER PRIMARY KEY NOT NULL CHECK(id=1), key_encrypted TEXT NOT NULL DEFAULT '',
 key_suffix TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 max_pages INTEGER NOT NULL DEFAULT 3 CHECK(max_pages BETWEEN 1 AND 10),
 concurrency INTEGER NOT NULL DEFAULT 1 CHECK(concurrency=1),
 timeout_ms INTEGER NOT NULL DEFAULT 45000 CHECK(timeout_ms BETWEEN 5000 AND 60000),
 proxy TEXT NOT NULL DEFAULT 'basic' CHECK(proxy IN ('basic','enhanced')),
 enhanced_allowed INTEGER NOT NULL DEFAULT 0 CHECK(enhanced_allowed IN (0,1)),
 favorites_only INTEGER NOT NULL DEFAULT 1 CHECK(favorites_only IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 0, connection_status TEXT NOT NULL DEFAULT 'missing',
 last_error TEXT NOT NULL DEFAULT '', checked_at TEXT,
 mode TEXT NOT NULL DEFAULT 'free' CHECK(mode IN ('free','hobby')),
 hobby_key_encrypted TEXT NOT NULL DEFAULT '', hobby_key_suffix TEXT NOT NULL DEFAULT '',
 hobby_connection_status TEXT NOT NULL DEFAULT 'missing', hobby_last_error TEXT NOT NULL DEFAULT '', hobby_checked_at TEXT,
 free_key_hash TEXT NOT NULL DEFAULT '', hobby_key_hash TEXT NOT NULL DEFAULT '',
 free_usage_json TEXT NOT NULL DEFAULT '', hobby_usage_json TEXT NOT NULL DEFAULT '',
 CHECK(proxy='basic' OR (mode='hobby' AND enhanced_allowed=1)),
 CHECK(enabled=0 OR (mode='free' AND length(key_encrypted)>0) OR (mode='hobby' AND length(hobby_key_encrypted)>0))
)`,
`CREATE TABLE IF NOT EXISTS vacancy_source_parsing (
 company_id INTEGER REFERENCES company_identity(id) ON DELETE CASCADE,
 agency_id INTEGER REFERENCES agency_identity(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL, source_url TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 method TEXT NOT NULL DEFAULT 'scrape' CHECK(method='scrape'), config_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(config_json)),
 revision INTEGER NOT NULL DEFAULT 0, last_success_at TEXT, last_error TEXT NOT NULL DEFAULT '',
 UNIQUE(company_id,source_id),UNIQUE(agency_id,source_id),CHECK((company_id IS NOT NULL)+(agency_id IS NOT NULL)=1)
)`,
// Short-lived server-owned previews; never import them from backups.
`CREATE TABLE IF NOT EXISTS vacancy_discovery_runs (
 id TEXT PRIMARY KEY NOT NULL, user_name TEXT NOT NULL, generation TEXT NOT NULL,
 company_id INTEGER, agency_id INTEGER, source_id TEXT NOT NULL, source_url TEXT NOT NULL,
 source_revision INTEGER NOT NULL, settings_revision INTEGER NOT NULL,
 result_json TEXT NOT NULL CHECK(json_valid(result_json)), created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
)`,
`CREATE INDEX IF NOT EXISTS discovery_run_expiry_idx ON vacancy_discovery_runs(expires_at)`,
`CREATE TABLE IF NOT EXISTS firecrawl_request_lease (
 id INTEGER PRIMARY KEY NOT NULL CHECK(id=1), generation TEXT NOT NULL, owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL
)`
];
