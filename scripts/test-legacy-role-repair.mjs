import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{db,sqlite,api}=f;
const {ensureVacancyRoleSchema}=await moduleFor('server/repositories/schema.js');
const {repairLegacyVacancyRoles,legacyVacancyRoleIds}=await moduleFor('server/repositories/legacy-vacancy-roles.js');
try {
 await ensureVacancyRoleSchema(db);
 sqlite.prepare("INSERT INTO company_crm(company_name) VALUES('Role repair fixture')").run();
 const catalog=()=>sqlite.prepare('SELECT id,stable_key,name,parent_id FROM vacancy_roles').all();
 const id=key=>catalog().find(r=>r.stable_key===key).id;
 const root=Number(sqlite.prepare("INSERT INTO vacancy_roles(stable_key,name,parent_id) VALUES('root-preserved','Сохранённые роли',NULL)").run().lastInsertRowid);
 const pairs=[['Менеджер продуктов; Руководитель разработки',['it-product-manager','it-development-manager']],['Менеджер проектов; Менеджер продуктов',['it-project-manager','it-product-manager']],['Руководитель разработки, Директор по разработке',['it-development-manager','it-development-director']]];
 const vacancies=[];
 for (const [index,[name,targets]] of pairs.entries()) {
  const roleId=Number(sqlite.prepare('INSERT INTO vacancy_roles(stable_key,name,parent_id) VALUES(?,?,?)').run('legacy-'+index,name,root).lastInsertRowid);
  const vacancyId=Number(sqlite.prepare("INSERT INTO company_vacancies(company_name,url,title,role) VALUES('Role repair fixture',?,?,?)").run('https://example.test/repair/'+index,'Keep title '+index,name).lastInsertRowid);
  sqlite.prepare('INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)').run(vacancyId,roleId);
  vacancies.push({vacancyId,roleId,targets:targets.map(id)});
 }
 const first=vacancies[0];
 sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(999,'role-fixture','Fixture',?,'https://example.test/source','hh','site','now','now','now','now')").run('a'.repeat(64));
 sqlite.prepare('INSERT INTO vacancy_parser_roles(vacancy_id,role_id,candidate_id) VALUES(?,?,999)').run(first.vacancyId,first.roleId);
 // Keep an unrelated, explicitly assigned role, and copy parser provenance.
 sqlite.prepare('INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)').run(first.vacancyId,id('journalist-editor'));
 sqlite.exec("CREATE TRIGGER reject_role_repair BEFORE DELETE ON vacancy_roles WHEN OLD.stable_key='legacy-0' BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END");
 await assert.rejects(()=>repairLegacyVacancyRoles(db),/synthetic rollback/);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_role_assignments WHERE role_id=?').get(first.roleId).n,1,'failed repair rolls back assignments');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_parser_roles WHERE role_id=?').get(first.roleId).n,1,'failed repair rolls back provenance');
 sqlite.exec('DROP TRIGGER reject_role_repair');
 await repairLegacyVacancyRoles(db);
 for(const [index,v] of vacancies.entries()) {
  const assigned=sqlite.prepare('SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id=? ORDER BY role_id').all(v.vacancyId).map(r=>r.role_id);
  assert.deepEqual(assigned,[...v.targets,...(index===0?[id('journalist-editor')]:[])].sort((a,b)=>a-b));
  assert.equal(sqlite.prepare('SELECT title FROM company_vacancies WHERE id=?').get(v.vacancyId).title,'Keep title '+index);
 }
 assert.deepEqual(sqlite.prepare('SELECT role_id FROM vacancy_parser_roles WHERE vacancy_id=? ORDER BY role_id').all(first.vacancyId).map(r=>r.role_id),first.targets.slice().sort((a,b)=>a-b));
 assert.ok(!catalog().some(r=>r.stable_key==='root-preserved'));
 await ensureVacancyRoleSchema(db);await ensureVacancyRoleSchema(db);
 assert.ok(!catalog().some(r=>r.stable_key==='root-preserved'),'cold bootstrap does not recreate multi-role leaves');
 const unassigned=Number(sqlite.prepare("INSERT INTO company_vacancies(company_name,url,title,role) VALUES('Role repair fixture','https://example.test/unassigned','Keep','Менеджер проектов; Менеджер продуктов')").run().lastInsertRowid);
 await ensureVacancyRoleSchema(db);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_role_assignments WHERE vacancy_id=?').get(unassigned).n,2,'unassigned legacy multi-selection creates both links');
 const backup=await api.createBackup(db);
 const legacy=structuredClone(backup);legacy.version=26;legacy.storage.tables=legacy.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));legacy.manifest=legacy.manifest.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));
 for(const name of ['vacancy_roles','vacancy_role_assignments','vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing','vacancy_detail_settings','vacancy_detail_jobs','vacancy_detail_leases','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions','vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations','apify_settings']) {
  legacy.storage.tables=legacy.storage.tables.filter(t=>t.name!==name);legacy.manifest=legacy.manifest.filter(t=>t.name!==name);
 }
 legacy.integrity.digest=await api.backupDigest(legacy);
 const plan=await api.prepareBackupRestore(db,legacy);
 assert.ok(!plan.tables.find(t=>t.name==='vacancy_roles').rows.some(r=>r.stable_key==='root-preserved'),'legacy backup splits standard combinations');
 assert.equal(plan.tables.find(t=>t.name==='vacancy_role_assignments').rows.filter(r=>r.vacancy_id===unassigned).length,2);
 assert.equal(legacyVacancyRoleIds('Менеджер продуктов; неизвестное',catalog(),{standardOnly:true}),null,'unknown component never silently discarded');
 assert.deepEqual(sqlite.prepare('PRAGMA foreign_key_check').all(),[]);
 console.log('Legacy role repair: exact standard splits, provenance, unrelated assignments, transaction rollback, repeat bootstrap, unassigned roles and old backup passed.');
} finally {f.close();}
