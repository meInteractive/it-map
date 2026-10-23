import {FIRECRAWL_SCHEMA} from './firecrawl-schema.js';
export const FIRECRAWL_USAGE_SCHEMA=[
`CREATE TABLE IF NOT EXISTS firecrawl_usage_periods (
 key_hash TEXT NOT NULL, period_start TEXT NOT NULL, period_end TEXT NOT NULL,
 provider_used_high INTEGER NOT NULL DEFAULT 0 CHECK(provider_used_high>=0),
 remaining_credits INTEGER NOT NULL CHECK(remaining_credits>=0), plan_credits INTEGER NOT NULL CHECK(plan_credits>0),
 checked_at INTEGER NOT NULL, PRIMARY KEY(key_hash,period_start,period_end)
)`,
`CREATE TABLE IF NOT EXISTS firecrawl_usage_events (
 id TEXT PRIMARY KEY NOT NULL, key_hash TEXT NOT NULL, profile TEXT NOT NULL CHECK(profile IN ('free','hobby')),
 period_start TEXT NOT NULL, period_end TEXT NOT NULL,
 operation TEXT NOT NULL CHECK(operation='scrape'), reserved_credits INTEGER NOT NULL CHECK(reserved_credits>0),
 charged_credits REAL CHECK(charged_credits IS NULL OR charged_credits>=0),
 outcome TEXT NOT NULL CHECK(outcome IN ('reserved','complete','error','uncertain')),
 started_at INTEGER NOT NULL, finished_at INTEGER, active_until INTEGER NOT NULL,
 provider_status INTEGER, error_code TEXT, generation TEXT NOT NULL
)`,
`CREATE INDEX IF NOT EXISTS firecrawl_budget_period_idx ON firecrawl_usage_events(period_start,period_end,profile,key_hash)`,
`CREATE INDEX IF NOT EXISTS firecrawl_rate_idx ON firecrawl_usage_events(started_at)`
];
export const OLD_FIRECRAWL_COLUMNS=['id','key_encrypted','key_suffix','enabled','max_pages','concurrency','timeout_ms','proxy','enhanced_allowed','revision','connection_status','last_error','checked_at'];
export function firecrawlSettingsUpgradeSql(){
 const columns=OLD_FIRECRAWL_COLUMNS.join(',');return [
 `SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM pragma_table_info('firecrawl_settings') WHERE name='mode') THEN 1 ELSE abs(-9223372036854775808) END`,
 FIRECRAWL_SCHEMA[0].replace('IF NOT EXISTS firecrawl_settings','firecrawl_settings_upgrade'),
 `INSERT INTO firecrawl_settings_upgrade(${columns}) SELECT id,key_encrypted,key_suffix,enabled,max_pages,concurrency,timeout_ms,'basic',0,revision+1,connection_status,last_error,checked_at FROM firecrawl_settings`,
 'DROP TABLE firecrawl_settings','ALTER TABLE firecrawl_settings_upgrade RENAME TO firecrawl_settings'
 ];
}
export async function ensureFirecrawlPolicySchema(db){
 const columns=(await db.prepare("PRAGMA table_info('firecrawl_settings')").all()).results||[];
 if(!columns.some(c=>c.name==='mode')){
  try{await db.batch(firecrawlSettingsUpgradeSql().map(sql=>db.prepare(sql)));}
  catch(e){const latest=(await db.prepare("PRAGMA table_info('firecrawl_settings')").all()).results||[];if(!latest.some(c=>c.name==='mode'))throw e;}
 }
 const latest=(await db.prepare("PRAGMA table_info('firecrawl_settings')").all()).results||[];
 if(!latest.some(c=>c.name==='favorites_only')){
  try{await db.prepare('ALTER TABLE firecrawl_settings ADD COLUMN favorites_only INTEGER NOT NULL DEFAULT 1 CHECK(favorites_only IN (0,1))').run();}
  catch(e){const fresh=(await db.prepare("PRAGMA table_info('firecrawl_settings')").all()).results||[];if(!fresh.some(c=>c.name==='favorites_only'))throw e;}
 }
 await db.batch(FIRECRAWL_USAGE_SCHEMA.map(sql=>db.prepare(sql)));
}
