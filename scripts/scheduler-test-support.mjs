import {DatabaseSync} from 'node:sqlite';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
export async function schedulerFixture(){
 await prepareBackend();const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of buildBackupSchema(process.cwd()).objects)sqlite.exec(o.sql);
 let chain=Promise.resolve();const db={prepare(sql){let args=[];return {bind(...a){args=a;return this},async first(){return sqlite.prepare(sql).get(...args)||null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)}}}},batch(statements){const p=chain.then(async()=>{sqlite.exec('BEGIN');try{const r=[];for(const s of statements)r.push(await s.run());sqlite.exec('COMMIT');return r}catch(e){sqlite.exec('ROLLBACK');throw e}});chain=p.catch(()=>{});return p}};
 const api=await import('../.tests/backend.mjs'),env={DB:db,SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'scheduler-test-secret',SCHEDULER_TOKEN:'synthetic-scheduler-token'};
 await api.firecrawlSettings(db);return {api,db,env,sqlite,close:()=>sqlite.close()};
}
