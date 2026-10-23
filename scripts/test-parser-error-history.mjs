import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{db,sqlite,api,env}=f;
const history=await moduleFor('server/repositories/parser-error-history.js');
const {parserIssues}=await moduleFor('server/services/parser-issues.js');
const {schedulerDashboard,schedulerRoute}=await moduleFor('server/services/scheduler-route.js');
const {runDueTasks}=await moduleFor('server/services/scheduler-dispatcher.js');
const oldFetch=globalThis.fetch,oldNow=Date.now,now=Date.parse('2026-10-22T22:00:00Z');
globalThis.fetch=async()=>{throw Error('History must not contact providers');};
Date.now=()=>now;
try{
 await history.recordParserErrorSnapshot(db,'Eugene',{p1:1,p2:4},now-86400000);
 await history.recordParserErrorSnapshot(db,'Eugene',{p1:0,p2:0},now-86400000);
 await Promise.all([history.recordParserErrorSnapshot(db,'Eugene',{p1:2,p2:3},now),history.recordParserErrorSnapshot(db,'Eugene',{p1:1,p2:8},now)]);
 await history.recordParserErrorSnapshot(db,'Nastya',{p1:99,p2:99},now);
 await history.recordParserErrorSnapshot(db,'Eugene',{p1:99,p2:99},now-30*86400000);
 let h=await history.readParserErrorHistory(db,'Eugene',now);
 assert.equal(h.points.length,30);assert.equal(h.points.at(-1).day,'2026-10-23');assert.equal(h.points[0].day,'2026-09-24');
 assert.deepEqual(h.average,{p1:1.5,p2:6});assert.equal(h.observedDays,2);assert.equal(h.points[0].p1,null);assert.equal(h.points.at(-1).p1,2);assert.equal(h.points.at(-1).p2,8);
 await history.recordParserErrorSnapshot(db,'Eugene',{p1:0,p2:0},now-2*86400000);
 h=await history.readParserErrorHistory(db,'Eugene',now);assert.equal(h.observedDays,3);assert.equal(h.average.p1,1);assert.equal(h.average.p2,4);
 await (await moduleFor('server/repositories/classification-prompts.js')).readClassificationSettings(db);
 const settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,favoritesOnly:false,agenciesEnabled:false,dayBudgetUsd:0,monthBudgetUsd:0,sourceIds:['companySite'],revision:settings.revision},'Eugene');
 sqlite.prepare("INSERT INTO company_identity(id,company_name) VALUES(991,'History Fixture')").run();
 sqlite.prepare("INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,enabled) VALUES(991,'companySite','https://history.fixture/jobs',1)").run();
 // Distinct candidates keep genuine failures distinct, regardless of grouping.
 for(let i=0;i<125;i++){
  const job=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'history-failed-'+i,companyId:991,candidateId:9000+i,sourceId:'companySite'});
  sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='invalid_output',updated_at=? WHERE id=?").run(now,job.id);
 }
 const waiting=await api.enqueueScheduledJob(db,{type:'scan_source',key:'history-waiting',companyId:991,sourceId:'companySite'});
 sqlite.prepare("UPDATE scheduler_jobs SET reason_code='budget',updated_at=? WHERE id=?").run(now,waiting.id);
 const dash=await schedulerDashboard(db);let report=await parserIssues(db,dash);
 assert.equal(report.activeCounts.p2,125);assert.equal(report.sampleLimited,true);assert.equal(report.issues.find(i=>i.reasonCode==='invalid_output').jobs.length,8);
 assert.ok(report.issues.some(i=>i.status==='verification'));assert.equal(report.activeCounts.p1,0,'verification backlog does not count as an error');
 for(const code of ['apify_busy','apify_budget','apify_cycle_limit','time_slice','scheduler_budget']){sqlite.prepare('UPDATE scheduler_jobs SET reason_code=? WHERE id=?').run(code,waiting.id);assert.equal((await parserIssues(db,dash)).activeCounts.p2,125,code+' is an expected wait, not a P2 error');}
 sqlite.prepare('DELETE FROM parser_error_daily').run();
 const unauthorized=await schedulerRoute(new Request('https://fixture/api/scheduler/issues'),env,new URL('https://fixture/api/scheduler/issues'),{name:'Nastya',isAdmin:false});assert.equal(unauthorized.status,403);assert.equal(sqlite.prepare('SELECT count(*) n FROM parser_error_daily').get().n,0);
 // Calendar assertions above use a fixed clock. The ownership guard below
 // compares with SQLite's real clock, so run the autonomous tick in real time.
 Date.now=oldNow;
 // A server tick alone collects history with the UI closed and no paid work.
 await runDueTasks(env,{trigger:'automatic',requestId:'history-automatic',limit:0,durationMs:0});
 h=await history.readParserErrorHistory(db,'Eugene',oldNow());assert.equal(h.observedDays,1);assert.equal(h.points.at(-1).p2,125);
 const {buildBackupSchema}=await import('./backup-schema.mjs'),schema=buildBackupSchema(process.cwd());assert.ok(!schema.operationalTables.some(t=>t.name==='parser_error_daily'));assert.ok(schema.tables.some(t=>t.name==='parser_error_daily'));
 const {candidateDb}=await moduleFor('server/repositories/vacancy-candidates.js'),bound=await candidateDb(db);
 sqlite.prepare('UPDATE app_runtime_generation SET generation=? WHERE id=1').run(crypto.randomUUID());
 await assert.rejects(()=>history.recordParserErrorSnapshot(bound,'Eugene',{p1:9,p2:9},now),e=>e.code==='restored');
 console.log('Parser history: Moscow boundaries, 30 dates, unknown/zero distinction, daily MAX concurrency, observed-day averages, owner isolation, exact counts beyond UI cap, expected waits/backlog exclusion, admin access, autonomous server tick, backup inclusion and restore fence passed.');
}finally{Date.now=oldNow;globalThis.fetch=oldFetch;f.close();}
