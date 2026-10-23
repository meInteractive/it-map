import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,sqlite}=f;
try{
 let s=await api.schedulerSettings(db);assert.equal(s.enabled,false);assert.equal(s.dayBudgetUsd,0);assert.equal(s.favoritesOnly,true);
 const paused=await api.beginSchedulerRun(db);assert.equal(paused.run.status,'paused');assert.equal(await api.claimScheduledJob(paused.db,paused.run.id),null);
 s=await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:2,monthBudgetUsd:10,revision:s.revision},'Eugene');
 await assert.rejects(()=>api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:2,monthBudgetUsd:10,revision:0},'Eugene'),e=>e.code==='changed');
 const a=await api.enqueueScheduledJob(db,{type:'diagnostic',key:'a'}),same=await api.enqueueScheduledJob(db,{type:'diagnostic',key:'a'});assert.equal(a.id,same.id);
 const future=await api.enqueueScheduledJob(db,{type:'diagnostic',key:'future',runAfter:Date.now()+86400000,priority:100});
 const important=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'important'});
 const start=await api.beginSchedulerRun(db,{requestId:'cycle-a'});assert.equal(start.reused,false);
 await assert.rejects(()=>api.beginSchedulerRun(db,{requestId:'cycle-b'}),e=>e.code==='scheduler_busy');assert.equal((await api.beginSchedulerRun(db,{requestId:'cycle-a'})).reused,true);
 const claims=await Promise.all([api.claimScheduledJob(start.db,start.run.id),api.claimScheduledJob(start.db,start.run.id)]);assert.equal(new Set(claims.map(x=>x.job.id)).size,2);assert.equal(claims[0].job.id,important.id);assert.ok(claims.every(x=>x.job.id!==future.id));
 await api.finishScheduledJob(start.db,claims[0]);assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(important.id).status,'completed');
 sqlite.prepare('UPDATE scheduler_jobs SET locked_until=0 WHERE id=?').run(claims[1].job.id);
 await assert.rejects(()=>claims[1].db.prepare('SELECT 1').first(),e=>e.code==='job_changed');
 sqlite.prepare('UPDATE scheduler_runs SET lease_until=0 WHERE id=?').run(start.run.id);
 await assert.rejects(()=>start.db.prepare('SELECT 1').first(),e=>e.code==='scheduler_changed');
 const next=await api.beginSchedulerRun(db,{requestId:'cycle-b'}),recovered=await api.claimScheduledJob(next.db,next.run.id);assert.equal(recovered.job.id,a.id);assert.equal(recovered.job.attempts,2);
 await api.finishScheduledJob(next.db,recovered,{status:'pending',code:'budget',delayMs:86400000,deferred:true});assert.equal(sqlite.prepare('SELECT attempts FROM scheduler_jobs WHERE id=?').get(a.id).attempts,1);
 const old=await api.enqueueScheduledJob(db,{type:'diagnostic',key:'old'});sqlite.prepare('UPDATE scheduler_jobs SET created_at=? WHERE id=?').run(Date.now()-200*3600000,old.id);const high=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'new-high'});assert.equal((await api.claimScheduledJob(next.db,next.run.id)).job.id,old.id,'aging prevents starvation');
 for(let i=0;i<35;i++)await api.enqueueScheduledJob(db,{type:'diagnostic',key:'page'+i});const page=await api.readScheduledQueue(db,{type:'diagnostic',page:2});assert.equal(page.pageSize,30);assert.equal(page.total,38);assert.equal(page.jobs.length,8);await assert.rejects(()=>api.readScheduledQueue(db,{type:'unknown'}),e=>e.status===400);
 const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.ok(!backup.storage.tables.some(t=>t.name.startsWith('scheduler_')));
 await api.restoreBackup(db,backup);assert.equal((await api.schedulerSettings(db)).enabled,false);assert.equal((await api.readScheduledQueue(db)).total,0,'restore fences previous generation, costs remain operational');
 console.log('Scheduler queue: paused/budget defaults, CAS, dedupe, atomic claims/concurrent cycles, priority+aging, future exclusion, stale-owner fencing/recovery, pagination and backup35 restore pause passed');
}finally{f.close();}

