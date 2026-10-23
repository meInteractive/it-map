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
const originalFetch=globalThis.fetch;let calls=0,mode='normal',timeoutController;
const raw='# Выпускающий редактор\n\nКомпания Fixture. Удалённая работа.\n## Обязанности\n'+('Редактура материалов и управление редакцией. '.repeat(8))+'\n## Требования\nОпыт в журналистике.\n## Условия\nОт 300 000 рублей в месяц. На руки.';
const output=Object.fromEntries(api.EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(output,{title:'Выпускающий редактор',description_md:raw,level:'Middle',employer_name:'Fixture',work_format:'remote',salary_from:300000,salary_currency:'RUB',salary_period:'month',salary_tax_type:'net',language:'ru'});
globalThis.fetch=async(url,options={})=>{
 assert.equal(url,'https://openrouter.ai/api/v1/chat/completions','extraction never calls Firecrawl');calls++;const body=JSON.parse(options.body);assert.equal(body.response_format.type,'json_schema');assert.equal(body.response_format.json_schema.strict,true);assert.equal(body.provider.max_price.prompt,0);assert.ok(!JSON.stringify(body).includes('resumeMd'));assert.equal(JSON.parse(body.messages[1].content).markdown,raw);
 if(mode==='timeout'){timeoutController.abort(new DOMException('Synthetic deadline','TimeoutError'));throw timeoutController.signal.reason;}
 if(mode==='rate')return Response.json({error:{code:429}},{status:429});
 const content=mode==='invalid'?'{}':JSON.stringify(output);return Response.json({model:'fixture/free',choices:[{finish_reason:'stop',message:{content}}],usage:{prompt_tokens:200,completion_tokens:100,cost:0}});
};
try{
 await request('/api/classification/settings');const candidate=await api.upsertVacancyCandidate(db,{titleRaw:'Head of Engineering',vacancyUrl:'https://careers.company.org/jobs/42',companyNameRaw:'Fixture'});
 await assert.rejects(()=>api.fetchCandidateMarkdown(env,candidate.id),e=>e.code==='not_accepted');assert.equal(calls,0);
 const c=sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=?').get(candidate.id),roles=await repo.candidateClassificationContext(db);
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,outcome,classified_at) VALUES('fixture',?,?,?,?,?,?,'[]','complete','2026-10-15')").run(c.id,c.title_raw,c.title_hash,c.title_revision,api.JEV_MODEL_ID,api.JEV_MODEL_ID);
 for(const r of roles)sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(?,'fixture',?,?,'match',?,?,?,?,?,?,'2026-10-15')").run(c.id,r.roleId,r.roleName||r.name,r.modelKey,r.modelKey,r.promptVersion,r.revision,c.title_hash,c.title_revision);

 const at=new Date().toISOString();sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('raw',?,?,?,?,?,?,'free','valid','{}',?)").run(c.id,c.vacancy_url,c.vacancy_url,raw,await repo.candidateHash(raw),at,at);
 await api.orSettings(env);const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-EXTRACTION');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=?,cache_json=?,models_synced_at=? WHERE id=1').run(encrypted,JSON.stringify([{id:'fixture/free',name:'Fixture',canonical_slug:'fixture/free',supports_structured_outputs:true}]),at);
 // Explicit legacy-provider fixture; production defaults are Luna.
 await api.savePromptModel(db,'vacancy_extraction',{provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'medium',revision:0});
 const first=await api.extractCandidateMarkdown(env,c.id);assert.equal(first.extraction.cost_usd,0);assert.equal(first.extraction.model_id,'fixture/free');assert.equal(calls,1);assert.equal((await api.vacancyDetailState(db,c.id)).readyForImport,true);
 assert.equal((await api.extractCandidateMarkdown(env,c.id)).reused,true);assert.equal(calls,1);
 const p=await(await request('/api/profile/prompts')).json();assert.ok(p.defaults.vacancy_extraction);assert.match(p.prompts.vacancy_extraction.markdown,/Не назначай дочерние роли/);
 const saved=await api.saveSystemPrompt(db,'vacancy_extraction',{markdown:p.prompts.vacancy_extraction.markdown+'\nНе добавляй факты.',revision:0});assert.equal(saved.revision,1);await assert.rejects(()=>api.saveSystemPrompt(db,'vacancy_extraction',{markdown:'stale',revision:0}),e=>e.status===409);
 assert.equal((await api.vacancyDetailState(db,c.id)).readyForImport,false);mode='invalid';await assert.rejects(()=>api.extractCandidateMarkdown(env,c.id),e=>e.code==='invalid_output');assert.equal(calls,2);assert.equal((await api.vacancyDetailState(db,c.id)).llmStatus,'error');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_extractions').get().n,2);mode='normal';await api.extractCandidateMarkdown(env,c.id);assert.equal(calls,3);assert.equal((await api.vacancyDetailState(db,c.id)).readyForImport,true);
 await api.savePromptModel(db,'vacancy_extraction',{provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'low',revision:1});assert.equal((await api.vacancyDetailState(db,c.id)).readyForImport,false);await api.extractCandidateMarkdown(env,c.id);assert.equal(calls,4);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_raw_snapshots').get().n,1);
 const promptBeforeTimeout=await(await request('/api/profile/prompts')).json();await api.saveSystemPrompt(db,'vacancy_extraction',{markdown:promptBeforeTimeout.prompts.vacancy_extraction.markdown+'\nTimeout fixture.',revision:1});
 mode='timeout';timeoutController=new AbortController();await assert.rejects(()=>api.extractCandidateMarkdown(env,c.id,{signal:timeoutController.signal}),e=>e.name==='TimeoutError');
 assert.equal(sqlite.prepare("SELECT error_code FROM vacancy_extractions WHERE status='error' ORDER BY rowid DESC LIMIT 1").get().error_code,'timeout');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_raw_snapshots').get().n,1);
 await assert.rejects(()=>api.extractCandidateMarkdown(env,c.id),e=>e.code==='detail_busy','uncertain request keeps its lease until cooldown');sqlite.prepare('UPDATE vacancy_detail_leases SET expires_at=1').run(); // isolated fixture: cooldown elapsed
 mode='normal';await api.extractCandidateMarkdown(env,c.id);assert.equal((await api.vacancyDetailState(db,c.id)).readyForImport,true,'retry reuses saved Markdown after timeout');
 const dateSchema=api.EXTRACTION_SCHEMA.properties.published_at;assert.match(dateSchema.description,/YYYY-MM-DD/);assert.match(dateSchema.description,/null/);const datePattern=new RegExp(dateSchema.pattern);assert.equal(datePattern.test('2026-10-17'),true);for(const date of ['17 октября','17.10.2026','2026-10-17T12:00:00Z','2026-13-01','2026-00-01','2026-01-32'])assert.equal(datePattern.test(date),false,date);
 for(const date of ['17 октября','2026-02-30'])assert.throws(()=>api.validateVacancyExtraction(JSON.stringify({...output,published_at:date}),raw),e=>e.code==='invalid_output');
 const dated=api.validateVacancyExtraction(JSON.stringify({...output,published_at:'2026-10-17'}),raw+'\nДата публикации: 2026-10-17');assert.equal(dated.published_at,'2026-10-17');
 assert.equal(api.EXTRACTION_SCHEMA_VERSION,2);assert.equal(api.EXTRACTION_SCHEMA.properties.level.type,'string');
 assert.deepEqual([...api.EXTRACTION_SCHEMA.properties.level.enum].sort(),['Head','Lead','Middle','Senior']);
 for(const level of [null,'','senior','Expert','Старший','Senior, Lead'])assert.throws(()=>api.validateVacancyExtraction(JSON.stringify({...output,level}),raw),e=>e.code==='invalid_output');
 for(const level of ['Middle','Senior','Lead','Head'])assert.equal(api.validateVacancyExtraction(JSON.stringify({...output,level}),raw).level,level);
 assert.equal(JSON.parse(first.extraction.extracted_data).description_md,raw,'source wording remains unchanged in the structured result');
 const valid=api.validateVacancyExtraction(JSON.stringify(output),raw);assert.equal(valid.salary_to,null);
 for(const change of [{salary_from:'300000'},{salary_from:900000},{salary_tax_type:'gross'},{work_format:'hybrid'},{published_at:'2026-10-15'},{salary_to:1},{url:'https://invented.org'},{description_md:''}])assert.throws(()=>api.validateVacancyExtraction(JSON.stringify({...output,...change}),raw));
 const normalized=api.normalizeVacancyExtraction(JSON.stringify({...output,salary_from:null,salary_to:null,salary_currency:null,salary_tax_type:null,salary_period:'month',work_format:'hybrid',published_at:'2026-10-01'}),'# Full vacancy\n'+('Описание обязанностей и требований. '.repeat(10)));
 assert.equal(normalized.data.salary_period,null);assert.equal(normalized.data.work_format,null);assert.equal(normalized.data.published_at,null);assert.deepEqual(normalized.corrections.map(c=>c.field).sort(),['published_at','salary_period','work_format']);assert.ok(normalized.data.description_md.length>100);assert.throws(()=>api.normalizeVacancyExtraction(JSON.stringify({...output,salary_from:999999}),raw),e=>e.code==='invalid_output');
 const examples=[['От 300 000 рублей',300000,null,'RUB',null],['250 000–400 000 рублей',250000,400000,'RUB',null],['Зарплата не указана',null,null,null,null],['От 5 000 долларов',5000,null,'USD',null],['От 300 000 рублей до вычета налогов',300000,null,'RUB','gross'],['От 300 000 рублей на руки',300000,null,'RUB','net']];
 for(const [text,from,to,currency,tax] of examples)api.validateVacancyExtraction(JSON.stringify({...output,salary_from:from,salary_to:to,salary_currency:currency,salary_tax_type:tax,salary_period:null,work_format:null}),text);
 sqlite.prepare('UPDATE vacancy_role_classification SET markdown=markdown||?').run(' Changed');assert.equal((await api.vacancyDetailState(db,c.id)).readyForImport,false);const before=calls;await assert.rejects(()=>api.extractCandidateMarkdown(env,c.id),e=>e.code==='not_accepted');assert.equal(calls,before);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);
 console.log('Extraction: shared free LLM/strict JSON, editor default/CAS, prompt/model/raw cache keys, valid readiness, errors/history/retry without Firecrawl, salary/tax/date/format checks, editor role and no main writes passed');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
