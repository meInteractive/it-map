import {PARSER_ERROR_HISTORY_SQL} from './parser-error-history.js';
export const SCHEDULER_APIFY_POLICY=Object.freeze({priorityPenalty:50,maxStagesPerCycle:1,maxHhStagesPerCycle:5,pollDelayMs:60000});
import {VACANCY_SOURCE_META} from '../constants.js';
const unavailable={telegram:'Telegram не поддерживается этим сборщиком.',dreamJob:'Нужен отдельный адаптер списка вакансий.',hCareers:'Нужен отдельный адаптер списка вакансий.'};
export const SCHEDULER_SOURCE_OPTIONS=VACANCY_SOURCE_META.map(([id,name])=>({id,name,supported:!unavailable[id],reason:unavailable[id]||null}));
export const SCHEDULER_DEFAULTS={enabled:false,agenciesEnabled:false,ownerName:'Eugene',dayBudgetUsd:0,monthBudgetUsd:0,revision:0,cloudOwnerId:'',sourceIds:[],intervalDays:7,priorities:{import_vacancy:100,assign_roles:90,score_relevance:85,extract_vacancy:80,fetch_full_text:70,classify_title:60,scan_source:20,diagnostic:0}};
export const SCHEDULER_SOURCE_COLUMNS={
 auto_enabled:"INTEGER NOT NULL DEFAULT 0 CHECK(auto_enabled IN (0,1))",
 interval_days:"INTEGER NOT NULL DEFAULT 7 CHECK(interval_days IN (1,3,7))",
 next_run_at:'INTEGER',last_attempt_at:'INTEGER',last_result:"TEXT NOT NULL DEFAULT ''"
};
export const SCHEDULER_SCHEMA=[
PARSER_ERROR_HISTORY_SQL,
`CREATE TABLE IF NOT EXISTS scheduler_jobs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, dedupe_key TEXT NOT NULL,
 type TEXT NOT NULL CHECK(type IN ('scan_source','classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance','diagnostic')),
 company_id INTEGER, agency_id INTEGER CHECK(agency_id IS NULL OR company_id IS NULL), candidate_id INTEGER, source_id TEXT,
 payload TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(payload)),
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','completed','failed','cancelled')),
 priority INTEGER NOT NULL, run_after INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0,
 max_attempts INTEGER NOT NULL DEFAULT 4, locked_by TEXT NOT NULL DEFAULT '', locked_until INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', reason_code TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, completed_at INTEGER,
 UNIQUE(generation,dedupe_key)
)`,
`CREATE UNIQUE INDEX IF NOT EXISTS scheduler_agency_source_active_idx ON scheduler_jobs(generation,agency_id,source_id) WHERE agency_id IS NOT NULL AND type='scan_source' AND status IN ('pending','running')`,
`CREATE INDEX IF NOT EXISTS scheduler_due_idx ON scheduler_jobs(generation,status,run_after,priority)`,
`CREATE UNIQUE INDEX IF NOT EXISTS scheduler_source_active_idx ON scheduler_jobs(generation,company_id,source_id) WHERE type='scan_source' AND status IN ('pending','running')`,
`CREATE TABLE IF NOT EXISTS scheduler_runs (
 id TEXT PRIMARY KEY, generation TEXT NOT NULL, trigger TEXT NOT NULL,
 started_at INTEGER NOT NULL, finished_at INTEGER, status TEXT NOT NULL,
 ready INTEGER NOT NULL DEFAULT 0, processed INTEGER NOT NULL DEFAULT 0, succeeded INTEGER NOT NULL DEFAULT 0,
 errors INTEGER NOT NULL DEFAULT 0, remaining INTEGER NOT NULL DEFAULT 0, reason TEXT NOT NULL DEFAULT '',
 cursor_id INTEGER NOT NULL DEFAULT 0, lease_until INTEGER NOT NULL DEFAULT 0
)`,
`CREATE INDEX IF NOT EXISTS scheduler_runs_date_idx ON scheduler_runs(started_at DESC)`,
`CREATE TABLE IF NOT EXISTS scheduler_attempts (
 id TEXT PRIMARY KEY, job_id TEXT NOT NULL, run_id TEXT NOT NULL, type TEXT NOT NULL,
 company_id INTEGER, source_id TEXT, started_at INTEGER NOT NULL, finished_at INTEGER,
 status TEXT NOT NULL, reason_code TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '',
 provider TEXT, model TEXT, reserved_usd REAL NOT NULL DEFAULT 0 CHECK(reserved_usd>=0),
 cost_usd REAL CHECK(cost_usd IS NULL OR cost_usd>=0), credits REAL,
 result_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(result_json))
)`,
`CREATE INDEX IF NOT EXISTS scheduler_cost_date_idx ON scheduler_attempts(started_at,type)`,
`CREATE INDEX IF NOT EXISTS scheduler_job_history_idx ON scheduler_attempts(job_id,started_at)`
];
export async function ensureSchedulerSchema(db){
 const columns=(await db.prepare("PRAGMA table_info('firecrawl_settings')").all()).results||[];
 if(!columns.some(c=>c.name==='scheduler_json'))await addColumn(db,'firecrawl_settings','scheduler_json',`TEXT NOT NULL DEFAULT '${JSON.stringify(SCHEDULER_DEFAULTS)}' CHECK(json_valid(scheduler_json))`);
 const sourceColumns=(await db.prepare("PRAGMA table_info('vacancy_source_parsing')").all()).results||[];
 for(const [name,type] of Object.entries(SCHEDULER_SOURCE_COLUMNS))if(!sourceColumns.some(c=>c.name===name))await addColumn(db,'vacancy_source_parsing',name,type);
 await db.batch(SCHEDULER_SCHEMA.map(sql=>db.prepare(sql)));
}
async function addColumn(db,table,name,type){try{await db.prepare(`ALTER TABLE ${table} ADD COLUMN ${name} ${type}`).run();}catch(e){const fresh=(await db.prepare(`PRAGMA table_info('${table}')`).all()).results||[];if(!fresh.some(c=>c.name===name))throw e;}}
