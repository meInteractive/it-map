import assert from 'node:assert/strict';import fs from 'node:fs';import {createHash} from 'node:crypto';import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const opts={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'jev-role-migration'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-jevr'},outboundService:async()=>{throw new Error('No live providers');}};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(opts):opts);
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 await db.prepare('ALTER TABLE vacancy_role_settings DROP COLUMN openrouter_model').run();
 await db.prepare("INSERT INTO vacancy_role_settings(id,markdown,provider,openai_model,prompt_revision,model_revision) VALUES(1,'CUSTOM ADMIN RULES','openai','gpt-6-luna',7,9)").run();
 const migration=fs.readFileSync('drizzle/0074_jev_leaf_roles.sql','utf8');await db.batch(migration.split(/;\s*(?=INSERT|UPDATE|$)/).map(s=>s.trim()).filter(Boolean).map(sql=>db.prepare(sql)));
 const row=await db.prepare('SELECT * FROM vacancy_role_settings').first();assert.equal(row.markdown,'CUSTOM ADMIN RULES');assert.equal(row.prompt_revision,7);assert.equal(row.model_revision,10);assert.equal(row.provider,'openrouter');assert.equal(row.openrouter_model,'jev');
 const api=await import('../.tests/backend.mjs');const model=(await api.readPromptModels(db)).vacancy_role;assert.equal(model.openrouterModel,'jev');
 const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.equal(backup.storage.tables.find(t=>t.name==='vacancy_role_settings').rows[0].openrouter_model,'jev');
 await db.prepare("UPDATE vacancy_role_settings SET openrouter_model='best_free'").run();await api.restoreBackup(db,backup);assert.equal((await api.readPromptModels(db)).vacancy_role.openrouterModel,'jev');
 const legacy=structuredClone(backup);legacy.version=36;legacy.storage.tables=legacy.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));legacy.manifest=legacy.manifest.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));const table=legacy.storage.tables.find(t=>t.name==='vacancy_role_settings');table.columns=table.columns.filter(c=>c.name!=='openrouter_model');for(const r of table.rows)delete r.openrouter_model;const manifest=legacy.manifest.find(t=>t.name==='vacancy_role_settings');manifest.columns=manifest.columns.filter(c=>c!=='openrouter_model');for(const t of legacy.storage.tables.filter(t=>['vacancy_candidates','vacancy_source_parsing'].includes(t.name))){t.columns=t.columns.filter(c=>c.name!=='agency_id');for(const row of t.rows)delete row.agency_id;legacy.manifest.find(m=>m.name===t.name).columns=t.columns.map(c=>c.name);}
 const {integrity,...contents}=legacy;legacy.integrity={algorithm:'SHA-256',digest:createHash('sha256').update(JSON.stringify(contents)).digest('hex')};
 const restored=await api.restoreBackup(db,legacy);assert.equal((await api.readPromptModels(db)).vacancy_role.openrouterModel,'best_free');assert.ok(restored.warnings.some(w=>w.includes('конечных ролей')));assert.equal((await db.prepare('SELECT markdown FROM vacancy_role_settings').first()).markdown,'CUSTOM ADMIN RULES');
 assert.equal((await db.prepare('PRAGMA foreign_key_check').all()).results.length,0);
 console.log('Native D1 JEV role migration: Luna switch/custom prompt/CAS preserved; backup37 model roundtrip, real36 layout/default+warning, FK integrity and zero provider calls passed.');
}finally{await mf.dispose();}
