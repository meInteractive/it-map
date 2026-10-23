// Native D1 concurrency, independent module instances: no process-local lock.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const a=await import('../.tests/backend.mjs'),b=await import('../.tests/backend.mjs?separate-letter-lease-instance');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'letter-leases'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB'),schema=buildBackupSchema(process.cwd());
 assert.equal(schema.tables.length,52);assert.equal(schema.operationalTables.length,19);
 await db.batch(schema.objects.map(o=>db.prepare(o.sql)));
 await a.ensureRuntimeGeneration(db);
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 for(const id of [1,2,3])await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(?,'Synthetic',?,'Role','Body')").bind(id,'https://lease.test/'+id).run();
 await db.prepare("INSERT INTO user_profiles(user_name,resume_md) VALUES('Nastya','PRIVATE CV'),('Eugene','OTHER PRIVATE CV')").run();
 await db.prepare("INSERT INTO system_prompts(kind,markdown) VALUES('cover_letter','PRIVATE PROMPT')").run();
 const read=(user='Nastya',id=1)=>a.readCoverLetterInput(db,user,id);
 await db.prepare('DROP TABLE cover_letter_generation_leases').run();
 const oldExport=await a.createBackup(db);assert.equal(oldExport.storage.tables.length,52);assert.equal(await db.prepare("SELECT name FROM sqlite_schema WHERE name='cover_letter_generation_leases'").first(),null,'Read-only export never bootstraps new coordination');
 const input=await read();
 const contenders=await Promise.all(Array.from({length:12},(_,i)=>(i%2?a:b).claimCoverLetterGeneration(db,'Nastya',1,input)));
 assert.equal(contenders.filter(Boolean).length,1);const owner=contenders.find(Boolean);
 await db.prepare(fs.readFileSync('drizzle/0046_cover_letter_generation_leases.sql','utf8')).run();
 assert.equal((await db.prepare('SELECT count(*) n FROM cover_letter_generation_leases').first()).n,1);
 const savedLease=await db.prepare('SELECT * FROM cover_letter_generation_leases').first();
 assert.match(savedLease.input_hash,/^[a-f0-9]{64}$/);assert.ok(!JSON.stringify(savedLease).includes('PRIVATE'),'No resume/prompt stored in lease');
 assert.ok(await b.renewCoverLetterGeneration(db,owner,input));
 assert.equal(await a.renewCoverLetterGeneration(db,{...owner,ownerToken:crypto.randomUUID()},input),false);
 await a.releaseCoverLetterGeneration(db,{...owner,ownerToken:crypto.randomUUID()});assert.equal(await b.claimCoverLetterGeneration(db,'Nastya',1,input),null);
 // Candidate and vacancy scopes are independent.
 const otherUser=await a.claimCoverLetterGeneration(db,'Eugene',1,await read('Eugene'));
 const otherVacancy=await b.claimCoverLetterGeneration(db,'Nastya',2,await read('Nastya',2));assert.ok(otherUser&&otherVacancy);
 // Neither stale nor updated input can silently renew a lease for another input.
 await db.prepare("UPDATE user_profiles SET resume_md='CHANGED CV',revision=1 WHERE user_name='Nastya'").run();
 const updated=await read();assert.equal(await b.renewCoverLetterGeneration(db,owner,input),false);assert.equal(await b.renewCoverLetterGeneration(db,owner,updated),false);
 assert.equal(await b.claimCoverLetterGeneration(db,'Nastya',1,updated),null,'Active old owner prevents overlapping inference after input change');
 const stale=await read('Nastya',3);await db.prepare("UPDATE company_vacancies SET title='Changed role' WHERE id=3").run();assert.equal(await a.claimCoverLetterGeneration(db,'Nastya',3,stale),null,'Claim CAS checks input at actual INSERT');
 // Deterministic expiry and takeover: no sleeps, expired owners cannot renew/release/write.
 await db.prepare('UPDATE cover_letter_generation_leases SET expires_at=1 WHERE user_name=? AND vacancy_id=1').bind('Nastya').run();
 assert.equal(await a.renewCoverLetterGeneration(db,owner,input),false);
 const successor=await b.claimCoverLetterGeneration(db,'Nastya',1,updated);assert.ok(successor);assert.notEqual(successor.ownerToken,owner.ownerToken);
 await a.releaseCoverLetterGeneration(db,owner);
 assert.equal((await db.prepare("SELECT owner_token FROM cover_letter_generation_leases WHERE user_name='Nastya' AND vacancy_id=1").first()).owner_token,successor.ownerToken);
 const write=lease=>db.prepare(`INSERT OR IGNORE INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) SELECT ?,?,'Subject','Fenced letter' WHERE ${a.coverLetterLeaseSql}`).bind(lease.userName,lease.vacancyId,...a.leaseBindings(lease)).run();
 assert.equal((await write(owner)).meta.changes,0);assert.equal((await write(successor)).meta.changes,1);
 await b.releaseCoverLetterGeneration(db,successor);assert.equal(await a.claimCoverLetterGeneration(db,'Nastya',1,updated),null,'Saved personal letter prevents new ownership');
 const regenerateInput={...await read(),regenerate:true};
 const regenContenders=await Promise.all(Array.from({length:6},(_,i)=>(i%2?a:b).claimCoverLetterGeneration(db,'Nastya',1,regenerateInput)));
 assert.equal(regenContenders.filter(Boolean).length,1,'Native D1 regeneration has one owner with a saved baseline');const regenOwner=regenContenders.find(Boolean);
 assert.ok(await a.renewCoverLetterGeneration(db,regenOwner,regenerateInput));
 await db.prepare("UPDATE vacancy_cover_letters SET revision=revision+1 WHERE user_name='Nastya' AND vacancy_id=1").run();
 assert.equal(await b.renewCoverLetterGeneration(db,regenOwner,regenerateInput),false,'Personal CAS revision fences regeneration even with identical text');
 await a.releaseCoverLetterGeneration(db,regenOwner);
 await db.prepare("DELETE FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=1").run();
 // Restore excludes leases, rotates generation, immediately permits new ownership.
 const backup=await a.createBackup(db);assert.equal(backup.version,39);assert.equal(backup.storage.tables.length,52);assert.ok(!backup.storage.tables.some(t=>t.name==='cover_letter_generation_leases'));
 const beforeRestore=await a.claimCoverLetterGeneration(db,'Nastya',1,await read());assert.ok(beforeRestore);
 await b.restoreBackup(db,backup);assert.equal(await a.renewCoverLetterGeneration(db,beforeRestore,await read()),false);assert.equal((await write(beforeRestore)).meta.changes,0);
 const afterRestore=await b.claimCoverLetterGeneration(db,'Nastya',1,await read());assert.ok(afterRestore);assert.notEqual(afterRestore.generation,beforeRestore.generation);
 await a.releaseCoverLetterGeneration(db,beforeRestore);assert.equal(await b.claimCoverLetterGeneration(db,'Nastya',1,await read()),null);
 assert.deepEqual((await a.createBackup(db)).storage,backup.storage,'Operational ownership does not change user backup');
 const injected=structuredClone(backup),table=schema.operationalTables.find(t=>t.name==='cover_letter_generation_leases');
 injected.storage.tables.push({name:table.name,columns:table.columns,rows:[]});injected.manifest.push({name:table.name,columns:table.columns.map(c=>c.name),rows:0});injected.integrity.digest=await a.backupDigest(injected);
 await assert.rejects(()=>a.restoreBackup(db,injected),/неизвестные разделы/);
 // Shared manual letter also prevents ownership; even with an existing expired lease.
 await a.releaseCoverLetterGeneration(db,afterRestore);await db.prepare("UPDATE company_vacancies SET cover_letter_subject='Shared',cover_letter_md='Manual shared' WHERE id=1").run();assert.equal(await a.claimCoverLetterGeneration(db,'Nastya',1,await read()),null);
 // Deletion invalidates renewal; lease rows have no restore-blocking FK.
 const deleteInput=await read('Nastya',2);assert.equal(await b.renewCoverLetterGeneration(db,otherVacancy,deleteInput),false);
 await db.prepare('DELETE FROM company_vacancies WHERE id=2').run();assert.equal(await b.renewCoverLetterGeneration(db,otherVacancy,deleteInput),false);
 await assert.rejects(async()=>a.claimCoverLetterGeneration(db,'Nastya',3,await read('Nastya',3),{ttlMs:0}),/Invalid generation lease duration/);
 console.log('Native D1 S06 storage: 12-way claim one owner, scoped inputs, renew/release fencing, expiry/takeover, execution-time CAS, saved letters, restore generation and backup24/28 application-table scope passed. HTTP integration has its own deduplication suite.');
}finally{await mf.dispose();}
