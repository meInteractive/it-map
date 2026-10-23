import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,sqlite}=f;
const {upsertVacancyCandidate}=await moduleFor('server/repositories/vacancy-candidates.js');
const {parserFunnel}=await moduleFor('server/services/parser-funnel.js');
const {recordParserErrorSnapshot,readParserErrorHistory}=await moduleFor('server/repositories/parser-error-history.js');
const now=Date.parse('2026-10-23T09:00:00Z'),oldFetch=globalThis.fetch;
globalThis.fetch=async()=>{throw Error('Restore QA must not call providers');};
try{
 sqlite.prepare('UPDATE firecrawl_settings SET scheduler_json=? WHERE id=1').run(JSON.stringify(api.SCHEDULER_DEFAULTS));
 sqlite.exec("INSERT INTO company_identity(id,company_name) VALUES(991,'Backup fixture'); INSERT INTO company_crm(company_name) VALUES('Backup fixture')");
 for(const [n,parser] of [[1,'apify'],[2,'firecrawl'],[3,'http']]){
  const c=await upsertVacancyCandidate(db,{companyId:991,titleRaw:'Backup fixture '+n,vacancyUrl:'https://backup.fixture/'+n,sourceId:'hh',acquisitionMethod:'site'},{intakeParser:parser});
  sqlite.prepare('UPDATE vacancy_candidates SET first_seen_at=?,last_seen_at=? WHERE id=?').run(new Date(now).toISOString(),new Date(now).toISOString(),c.id);
  sqlite.prepare('UPDATE parser_candidate_intakes SET received_at=? WHERE candidate_token=(SELECT creation_token FROM vacancy_candidates WHERE id=?)').run(new Date(now).toISOString(),c.id);
 }
 for(let d=0;d<30;d++)await recordParserErrorSnapshot(db,'Eugene',{p1:d%3,p2:d},now-d*86400000);
 const history=await readParserErrorHistory(db,'Eugene',now);
 const filters=[{}, {sourceId:'hh',parser:'apify'},{parser:'firecrawl'},{parser:'http'}];
 const reports=await Promise.all(filters.map(filter=>parserFunnel(db,{now,...filter})));
 const backup=await api.createBackup(db);assert.equal(backup.version,39);
 for(const name of ['parser_error_daily','parser_candidate_intakes'])assert.ok(backup.storage.tables.find(t=>t.name===name).rows.length);
 // Restore into a genuinely empty DB, without source operational state.
 const target=await schedulerFixture();
 try{
  target.sqlite.exec('PRAGMA foreign_keys=OFF');
  for(const o of [...(await import('./backup-schema.mjs')).buildBackupSchema(process.cwd()).objects].filter(o=>o.type==='table').reverse())target.sqlite.exec(`DROP TABLE IF EXISTS "${o.name}"`);
  target.sqlite.exec('PRAGMA foreign_keys=ON');
  await target.api.restoreBackup(target.db,backup);
  assert.deepEqual((await target.api.createBackup(target.db)).storage,backup.storage);
  assert.deepEqual(await readParserErrorHistory(target.db,'Eugene',now),history);
  for(let i=0;i<filters.length;i++)assert.deepEqual(await parserFunnel(target.db,{now,...filters[i]}),reports[i]);
 }finally{target.close();}
 for(const name of ['parser_error_daily','parser_candidate_intakes']){
  const incomplete=structuredClone(backup);incomplete.storage.tables=incomplete.storage.tables.filter(t=>t.name!==name);incomplete.manifest=incomplete.manifest.filter(t=>t.name!==name);incomplete.integrity.digest=await api.backupDigest(incomplete);
  await assert.rejects(()=>api.restoreBackup(db,incomplete),/отсутствуют таблицы/);
 }
 const legacy=structuredClone(backup);legacy.version=38;legacy.storage.tables=legacy.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));legacy.manifest=legacy.manifest.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));legacy.integrity.digest=await api.backupDigest(legacy);
 const token=sqlite.prepare('SELECT creation_token FROM vacancy_candidates ORDER BY id LIMIT 1').get().creation_token;
 sqlite.prepare('INSERT INTO parser_candidate_intakes VALUES(?,?,?,?)').run('unrelated-token','apify','hh',new Date(now).toISOString());
 const result=await api.restoreBackup(db,legacy);assert.ok(result.warnings.some(w=>w.includes('нет истории')));
 assert.equal(sqlite.prepare('SELECT count(*) n FROM parser_candidate_intakes').get().n,3);
 assert.ok(sqlite.prepare('SELECT 1 FROM parser_candidate_intakes WHERE candidate_token=?').get(token));
 assert.deepEqual(await readParserErrorHistory(db,'Eugene',now),history);
 const broken=structuredClone(backup);broken.storage.tables.find(t=>t.name==='parser_error_daily').rows[0].p1_peak=-1;broken.integrity.digest=await api.backupDigest(broken);
 const before=await api.createBackup(db),generation=sqlite.prepare('SELECT generation FROM app_runtime_generation').get();
 await assert.rejects(()=>api.restoreBackup(db,broken),/CHECK constraint/);
 assert.deepEqual((await api.createBackup(db)).storage,before.storage);assert.deepEqual(sqlite.prepare('SELECT generation FROM app_runtime_generation').get(),generation);
 console.log('Backup39: empty-target exact roundtrip, 30-day daily history/averages, source+parser funnel equality, mandatory sections, legacy38 token-safe preservation/warning and history-constraint atomic rollback passed without providers.');
}finally{globalThis.fetch=oldFetch;f.close();}
