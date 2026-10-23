import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?vacancy-roles'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);



try{
 await request('/api/classification/settings');
 const root=sqlite.prepare("SELECT id FROM vacancy_roles WHERE stable_key='root-it-manager'").get().id;
 const now='2026-10-13T18:54:00.000Z',hash='a'.repeat(64);
 sqlite.prepare("INSERT INTO company_identity(id,company_name) VALUES(9876,'Isolated candidate employer')").run();
 sqlite.prepare(`INSERT INTO vacancy_candidates(id,creation_token,company_id,company_name_raw,title_raw,title_hash,canonical_url,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(1,?,9876,'Original employer','Senior Engineering Manager',?,'https://fixture.invalid/job/1','manual',?,?,?,?)`).run(crypto.randomUUID(),hash,now,now,now,now);
 sqlite.prepare("INSERT INTO vacancy_candidate_identities(identity_key,candidate_id) VALUES('url:fixture',1)").run();
 assert.throws(()=>sqlite.exec("INSERT INTO vacancy_candidate_identities VALUES('url:fixture',1)"),/UNIQUE/);
 const attempt=crypto.randomUUID();
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,outcome,classified_at,cost_usd) VALUES(?,1,'Senior Engineering Manager',?,1,'typesafe/jev-1.13','typesafe/jev-1.13-20260917','[]','complete',?,.00002)").run(attempt,hash,now);
 const decision=sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(1,?,?,'IT Manager',?,'typesafe/jev-1.13','typesafe/jev-1.13-20260917',?,1,?,1,?)");
 assert.throws(()=>decision.run(attempt,root,'uncertain',hash,hash,now),/CHECK/);decision.run(attempt,root,'match',hash,hash,now);
 assert.throws(()=>sqlite.prepare('DELETE FROM vacancy_roles WHERE id=?').run(root),/FOREIGN KEY/);
 sqlite.prepare('UPDATE vacancy_roles SET name=? WHERE id=?').run('Renamed root',root);assert.equal(sqlite.prepare('SELECT root_role_id FROM vacancy_classifications').get().root_role_id,root);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0,'staging does not create main vacancy');
 const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.ok(!backup.storage.tables.some(t=>t.name==='vacancy_candidate_classification_leases'));
 assert.equal(backup.storage.tables.find(t=>t.name==='vacancy_classifications').rows.length,1);
 sqlite.exec('DELETE FROM vacancy_classifications; DELETE FROM vacancy_classification_attempts; DELETE FROM vacancy_candidate_identities; DELETE FROM vacancy_candidates');
 await api.restoreBackup(db,backup);assert.equal(sqlite.prepare('SELECT title_raw FROM vacancy_candidates').get().title_raw,'Senior Engineering Manager');assert.equal(sqlite.prepare('SELECT decision FROM vacancy_classifications').get().decision,'match');assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 const legacy=structuredClone(backup);legacy.version=28;legacy.storage.tables=legacy.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));legacy.manifest=legacy.manifest.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));legacy.storage.tables=legacy.storage.tables.filter(t=>!['apify_settings','vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations','vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));legacy.manifest=legacy.manifest.filter(t=>!['apify_settings','vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations','vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));legacy.integrity.digest=await api.backupDigest(legacy);
 const restored=await api.restoreBackup(db,legacy);assert.ok(restored.warnings.some(w=>w.includes('промежуточных вакансий')));assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,0);
 console.log('Candidate schema: isolated primary storage, identity/decision constraints, role history, FK safety, complete backup30 and explicit v28 initialization passed');
}finally{sqlite.close();}
