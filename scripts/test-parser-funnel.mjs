import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{db,sqlite,api,env}=f,{parserFunnel}=await moduleFor('server/services/parser-funnel.js'),{schedulerRoute}=await moduleFor('server/services/scheduler-route.js');
const candidates=await moduleFor('server/repositories/vacancy-candidates.js'),oldFetch=globalThis.fetch,now=Date.parse('2026-10-22T12:00:00Z');
globalThis.fetch=async()=>{throw Error('Analytics must not call providers');};
try{
 await (await moduleFor('server/repositories/classification-prompts.js')).readClassificationSettings(db);
 sqlite.prepare("INSERT INTO company_identity(id,company_name) VALUES(991,'Funnel Fixture')").run();sqlite.prepare("INSERT INTO company_crm(company_name) VALUES('Funnel Fixture')").run();
 const rows=new Map();
 const add=async(n,parser,sourceId,date,method=parser==='firecrawl'?'firecrawl':'site')=>{
  const input={companyId:991,titleRaw:'Fixture '+n,vacancyUrl:'https://funnel.fixture/'+n,sourceId,acquisitionMethod:method};
  const r=await candidates.upsertVacancyCandidate(db,input,{intakeParser:parser});rows.set(n,{...r,input});
  sqlite.prepare('UPDATE vacancy_candidates SET first_seen_at=?,last_seen_at=? WHERE id=?').run(date,date,r.id);sqlite.prepare('UPDATE parser_candidate_intakes SET received_at=? WHERE candidate_token=(SELECT creation_token FROM vacancy_candidates WHERE id=?)').run(date,r.id);return r.id;
 };
 for(const [n,p,s,d,m] of [[1,'apify','hh','2026-10-20T10:00:00Z'],[2,'firecrawl','hh','2026-10-19T10:00:00Z'],[3,'firecrawl','companySite','2026-10-22T10:00:00Z'],[4,'http','companySite','2026-10-21T10:00:00Z'],[5,'unknown','hh','2026-10-01T10:00:00Z'],[6,'manual','companySite','2026-10-22T10:00:00Z','manual'],[7,'apify','hh','2026-09-01T10:00:00Z'],[8,'apify','hh','2026-10-21T10:00:00Z']])await add(n,p,s,d,m);
 const roleContext=await candidates.candidateClassificationContext(db);
 function classify(n,decision,at){const c=sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=?').get(rows.get(n).id),attempt=crypto.randomUUID();sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at) VALUES(?,?,?,?,?,?,?,'complete',?)").run(attempt,c.id,c.title_raw,c.title_hash,c.title_revision,roleContext[0].modelKey,JSON.stringify(roleContext.map(r=>r.roleId)),at);for(const role of roleContext)sqlite.prepare('INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)').run(c.id,attempt,role.roleId,role.name,decision,role.modelKey,'synthetic',role.promptVersion,1,c.title_hash,c.title_revision,at);}
 classify(1,'match','2026-10-21T10:00:00Z');classify(1,'match','2026-10-22T10:00:00Z');classify(2,'match','2026-10-20T10:00:00Z');classify(3,'reject','2026-10-22T10:00:00Z');
 function snapshot(n,at){const id='raw-'+n;sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES(?,?,?,?,'SYNTHETIC',?,?,'free','valid',?,?)").run(id,rows.get(n).id,'https://funnel.fixture/'+n,'https://funnel.fixture/'+n,'a'.repeat(64),at,JSON.stringify({provider:'apify'}),at);return id;}
 for(const [n,at] of [[1,'2026-10-20T10:00:00Z'],[2,'2026-10-19T10:00:00Z'],[5,'2026-10-01T10:00:00Z']]){
  const raw=snapshot(n,at);if(n===5)continue;
  sqlite.prepare("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,config_hash,prompt_version,schema_version,processed_at,duration_ms,status) VALUES(?,?,?,'synthetic','config','prompt',1,'2026-10-21T10:00:00Z',1,'success')").run('extract-'+n,rows.get(n).id,raw);
  sqlite.prepare("INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,prompt_version,config_hash,provider,role_ids,status,processed_at,duration_ms) VALUES(?,?,?,'input','tree','prompt','config','synthetic','[1]','success','2026-10-21T10:00:00Z',1)").run('role-'+n,rows.get(n).id,raw);
 }
 for(const [n,target,action] of [[1,701,'create'],[2,701,'update'],[7,702,'create']]){
  sqlite.prepare("INSERT OR IGNORE INTO company_vacancies(id,company_name,title,url) VALUES(?,'Funnel Fixture','Published fixture',?)").run(target,'https://funnel.fixture/main/'+target);
  sqlite.prepare('UPDATE vacancy_candidates SET main_vacancy_id=? WHERE id=?').run(target,rows.get(n).id);
  sqlite.prepare("INSERT INTO vacancy_import_operations(id,candidate_id,vacancy_id,action,status,processed_at,changes_json) VALUES(?,?,?,?,'success','2026-10-22T10:00:00Z','{}')").run('import-'+n,rows.get(n).id,target,action);
 }
 const error=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'funnel-error',companyId:991,candidateId:rows.get(8).id,sourceId:'hh'});sqlite.prepare("UPDATE scheduler_jobs SET status='pending',reason_code='timeout' WHERE id=?").run(error.id);
 let report=await parserFunnel(db,{now});assert.equal(report.daily.length,30);assert.equal(report.from,'2026-09-23');assert.equal(report.to,'2026-10-22');assert.equal(report.cohort.received,6);assert.equal(report.cohort.published,2);assert.equal(report.cohort.rejected,1);assert.equal(report.cohort.error,1);assert.equal(report.cohort.waiting,2);assert.equal(report.cohort.created,1);assert.equal(report.cohort.linked,1);
 assert.equal(report.totals.published,3,'old intake can publish in this window');assert.equal(report.totals.accepted,2,'repeated decisions count once');assert.equal(report.totals.fulltext,3,'Apify may get text before classification');assert.equal(report.averages.published,.1);assert.equal(report.daily.at(-1).published,3);assert.equal(report.daily.find(p=>p.day==='2026-10-20').received,1);
 assert.equal((await parserFunnel(db,{now,sourceId:'hh',parser:'apify'})).cohort.received,2);assert.equal((await parserFunnel(db,{now,sourceId:'hh',parser:'firecrawl'})).cohort.received,1);assert.equal((await parserFunnel(db,{now,parser:'http'})).cohort.received,1);assert.equal((await parserFunnel(db,{now,parser:'unknown'})).cohort.received,1,'later Apify text does not prove listing provenance');
 await candidates.upsertVacancyCandidate(db,{...rows.get(1).input,acquisitionMethod:'firecrawl'},{intakeParser:'firecrawl'});assert.equal((await parserFunnel(db,{now,parser:'apify'})).cohort.received,2,'rediscovery preserves original parser and date');
 // Exact creation receipt backfill is repeatable; stale-generation/other receipts are not guessed.
 sqlite.prepare('DELETE FROM parser_candidate_intakes WHERE candidate_token=(SELECT creation_token FROM vacancy_candidates WHERE id=?)').run(rows.get(5).id);
 const generation=sqlite.prepare('SELECT generation FROM app_runtime_generation WHERE id=1').get().generation;
 sqlite.prepare("INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES('backfill','Eugene',?,991,'hh','https://funnel.fixture',1,1,?,1,9999999999999)").run(generation,JSON.stringify({mode:'scrape',jobs:[{existingId:rows.get(5).id,saveState:'new'},{existingId:rows.get(1).id,saveState:'new'}]}));
 sqlite.exec(fs.readFileSync('drizzle/0083_parser_candidate_intakes.sql','utf8'));sqlite.exec(fs.readFileSync('drizzle/0083_parser_candidate_intakes.sql','utf8'));assert.equal((await parserFunnel(db,{now,parser:'firecrawl'})).cohort.received,3);assert.equal((await parserFunnel(db,{now,parser:'apify'})).cohort.received,2);
 for(const filters of [{parser:'wrong'},{sourceId:'wrong'}])await assert.rejects(()=>parserFunnel(db,{now,...filters}),e=>e.status===400);
 const route=async(query,user={name:'Eugene',isAdmin:true})=>{const url=new URL('https://fixture/api/scheduler/funnel'+query);return schedulerRoute(new Request(url),env,url,user);};
 assert.equal((await route('',{name:'Nastya',isAdmin:false})).status,403);assert.equal((await route('?period=all')).status,400);assert.equal((await route('?parser=wrong')).status,400);const response=await route('?sourceId=hh&parser=apify');assert.equal(response.status,200);assert.equal((await response.json()).daily.length,30);
 assert.ok(!JSON.stringify(report).includes('SYNTHETIC'));assert.ok(!JSON.stringify(report).includes('Fixture 1'));
 sqlite.prepare('UPDATE vacancy_candidates SET main_vacancy_id=701 WHERE id=?').run(rows.get(5).id);report=await parserFunnel(db,{now});assert.equal(report.cohort.undatedPublished,1);assert.equal(report.cohort.linked,1,'legacy main link without an import audit is not declared a known duplicate');assert.equal(report.totals.published,3,'do not invent an old publication timestamp');
 const before=sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n;
 sqlite.exec("CREATE TRIGGER reject_intake BEFORE INSERT ON parser_candidate_intakes WHEN NEW.parser='http' BEGIN SELECT RAISE(ABORT,'synthetic intake failure'); END");
 await assert.rejects(()=>candidates.upsertVacancyCandidate(db,{companyId:991,titleRaw:'Atomic fixture',vacancyUrl:'https://funnel.fixture/atomic',sourceId:'companySite',acquisitionMethod:'site'},{intakeParser:'http'}));
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,before,'intake failure rolls back candidate creation');assert.equal(sqlite.prepare("SELECT count(*) n FROM vacancy_candidates WHERE canonical_url='https://funnel.fixture/atomic'").get().n,0);sqlite.exec('DROP TRIGGER reject_intake');
 console.log('Funnel: six milestones/first-success dates, 30 MSK dates, distinct retries, cohort vs stage dates, rejected/error/waiting partition, new vs linked cards, source/parser intersection, immutable intake, exact migration backfill, legacy unknown, manual exclusion, admin validation and no providers passed.');
}finally{globalThis.fetch=oldFetch;f.close();}
