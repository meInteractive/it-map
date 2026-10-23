export const PARSER_INTAKE_SQL=`CREATE TABLE IF NOT EXISTS parser_candidate_intakes (
 candidate_token TEXT PRIMARY KEY NOT NULL,
 parser TEXT NOT NULL CHECK(parser IN ('apify','firecrawl','http','manual','excel','unknown')),
 source_id TEXT,
 received_at TEXT NOT NULL
)`;
