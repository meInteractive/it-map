import assert from 'node:assert/strict';import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,oldFetch=globalThis.fetch;
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company='Same ID company',agency='Agency Fixture';
catalog.companies=[{...catalog.companies[0],name:company,vacancySources:{}}];catalog.agencies=[{...(catalog.agencies?.[0]||{}),name:agency,vacancySources:{companySite:'https://agency.fixture.org/jobs'}}];
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
sqlite.prepare('INSERT INTO company_identity(id,company_name) VALUES(77,?)').run(company);sqlite.prepare('INSERT INTO agency_identity(id,agency_name) VALUES(77,?)').run(agency);
sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(agency);
sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(agency);
const ref={agencyId:77},raw='# Head of Engineering\n\n## Обязанности\n'+('Управление несколькими командами, стратегия развития направления. '.repeat(9))+'\n## Требования\nОпыт руководства командами.\n## Условия\nУдалённая работа.';
const extracted=Object.fromEntries(api.EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(extracted,{title:'Head of Engineering',description_md:raw,level:'Head',work_format:'remote'});
let scans=0,texts=0,jev=0,llm=0,disableDuringText=false,disableDuringListing=false;
globalThis.fetch=async(url,options={})=>{
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]});if(options.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('credit-usage'))return Response.json({success:true,data:{remainingCredits:1000,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 if(url.endsWith('/scrape')){const b=JSON.parse(options.body),full=b.formats.length===1;full?texts++:scans++;if(full&&disableDuringText||!full&&disableDuringListing)sqlite.prepare("UPDATE firecrawl_settings SET scheduler_json=json_set(scheduler_json,'$.agenciesEnabled',json('false'))").run();return Response.json({success:true,data:{markdown:full?raw:'[Head of Engineering](/vacancies/42)',links:full?[]:['/vacancies/42'],metadata:{statusCode:200,url:b.url,creditsUsed:1}}});}
 if(url.includes('models?output_modalities'))return Response.json({data:[{id:api.JEV_MODEL_ID,context_length:32000,pricing:{prompt:'0.000000042',completion:'0',request:'0'}}]});
 if(url.endsWith('/decisions')){jev++;const b=JSON.parse(options.body);return Response.json({model:api.JEV_MODEL_ID,answers:Object.fromEntries(Object.keys(b.questions).map(k=>[k,{type:'choice',choice:'match'}])),usage:{cost:.00002}});}
 if(url.endsWith('/chat/completions')){llm++;const b=JSON.parse(options.body),input=JSON.parse(b.messages.at(-1).content);return Response.json({model:'fixture/free',choices:[{finish_reason:'stop',message:{content:JSON.stringify(input.allowedRoles?{role_ids:[input.allowedRoles[0].id]}:extracted)}}],usage:{cost:0,prompt_tokens:100,completion_tokens:100}});}
 throw new Error('Unexpected outbound '+url);
};
const settings=()=>api.schedulerSettings(db),save=async patch=>{const s=await settings();return api.saveSchedulerSettings(db,{enabled:s.enabled,dayBudgetUsd:s.dayBudgetUsd,monthBudgetUsd:s.monthBudgetUsd,revision:s.revision,...patch},'Eugene');};
try{
 assert.equal((await settings()).agenciesEnabled,false);
 await api.candidateClassificationContext(db);await api.orSettings(env);const cipher=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-AGENCY-KEY');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=?,cache_json=?,models_synced_at=? WHERE id=1').run(cipher,JSON.stringify([{id:'fixture/free',name:'Fixture',canonical_slug:'fixture/free',supports_structured_outputs:true}]),new Date().toISOString());
 let fc=await api.firecrawlSettings(db);await api.saveFirecrawlKey(env,{apiKey:'fc-SYNTHETIC-AGENCY-KEY',revision:fc.revision});fc=await api.firecrawlSettings(db);await api.saveFirecrawlSettings(db,{enabled:true,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:fc.revision});
 for(const kind of ['vacancy_extraction','vacancy_role'])await api.savePromptModel(db,kind,{provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'medium',revision:0});
 await save({enabled:true,sourceIds:['companySite'],favoritesOnly:true,dayBudgetUsd:5,monthBudgetUsd:100});
 const off=await api.readVacancyParsingSource(db,ref,'companySite');assert.equal(off.agencyId,77);assert.equal(off.companyId,null);assert.equal(off.supported,false);
 await assert.rejects(()=>api.fetchVacancyListing(env,ref,'companySite',{userName:'Eugene'}),e=>e.status===403);
 await api.runDueTasks(env,{requestId:'agency-default-off'});assert.deepEqual([scans,texts,jev,llm],[0,0,0,0]);
 await save({agenciesEnabled:true});const result=await api.runDueTasks(env,{requestId:'agency-on'});assert.equal(result.errors,0,JSON.stringify(sqlite.prepare('SELECT type,reason_code,last_error FROM scheduler_jobs').all()));
 const main=sqlite.prepare('SELECT * FROM company_vacancies').get(),candidate=sqlite.prepare('SELECT * FROM vacancy_candidates').get();assert.ok(main);assert.equal(main.entity_type,'agency');assert.equal(main.company_name,agency);assert.equal(main.level,'Head');assert.equal(main.description_md,raw);assert.equal(candidate.company_id,null);assert.equal(candidate.agency_id,77);assert.equal(candidate.main_vacancy_id,main.id);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies WHERE entity_type=\'company\'').get().n,0);assert.ok(sqlite.prepare('SELECT count(*) n FROM scheduler_jobs WHERE agency_id=77 AND company_id IS NULL').get().n>=6);
 await assert.rejects(()=>api.upsertVacancyCandidate(db,{companyId:77,titleRaw:'Wrong owner',vacancyUrl:candidate.vacancy_url,sourceId:'companySite'},{preserveCompany:true}),e=>e.code==='candidate_identity');assert.equal(sqlite.prepare('SELECT title_raw FROM vacancy_candidates WHERE id=?').get(candidate.id).title_raw,'Head of Engineering');
 const counts=[scans,texts,jev,llm];await save({agenciesEnabled:false});sqlite.prepare('UPDATE vacancy_source_parsing SET next_run_at=1 WHERE agency_id=77').run();await api.runDueTasks(env,{requestId:'agency-disabled-again'});assert.deepEqual([scans,texts,jev,llm],counts);
 // Pending agency jobs remain pending while the switch is off, then resume.
 const pending=await api.enqueueScheduledJob(db,{type:'scan_source',key:'agency-pending',agencyId:77,sourceId:'companySite',payload:{manual:true,sourceRevision:1,sourceUrl:catalog.agencies[0].vacancySources.companySite}});await api.runDueTasks(env,{requestId:'agency-pending-off'});assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(pending.id).status,'pending');assert.deepEqual([scans,texts,jev,llm],counts);
 await save({agenciesEnabled:true});await api.runDueTasks(env,{requestId:'agency-resume'});assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(pending.id).status,'completed');assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,1);
 // OFF during a scheduled provider call pauses the owned job instead of exhausting it.
 const inFlight=await api.enqueueScheduledJob(db,{type:'scan_source',key:'agency-off-in-flight',agencyId:77,sourceId:'companySite',payload:{manual:true,sourceRevision:1,sourceUrl:catalog.agencies[0].vacancySources.companySite}});disableDuringListing=true;
 await api.runDueTasks(env,{requestId:'agency-off-during-scan',jobId:inFlight.id,limit:1});const paused=sqlite.prepare('SELECT status,reason_code,attempts FROM scheduler_jobs WHERE id=?').get(inFlight.id);assert.equal(paused.status,'pending');assert.equal(paused.reason_code,'agencies_disabled');assert.equal(paused.attempts,0);disableDuringListing=false;assert.equal(sqlite.prepare('SELECT count(*) n FROM firecrawl_request_lease').get().n,0,'switch OFF must release its global listing lease without clearing billing reserves');await save({agenciesEnabled:true});
 // Isolated fixture: advance the uncertainty/rate window; do not clear usage or reserved credits.
 sqlite.prepare('UPDATE firecrawl_usage_events SET started_at=started_at-600000,active_until=1').run();
 // Manual fulltext cannot commit a provider result after the agency switch changes.
 const c2=await api.upsertVacancyCandidate(db,{agencyId:77,titleRaw:'Head of Engineering',vacancyUrl:'https://agency.fixture.org/vacancies/43',sourceId:'companySite'});await api.classifyStoredCandidate(env,c2.id);disableDuringText=true;await assert.rejects(()=>api.fetchCandidateMarkdown(env,c2.id),e=>e.code==='agencies_disabled');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_raw_snapshots WHERE candidate_id=?').get(c2.id).n,0);disableDuringText=false;
 const backup=await api.createBackup(db);assert.equal(backup.version,39);await api.restoreBackup(db,backup);assert.equal(sqlite.prepare('SELECT agency_id FROM vacancy_candidates WHERE id=?').get(candidate.id).agency_id,77);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Agency parser: OFF default/no requests, same numeric company/agency IDs isolated, automatic Firecrawl→JEV→structured→roles→agency import, pending pause/resume, manual in-flight OFF fence, original text/level/backup38 passed');
}finally{globalThis.fetch=oldFetch;f.close();}
