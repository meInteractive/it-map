// Source v39 round-trip oracle. Only public synthetic rows, SQLite :memory:,
// and stdout; never an uploaded archive, filesystem DB, network or provider.
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import assert from 'node:assert/strict';
import {createBackup,restoreBackup} from '../server/backup/index.js';
globalThis.fetch=()=>{throw new Error('Restore oracle forbids network');};
const schema=JSON.parse(readFileSync(new URL('../server/generated/backup-schema.json',import.meta.url),'utf8'));
function instance(){
 const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
 for(const type of ['table','index','trigger'])for(const o of schema.objects)if(o.type===type)sqlite.exec(o.sql);
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async first(){return sqlite.prepare(sql).get(...args)??null;},async run(){const s=sqlite.prepare(sql);if(s.columns().length)return {results:s.all(...args)};const r=s.run(...args);return {success:true,meta:{changes:Number(r.changes)}};}};},async batch(statements){sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 return {sqlite,db};
}
const src=instance();
// Public seed originally in scripts/test-backup-roundtrip.mjs.
src.sqlite.exec(`
 INSERT INTO openrouter_settings(id,key_encrypted,key_suffix) VALUES(1,'SYNTHETIC-CIPHERTEXT','TEST');
 INSERT INTO openrouter_ai_logs(created_at,task,success) VALUES('2026-09-25','test',1);
 INSERT INTO openrouter_model_metrics(model_id,success_count,total_duration_ms,last_duration_ms,last_success_at) VALUES('test/model',2,125000,70000,'2026-10-01');
 INSERT INTO openai_settings(id,key_encrypted,key_suffix,revision) VALUES(1,'SYNTHETIC-CIPHERTEXT-OPENAI','TEST',2);
 INSERT INTO apify_settings(id) VALUES(1);
 INSERT INTO prompt_model_settings(kind,provider,openai_model,reasoning_effort,revision) VALUES('cover_letter','openai','gpt-6-luna','medium',3),('relevance','openrouter','','medium',1);
 INSERT INTO system_prompts(kind,markdown,revision) VALUES('cover_letter','# Synthetic letter',1),('relevance','## Synthetic relevance',2);
 INSERT INTO user_profiles(user_name,resume_md,revision) VALUES('Eugene','# Synthetic resume',1),('Nastya','Личное резюме',2);
 INSERT INTO company_crm(company_name,comment_md) VALUES('Synthetic company','Two months of notes: июль / август / сентябрь');
 INSERT INTO company_identity(id,company_name) VALUES(9,'Synthetic company');
 INSERT INTO agency_identity(id,agency_name) VALUES(7,'Synthetic agency');
 INSERT INTO company_vacancies(id,company_name,url,title,entity_type,role,description_md,archived,last_checked_at,cover_letter_subject,cover_letter_md) VALUES(1,'Synthetic company','https://example.invalid/job/1','Developer','company','Менеджер проектов','Full text: перенос строки\nкириллица',1,'2026-09-19','Subject','Cover letter'),(2,'Synthetic company','https://example.invalid/job/2','Recruiter','agency','Директор по разработке','Agency vacancy',0,'2026-09-18','','');
 INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order) VALUES
 (1,'root-it-manager','IT Manager',NULL,10),(2,'root-journalist','Журналист',NULL,20),
 (3,'it-project-manager','Менеджер проектов',1,10),(4,'it-product-manager','Менеджер продуктов',1,20),
 (5,'it-development-manager','Руководитель разработки',1,30),(6,'journalist-editor','Редактор',2,10),
 (7,'journalist-data','Дата-журналист',2,20),(8,'it-development-director','Директор по разработке',1,40);
 INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(1,3),(2,8);
 INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled) VALUES(1,'# Custom classification',4,0);
 INSERT INTO company_applications(id,company_name,vacancy_id,comment_md,application_sources) VALUES(1,'Synthetic company',1,'Application notes','["hh","telegram"]');
 INSERT INTO company_application_status_history(id,application_id,from_status,to_status) VALUES(1,1,'','Откликнулся');
 INSERT INTO company_status_history(id,company_name,to_status,user_name) VALUES(1,'Synthetic company','Откликнулся','Nastya');
 INSERT INTO company_tasks(id,company_name,vacancy_id,type,note,due_at) VALUES(1,'Synthetic company',1,'call','Task notes','2026-09-21');
 INSERT INTO company_user_state(user_name,company_name,comment_md,favorite,hidden) VALUES('Nastya','Synthetic company','Personal notes',1,1);
 INSERT INTO company_vacancy_state(company_name) VALUES('Synthetic company');
 INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite,hidden) VALUES('Nastya',1,83,1,1);
 INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES('Nastya',2,'Личная тема','Личное письмо');
 INSERT INTO google_calendar_connection(id,refresh_token_encrypted) VALUES(1,'SYNTHETIC-CIPHERTEXT-LEGACY');
 INSERT INTO google_calendar_user_connection(user_name,refresh_token_encrypted) VALUES('Nastya','SYNTHETIC-CIPHERTEXT');
 INSERT INTO hiring_contacts(id,company_name,position,full_name,email,specialization,setka_url) VALUES(1,'Synthetic company',1,'Test Person','test@example.invalid','Engineering','https://example.invalid/setka');
 INSERT INTO telegram_channels(id,name,url,normalized_username,comment,last_successful_check_at) VALUES(1,'Test channel','https://t.me/test_fixture','test_fixture','Channel notes','2026-09-19');
 INSERT INTO vacancy_telegram_sources(id,vacancy_id,channel_id,message_url,telegram_contact) VALUES(1,1,1,'https://t.me/test_fixture/123','@fixture');
 INSERT INTO vacancy_employer_guesses(id,agency_vacancy_id,position,employer_name,linked_company_name,matched_vacancy_id) VALUES(1,2,1,'Synthetic company','Synthetic company',1);
 INSERT INTO vacancy_relevance_profile_state(user_name,profile_version) VALUES('Nastya','2026-09');
 INSERT INTO vacancy_audit_runs(run_id,deleted_count,publication_date_update_count,completed_at) VALUES('audit-july',3,4,'2026-07-22'),('audit-september',0,2,'2026-09-19');
 UPDATE sqlite_sequence SET seq=999999 WHERE name='company_vacancies';
 CREATE TABLE d1_migrations(id INTEGER PRIMARY KEY,name TEXT);
 INSERT INTO d1_migrations VALUES(1,'fixture_migration');
 CREATE TABLE __drizzle_migrations(id INTEGER PRIMARY KEY,hash TEXT,created_at INTEGER);
 INSERT INTO __drizzle_migrations VALUES(1,'fixture-hash',1789900000000);
 CREATE TABLE __appgarden_migrations(id INTEGER PRIMARY KEY,name TEXT);
 INSERT INTO __appgarden_migrations VALUES(1,'source-hosting-migration');
`);
const catalog={companies:[{name:'Synthetic company',vacancySources:{companySite:'https://example.invalid/jobs'}}],agencies:[]};
src.sqlite.prepare('INSERT INTO app_catalog(id,data_json,updated_at) VALUES(1,?,?)').run(JSON.stringify(catalog,null,2),'CI original catalog date');
src.sqlite.exec(`INSERT INTO agency_identity(id,agency_name) VALUES(0,'CI zero-ID agency');
INSERT INTO firecrawl_settings(id,enabled,key_encrypted,scheduler_json) VALUES(1,1,'SYNTHETIC-CIPHERTEXT','{"enabled":true,"cloudOwnerId":"CI old owner","ownerName":"Nastya","sourceIds":[]}');
UPDATE apify_settings SET enabled=1,paid_allowed=1,free_account_json='{"CI":true}',free_error='CI stale';
INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(11,'CI candidate','CI title','${'a'.repeat(64)}','manual','CI first','CI last','CI created','CI updated');
INSERT INTO vacancy_candidate_identities(identity_key,candidate_id) VALUES('CI identity',11);`);
src.sqlite.prepare("UPDATE user_profiles SET resume_md=? WHERE user_name='Eugene'").run('  CI 😀 кириллица \u2028 \0 end  ');
const payload=await createBackup(src.db),body=JSON.stringify(payload),dst=instance();
const reply=await restoreBackup(dst.db,payload),expected=await createBackup(dst.db);
assert.equal(JSON.stringify(payload),body,'Source mutated input');
assert.equal(expected.storage.tables.length,52);
assert.ok(expected.storage.tables.filter(t=>t.rows.length).length>=33);
assert.equal(JSON.parse(expected.storage.tables.find(t=>t.name==='firecrawl_settings').rows[0].scheduler_json).enabled,false);
if(process.argv.includes('--stdio'))process.stdout.write(JSON.stringify({body,expected:expected.storage.tables,warnings:reply.warnings,rows:reply.rows}));
else console.log(`PASS: unchanged source restore/export round-trip, ${reply.rows} synthetic rows, 52 tables; memory only`);
src.sqlite.close();dst.sqlite.close();
