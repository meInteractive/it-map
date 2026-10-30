// Actual extraction service работает только на SQLite :memory: и mock fetch.
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {extractCandidateMarkdown} from '../server/services/vacancy-extraction.js';
import {candidateHash} from '../server/repositories/vacancy-candidates.js';
import {EXTRACTION_SCHEMA} from '../server/services/vacancy-extraction-schema.js';
const oldFetch=globalThis.fetch,oldInfo=console.info,gen='11111111-2222-4333-8444-555555555555',frames=[];schemaState.ready=true;console.info=()=>{};
const title='CI_TITLE',url='https://example.com/ci-job',company='CI_STAGE_COMPANY',prompt='CI_EXTRACTION_RULES';
const valid=Object.fromEntries(EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(valid,{title,level:'Senior',description_md:'CI_DESCRIPTION '.repeat(12)});
try{for(const spec of [{name:'valid',raw:'CI_MARKDOWN',value:valid},{name:'html_characters',raw:'CI <tag> & \u2028 full text',value:valid},{name:'emoji_too_long',raw:'😀'.repeat(60001),value:valid},{name:'correction',raw:'CI_MARKDOWN',value:{...valid,work_format:'remote',published_at:'2026-10-23'}},{name:'invalid_salary',raw:'CI_MARKDOWN',value:{...valid,salary_from:999999}},{name:'oversized',raw:'A'.repeat(120001),value:valid},{name:'empty_prompt',raw:'CI_MARKDOWN',prompt:' ',value:valid}]){
 const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const st=sqlite.prepare(sql);return st.columns().length?{results:st.all(...args)}:{meta:st.run(...args)}}}},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const st of ss)out.push(await st.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}}};
 try{
 const titleHash=await candidateHash(title),rule='CI_ROLE_RULES',ruleHash=await candidateHash(rule),contentHash=await candidateHash(spec.raw);
 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation)VALUES(1,?)').run(gen);
 sqlite.exec("INSERT INTO company_identity(id,company_name)VALUES(990002,'CI_STAGE_COMPANY')");
 sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN',990002,?, ?,?,'companySite','manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')").run(title,titleHash,url);
 sqlite.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(990101,'CI_ROOT','CI_ROLE',1)");sqlite.prepare("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(990101,?,1,1)").run(rule);
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at)VALUES('CI_ATTEMPT',990001,?,?,1,'typesafe/jev-1.13','[]','complete','CI_TIME')").run(title,titleHash);
 sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at)VALUES(990001,'CI_ATTEMPT',990101,'CI_ROLE','match','typesafe/jev-1.13','typesafe/jev-1.13',?,1,?,1,'CI_TIME')").run(ruleHash,titleHash);
 sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at)VALUES('CI_SNAPSHOT',990001,?,?,?,?, 'CI_TIME','free','valid','{}','CI_TIME')").run(url,url,spec.raw,contentHash);
 sqlite.prepare("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision)VALUES(1,?,1,'openai','gpt-6-luna','medium',1)").run(spec.prompt??prompt);
 let payload,metadata,calls=0;globalThis.fetch=async(url,opts)=>{if(String(url).endsWith('/models'))return Response.json({data:[{id:'gpt-6-luna'}]});calls++;payload=JSON.parse(opts.body);return Response.json({model:'gpt-6-luna',status:'completed',output:[{type:'message',role:'assistant',content:[{type:'output_text',text:JSON.stringify(spec.value)}]}],usage:{input_tokens:10,output_tokens:2}})};
 const env={DB:generationBoundDb(db,gen),OPENAI_API_KEY:'CI_SYNTHETIC_KEY',SCHEDULER_BILLING:{run(meta,work){metadata=meta;return work()}}};let expected;
 try{let r=await extractCandidateMarkdown(env,990001,{allowImported:true});expected={ok:true,data:JSON.parse(r.extraction.extracted_data),diagnostics:JSON.parse(r.extraction.diagnostics)}}catch(e){expected={ok:false,code:e.code||null,status:e.status||null,message:e.message}}
 const stored=sqlite.prepare('SELECT config_hash,prompt_version,schema_version,status,error_code,diagnostics FROM vacancy_extractions WHERE candidate_id=990001').get();
 if(stored)stored.diagnostics=JSON.parse(stored.diagnostics);
 frames.push({name:spec.name,raw:spec.raw,prompt:spec.prompt??prompt,title,company,url,value:spec.value,payload,metadata,calls,stored,...expected});
 }finally{sqlite.close()}
}}finally{globalThis.fetch=oldFetch;console.info=oldInfo}
console.log(JSON.stringify(frames));
