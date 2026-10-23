import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();const first=await import('../.tests/backend.mjs'),second=await import('../.tests/backend.mjs?independent-generation-module');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'runtime-generation'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB'),schema=buildBackupSchema(process.cwd());
 assert.equal(schema.tables.length,52);assert.deepEqual(schema.operationalTables.map(t=>t.name),['apify_account_usage','apify_runs','app_runtime_generation','application_creation_receipts','cover_letter_generation_leases','firecrawl_request_lease','firecrawl_usage_events','firecrawl_usage_periods','scheduler_attempts','scheduler_jobs','scheduler_runs','vacancy_candidate_classification_leases','vacancy_classification_jobs','vacancy_classification_slots','vacancy_detail_jobs','vacancy_detail_leases','vacancy_discovery_runs','vacancy_import_batches','vacancy_import_write_context']);
 // Start with a real schema0043 DB. Download must not create the operational table.
 await db.batch(schema.objects.filter(o=>!schema.operationalTables.some(table=>table.name===o.tbl_name)).map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(1,'Synthetic','https://generation.test/1','Backup title','Body')").run();
 const oldBackup=await first.createBackup(db);assert.equal(oldBackup.version,39);
 assert.equal(await db.prepare("SELECT name FROM sqlite_schema WHERE name='app_runtime_generation'").first(),null);
 const generations=await Promise.all([first.ensureRuntimeGeneration(db),second.ensureRuntimeGeneration(db)]);
 assert.equal(generations[0],generations[1],'Concurrent independent initializers share one durable value');assert.match(generations[0],/^[a-f0-9-]{36}$/);
 const initial=generations[0],generation=async()=>(await db.prepare('SELECT generation FROM app_runtime_generation WHERE id=1').first()).generation;
 assert.equal(await first.ensureRuntimeGeneration(db),initial,'Initialization must never rotate a live epoch');
 await db.prepare("INSERT INTO parser_error_daily VALUES('Eugene','2026-10-22',1,7,1,2)").run();const savedHistory=await db.prepare('SELECT * FROM parser_error_daily').first();
 const backup=await first.createBackup(db);assert.equal(backup.version,39);assert.equal(backup.storage.tables.length,52);assert.ok(!backup.storage.tables.some(t=>t.name==='app_runtime_generation'));assert.ok(!JSON.stringify(backup.storage).includes(initial));
 oldBackup.version=38;oldBackup.storage.tables=oldBackup.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));oldBackup.manifest=oldBackup.manifest.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));oldBackup.integrity.digest=await first.backupDigest(oldBackup);
assert.equal((await first.backupInventory(db)).length,52);
 await first.restoreBackup(db,oldBackup);assert.deepEqual(await db.prepare('SELECT * FROM parser_error_daily').first(),savedHistory,'Legacy v38 preserves destination daily history');const restored=await generation();assert.notEqual(restored,initial);assert.deepEqual((await first.createBackup(db)).storage,backup.storage);
 await second.restoreBackup(db,backup);const repeated=await generation();assert.notEqual(repeated,restored);assert.equal(await first.ensureRuntimeGeneration(db),repeated);
 // The file cannot contain or supply an operational generation, even if re-signed.
 const injected=structuredClone(backup);const op=schema.operationalTables.find(table=>table.name==='app_runtime_generation');injected.storage.tables.push({name:op.name,columns:op.columns,rows:[{id:1,generation:initial,updated_at:'synthetic'}]});injected.manifest.push({name:op.name,columns:op.columns.map(c=>c.name),rows:1});injected.integrity.digest=await first.backupDigest(injected);
 await assert.rejects(()=>first.restoreBackup(db,injected),/неизвестные разделы/);assert.equal(await generation(),repeated);
 // Failure at the LAST write rolls back all replacements and epoch rotation.
 await db.prepare("UPDATE company_vacancies SET title='Live title' WHERE id=1").run();const before=await first.createBackup(db);
 await db.prepare("CREATE TRIGGER fail_epoch BEFORE UPDATE ON app_runtime_generation BEGIN SELECT RAISE(ABORT,'Synthetic epoch failure'); END").run();
 await assert.rejects(()=>first.restoreBackup(db,backup),/Synthetic epoch failure/);assert.equal(await generation(),repeated);assert.deepEqual((await first.createBackup(db)).storage,before.storage);
 await db.prepare('DROP TRIGGER fail_epoch').run();await first.restoreBackup(db,backup);assert.notEqual(await generation(),repeated);assert.deepEqual((await first.createBackup(db)).storage,backup.storage);
 // Failed validation must not initialize or rotate anything.
 const latest=await generation(),bad=structuredClone(backup);bad.integrity.digest='invalid';await assert.rejects(()=>first.restoreBackup(db,bad),/Контрольная сумма/);assert.equal(await generation(),latest);
 // Parallel restore transactions generate distinct epochs; final data is coherent.
 await Promise.all([first.restoreBackup(db,backup),second.restoreBackup(db,oldBackup)]);assert.notEqual(await generation(),latest);assert.deepEqual((await first.createBackup(db)).storage,backup.storage);
 // A recognized operational name with a wrong layout must still stop export.
 await db.prepare('DROP TABLE app_runtime_generation').run();await db.prepare('CREATE TABLE app_runtime_generation(id INTEGER PRIMARY KEY,unrecognized TEXT)').run();await assert.rejects(()=>first.createBackup(db),/несовместимая структура/);
 console.log('Native D1: durable shared initialization, schema0043 compatibility, backup24/28 application-table scope, no exported epoch, repeated/parallel rotation, injected generation rejection, last-write rollback, validation failure and malformed operational layout passed.');
}finally{await mf.dispose();}
