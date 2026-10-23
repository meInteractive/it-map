export const CLASSIFICATION_JOB_SCHEMA=[
`CREATE TABLE IF NOT EXISTS vacancy_classification_jobs (
 id TEXT PRIMARY KEY NOT NULL, generation TEXT NOT NULL, request_key TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','paused','completed')),
 items_json TEXT NOT NULL CHECK(json_valid(items_json)), owner_token TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 duration_ms INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 UNIQUE(generation,request_key)
)`,
`CREATE INDEX IF NOT EXISTS classification_job_generation_idx ON vacancy_classification_jobs(generation,created_at)`,
`CREATE TABLE IF NOT EXISTS vacancy_classification_slots (
 slot INTEGER PRIMARY KEY NOT NULL CHECK(slot BETWEEN 1 AND 2), generation TEXT NOT NULL,
 owner_token TEXT NOT NULL, expires_at INTEGER NOT NULL
)`
];
