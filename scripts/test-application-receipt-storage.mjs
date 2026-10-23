// Native D1 storage/backup contract; HTTP and React retries have separate suites.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'receipt-storage'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB'),schema=buildBackupSchema(process.cwd());
 assert.equal(schema.tables.length,52);assert.deepEqual(schema.operationalTables.map(t=>t.name),['apify_account_usage','apify_runs','app_runtime_generation','application_creation_receipts','cover_letter_generation_leases','firecrawl_request_lease','firecrawl_usage_events','firecrawl_usage_periods','scheduler_attempts','scheduler_jobs','scheduler_runs','vacancy_candidate_classification_leases','vacancy_classification_jobs','vacancy_classification_slots','vacancy_detail_jobs','vacancy_detail_leases','vacancy_discovery_runs','vacancy_import_batches','vacancy_import_write_context']);
 const definition=schema.operationalTables.find(table=>table.name==='application_creation_receipts');assert.deepEqual(definition.foreignKeys,[]);
 // Existing schema0044, export must not bootstrap a missing receipt table.
 await db.batch(schema.objects.filter(object=>object.tbl_name!==definition.name).map(object=>db.prepare(object.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 await db.prepare("INSERT INTO company_applications(user_name,company_name,status) VALUES('Nastya','Synthetic','')").run();
 const oldBackup=await api.createBackup(db);assert.equal(oldBackup.version,39);assert.equal(await db.prepare("SELECT name FROM sqlite_schema WHERE name='application_creation_receipts'").first(),null);
 const generation=await api.ensureRuntimeGeneration(db);await Promise.all([api.ensureApplicationReceiptSchema(db),api.ensureApplicationReceiptSchema(db)]);
 // Runtime bootstrap and subsequently applying the new migration are compatible.
 await db.prepare(fs.readFileSync('drizzle/0045_application_creation_receipts.sql','utf8')).run();
 const key=crypto.randomUUID(),hash='a'.repeat(64),id=(await db.prepare('SELECT id FROM company_applications').first()).id;
 const insert=(user='Nastya',token=key,epoch=generation,digest=hash,applicationId=id)=>db.prepare('INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash,created_application_id) VALUES(?,?,?,?,?)').bind(user,token,epoch,digest,applicationId).run();
 await insert();const receipt=await api.readApplicationReceipt(db,'Nastya',key);assert.equal(receipt.applicationId,id);assert.equal(receipt.requestHash,hash);assert.equal(await api.readApplicationReceipt(db,'Eugene',key),null);
 await assert.rejects(()=>insert(),/UNIQUE/);await insert('Eugene');
 for(const bad of [()=>insert('',crypto.randomUUID()),()=>insert('Nastya','bad'),()=>insert('Nastya',crypto.randomUUID(),'bad'),()=>insert('Nastya',crypto.randomUUID(),generation,'G'.repeat(64)),()=>insert('Nastya',crypto.randomUUID(),generation,hash,0)])await assert.rejects(bad,/CHECK/);
 const receipts=async()=>(await db.prepare('SELECT * FROM application_creation_receipts ORDER BY user_name').all()).results;
 const before=await receipts(),backup=await api.createBackup(db);assert.deepEqual(backup.storage,oldBackup.storage);assert.equal(backup.manifest.length,52);assert.ok(!JSON.stringify(backup.storage).includes(key));assert.ok(!backup.metadata.sequences.some(row=>row.name===definition.name));
 // No cascade: deletion is a tombstone for the later replay protocol.
 await db.prepare('DELETE FROM company_applications WHERE id=?').bind(id).run();assert.deepEqual(await receipts(),before);
 // Restore preserves operational rows, while changing the generation.
 await api.restoreBackup(db,oldBackup);assert.deepEqual(await receipts(),before);assert.notEqual(await api.ensureRuntimeGeneration(db),generation);assert.deepEqual((await api.createBackup(db)).storage,oldBackup.storage);
 // Failure at final restore write rolls application rows and generation back.
 await db.prepare("UPDATE company_applications SET comment_md='Live' WHERE id=?").bind(id).run();const live=await api.createBackup(db),liveGeneration=await api.ensureRuntimeGeneration(db);
 await db.prepare("CREATE TRIGGER fail_receipt_restore BEFORE UPDATE ON app_runtime_generation BEGIN SELECT RAISE(ABORT,'Synthetic failure'); END").run();
 await assert.rejects(()=>api.restoreBackup(db,backup),/Synthetic failure/);assert.deepEqual(await receipts(),before);assert.equal(await api.ensureRuntimeGeneration(db),liveGeneration);assert.deepEqual((await api.createBackup(db)).storage,live.storage);await db.prepare('DROP TRIGGER fail_receipt_restore').run();
 // Even a correctly signed backup may not supply operational receipts.
 const injected=structuredClone(backup);injected.storage.tables.push({...definition,rows:before});injected.manifest.push({name:definition.name,columns:definition.columns.map(c=>c.name),rows:before.length});injected.integrity.digest=await api.backupDigest(injected);await assert.rejects(()=>api.restoreBackup(db,injected),/неизвестные разделы/);assert.deepEqual(await receipts(),before);
 await db.prepare('DROP TABLE application_creation_receipts').run();await db.prepare('CREATE TABLE application_creation_receipts(user_name TEXT, wrong TEXT)').run();await assert.rejects(()=>api.createBackup(db),/несовместимая структура/);
 console.log('Native D1 receipt storage: schema0044 read-only export, concurrent bootstrap/migration, key isolation and constraints, no FK/delete cascade, backup24/28 application tables, restore preservation/rollback, injected receipts and malformed layout rejection passed.');
}finally{await mf.dispose();}