// Global selection covers existing and newly added companies without row opt-ins.
const globalFixture=await schedulerFixture();
try{
 const {api,db,sqlite,env}=globalFixture;
 const catalog={companies:[{name:'Selected favorite',vacancySources:{companySite:'https://fixture.org/jobs',habr:'https://career.fixture.org/jobs',hh:'https://hh.ru/employer/1'}},{name:'Other company',vacancySources:{companySite:'https://other.fixture.org/jobs',habr:'https://habr.fixture.org/jobs'}}],agencies:[],roles:[]};
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(catalog.companies[0].name);
 sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(catalog.companies[0].name);
 let s=await api.schedulerSettings(db);const save=async patch=>s=await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,revision:s.revision,...patch},'Eugene');
 await save({sourceIds:['companySite','habr'],intervalDays:3});
 let scans=[];const cycle=async id=>{await api.scheduleDueSources(db,'Eugene');return api.runDueTasks(env,{requestId:id,handlers:{scan_source:async(_,claim)=>{scans.push(claim.job);await db.prepare('UPDATE vacancy_source_parsing SET next_run_at=? WHERE company_id=? AND source_id=?').bind(Date.now()+3*86400000,claim.job.company_id,claim.job.source_id).run();return {};}}});};
 await cycle('favorite');assert.equal(scans.length,2,'all selected types for favorite without per-company setup');assert.equal(new Set(scans.map(j=>j.company_id)).size,1);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_source_parsing WHERE enabled=1 AND interval_days=3').get().n,2);
 await save({favoritesOnly:false});await cycle('all');assert.equal(new Set(scans.map(j=>j.company_id)).size,2,'all companies switch');
 catalog.companies.push({name:'New company',vacancySources:{companySite:'https://new.fixture.org/jobs'}});sqlite.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').run(JSON.stringify(catalog));
 await cycle('new');assert.equal(new Set(scans.map(j=>j.company_id)).size,3,'new company automatically included');
 await save({sourceIds:[]});const before=scans.length;await cycle('empty');assert.equal(scans.length,before,'empty source selection queues no scans');
 const source=(await api.readVacancyParsingSources(db))[0];const pending=await api.enqueueScheduledJob(db,{type:'scan_source',key:'deselected',companyId:source.companyId,sourceId:source.sourceId,payload:{}});
 await api.runDueTasks(env,{requestId:'deselected'});const blocked=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(pending.id);assert.equal(blocked.status,'pending');assert.equal(blocked.reason_code,'source_scope');assert.equal(blocked.attempts,0,'deselection stops pending work without consuming retries');
 const fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:false,favoritesOnly:true,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 await assert.rejects(()=>save({favoritesOnly:false}),e=>e.code==='changed','Firecrawl scope change invalidates stale global form');
 s=await api.schedulerSettings(db);await save({sourceIds:['companySite'],favoritesOnly:true});const backup=await api.createBackup(db);await api.restoreBackup(db,backup);s=await api.schedulerSettings(db);assert.deepEqual(s.sourceIds,['companySite']);assert.equal(s.favoritesOnly,true);assert.equal(s.enabled,false,'global config survives restore while execution pauses');
 console.log('Global scheduler selection: multiple types/all favorites/all companies/new company, empty selection, deselected queued work and live scope CAS, backup persistence passed');
}finally{globalFixture.close();}

// Dead-letter or completed scans for an unchanged due window must not starve
// the next companies when the global scope contains more than fifty sources.
const broad=await schedulerFixture();
try{
 const {api,db,sqlite,env}=broad,catalog={companies:Array.from({length:65},(_,i)=>({name:'Broad company '+i,vacancySources:{companySite:'https://fixture.org/jobs/'+i}})),agencies:[]};
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:0,monthBudgetUsd:0,favoritesOnly:false,sourceIds:['companySite'],intervalDays:7,revision:s.revision},'Eugene');
 await api.scheduleDueSources(db,'Eugene');assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='scan_source'").get().n,50);
 await api.runDueTasks(env,{requestId:'dead-letters',handlers:{scan_source:async()=>{throw Object.assign(new Error('Permanent fixture failure'),{code:'source_error',status:422});}}});
 await api.scheduleDueSources(db,'Eugene');assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='scan_source'").get().n,65,'failed first fifty do not block remaining sources');
 assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE status='pending'").get().n,15);console.log('Global scheduler fairness: 65 companies progress beyond 50 permanent source failures passed');
}finally{broad.close();}

