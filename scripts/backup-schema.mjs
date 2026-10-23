import fs from 'node:fs';
import path from 'node:path';
import { DatabaseSync } from 'node:sqlite';

// Build trusted restore DDL from the existing migration history. Never execute
// schema SQL supplied by a backup. Seed records are deliberately not exported.
export function buildBackupSchema(root) {
  const db = new DatabaseSync(':memory:');
  try {
    for (const name of fs.readdirSync(path.join(root, 'drizzle')).filter(n => n.endsWith('.sql')).sort()) {
      db.exec(fs.readFileSync(path.join(root, 'drizzle', name), 'utf8'));
    }
    const objects = db.prepare("SELECT type, name, tbl_name, sql FROM sqlite_schema WHERE sql IS NOT NULL AND name NOT LIKE 'sqlite_%' ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 ELSE 2 END, name").all();
    const tables = objects.filter(o => o.type === 'table').map(o => ({
        name:o.name,
        columns:db.prepare(`PRAGMA table_info("${o.name}")`).all().map(c => ({name:c.name,type:c.type,notNull:!!c.notnull,defaultValue:c.dflt_value,primaryKey:!!c.pk})),
        foreignKeys:db.prepare(`PRAGMA foreign_key_list("${o.name}")`).all(),
        parentTables:[...new Set(db.prepare(`PRAGMA foreign_key_list("${o.name}")`).all().map(k => k.table))],
      }));
    const operationalNames = new Set(['apify_account_usage','apify_runs','scheduler_jobs','scheduler_runs','scheduler_attempts','app_runtime_generation', 'application_creation_receipts', 'cover_letter_generation_leases', 'vacancy_candidate_classification_leases', 'vacancy_discovery_runs', 'firecrawl_request_lease', 'firecrawl_usage_periods', 'firecrawl_usage_events','vacancy_classification_jobs','vacancy_classification_slots','vacancy_detail_jobs','vacancy_detail_leases','vacancy_import_write_context','vacancy_import_batches']);
    return {
      objects,
      tables: tables.filter(table => !operationalNames.has(table.name)),
      operationalTables: tables.filter(table => operationalNames.has(table.name)),
    };
  } finally { db.close(); }
}
