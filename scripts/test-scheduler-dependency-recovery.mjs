import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,originalFetch=fetch;
const raw='# Head of Engineering\n\n## Обязанности\n'+('Управление командой разработки и поставкой продукта. '.repeat(12))+'\n## Требования\nОпыт руководства командой.\n## Условия\nУдалённая работа.';
const extracted=Object.fromEntries(api.EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(extracted,{title:'Head of Engineering',description_md:raw,level:'Head'});
let calls=0,rejectLeaves=false,onLegacyRole=null,missingBody=false;
globalThis.fetch=async(url,o={})=>{
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});
 if(o.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('credit-usage'))return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 if(url.endsWith('/scrape'))return Response.json({success:true,data:{markdown:missingBody?'# Project Manager\n'+('Опыт работы в Москве. Условия и зарплата обсуждаются. '.repeat(12)):raw,metadata:{statusCode:200,url:JSON.parse(o.body).url,creditsUsed:1}}});
 if(url.includes('models?output_modalities'))return Response.json({data:[{id:api.JEV_MODEL_ID,context_length:32000,pricing:{prompt:'0.000000042',completion:'0',request:'0'}}]});
 if(url.endsWith('/decisions')){calls++;const p=JSON.parse(o.body);return Response.json({model:api.JEV_MODEL_ID,answers:Object.fromEntries(Object.keys(p.questions).map(k=>[k,{type:'choice',choice:rejectLeaves&&k.startsWith('leaf_')?'reject':'match'}])),usage:{cost:.00001}});}
 if(url.endsWith('/chat/completions')){calls++;const p=JSON.parse(o.body),roles=JSON.parse(p.messages.at(-1).content).allowedRoles;if(roles&&onLegacyRole){const hook=onLegacyRole;onLegacyRole=null;await hook();}return Response.json({model:'fixture/free',choices:[{finish_reason:'stop',message:{content:JSON.stringify(roles?{role_ids:[roles[0].id]}:extracted)}}],usage:{cost:0,prompt_tokens:100,completion_tokens:100}});}
 throw Error('Unexpected outbound: '+url);
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const co=catalog.companies[0];co.vacancySources={companySite:'https://careers.fixture.org/jobs'};
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(co.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(co.name);sqlite.prepare('INSERT INTO company_vacancy_state(company_name) VALUES(?)').run(co.name);
 await api.candidateClassificationContext(db);await api.orSettings(env);sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=?,cache_json=?,models_synced_at=? WHERE id=1').run(await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC'),JSON.stringify([{id:'fixture/free',canonical_slug:'fixture/free',supports_structured_outputs:true}]),new Date().toISOString());
 let fc=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC-FREE-KEY',revision:fc.revision});fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,maxPages:1,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 await api.savePromptModel(db,'vacancy_extraction',{provider:'openrouter',openrouterModel:'best_free',openaiModel:'',reasoningEffort:'medium',revision:0});
 const setRole=async model=>{const r=sqlite.prepare('SELECT model_revision FROM vacancy_role_settings WHERE id=1').get();return api.savePromptModel(db,'vacancy_role',{provider:'openrouter',openrouterModel:model,openaiModel:'',reasoningEffort:'medium',revision:r?.model_revision??0});};
 const source=(await api.readVacancyParsingSources(db)).find(s=>s.sourceId==='companySite');await api.saveVacancyParsingSource(db,source.companyId,source.sourceId,{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['companySite'],dayBudgetUsd:1,monthBudgetUsd:5,revision:s.revision},'Eugene');
 const prepare=async n=>{const c=await api.upsertVacancyCandidate(db,{companyId:source.companyId,sourceId:'companySite',titleRaw:'Head of Engineering',vacancyUrl:'https://careers.fixture.org/vacancies/'+n});await api.classifyStoredCandidate(env,c.id);await api.fetchCandidateMarkdown(env,c.id);await api.extractCandidateMarkdown(env,c.id);return c;};
 await setRole('best_free');const c=await prepare(1);await api.analyzeVacancyLeafRoles(env,c.id);const stale=await api.enqueueNextStage(env,c.id);assert.equal(stale.type,'import_vacancy');await setRole('jev');
 // Same late change as the production migration: a legacy import is already queued.
 const run=await api.runDueTasks(env,{requestId:'queued-model-change'});assert.equal(run.errors,0,JSON.stringify(sqlite.prepare('SELECT type,status,reason_code,last_error FROM scheduler_jobs').all()));
 assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(stale.id).status,'completed');
 const outcome=JSON.parse(sqlite.prepare('SELECT result_json FROM scheduler_attempts WHERE job_id=?').get(stale.id).result_json);assert.deepEqual(outcome,{superseded:true,nextStage:'assign_roles'});
 assert.ok(sqlite.prepare('SELECT main_vacancy_id FROM vacancy_candidates WHERE id=?').get(c.id).main_vacancy_id);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM vacancy_role_results WHERE candidate_id=? AND model_id=? AND status='success'").get(c.id,api.JEV_MODEL_ID).n,1);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,1);
 // Historical roles_pending is repaired despite a continuously ready backlog.
 await setRole('best_free');const stuck=await prepare(2);await api.analyzeVacancyLeafRoles(env,stuck.id);const failed=await api.enqueueNextStage(env,stuck.id);sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='roles_pending',last_error='historical dependency failure' WHERE id=?").run(failed.id);await setRole('jev');
 await api.enqueueScheduledJobs(db,Array.from({length:55},(_,i)=>({type:'diagnostic',key:'backlog-repair:'+i})));
 const repair=await api.runDueTasks(env,{requestId:'repair-with-backlog',limit:0});assert.equal(repair.errors,0);
 let jobs=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE candidate_id=? AND type='assign_roles'").all(stuck.id);assert.equal(jobs.length,1);assert.equal(jobs[0].status,'pending');
 assert.equal(sqlite.prepare('SELECT status,reason_code,last_error FROM scheduler_jobs WHERE id=?').get(failed.id).reason_code,'roles_pending','historical failure is not erased');
 const recovery=JSON.parse(sqlite.prepare('SELECT payload FROM scheduler_jobs WHERE id=?').get(failed.id).payload).dependencyRecovery;assert.equal(recovery.jobId,jobs[0].id);
 await api.runDueTasks(env,{requestId:'repair-repeat',limit:0});assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=? AND type='assign_roles'").get(stuck.id).n,1,'exactly one deduplicated current dependency');
 rejectLeaves=true;await api.runDueTasks(env,{requestId:'recovered-empty-role'});assert.equal(sqlite.prepare('SELECT main_vacancy_id FROM vacancy_candidates WHERE id=?').get(stuck.id).main_vacancy_id,null);assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=? AND type='import_vacancy'").get(stuck.id).n,1,'valid empty current decision creates no new import');rejectLeaves=false;
 // A change while the old provider is in flight selects the new stage at handoff.
 await setRole('best_free');const flying=await prepare(3);onLegacyRole=()=>setRole('jev');await api.analyzeVacancyLeafRoles(env,flying.id);assert.equal((await api.enqueueNextStage(env,flying.id)).type,'assign_roles');
 // A historical empty result is stale after a model change, not a forced match.
 await setRole('best_free');rejectLeaves=true;
 // Legacy generative fixture returns a leaf, so insert the observed empty outcome.
 const legacyEmpty=await prepare(5);await api.analyzeVacancyLeafRoles(env,legacyEmpty.id);
 sqlite.prepare("UPDATE vacancy_role_results SET role_ids='[]' WHERE candidate_id=?").run(legacyEmpty.id);
 const oldEmpty=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'historical-roles-unresolved',candidateId:legacyEmpty.id,companyId:source.companyId,sourceId:'companySite'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='roles_unresolved' WHERE id=?").run(oldEmpty.id);
 await setRole('jev');const callsBeforeRepair=calls;
 await api.reconcileFailedCandidateDependencies(env);assert.equal(calls,callsBeforeRepair,'repair only queues, no paid inference');
 const currentRoleJob=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE candidate_id=? AND status='pending'").get(legacyEmpty.id);assert.equal(currentRoleJob.type,'assign_roles');
 const existingSnapshots=sqlite.prepare('SELECT count(*) n FROM vacancy_raw_snapshots WHERE candidate_id=?').get(legacyEmpty.id).n;
 const scopedKey='candidate-step:'+legacyEmpty.id+':11111111-1111-4111-8111-111111111111';
 const scopedStep=await api.runScheduledSeriesTick(env,scopedKey);assert.equal(scopedStep.processed,1);assert.equal(scopedStep.stage,'assign_roles');
 const afterScopedCalls=calls;assert.equal((await api.runScheduledSeriesTick(env,scopedKey)).reused,true);assert.equal(calls,afterScopedCalls);
 const terminalStep=await api.runScheduledSeriesTick(env,'candidate-step:'+legacyEmpty.id+':22222222-2222-4222-8222-222222222222');assert.equal(terminalStep.decision.state,'filtered');assert.equal(terminalStep.processed,0);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_raw_snapshots WHERE candidate_id=?').get(legacyEmpty.id).n,existingSnapshots,'saved body is reused');
 assert.equal(sqlite.prepare('SELECT main_vacancy_id FROM vacancy_candidates WHERE id=?').get(legacyEmpty.id).main_vacancy_id,null);
 // A historical failure pointing at a current empty answer is a terminal filter.
 const currentEmpty=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'current-roles-unresolved',candidateId:legacyEmpty.id,companyId:source.companyId,sourceId:'companySite'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='roles_unresolved' WHERE id=?").run(currentEmpty.id);
 const beforeEmptyCalls=calls;await api.reconcileFailedCandidateDependencies(env);
 const terminal=JSON.parse(sqlite.prepare('SELECT payload FROM scheduler_jobs WHERE id=?').get(currentEmpty.id).payload).dependencyRecovery;
 assert.equal(terminal.state,'filtered');assert.equal(terminal.reason,'roles_no_match');assert.ok(terminal.inputHash);assert.equal(calls,beforeEmptyCalls);
 const noRepeat=sqlite.prepare('SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=?').get(legacyEmpty.id).n;
 await api.reconcileFailedCandidateDependencies(env);assert.equal(sqlite.prepare('SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=?').get(legacyEmpty.id).n,noRepeat);
 assert.equal(sqlite.prepare('SELECT reason_code FROM scheduler_jobs WHERE id=?').get(oldEmpty.id).reason_code,'roles_unresolved');rejectLeaves=false;
 // Exhausted current dependencies are linked once, without starving later failures.
 const exhausted=await prepare(6),exhaustedStage=await api.enqueueNextStage(env,exhausted.id);
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='invalid_output' WHERE id=?").run(exhaustedStage.id);
 const exhaustedImport=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'exhausted-dependency-import',candidateId:exhausted.id,companyId:source.companyId,sourceId:'companySite'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='roles_unresolved' WHERE id=?").run(exhaustedImport.id);
 const beforeExhausted=calls;await api.reconcileFailedCandidateDependencies(env);
 const exhaustedRecovery=JSON.parse(sqlite.prepare('SELECT payload FROM scheduler_jobs WHERE id=?').get(exhaustedImport.id).payload).dependencyRecovery;
 const blockedStep=await api.runScheduledSeriesTick(env,'candidate-step:'+exhausted.id+':33333333-3333-4333-8333-333333333333');assert.equal(blockedStep.status,'blocked');assert.equal(blockedStep.processed,0);
 assert.equal(exhaustedRecovery.state,'blocked');assert.equal(exhaustedRecovery.jobId,exhaustedStage.id);assert.equal(exhaustedRecovery.reason,'invalid_output');
 await api.reconcileFailedCandidateDependencies(env);assert.equal(calls,beforeExhausted);assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=? AND type='assign_roles'").get(exhausted.id).n,1);
 const denied=await prepare(4),blocked=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'denied-history',candidateId:denied.id,companyId:source.companyId,sourceId:'companySite'});sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='roles_pending' WHERE id=?").run(blocked.id);
 sqlite.prepare("UPDATE company_user_state SET favorite=0 WHERE user_name='Eugene'").run();const before=calls;assert.equal(await api.reconcileFailedCandidateDependencies(env),0);assert.equal(calls,before);assert.equal(sqlite.prepare('SELECT payload FROM scheduler_jobs WHERE id=?').get(blocked.id).payload,'{}');
 assert.equal(await api.reconcileFailedCandidateDependencies(env,{deadline:0}),0);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 // Legacy Rabota invalid_content is recovered only for the exact single body defect.
 sqlite.prepare("UPDATE company_user_state SET favorite=1 WHERE user_name='Eugene'").run();
 co.vacancySources.rabota='https://www.rabota.ru/company/fixture/vacancies/';sqlite.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').run(JSON.stringify(catalog));
 let settings=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['companySite','rabota'],dayBudgetUsd:1,monthBudgetUsd:5,revision:settings.revision},'Eugene');
 const bodyCandidate=await api.upsertVacancyCandidate(db,{companyId:source.companyId,sourceId:'rabota',titleRaw:'Project Manager',vacancyUrl:'https://www.rabota.ru/vacancy/123456/'});
 await api.classifyStoredCandidate(env,bodyCandidate.id);missingBody=true;
 await assert.rejects(()=>api.fetchCandidateMarkdown(env,bodyCandidate.id),e=>e.code==='rabota_incomplete_body');missingBody=false;
 const oldBody=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'legacy-body-v2',candidateId:bodyCandidate.id,companyId:source.companyId,sourceId:'rabota'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='invalid_content' WHERE id=?").run(oldBody.id);
 await api.reconcileFailedCandidateDependencies(env);let bodyJobs=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE candidate_id=? AND status='pending'").all(bodyCandidate.id);assert.equal(bodyJobs.length,1);assert.equal(bodyJobs[0].type,'fetch_full_text');
 await api.reconcileFailedCandidateDependencies(env);assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=? AND status='pending'").get(bodyCandidate.id).n,1);
 const bodyRun=await api.runDueTasks(env,{requestId:'recover-legacy-rabota-body',jobId:bodyJobs[0].id,limit:1});assert.equal(bodyRun.errors,0);assert.equal(bodyRun.succeeded,1);
 const bodySnapshot=sqlite.prepare("SELECT * FROM vacancy_raw_snapshots WHERE candidate_id=? AND content_status='valid'").get(bodyCandidate.id);assert.ok(bodySnapshot);
 assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(oldBody.id).status,'failed');assert.ok(JSON.parse(sqlite.prepare('SELECT payload FROM scheduler_jobs WHERE id=?').get(oldBody.id).payload).dependencyRecovery);
 const noMore=sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=?").get(bodyCandidate.id).n;await api.reconcileFailedCandidateDependencies(env);assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=?").get(bodyCandidate.id).n,noMore,'recovery is one-shot');
 console.log('Dependency recovery: queued/in-flight model change, exact current JEV stage, one safe import, historical failure preserved, repair with backlog, dedupe, empty decision, favorites and deadline guards passed.');
}finally{globalThis.fetch=originalFetch;f.close();}
