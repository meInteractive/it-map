// Unchanged source restore on synthetic SQLite :memory:. Only JSON stdout/pipes.
import {execFileSync} from 'node:child_process';
import {DatabaseSync} from 'node:sqlite';
import assert from 'node:assert/strict';
import {createBackup,restoreBackup} from '../server/backup/index.js';
import {initial,table,sign,schema} from './historical-format-support.mjs';
globalThis.fetch=()=>{throw new Error('Historical SQL oracle forbids network');};
const index=process.argv.indexOf('--version'),version=Number(process.argv[index+1]);assert.ok(index>0&&Number.isInteger(version)&&version>=16&&version<=39);
const base=JSON.parse(JSON.parse(execFileSync(process.execPath,[new URL('./check-v39-restore-sql.mjs',import.meta.url).pathname,'--stdio'],{encoding:'utf8'})).body);
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async first(){return sqlite.prepare(sql).get(...args)??null;},async run(){const s=sqlite.prepare(sql);if(s.columns().length)return {results:s.all(...args)};s.run(...args);return {success:true};}};},async batch(statements){sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
await restoreBackup(db,base);
sqlite.exec(`INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('Demo','CI destination resume 😀',7);
UPDATE system_prompts SET markdown='CI destination prompt',revision=8;
UPDATE vacancy_role_classification SET markdown='CI destination role prompt',revision=9 WHERE role_id=1;
UPDATE firecrawl_settings SET revision=7,hobby_key_encrypted='CI synthetic saved opaque';
UPDATE apify_settings SET revision=9,free_key_encrypted='CI synthetic saved opaque';
INSERT INTO parser_error_daily(owner_name,day,p1_peak,p2_peak,first_sample_at,last_sample_at) VALUES('Demo','2026-10-27',2,1,1000,2000);
INSERT INTO parser_candidate_intakes(candidate_token,parser,source_id,received_at) VALUES('CI candidate','manual',NULL,'CI original date'),('CI stale token','manual',NULL,'CI old date');`);
const current=await createBackup(db);await restoreBackup(db,current);const seed=await createBackup(db);
const incoming=initial(version);incoming.createdAt=base.createdAt;incoming.catalog=base.catalog;
for(const t of incoming.storage.tables){const source=table(base,t.name);t.rows=source.rows.map(r=>Object.fromEntries(t.columns.map(c=>[c.name,r[c.name]])));}
incoming.metadata=structuredClone(base.metadata);
if(version===27){for(const r of table(incoming,'vacancy_roles').rows){if(r.id===1)r.id=101;if(r.parent_id===1)r.parent_id=101;}}
await sign(incoming);const body=JSON.stringify(incoming),before=JSON.stringify(seed);
const reply=await restoreBackup(db,incoming),after=await createBackup(db);assert.equal(JSON.stringify(incoming),body);assert.equal(JSON.stringify(seed),before);assert.equal(after.storage.tables.length,52);
if(process.argv.includes('--stdio'))process.stdout.write(JSON.stringify({initial:JSON.stringify(seed),body,expected:after.storage.tables,warnings:reply.warnings,rows:reply.rows,version}));else console.log(`PASS: unchanged source SQL v${version} restore, ${reply.rows} rows; memory only`);
sqlite.close();
