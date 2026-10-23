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
const originalFetch=globalThis.fetch;let calls=0,mode='normal';
const raw='# Выпускающий редактор\n\nКомпания Fixture. Удалённая работа.\n## Обязанности\n'+('Редактура материалов и управление редакцией. '.repeat(8))+'\n## Требования\nОпыт в журналистике.\n## Условия\nОт 300 000 рублей в месяц. На руки.';
const output=Object.fromEntries(api.EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(output,{title:'Выпускающий редактор',description_md:raw,level:'Middle',employer_name:'Fixture',work_format:'remote',salary_from:300000,salary_currency:'RUB',salary_period:'month',salary_tax_type:'net',language:'ru'});
globalThis.fetch=async(url,options={})=>{assert.equal(url,'https://openrouter.ai/api/v1/chat/completions');calls++;const body=JSON.parse(options.body),input=JSON.parse(body.messages[1].content);assert.equal(input.markdown,raw);assert.ok(input.allowedRoles.every(r=>Number.isSafeInteger(r.id)&&r.path.includes(' → ')));assert.equal(body.provider.max_price.prompt,0);let ids=mode==='empty'?[]:mode==='invalid'?[999999]:input.allowedRoles.slice(0,2).map(r=>r.id);return Response.json({model:'fixture/free',choices:[{finish_reason:'stop',message:{content:JSON.stringify({role_ids:ids})}}],usage:{cost:0,prompt_tokens:100,completion_tokens:10}});};
try{
 await request('/api/classification/settings');const candidate=await api.upsertVacancyCandidate(db,{titleRaw:'Head of Engineering',vacancyUrl:'https://careers.company.org/jobs/42',companyNameRaw:'Fixture'});
 await assert.rejects(()=>api.fetchCandidateMarkdown(env,candidate.id),e=>e.code==='not_accepted');assert.equal(calls,0);
 const c=sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=?').get(candidate.id),roles=await repo.candidateClassificationContext(db);
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,outcome,classified_at) VALUES('fixture',?,?,?,?,?,?,'[]','complete','2026-10-15')").run(c.id,c.title_raw,c.title_hash,c.title_revision,api.JEV_MODEL_ID,api.JEV_MODEL_ID);
 for(const r of roles)sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(?,'fixture',?,?,'match',?,?,?,?,?,?,'2026-10-15')").run(c.id,r.roleId,r.roleName||r.name,r.modelKey,r.modelKey,r.promptVersion,r.revision,c.title_hash,c.title_revision);

 const at=new Date().toISOString();sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('raw',?,?,?,?,?,?,'free','valid','{}',?)").run(c.id,c.vacancy_url,c.vacancy_url,raw,await repo.candidateHash(raw),at,at);
 await api.orSettings(env);const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','SYNTHETIC-EXTRACTION');sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=?,cache_json=?,models_synced_at=? WHERE id=1').run(encrypted,JSON.stringify([{id:'fixture/free',name:'Fixture',canonical_slug:'fixture/free',supports_structured_outputs:true}]),at);

 const x=await api.extractionContext(db);sqlite.prepare("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,extracted_data,processed_at,duration_ms,status) VALUES('extraction',?,'raw','openrouter','fixture/free',?,?,?,?,?,1,'success')").run(c.id,x.configHash,x.promptVersion,x.schemaVersion,JSON.stringify(output),at);
 // Explicit legacy-provider fixture; production defaults are Luna.
 await api.savePromptModel(db,'vacancy_role',{provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'medium',revision:0});
 const first=await api.analyzeVacancyLeafRoles(env,c.id);assert.equal(first.roleIds.length,2);assert.equal(first.result.cost_usd,0);assert.equal((await api.analyzeVacancyLeafRoles(env,c.id)).reused,true);assert.equal(calls,1);
 const ctx=await api.leafRoleContext(db,c.id);assert.ok(first.roleIds.every(id=>ctx.allowed.some(r=>r.id===id)));assert.throws(()=>api.validateLeafRoleIds(JSON.stringify({role_ids:[roles[0].roleId]}),ctx.allowed));assert.throws(()=>api.validateLeafRoleIds(JSON.stringify({role_ids:[9999]}),ctx.allowed));assert.throws(()=>api.validateLeafRoleIds(JSON.stringify({role_ids:[],extra:true}),ctx.allowed));
 const settings=await(await request('/api/profile/prompts')).json();assert.match(settings.prompts.vacancy_role.markdown,/Не выбирай родительские категории/);assert.equal(settings.maxLengths.vacancy_role,5000);await api.saveSystemPrompt(db,'vacancy_role',{markdown:settings.prompts.vacancy_role.markdown+'\nНе придумывай.',revision:0});mode='empty';const empty=await api.analyzeVacancyLeafRoles(env,c.id);assert.deepEqual(empty.roleIds,[]);await api.analyzeVacancyLeafRoles(env,c.id);assert.equal(calls,2,'empty is durable, not infinite retries');
 const leaf=ctx.allowed[0];sqlite.prepare('UPDATE vacancy_roles SET name=name||? WHERE id=?').run(' renamed',leaf.id);mode='invalid';await assert.rejects(()=>api.analyzeVacancyLeafRoles(env,c.id),e=>e.code==='invalid_output');assert.equal(calls,3);mode='normal';await api.analyzeVacancyLeafRoles(env,c.id);assert.equal(calls,4,'tree rename invalidates cached selection');
 const backup=await api.createBackup(db);assert.equal(backup.version,39);await api.restoreBackup(db,backup);assert.equal((await api.analyzeVacancyLeafRoles(env,c.id)).reused,true);assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);
 console.log('Leaf roles: shared free structured LLM, dynamic multi-root/deep leaves, ID validation, empty cache, prompt/CAS/tree invalidation, history/usage/backup33, no Firecrawl and no main writes passed');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
