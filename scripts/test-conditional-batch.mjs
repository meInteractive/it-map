import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {prepareBackend,moduleFor} from './test-support.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();
const {batchWithRequiredUpdate}=await moduleFor('server/repositories/conditional-batch.js');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'conditional-batch-synthetic'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try {
 const db=await mf.getD1Database('DB');
 await db.batch([
  db.prepare('CREATE TABLE guard_records(id INTEGER PRIMARY KEY, version INTEGER NOT NULL, value TEXT NOT NULL)'),
  db.prepare('CREATE TABLE guard_dependents(id INTEGER PRIMARY KEY, owner INTEGER REFERENCES guard_records(id) ON DELETE CASCADE, value TEXT NOT NULL)'),
  db.prepare("INSERT INTO guard_records VALUES(1,1,'Initial'),(2,1,'Second')"),
 ]);
 const snapshot=async()=>Promise.all(['guard_records','guard_dependents'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY id').all()).results));
 const update=(id,version,value)=>db.prepare('UPDATE guard_records SET value=?,version=version+1 WHERE id=? AND version=?').bind(value,id,version);
 const dependent=(value)=>db.prepare('INSERT INTO guard_dependents(owner,value) VALUES(1,?)').bind(value);
 // Matching CAS commits all statements and retains the caller's result indexes.
 const result=await batchWithRequiredUpdate(db,update(1,1,'Winner'),[dependent('Committed')]);
 assert.equal(result.length,2);assert.equal(result[0].meta.changes,1);assert.equal(result[1].meta.changes,1);
 const afterWinner=await snapshot();
 // Zero matched rows would silently run following writes in an ordinary batch.
 await assert.rejects(()=>batchWithRequiredUpdate(db,update(1,1,'Stale'),[dependent('Must not exist')]),/integer overflow/);
 assert.deepEqual(await snapshot(),afterWinner);
 // A concurrently deleted row also cannot produce detached side effects.
 await assert.rejects(()=>batchWithRequiredUpdate(db,update(99,1,'Missing'),[dependent('Detached')]),/integer overflow/);
 assert.deepEqual(await snapshot(),afterWinner);
 // More than one changed row violates the helper contract and rolls back UPDATE.
 await assert.rejects(()=>batchWithRequiredUpdate(db,db.prepare("UPDATE guard_records SET value='Multiple'"),[dependent('Multiple')]),/integer overflow/);
 assert.deepEqual(await snapshot(),afterWinner);
 // AFTER-trigger writes do not replace the direct UPDATE's changes() count.
 await db.prepare("CREATE TRIGGER guard_after AFTER UPDATE ON guard_records BEGIN INSERT INTO guard_dependents(owner,value) VALUES(NEW.id,'Triggered'); END").run();
 await batchWithRequiredUpdate(db,update(1,2,'With trigger'),[dependent('After guard')]);
 let current=await snapshot();assert.equal(current[0].find(r=>r.id===1).version,3);assert.equal(current[1].filter(r=>r.value==='Triggered').length,1);
 // A later failure still rolls back matching UPDATE, triggers and earlier writes.
 const beforeFailure=await snapshot();
 await assert.rejects(()=>batchWithRequiredUpdate(db,update(1,3,'Will roll back'),[dependent('Earlier'),db.prepare('INSERT INTO guard_dependents(owner,value) VALUES(1,NULL)')]),/NOT NULL/);
 assert.deepEqual(await snapshot(),beforeFailure);
 // Even an integer overflow AFTER the guard keeps its original error; no false
 // conflict status is assigned by the helper.
 await assert.rejects(()=>batchWithRequiredUpdate(db,update(1,3,'Overflow later'),[db.prepare('SELECT abs(-9223372036854775808)')]),error=>/integer overflow/.test(error.message)&&error.status===undefined);
 assert.deepEqual(await snapshot(),beforeFailure);
 // No-op UPDATE of one matching row is valid; no timestamps are used as tokens.
 await batchWithRequiredUpdate(db,db.prepare('UPDATE guard_records SET value=value WHERE id=1'),[]);
 assert.deepEqual(await snapshot(),beforeFailure.map((rows,index)=>index===1?[...rows,{id:rows.at(-1).id+1,owner:1,value:'Triggered'}]:rows));
 // Concurrent clients with one version: only one batch and its dependents commit.
 await db.prepare('DROP TRIGGER guard_after').run();const beforeRace=await snapshot();
 const race=await Promise.allSettled(['A','B'].map(value=>batchWithRequiredUpdate(db,update(1,3,value),[dependent(value)])));
 assert.equal(race.filter(r=>r.status==='fulfilled').length,1);assert.equal(race.filter(r=>r.status==='rejected').length,1);
 current=await snapshot();assert.equal(current[0].find(r=>r.id===1).version,4);assert.equal(current[1].length,beforeRace[1].length+1);
 assert.equal(current[1].at(-1).value,current[0].find(r=>r.id===1).value);
 // No guard table/row/schema migration is left behind by the assertion.
 const tables=(await db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name LIKE 'guard_%' ORDER BY name").all()).results.map(row=>row.name);
 assert.deepEqual(tables,['guard_dependents','guard_records']);
 console.log('Real D1: required single-row UPDATE, stale/deleted/multi-row rollback, downstream failure, trigger/no-op semantics, unclassified errors and parallel dependent writes passed. Helper is not yet wired into vacancy PUT.');
} finally {await mf.dispose();}
