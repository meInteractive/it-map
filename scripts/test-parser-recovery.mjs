import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {DEFAULT_ROLE_PROMPT} from '../server/services/vacancy-role-prompt.js';
const f=await schedulerFixture(),{db,api,sqlite}=f;
try{
 const migration=fs.readFileSync('drizzle/0076_parser_recovery.sql','utf8');
 for(const name of ['Сбер','СберТех','Other']){sqlite.prepare('INSERT OR IGNORE INTO company_identity(company_name) VALUES(?)').run(name);sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(name);}
 const id=name=>sqlite.prepare('SELECT id FROM company_identity WHERE company_name=?').get(name).id,from=id('СберТех'),to=id('Сбер'),source='https://www.rabota.ru/company/sberbank/vacancies/';
 for(const companyId of [from,to])sqlite.prepare("INSERT OR REPLACE INTO vacancy_source_parsing(company_id,source_id,source_url,enabled,auto_enabled,revision) VALUES(?,'rabota',?,1,1,1)").run(companyId,source);
 const main=sqlite.prepare("INSERT INTO company_vacancies(company_name,url,title,description_md) VALUES('СберТех','https://www.rabota.ru/vacancy/12345/','My title','My edited text')").run().lastInsertRowid;
 async function candidate(n,overrides={}){const r=await api.upsertVacancyCandidate(db,{titleRaw:'Manager',vacancyUrl:'https://www.rabota.ru/vacancy/'+n+'/',sourceUrl:source,companyId:from,sourceId:'rabota',acquisitionMethod:'firecrawl',...overrides});return r.id;}
 const wrong=await candidate(12345),unimported=await candidate(12346),manual=await candidate(12347,{acquisitionMethod:'manual'}),other=await candidate(12348,{sourceUrl:'https://www.rabota.ru/company/other/vacancies/'});
 sqlite.prepare('UPDATE vacancy_candidates SET main_vacancy_id=? WHERE id=?').run(main,wrong);
 const application=sqlite.prepare("INSERT INTO company_applications(company_name,vacancy_id,comment_md) VALUES('СберТех',?,'Keep application note')").run(main).lastInsertRowid;
 const task=sqlite.prepare("INSERT INTO company_tasks(company_name,vacancy_id,type,note,due_at) VALUES('СберТех',?,'task','Keep task note','2026-11-01T12:00:00Z')").run(main).lastInsertRowid;
 const job=await api.enqueueScheduledJob(db,{type:'assign_roles',key:'repair-test',candidateId:wrong,companyId:from,sourceId:'rabota'});
 const old=fs.readFileSync('drizzle/0074_jev_leaf_roles.sql','utf8').match(/VALUES\(1,'([\s\S]*?)','openrouter','jev',1\)/)[1].replace(/''/g,"'");
 sqlite.prepare('INSERT OR REPLACE INTO vacancy_role_settings(id,markdown,prompt_revision) VALUES(1,?,3)').run(old);
 sqlite.exec(migration);
 assert.equal(sqlite.prepare('SELECT company_name FROM company_vacancies WHERE id=?').get(main).company_name,'Сбер');
 assert.equal(sqlite.prepare('SELECT description_md FROM company_vacancies WHERE id=?').get(main).description_md,'My edited text');
 for(const n of [wrong,unimported])assert.equal(sqlite.prepare('SELECT company_id FROM vacancy_candidates WHERE id=?').get(n).company_id,to);
 for(const n of [manual,other])assert.equal(sqlite.prepare('SELECT company_id FROM vacancy_candidates WHERE id=?').get(n).company_id,from);
 assert.equal(sqlite.prepare('SELECT company_id FROM scheduler_jobs WHERE id=?').get(job.id).company_id,to);
 assert.equal(sqlite.prepare("SELECT enabled FROM vacancy_source_parsing WHERE company_id=? AND source_id='rabota'").get(from).enabled,0);
 assert.equal(sqlite.prepare("SELECT enabled FROM vacancy_source_parsing WHERE company_id=? AND source_id='rabota'").get(to).enabled,1);
 assert.equal(sqlite.prepare('SELECT company_name FROM company_applications WHERE id=?').get(application).company_name,'Сбер');
 assert.equal(sqlite.prepare('SELECT comment_md FROM company_applications WHERE id=?').get(application).comment_md,'Keep application note');
 assert.equal(sqlite.prepare('SELECT company_name FROM company_tasks WHERE id=?').get(task).company_name,'Сбер');
 assert.equal(sqlite.prepare('SELECT note FROM company_tasks WHERE id=?').get(task).note,'Keep task note');
 const prompt=sqlite.prepare('SELECT markdown,prompt_revision FROM vacancy_role_settings').get();assert.equal(prompt.markdown,DEFAULT_ROLE_PROMPT);assert.equal(prompt.prompt_revision,4);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM vacancy_import_operations WHERE action='repair_company'").get().n,1);
 sqlite.prepare("UPDATE vacancy_role_settings SET markdown='Custom prompt',prompt_revision=9").run();
 sqlite.prepare("UPDATE company_vacancies SET company_name='Other' WHERE id=?").run(main);
 sqlite.exec(migration);assert.equal(sqlite.prepare('SELECT company_name FROM company_vacancies WHERE id=?').get(main).company_name,'Other','replay cannot undo later manual move');
 assert.equal(sqlite.prepare('SELECT markdown FROM vacancy_role_settings').get().markdown,'Custom prompt');
 // A later global scheduler tick must not undo the confirmed feed exclusion.
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),base=catalog.companies[0];catalog.companies=[{...base,name:'СберТех',vacancySources:{rabota:source}},{...base,name:'Сбер',vacancySources:{rabota:source}}];catalog.agencies=[];
 sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 let scheduler=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,sourceIds:['rabota'],favoritesOnly:false,revision:scheduler.revision},'Eugene');
 const beforeSource=sqlite.prepare("SELECT * FROM vacancy_source_parsing WHERE company_id=? AND source_id='rabota'").get(from),readSources=await api.readVacancyParsingSources(db);
 const disabledSource=readSources.find(s=>s.companyId===from&&s.sourceId==='rabota');assert.equal(disabledSource.supported,false);assert.equal(disabledSource.enabled,false);assert.match(disabledSource.reason,/источник Сбера/);
 await api.scheduleDueSources(db,'Eugene');assert.deepEqual(sqlite.prepare("SELECT * FROM vacancy_source_parsing WHERE company_id=? AND source_id='rabota'").get(from),beforeSource);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE company_id=? AND source_id='rabota' AND type='scan_source'").get(from).n,0,'global scheduler never re-enables or scans the duplicate');
 assert.equal(readSources.find(s=>s.companyId===to&&s.sourceId==='rabota').supported,true,'correct Sber source remains available');
 catalog.companies[0].vacancySources.rabota='https://www.rabota.ru/company/synthetic-only/vacancies/';sqlite.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').run(JSON.stringify(catalog));
 assert.equal((await api.readVacancyParsingSources(db)).find(s=>s.companyId===from&&s.sourceId==='rabota').supported,true,'only the verified wrong feed is excluded');
 catalog.companies=[{...base,name:'Bip.ru',vacancySources:{companySite:'https://bip.ru/company',hh:'https://hh.ru/employer/2652595'}},{...base,name:'Rubytech',vacancySources:{companySite:'https://rubytech.ru/career/',hh:'https://hh.ru/employer/4670572'}}];
 sqlite.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').run(JSON.stringify(catalog));
 scheduler=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,sourceIds:['companySite','hh'],favoritesOnly:false,revision:scheduler.revision},'Eugene');
 const corporate=await api.readVacancyParsingSources(db);for(const row of corporate.filter(s=>['Bip.ru','Rubytech'].includes(s.companyName)&&['companySite','hh'].includes(s.sourceId))){assert.equal(row.supported,row.sourceId==='hh',JSON.stringify(row));if(row.sourceId==='companySite')assert.match(row.reason,/отдельный источник HH/);}
 await api.scheduleDueSources(db,'Eugene');assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='scan_source' AND source_id='companySite'").get().n,0,'known corporate pages are never auto-enabled or charged again');
 catalog.companies[0].vacancySources.companySite='https://bip.ru/vacancies/';sqlite.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').run(JSON.stringify(catalog));
 assert.equal((await api.readVacancyParsingSources(db)).find(s=>s.companyName==='Bip.ru'&&s.sourceId==='companySite').supported,true,'a corrected listing URL is not excluded');
 assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Parser recovery: exact feed/employer/method guards, stable main IDs/manual text, pending ownership, duplicate source disabled, old prompt only, audit receipt and repeat/manual-edit protection passed.');
}finally{f.close();}
