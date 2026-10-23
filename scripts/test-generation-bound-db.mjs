import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'generation-bound'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB'),epoch=await api.ensureRuntimeGeneration(db);
 await db.prepare('CREATE TABLE synthetic_counter(id INTEGER PRIMARY KEY,value INTEGER NOT NULL)').run();
 const bound=api.generationBoundDb(db,epoch);
 assert.equal((await bound.prepare('INSERT INTO synthetic_counter VALUES(1,0)').run()).meta.changes,1);
 assert.deepEqual(await bound.prepare('SELECT * FROM synthetic_counter WHERE id=?').bind(1).first(),{id:1,value:0});
 assert.equal(await bound.prepare('SELECT value FROM synthetic_counter WHERE id=1').first('value'),0);
 assert.equal(await bound.prepare('SELECT * FROM synthetic_counter WHERE id=9').first(),null);
 const results=await bound.batch([bound.prepare('UPDATE synthetic_counter SET value=value+1 WHERE id=1'),bound.prepare('SELECT * FROM synthetic_counter')]);assert.equal(results.length,2);assert.equal(results[0].meta.changes,1);assert.deepEqual(results[1].results,[{id:1,value:1}]);
 const delayed=bound.prepare('UPDATE synthetic_counter SET value=99 WHERE id=1');await db.batch([api.rotateRuntimeGenerationStatement(db)]);
 const restored=error=>error.status===409&&error.code==='restored';
 for(const request of [()=>delayed.run(),()=>bound.prepare('SELECT * FROM synthetic_counter').first(),()=>bound.prepare('SELECT * FROM synthetic_counter').all(),()=>bound.prepare('UPDATE synthetic_counter SET value=99 RETURNING *').first(),()=>bound.batch([bound.prepare('INSERT INTO synthetic_counter VALUES(2,2)'),delayed])])await assert.rejects(request,restored);
 assert.deepEqual((await db.prepare('SELECT * FROM synthetic_counter').all()).results,[{id:1,value:1}]);
 // Force rotation right at batch execution, after preparing a valid statement.
 let hits=0;const current=await api.ensureRuntimeGeneration(db);
 const racing={prepare:db.prepare.bind(db),async batch(statements){hits++;await db.batch([api.rotateRuntimeGenerationStatement(db)]);return db.batch(statements);}};
 await assert.rejects(()=>api.generationBoundDb(racing,current).prepare('UPDATE synthetic_counter SET value=77').run(),restored);assert.equal(hits,1);assert.equal((await db.prepare('SELECT value FROM synthetic_counter').first()).value,1);
 // Genuine later SQL overflow is not a fake restore; earlier writes roll back.
 const fresh=api.generationBoundDb(db,await api.ensureRuntimeGeneration(db));
 await assert.rejects(()=>fresh.batch([fresh.prepare('UPDATE synthetic_counter SET value=10'),fresh.prepare('SELECT abs(-9223372036854775808)')]),error=>error.code!=='restored'&&/overflow/.test(error.message));assert.equal((await db.prepare('SELECT value FROM synthetic_counter').first()).value,1);
 await assert.rejects(()=>fresh.batch([db.prepare('SELECT 1')]),/другому подключению/);
 const other=api.generationBoundDb(db,await api.ensureRuntimeGeneration(db));await Promise.all([fresh.prepare('UPDATE synthetic_counter SET value=value+1').run(),other.prepare('UPDATE synthetic_counter SET value=value+1').run()]);assert.equal((await db.prepare('SELECT value FROM synthetic_counter').first()).value,3);
 await db.prepare('DELETE FROM app_runtime_generation').run();await assert.rejects(()=>fresh.prepare('UPDATE synthetic_counter SET value=99').run(),restored);assert.equal((await db.prepare('SELECT value FROM synthetic_counter').first()).value,3);
 console.log('Native D1 generation facade: first/column/null/all/run/batch results, prepared-before-restore operations, forced execution-time rotation, whole-batch rollback, genuine overflow, mixed statements, independent facades and missing generation passed.');
}finally{await mf.dispose();}
