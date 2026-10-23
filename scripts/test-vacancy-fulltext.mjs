import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?firecrawl-settings'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);



const repo=await moduleFor('server/repositories/vacancy-candidates.js');
const originalFetch=globalThis.fetch;let paid=0,balance=1000,raw='# Head of Engineering\n\n## Обязанности\n'+('Управление командой разработки и развитие процессов. '.repeat(8))+'\n## Требования\nОпыт управления командой.\n## Условия\nОт 300 000 рублей в месяц. На руки. Удалённая работа.';
globalThis.fetch=async(url,options={})=>{
 if(url.includes('cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'93.184.215.14'}]});
 if(options.method==='HEAD')return new Response(null,{status:200});
 if(url.endsWith('credit-usage'))return Response.json({success:true,data:{remainingCredits:balance,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');paid++;const body=JSON.parse(options.body);assert.deepEqual(body.formats,['markdown']);assert.equal(body.onlyMainContent,true);assert.equal(body.onlyCleanContent,false);assert.equal(body.checkPromptInjection,false);assert.equal(body.proxy,'basic');assert.deepEqual(body.parsers,[]);
 return Response.json({success:true,data:{markdown:raw,metadata:{statusCode:200,url:body.url,creditsUsed:1}}});
};
const count=table=>sqlite.prepare('SELECT count(*) n FROM '+table).get().n;
try{
 let s=await(await request('/api/firecrawl/settings')).json();s=await(await request('/api/firecrawl/key','PUT',{apiKey:'fc-SYNTHETIC-DETAILS',revision:s.revision})).json();await request('/api/firecrawl/settings','PUT',{enabled:true,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:s.revision});
 await request('/api/classification/settings');const candidate=await api.upsertVacancyCandidate(db,{titleRaw:'Head of Engineering',vacancyUrl:'https://careers.company.org/jobs/42',companyNameRaw:'Fixture'});
 await assert.rejects(()=>api.fetchCandidateMarkdown(env,candidate.id),e=>e.code==='not_accepted');assert.equal(paid,0);
 const c=sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=?').get(candidate.id),roles=await repo.candidateClassificationContext(db);
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,outcome,classified_at) VALUES('fixture',?,?,?,?,?,?,'[]','complete','2026-10-15')").run(c.id,c.title_raw,c.title_hash,c.title_revision,api.JEV_MODEL_ID,api.JEV_MODEL_ID);
 for(const r of roles)sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(?,'fixture',?,?,'match',?,?,?,?,?,?,'2026-10-15')").run(c.id,r.roleId,r.roleName||r.name,r.modelKey,r.modelKey,r.promptVersion,r.revision,c.title_hash,c.title_revision);
 const first=await api.fetchCandidateMarkdown(env,c.id);assert.equal(paid,1);assert.equal(await api.snapshotMarkdown(db,first.snapshot),raw);assert.equal(first.snapshot.content_hash,await repo.candidateHash(raw));
 assert.equal((await api.fetchCandidateMarkdown(env,c.id)).reused,true);assert.equal(paid,1,'multiple matches still one fetch');
 await assert.rejects(()=>api.fetchCandidateMarkdown(env,c.id,{refresh:true}));assert.equal(paid,1);
 await api.fetchCandidateMarkdown(env,c.id,{refresh:true,confirmed:true});assert.equal(paid,2);assert.equal(count('vacancy_raw_snapshots'),1,'identical refresh audits cost but preserves version');
 raw+='\n'+('Описание условий: 😀 исходные данные. '.repeat(9000))+'\nХВОСТ НЕ ОБРЕЗАН';
 const large=await api.fetchCandidateMarkdown(env,c.id,{refresh:true,confirmed:true});assert.equal(large.snapshot.markdown_raw,null);assert.ok(count('vacancy_raw_snapshot_parts')>1);assert.equal(await api.snapshotMarkdown(db,large.snapshot),raw,'large Unicode original survives exact reconstruction');assert.equal(count('vacancy_raw_snapshots'),2);
 sqlite.prepare("INSERT INTO vacancy_detail_settings(id,markdown) VALUES(1,'Fixture prompt')").run();sqlite.prepare("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,extracted_data,processed_at,duration_ms,status) VALUES('fixture',?,?,'openrouter','fixture','hash','prompt',1,'{}','2026-10-15',7,'success')").run(c.id,large.snapshot.id);
 const backup=await api.createBackup(db);assert.equal(backup.version,39);for(const name of ['vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions'])assert.ok(backup.storage.tables.find(t=>t.name===name).rows.length,name+' backed up');await api.restoreBackup(db,backup);assert.equal(await api.snapshotMarkdown(db,large.snapshot),raw);assert.equal(count('vacancy_content_fetches'),3);assert.equal(count('vacancy_extractions'),1);
 balance=3;const before=paid;await assert.rejects(()=>api.fetchCandidateMarkdown(env,c.id,{refresh:true,confirmed:true}),e=>e.code==='free_exhausted');assert.equal(paid,before);
 const currentS=await api.firecrawlSettings(db);sqlite.prepare('UPDATE firecrawl_settings SET enabled=0,revision=revision+1').run();assert.equal((await api.fetchCandidateMarkdown(env,c.id)).reused,true);assert.equal(paid,before,'cached Markdown works with Firecrawl disabled');
 for(const [text,meta] of [['# CAPTCHA'+raw,{}],[raw,{targetStatus:404}],[raw,{finalUrl:'https://careers.company.org/jobs'}]])assert.equal(api.checkVacancyMarkdown(text,{sourceUrl:c.vacancy_url,finalUrl:c.vacancy_url,targetStatus:200,...meta}).valid,false);
 assert.equal(count('company_vacancies'),0);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Fulltext: accepted gate, shared Free cap, Markdown-only basic scrape, cache/confirmed refresh, immutable hash versions, large Unicode/backup32, invalid content and no main writes passed');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