// Capacity migration resumes only the obsolete import failure in this generation.
const capacity=await schedulerFixture();
try{
 const {api,db,sqlite}=capacity;
 const blocked=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'capacity'});
 const unrelated=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'llm-timeout'});
 const previous=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'old-generation'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='company_limit',attempts=3,completed_at=1 WHERE id IN (?,?)").run(blocked.id,previous.id);
 sqlite.prepare("UPDATE scheduler_jobs SET generation='previous-restore' WHERE id=?").run(previous.id);
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='23.0',attempts=1 WHERE id=?").run(unrelated.id);
 const fs=await import('node:fs'),migration=fs.readFileSync('drizzle/0062_unlimited_company_vacancies.sql','utf8');sqlite.exec(migration);sqlite.exec(migration);
 const resumed=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(blocked.id);assert.equal(resumed.status,'pending');assert.equal(resumed.attempts,0);assert.equal(resumed.reason_code,'');assert.equal(resumed.completed_at,null);
 for(const id of [unrelated.id,previous.id])assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(id).status,'failed');
 console.log('Unlimited vacancies migration: obsolete import failures requeued idempotently; other errors and old restore generations untouched');
}finally{capacity.close();}


const legacyTimeout=await schedulerFixture();
try{
 const {api,db,sqlite}=legacyTimeout;
 const timed=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'old-numeric-timeout'}),cancelled=await api.enqueueScheduledJob(db,{type:'assign_roles',key:'cancelled-timeout'}),other=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'provider-error'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='23.0',attempts=1 WHERE id=?").run(timed.id);sqlite.prepare("UPDATE scheduler_jobs SET status='cancelled',reason_code='23.0' WHERE id=?").run(cancelled.id);sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='provider_401' WHERE id=?").run(other.id);
 const fs=await import('node:fs'),sql=fs.readFileSync('drizzle/0063_llm_timeout_retry.sql','utf8');sqlite.exec(sql);sqlite.exec(sql);
 const resumed=sqlite.prepare('SELECT * FROM scheduler_jobs WHERE id=?').get(timed.id);assert.equal(resumed.status,'pending');assert.equal(resumed.attempts,0);assert.equal(resumed.reason_code,'');assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(cancelled.id).status,'cancelled');assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(other.id).status,'failed');
 console.log('Historical 23.0 repair: only misclassified failed LLM timeout resumes; cancellations/provider errors remain untouched');
}finally{legacyTimeout.close();}

const metadata=await schedulerFixture();
try{
 const {api,db,sqlite}=metadata;
 const optional=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'optional-field'}),amount=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'salary-amount'});
 for(const [id,message] of [[optional.id,'Значение salary_period не подтверждается исходным текстом.'],[amount.id,'Зарплата не подтверждается исходным текстом.']])sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='invalid_output',last_error=? WHERE id=?").run(message,id);
 const fs=await import('node:fs'),sql=fs.readFileSync('drizzle/0064_extraction_metadata_recovery.sql','utf8');sqlite.exec(sql);sqlite.exec(sql);
 assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(optional.id).status,'pending');assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(amount.id).status,'failed');
 console.log('Metadata recovery: optional unsupported salary period retries, unsupported salary amount remains failed');
}finally{metadata.close();}

const levelMapping=await schedulerFixture();try{const {api,db,sqlite}=levelMapping;const recover=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'unmapped-level'}),other=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'other-mapping'});for(const [id,message] of [[recover.id,'Уровень вакансии указан некорректно: используйте Senior, Middle, Lead или Head.'],[other.id,'Некорректная ссылка.']])sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='mapping_invalid',last_error=? WHERE id=?").run(message,id);const fs=await import('node:fs');sqlite.exec(fs.readFileSync('drizzle/0065_import_level_metadata.sql','utf8'));assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(recover.id).status,'pending');assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(other.id).status,'failed');}finally{levelMapping.close();}
