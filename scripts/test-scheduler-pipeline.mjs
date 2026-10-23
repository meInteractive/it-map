import assert from 'node:assert/strict';import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,oldFetch=globalThis.fetch;let scans=0,texts=0,jev=0,llm=0,noLeafRoles=false;
const raw='# Head of Engineering\n\n## Обязанности\n'+('Развитие команды и управление разработкой. '.repeat(9))+'\n## Требования\nОпыт руководства командой.\n## Условия\nУдалённая работа.';
const extracted=Object.fromEntries(api.EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(extracted,{title:'Head of Engineering',description_md:raw,level:'Head',work_format:'remote'});
globalThis.fetch=async(url,options={})=>{
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});if(options.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('credit-usage'))return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 if(url.endsWith('/scrape')){const b=JSON.parse(options.body),full=b.formats.length===1;full?texts++:scans++;return Response.json({success:true,data:{markdown:full?raw:'[Head of Engineering](/vacancies/42)',links:full?[]:['/vacancies/42'],metadata:{statusCode:200,url:b.url,creditsUsed:1}}});}
 if(url.includes('models?output_modalities'))return Response.json({data:[{id:api.JEV_MODEL_ID,context_length:32000,pricing:{prompt:'0.000000042',completion:'0',request:'0'}}]});
 if(url.endsWith('/decisions')){jev++;const b=JSON.parse(options.body);return Response.json({model:api.JEV_MODEL_ID,answers:Object.fromEntries(Object.keys(b.questions).map(k=>[k,{type:'choice',choice:'match'}])),usage:{cost:.00002}});}
 if(url.endsWith('/chat/completions')){llm++;const b=JSON.parse(options.body),input=JSON.parse(b.messages.at(-1).content),roles=input.allowedRoles;assert.equal(b.provider.max_price.prompt,0);return Response.json({model:'fixture/free',choices:[{finish_reason:'stop',message:{content:JSON.stringify(roles?{role_ids:noLeafRoles?[]:[roles[0].id]}:extracted)}}],usage:{cost:0,prompt_tokens:100,completion_tokens:100}});}
 throw new Error('Unexpected outbound: '+url);
};
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const company=catalog.companies[0].name;catalog.companies[0].vacancySources={companySite:'https://careers.fixture.org/jobs'};sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(company);sqlite.prepare("INSERT OR REPLACE INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(company);sqlite.prepare('INSERT OR IGNORE INTO company_vacancy_state(company_name) VALUES(?)').run(company);
 await api.candidateClassificationContext(db);await api.orSettings(env);const cipher=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-OR-KEY');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=?,cache_json=?,models_synced_at=? WHERE id=1').run(cipher,JSON.stringify([{id:'fixture/free',name:'Fixture',canonical_slug:'fixture/free',supports_structured_outputs:true}]),new Date().toISOString());
 let fc=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC-FREE-KEY',revision:fc.revision});fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 // Explicit legacy-provider fixture; production defaults are Luna.
 await api.savePromptModel(db,'vacancy_extraction',{provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'medium',revision:0});
 await api.savePromptModel(db,'vacancy_role',{provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'medium',revision:0});
 const source=(await api.readVacancyParsingSources(db)).find(s=>s.sourceId==='companySite');await api.saveVacancyParsingSource(db,source.companyId,source.sourceId,{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});sqlite.prepare('UPDATE vacancy_source_parsing SET auto_enabled=1,interval_days=1,next_run_at=1 WHERE company_id=?').run(source.companyId);
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,dayBudgetUsd:1,monthBudgetUsd:5,sourceIds:['companySite'],intervalDays:1,revision:s.revision},'Eugene');
 let cycle=await api.runDueTasks(env,{requestId:'end-to-end'});assert.equal(cycle.errors,0,JSON.stringify(sqlite.prepare('SELECT type,reason_code,last_error FROM scheduler_jobs').all()));
 assert.deepEqual([scans,texts,jev,llm],[1,1,1,2]);assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,1,'automatic import without confirmation');
 const main=sqlite.prepare('SELECT * FROM company_vacancies').get(),candidate=sqlite.prepare('SELECT * FROM vacancy_candidates').get();assert.equal(candidate.main_vacancy_id,main.id);assert.ok(sqlite.prepare('SELECT next_run_at FROM vacancy_source_parsing').get().next_run_at>Date.now());
 assert.equal(main.level,'Head');assert.equal(main.description_md,raw,'import keeps the original text and canonical level');
 const before=[scans,texts,jev,llm];await api.runDueTasks(env,{requestId:'again'});assert.deepEqual([scans,texts,jev,llm],before,'unchanged stages are cached');assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,1);
 // A valid empty leaf selection terminates without an import/error/retry loop.
 noLeafRoles=true;
 const filtered=await api.upsertVacancyCandidate(db,{companyId:source.companyId,sourceId:'companySite',titleRaw:'Head of Engineering',vacancyUrl:'https://careers.fixture.org/vacancies/43'});
 await api.enqueueNextStage(env,filtered.id);
 const emptyCycle=await api.runDueTasks(env,{requestId:'empty-leaf-roles'});assert.equal(emptyCycle.errors,0);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,1);
 assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE candidate_id=? AND type IN ('import_vacancy','score_relevance')").get(filtered.id).n,0);
 assert.equal(await api.nextCandidateStage(env,filtered.id),null);
 const emptyResult=sqlite.prepare("SELECT result_json FROM scheduler_attempts a JOIN scheduler_jobs j ON j.id=a.job_id WHERE j.candidate_id=? AND a.type='assign_roles'").get(filtered.id);
 assert.equal(JSON.parse(emptyResult.result_json).filtered,true);
 // Title-only ambiguity must not stop unattended import or merge publications.
 noLeafRoles=false;
 const separate=await api.upsertVacancyCandidate(db,{companyId:source.companyId,sourceId:'companySite',titleRaw:'Head of Engineering',vacancyUrl:'https://careers.fixture.org/vacancies/44'});
 await api.classifyStoredCandidate(env,separate.id);await api.fetchCandidateMarkdown(env,separate.id);await api.extractCandidateMarkdown(env,separate.id);await api.analyzeVacancyLeafRoles(env,separate.id);
 assert.equal((await api.prepareVacancyImport(env,separate.id)).status,'possible_duplicate','manual preview still asks for a resolution');
 const oldImport=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'historical-title-only',companyId:source.companyId,candidateId:separate.id,sourceId:'companySite'});
 sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='possible_duplicate' WHERE id=?").run(oldImport.id);
 const separateRun=await api.runDueTasks(env,{requestId:'automatic-title-only'});assert.equal(separateRun.errors,0);
 const separateMain=sqlite.prepare('SELECT main_vacancy_id FROM vacancy_candidates WHERE id=?').get(separate.id).main_vacancy_id;
 assert.ok(separateMain&&separateMain!==main.id,'unconfirmed publication creates a separate main');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,2);
 assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(oldImport.id).status,'failed','old failure remains audit history');
 const importAttempt=JSON.parse(sqlite.prepare("SELECT a.result_json FROM scheduler_attempts a JOIN scheduler_jobs j ON j.id=a.job_id WHERE j.candidate_id=? AND a.type='import_vacancy' AND a.status='completed'").get(separate.id).result_json);
 assert.equal(importAttempt.duplicateResolution,'separate');assert.deepEqual(importAttempt.possibleDuplicateIds,[main.id]);
 const audit=JSON.parse(sqlite.prepare("SELECT changes_json FROM vacancy_import_operations WHERE candidate_id=? AND status='success'").get(separate.id).changes_json);assert.deepEqual(audit.resolution,{createNew:true});
 await api.runDueTasks(env,{requestId:'automatic-title-only-replay'});assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,2,'retry does not create another card');
 const stopped=[scans,texts,jev,llm];await api.runDueTasks(env,{requestId:'empty-cached'});assert.deepEqual([scans,texts,jev,llm],stopped);
 // Revising the role prompt still invalidates the cached no-match normally.
 sqlite.prepare("UPDATE vacancy_role_settings SET markdown=markdown||' Updated',prompt_revision=prompt_revision+1 WHERE id=1").run();
 assert.equal((await api.nextCandidateStage(env,filtered.id)).type,'assign_roles');
 // Restore fixture prompt so the first candidate remains cached for scope tests.
 sqlite.prepare("UPDATE vacancy_role_settings SET markdown=substr(markdown,1,length(markdown)-8),prompt_revision=prompt_revision-1 WHERE id=1").run();
 before.splice(0,before.length,...[scans,texts,jev,llm]);
 sqlite.prepare('UPDATE company_vacancies SET title=? WHERE id=?').run('Моя ручная правка',main.id);sqlite.prepare("UPDATE company_user_state SET favorite=0 WHERE user_name='Eugene'").run();
 sqlite.prepare('UPDATE vacancy_source_parsing SET next_run_at=1').run();await api.runDueTasks(env,{requestId:'nonfavorite'});assert.deepEqual([scans,texts,jev,llm],before);assert.equal(sqlite.prepare('SELECT title FROM company_vacancies').get().title,'Моя ручная правка');
 const denied=await api.enqueueScheduledJob(db,{type:'import_vacancy',key:'excluded-import',companyId:source.companyId,candidateId:candidate.id});await api.runDueTasks(env,{requestId:'denied-import'});assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(denied.id).status,'cancelled','pending automatic import rechecks favorites');
 assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);assert.ok(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='import_vacancy' AND status='completed'").get().n>0);
 console.log('Scheduler pipeline: known source→staging→JEV→Markdown→structured free LLM→leaf role→atomic automatic main import; six stages, shared Free policy, budgets, repeat/no new credits/dedupe, next source period and personal favorite exclusion/manual title preservation passed');
}finally{globalThis.fetch=oldFetch;f.close();}
