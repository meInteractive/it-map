// Unchanged legacy generationBoundDb on SQLite :memory:, zero providers/files.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
globalThis.fetch=()=>{throw new Error('Generation oracle forbids network');};
const sqlite=new DatabaseSync(':memory:');
sqlite.exec("CREATE TABLE app_runtime_generation(id INTEGER PRIMARY KEY,generation TEXT);INSERT INTO app_runtime_generation VALUES(1,'CI original');CREATE TABLE ci_result(id INTEGER PRIMARY KEY,value TEXT CHECK(value<>'CI reject'));INSERT INTO ci_result VALUES(1,'CI initial');");
const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(){return sqlite.prepare(sql).get(...args)??null;},async run(){const s=sqlite.prepare(sql);if(s.columns().length)return {results:s.all(...args)};s.run(...args);return {success:true};}};},async batch(statements){sqlite.exec('BEGIN');try{const results=[];for(const s of statements)results.push(await s.run());sqlite.exec('COMMIT');return results;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
const value=()=>sqlite.prepare('SELECT value FROM ci_result WHERE id=1').get().value;
const original=generationBoundDb(db,'CI original');
await original.prepare("UPDATE ci_result SET value='CI before restore'").run();assert.equal(value(),'CI before restore');
sqlite.exec("UPDATE app_runtime_generation SET generation='CI restored';UPDATE ci_result SET value='CI restored value'");
await assert.rejects(original.prepare("UPDATE ci_result SET value='CI stale result'").run(),e=>e.status===409&&e.code==='restored');assert.equal(value(),'CI restored value');
const fresh=generationBoundDb(db,'CI restored');await fresh.prepare("UPDATE ci_result SET value='CI fresh result'").run();assert.equal(value(),'CI fresh result');
await assert.rejects(fresh.batch([fresh.prepare("UPDATE ci_result SET value='CI partial'"),fresh.prepare("UPDATE ci_result SET value='CI reject'")]));assert.equal(value(),'CI fresh result');
sqlite.close();console.log('PASS: 4 unchanged legacy generation transaction cases; memory only');
