export const APIFY_SCHEMA=[
// Durable account high-water marks are excluded from user backup/restore.
`CREATE TABLE IF NOT EXISTS apify_account_usage (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL, period_start TEXT NOT NULL, period_end TEXT NOT NULL,
 provider_used_usd REAL NOT NULL CHECK(provider_used_usd>=0), updated_at INTEGER NOT NULL
)`,
`CREATE TABLE IF NOT EXISTS apify_settings (
 id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 paid_allowed INTEGER NOT NULL DEFAULT 0 CHECK(paid_allowed IN (0,1)), revision INTEGER NOT NULL DEFAULT 0,
 free_key_encrypted TEXT NOT NULL DEFAULT '', free_key_suffix TEXT NOT NULL DEFAULT '',
 starter_key_encrypted TEXT NOT NULL DEFAULT '', starter_key_suffix TEXT NOT NULL DEFAULT '',
 free_account_json TEXT NOT NULL DEFAULT '', starter_account_json TEXT NOT NULL DEFAULT '',
 free_error TEXT NOT NULL DEFAULT '', starter_error TEXT NOT NULL DEFAULT ''
)`,
// Not restored/exported: charges and uncertain starts must survive a restore.
`CREATE TABLE IF NOT EXISTS apify_runs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, operation_key TEXT NOT NULL,
 profile TEXT NOT NULL CHECK(profile IN ('free','starter')), account_id TEXT NOT NULL, key_hash TEXT NOT NULL,
 period_start TEXT NOT NULL, period_end TEXT NOT NULL, reserved_usd REAL NOT NULL CHECK(reserved_usd>0),
 charged_usd REAL CHECK(charged_usd IS NULL OR charged_usd>=0),
 state TEXT NOT NULL CHECK(state IN ('starting','running','succeeded','failed','uncertain')),
 run_id TEXT NOT NULL DEFAULT '', dataset_id TEXT NOT NULL DEFAULT '',
 error_code TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 UNIQUE(generation,operation_key)
)`,
`CREATE INDEX IF NOT EXISTS apify_budget_idx ON apify_runs(account_id,period_start,period_end)`
];
