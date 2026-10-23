import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { buildBackupSchema } from './backup-schema.mjs';
import { createHash } from 'node:crypto';

const schema = buildBackupSchema(process.cwd());
const databases = [];
let serial = 0;
const sign = payload => {const added=['vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations'],details=['vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions'];const omit=[...(payload.version<39?['parser_error_daily','parser_candidate_intakes']:[]),...(payload.version<36?['apify_settings']:[]),...(payload.version<33?added:[]),...(payload.version<32?details:[])];payload.storage.tables=payload.storage.tables.filter(t=>!omit.includes(t.name));payload.manifest=payload.manifest.filter(t=>!omit.includes(t.name));if(payload.version<38){for(const t of payload.storage.tables.filter(t=>['vacancy_candidates','vacancy_source_parsing'].includes(t.name))){t.columns=t.columns.filter(c=>c.name!=='agency_id');for(const r of t.rows)delete r.agency_id;const m=payload.manifest.find(m=>m.name===t.name);if(m)m.columns=m.columns.filter(c=>c!=='agency_id');}}if(payload.version<37){const t=payload.storage.tables.find(t=>t.name==='vacancy_role_settings');if(t){t.columns=t.columns.filter(c=>c.name!=='openrouter_model');for(const r of t.rows)delete r.openrouter_model;const m=payload.manifest.find(m=>m.name===t.name);if(m)m.columns=m.columns.filter(c=>c!=='openrouter_model');}}if(payload.version<34){const t=payload.storage.tables.find(t=>t.name==='firecrawl_settings');if(t){t.columns=t.columns.filter(c=>c.name!=='favorites_only');for(const r of t.rows)delete r.favorites_only;const m=payload.manifest.find(m=>m.name===t.name);if(m)m.columns=m.columns.filter(c=>c!=='favorites_only');}}if(payload.version<35){for(const t of payload.storage.tables){const added=t.name==='firecrawl_settings'?['scheduler_json']:t.name==='vacancy_source_parsing'?['auto_enabled','interval_days','next_run_at','last_attempt_at','last_result']:[];t.columns=t.columns.filter(c=>!added.includes(c.name));for(const r of t.rows)for(const name of added)delete r[name];const m=payload.manifest.find(m=>m.name===t.name);if(m)m.columns=m.columns.filter(c=>!added.includes(c));}}const {integrity,...contents}=payload;payload.integrity={algorithm:'SHA-256',digest:createHash('sha256').update(JSON.stringify(contents)).digest('hex')};return payload;};
const canonical = value => Array.isArray(value) ? value.map(canonical) : value && typeof value === 'object' ? Object.fromEntries(Object.keys(value).sort().map(key=>[key,canonical(value[key])])) : value;
const records = backup => canonical(backup.storage.tables.map(t=>({name:t.name,rows:t.rows.map(row=>t.name==='vacancy_roles'?(({created_at,updated_at,...stable})=>stable)(row):row).map(canonical).sort((a,b)=>JSON.stringify(a).localeCompare(JSON.stringify(b)))})).sort((a,b)=>a.name.localeCompare(b.name)));
const stripRoleTables = backup => {backup.storage.tables=backup.storage.tables.filter(t=>!['vacancy_roles','vacancy_role_assignments','vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));backup.manifest=backup.manifest.filter(t=>!['vacancy_roles','vacancy_role_assignments','vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));return backup;};
async function instance() {
  const sqlite = new DatabaseSync(':memory:'); databases.push(sqlite); sqlite.exec('PRAGMA foreign_keys=ON');
  let writes = 0;
  const db = {
    prepare(sql) { let params=[]; return {
      bind(...values){params=values;return this;},
      async all(){return {results:sqlite.prepare(sql).all(...params)};},
      async first(){return sqlite.prepare(sql).get(...params) ?? null;},
      async run(){const p=sqlite.prepare(sql);if(p.columns().length)return {success:true,results:p.all(...params)};writes++;const r=p.run(...params);return {success:true,meta:{changes:Number(r.changes),last_row_id:Number(r.lastInsertRowid)}};},
    };},
    async batch(statements){sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results;}catch(e){sqlite.exec('ROLLBACK');throw e;}}
  };
  const worker=(await import(new URL(`../.tests/backend.mjs?backup=${serial++}`,import.meta.url))).default;
  const env={DB:db,SITE_PASSWORD:'local-test',NASTYA_PASSWORD:'local-test',SESSION_SECRET:'local-test-secret'};
  async function login(name){const r=await worker.fetch(new Request('https://test.invalid/auth',{method:'POST',body:new URLSearchParams({login:name,password:'local-test'})}),env);assert.equal(r.status,303);return r.headers.get('set-cookie').split(';')[0];}
  const cookie=await login('Eugene');
  const req=(payload,path='/api/backup',selectedCookie=cookie)=>worker.fetch(new Request('https://test.invalid'+path,{method:payload?'POST':'GET',headers:{cookie:selectedCookie,'content-type':'application/json'},body:payload?JSON.stringify(payload):undefined}),env);
  const backup=async()=>{const r=await req();const b=await r.json();assert.equal(r.status,200,JSON.stringify(b));return b;};
  const restore=async b=>{const r=await req(b);const body=await r.json();assert.equal(r.status,200,JSON.stringify(body));return body;};
  const dump=()=>JSON.stringify(sqlite.prepare("SELECT name,sql FROM sqlite_schema ORDER BY name").all().map(o=>({ ...o,rows:o.sql?.startsWith('CREATE TABLE')?sqlite.prepare(`SELECT * FROM "${o.name}"`).all():null})));
  return {sqlite,worker,env,req,backup,restore,dump,login,get writes(){return writes;}};
}
try {
 const src=await instance();
 const empty=await src.backup();assert.equal(src.writes,0,'Export must not bootstrap or migrate the source');assert.equal(empty.storage.tables.length,52);
 await src.restore(empty);
 const catalog=structuredClone(empty.catalog);catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];
 catalog.companies[0].vacancySources={...catalog.companies[0].vacancySources,dreamJob:'https://dreamjob.ru/employers/fixture/vakansii',hCareers:'https://h.careers/companies/fixture',rabota:'https://rabota.ru/company/fixture/vacancies'};
 src.sqlite.prepare('INSERT INTO app_catalog(id,data_json,updated_at) VALUES(1,?,?)').run(JSON.stringify(catalog,null,2),'2026-07-21 12:34:56');
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
 src.sqlite.prepare("UPDATE system_prompts SET markdown=? WHERE kind='cover_letter'").run('😀'.repeat(15000));
 const writes=src.writes,before=src.dump();const exported=await src.backup();
 assert.equal(exported.version,39);
 assert.equal(exported.catalog.companies[0].vacancySources.dreamJob,'https://dreamjob.ru/employers/fixture/vakansii');
 assert.equal(exported.catalog.companies[0].vacancySources.hCareers,'https://h.careers/companies/fixture');
 assert.equal(src.dump(),before);assert.equal(src.writes,writes);
 assert.ok(exported.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes','vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations','vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name)).every(t=>t.rows.length),'Every application table is populated in the fixture');
 assert.equal(exported.metadata.migrations[0].rows[0].name,'fixture_migration');
 assert.equal(exported.metadata.migrations.find(t=>t.name==='__appgarden_migrations').rows[0].name,'source-hosting-migration');
 assert.equal(exported.metadata.migrations.find(t=>t.name==='__drizzle_migrations').rows[0].hash,'fixture-hash');
 assert.equal(exported.storage.tables.length,52,'Hosting journals are metadata, not application tables');
 if (process.argv[2]) fs.writeFileSync(process.argv[2],JSON.stringify(exported));
 const dst=await instance();assert.equal((await dst.req(exported,'/api/backup/validate')).status,200);assert.equal(dst.writes,0);
 dst.sqlite.exec("CREATE TABLE __appgarden_migrations(id INTEGER PRIMARY KEY,name TEXT); INSERT INTO __appgarden_migrations VALUES(2,'destination-hosting-migration');");
 assert.equal((await dst.req(exported,'/api/backup/validate')).status,200);
 await dst.restore(exported);const restored=await dst.backup();assert.equal(restored.catalog.companies[0].vacancySources.rabota,catalog.companies[0].vacancySources.rabota);assert.deepEqual(records(restored),records(exported));assert.deepEqual(restored.metadata.sequences,exported.metadata.sequences);
 assert.equal(restored.catalog.companies[0].vacancySources.dreamJob,exported.catalog.companies[0].vacancySources.dreamJob);
 assert.equal(restored.catalog.companies[0].vacancySources.hCareers,exported.catalog.companies[0].vacancySources.hCareers);
 assert.deepEqual(restored.metadata.migrations.find(t=>t.name==='__appgarden_migrations').rows,[{id:2,name:'destination-hosting-migration'}],'Restore must preserve the destination hosting journal');
 assert.equal(dst.sqlite.prepare("SELECT COUNT(*) n FROM sqlite_schema WHERE name IN ('d1_migrations','__drizzle_migrations')").get().n,0,'Source migration journals must not be replayed');
 assert.equal(dst.sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 await dst.restore(restored);assert.deepEqual(records(await dst.backup()),records(exported));
 const regenerated=structuredClone(exported);const regeneratedRow=regenerated.storage.tables.find(t=>t.name==='vacancy_cover_letters').rows[0];regeneratedRow.override_shared=1;regeneratedRow.revision=7;sign(regenerated);await dst.restore(regenerated);assert.deepEqual(records(await dst.backup()),records(regenerated),'Explicit override/revision survive full backup');await dst.restore(exported);
 const previous37=structuredClone(exported);previous37.version=37;sign(previous37);await dst.restore(previous37);assert.equal(dst.sqlite.prepare('SELECT count(*) n FROM vacancy_source_parsing WHERE agency_id IS NOT NULL').get().n,0);assert.equal(JSON.parse(dst.sqlite.prepare('SELECT scheduler_json FROM firecrawl_settings').get()?.scheduler_json||'{}').agenciesEnabled??false,false);await dst.restore(exported);
 const previous27=structuredClone(exported);previous27.version=27;previous27.storage.tables=previous27.storage.tables.filter(t=>!['vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));previous27.manifest=previous27.manifest.filter(t=>!['vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));sign(previous27);
 await dst.restore(previous27);assert.deepEqual(records(await dst.backup()),records(exported),'v27 preserves settings by stable identity');
 const changedIdentity=structuredClone(previous27);changedIdentity.storage.tables.find(t=>t.name==='vacancy_roles').rows.find(r=>r.id===1).stable_key='replacement-root';sign(changedIdentity);
 await dst.restore(changedIdentity);assert.equal(dst.sqlite.prepare('SELECT count(*) n FROM vacancy_role_classification').get().n,0,'reused numeric ID never inherits another role prompt');await dst.restore(exported);
 const previous25=stripRoleTables(structuredClone(exported));previous25.version=25;const pm25=previous25.storage.tables.find(t=>t.name==='prompt_model_settings');pm25.columns=pm25.columns.filter(c=>c.name!=='openrouter_model');for(const row of pm25.rows)delete row.openrouter_model;previous25.manifest.find(t=>t.name==='prompt_model_settings').columns=pm25.columns.map(c=>c.name);sign(previous25);
 assert.ok((await dst.restore(previous25)).warnings.some(w=>w.includes('выбор JEV')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previous24=stripRoleTables(structuredClone(exported));previous24.version=24;
 const oldLetters=previous24.storage.tables.find(t=>t.name==='vacancy_cover_letters');oldLetters.columns=oldLetters.columns.filter(c=>!['override_shared','revision'].includes(c.name));oldLetters.rows=oldLetters.rows.map(({override_shared,revision,...row})=>row);
 previous24.manifest=previous24.storage.tables.map(t=>({name:t.name,columns:t.columns.map(c=>c.name),rows:t.rows.length}));sign(previous24);
 await dst.restore(previous24);assert.deepEqual(records(await dst.backup()),records(exported),'Real v24 old letter layout gets zero defaults');
 const missingNew=structuredClone(previous24);missingNew.version=25;sign(missingNew);assert.equal((await dst.req(missingNew,'/api/backup/validate')).status,400,'v25 requires new columns');
 const previous23=stripRoleTables(structuredClone(exported));previous23.version=23;previous23.storage.tables=previous23.storage.tables.filter(t=>t.name!=='openai_settings');previous23.manifest=previous23.manifest.filter(t=>t.name!=='openai_settings');sign(previous23);assert.ok((await dst.restore(previous23)).warnings.some(w=>w.includes('подключения OpenAI')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previous22=structuredClone(previous23);previous22.version=22;previous22.storage.tables=previous22.storage.tables.filter(t=>t.name!=='prompt_model_settings');previous22.manifest=previous22.manifest.filter(t=>t.name!=='prompt_model_settings');sign(previous22);assert.ok((await dst.restore(previous22)).warnings.some(w=>w.includes('провайдера')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previous21=structuredClone(previous22);previous21.version=21;previous21.storage.tables=previous21.storage.tables.filter(t=>t.name!=='openrouter_model_metrics');previous21.manifest=previous21.manifest.filter(t=>t.name!=='openrouter_model_metrics');sign(previous21);assert.ok((await dst.restore(previous21)).warnings.some(w=>w.includes('замеров времени')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previous20=structuredClone(previous21);previous20.version=20;previous20.storage.tables=previous20.storage.tables.filter(t=>t.name!=='vacancy_cover_letters');previous20.manifest=previous20.manifest.filter(t=>t.name!=='vacancy_cover_letters');sign(previous20);
 assert.ok((await dst.restore(previous20)).warnings.some(w=>w.includes('личных сопроводительных писем')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previous19=structuredClone(previous20);previous19.version=19;previous19.storage.tables=previous19.storage.tables.filter(t=>t.name!=='system_prompts');previous19.manifest=previous19.manifest.filter(t=>t.name!=='system_prompts');sign(previous19);
 assert.ok((await dst.restore(previous19)).warnings.some(w=>w.includes('промтов')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previous18=structuredClone(previous19);previous18.version=18;previous18.storage.tables=previous18.storage.tables.filter(t=>!t.name.startsWith('openrouter_'));previous18.manifest=previous18.manifest.filter(t=>!t.name.startsWith('openrouter_'));sign(previous18);
 assert.ok((await dst.restore(previous18)).warnings.some(w=>w.includes('OpenRouter')));assert.deepEqual(records(await dst.backup()),records(exported));
 const previousModern=structuredClone(previous22);previousModern.version=16;previousModern.storage.tables=previousModern.storage.tables.filter(t=>!['user_profiles','openrouter_settings','openrouter_ai_logs','openrouter_model_metrics','system_prompts','vacancy_cover_letters','prompt_model_settings','openai_settings'].includes(t.name));previousModern.manifest=previousModern.manifest.filter(t=>!['user_profiles','openrouter_settings','openrouter_ai_logs','openrouter_model_metrics','system_prompts','vacancy_cover_letters','prompt_model_settings','openai_settings'].includes(t.name));sign(previousModern);
 await dst.restore(previousModern);
 assert.deepEqual(records(await dst.backup()),records(exported),'Version 16 must remain compatible with the modern restore path');
 const previous17=structuredClone(previousModern);previous17.version=17;sign(previous17);
 const oldRestore=await dst.restore(previous17);assert.ok(oldRestore.warnings.some(w=>w.includes('резюме')));
 assert.equal(dst.sqlite.prepare('SELECT resume_md FROM user_profiles WHERE user_name = ?').get('Nastya').resume_md,'Личное резюме');
 const oldEmpty=await instance();await oldEmpty.restore(previous17);assert.equal(oldEmpty.sqlite.prepare('SELECT COUNT(*) n FROM user_profiles').get().n,0);
 async function rejected(payload,message,preflight=true) {
   const original=dst.dump();
   if(preflight)assert.equal((await dst.req(payload,'/api/backup/validate')).status,400,message+' preflight');
   const r=await dst.req(payload);assert.equal(r.status,400,message+': '+JSON.stringify(await r.json()));assert.equal(dst.dump(),original,message+' must be atomic');
 }
 let noMetrics=structuredClone(exported);noMetrics.storage.tables=noMetrics.storage.tables.filter(t=>t.name!=='openrouter_model_metrics');noMetrics.manifest=noMetrics.manifest.filter(t=>t.name!=='openrouter_model_metrics');await rejected(sign(noMetrics),'v22 requires model metrics');
 let noPrompts=structuredClone(exported);noPrompts.storage.tables=noPrompts.storage.tables.filter(t=>t.name!=='system_prompts');noPrompts.manifest=noPrompts.manifest.filter(t=>t.name!=='system_prompts');await rejected(sign(noPrompts),'v20 requires prompts');
 let noLetters=structuredClone(exported);noLetters.storage.tables=noLetters.storage.tables.filter(t=>t.name!=='vacancy_cover_letters');noLetters.manifest=noLetters.manifest.filter(t=>t.name!=='vacancy_cover_letters');await rejected(sign(noLetters),'v21 requires personal letters');
 let noProfiles=structuredClone(exported);noProfiles.storage.tables=noProfiles.storage.tables.filter(t=>t.name!=='user_profiles');noProfiles.manifest=noProfiles.manifest.filter(t=>t.name!=='user_profiles');await rejected(sign(noProfiles),'v18 requires profiles');
 let broken=structuredClone(exported);broken.storage.tables[0].rows=[];await rejected(broken,'checksum');
 broken=structuredClone(exported);broken.storage.tables=broken.storage.tables.filter(t=>t.name!=='company_identity');broken.manifest=broken.manifest.filter(t=>t.name!=='company_identity');await rejected(sign(broken),'missing table, even with recomputed digest');
 broken=structuredClone(exported);let t=broken.storage.tables.find(t=>t.name==='company_vacancies');t.columns=t.columns.filter(c=>c.name!=='archived');for(const row of t.rows)delete row.archived;broken.manifest.find(t=>t.name==='company_vacancies').columns=t.columns.map(c=>c.name);await rejected(sign(broken),'missing recent column');
 broken=structuredClone(exported);delete broken.storage.tables.find(t=>t.name==='company_tasks').rows[0].user_name;await rejected(sign(broken),'missing row field');
 broken=structuredClone(exported);broken.storage.tables.find(t=>t.name==='company_tasks').rows[0].company_name='No such parent';await rejected(sign(broken),'foreign key preflight');
 broken=structuredClone(exported);broken.storage.tables.find(t=>t.name==='hiring_contacts').rows[0].position=999;await rejected(sign(broken),'constraint rollback',false);
 dst.sqlite.exec("CREATE TRIGGER force_failure BEFORE INSERT ON company_tasks BEGIN SELECT RAISE(ABORT,'injected failure'); END;");await rejected(exported,'injected mid-import failure',false);dst.sqlite.exec('DROP TRIGGER force_failure');
 dst.sqlite.exec("CREATE TRIGGER silent_change AFTER INSERT ON company_tasks BEGIN UPDATE company_tasks SET note='unexpected mutation' WHERE id=NEW.id; END;");await rejected(exported,'silent trigger mutation rollback',false);dst.sqlite.exec('DROP TRIGGER silent_change');
 const legacy={...structuredClone(exported),version:15};legacy.storage.tables=legacy.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes','apify_settings','vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations','vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions','app_catalog','google_calendar_connection','user_profiles','openrouter_settings','openrouter_ai_logs','openrouter_model_metrics','system_prompts','vacancy_cover_letters','prompt_model_settings','openai_settings','vacancy_roles','vacancy_role_assignments','vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name));
 assert.ok((await dst.restore(legacy)).warnings.length);assert.equal(dst.sqlite.prepare('SELECT refresh_token_encrypted FROM google_calendar_connection').get().refresh_token_encrypted,'SYNTHETIC-CIPHERTEXT-LEGACY');
 broken=structuredClone(legacy);broken.storage.tables=broken.storage.tables.filter(t=>t.name!=='company_identity');await rejected(broken,'legacy missing table');
 const nonAdmin=await dst.login('Nastya');for(const path of ['/api/backup','/api/backup/validate'])assert.equal((await dst.req(exported,path,nonAdmin)).status,403);
 const large=new Request('https://test.invalid/api/backup',{method:'POST',headers:{cookie:(await dst.login('Eugene'))},body:new ReadableStream({start(controller){controller.enqueue(new TextEncoder().encode(' '.repeat(25*1024*1024+1)));controller.close();}}),duplex:'half'});
 assert.equal((await dst.worker.fetch(large,dst.env)).status,413,'Size enforced without Content-Length');
 dst.sqlite.exec('CREATE TABLE unknown_data(id INTEGER)');assert.equal((await dst.req()).status,400,'Unknown data must not be silently omitted');
 console.log(`Backup v${exported.version} verified: ${exported.storage.tables.length} application tables, role tree, timing metrics, personal letters, raw catalog/dates/sequences, legacy compatibility, repeated restore, checksum/completeness and atomic rollback.`);
} finally {for(const db of databases)db.close();}
