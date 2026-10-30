// Actual leaf-role stage, SQLite :memory: и только подставные provider ответы.
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {candidateHash} from '../server/repositories/vacancy-candidates.js';
import {extractCandidateMarkdown} from '../server/services/vacancy-extraction.js';
import {analyzeVacancyLeafRoles} from '../server/services/vacancy-leaf-roles.js';
import {EXTRACTION_SCHEMA} from '../server/services/vacancy-extraction-schema.js';
import {encryptGoogleToken} from '../server/integrations/google-calendar.js';
const oldFetch=globalThis.fetch,oldInfo=console.info,gen='11111111-2222-4333-8444-555555555555',frames=[];schemaState.ready=true;console.info=()=>{};
const title='CI_TITLE',url='https://example.com/ci-job',markdown='CI_MARKDOWN',prompt='CI_ROLE_PROMPT',secret='CI_SYNTHETIC_SECRET',cipher=await encryptGoogleToken(secret+':openrouter:v1','CI_SYNTHETIC_KEY');
const extracted=Object.fromEntries(EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(extracted,{title,level:'Senior',description_md:'CI_DESCRIPTION '.repeat(12)});
const cases=[{name:'openai_match',provider:'openai',value:{role_ids:[990103,990102]}},{name:'openai_invalid',provider:'openai',value:{role_ids:[999999]}},{name:'jev_match',provider:'jev',answers:{leaf_990102:{type:'choice',choice:'match'},leaf_990103:{type:'choice',choice:'reject'}}},{name:'jev_missing',provider:'jev',answers:{leaf_990102:{type:'choice',choice:'match'}}}];
try{for(const spec of cases){
 const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const st=sqlite.prepare(sql);return st.columns().length?{results:st.all(...args)}:{meta:st.run(...args)}}}},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const st of ss)out.push(await st.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}}};
 try{
 const titleHash=await candidateHash(title),rule='CI_ROLE_RULES',ruleHash=await candidateHash(rule),contentHash=await candidateHash(markdown);
 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation)VALUES(1,?)').run(gen);sqlite.exec("INSERT INTO company_identity(id,company_name)VALUES(990002,'CI_STAGE_COMPANY')");
 sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN',990002,?, ?,?,'companySite','manual','CI','CI','CI','CI')").run(title,titleHash,url);
 sqlite.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(990101,'CI_ROOT','CI_ROLE',1)");sqlite.exec("INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order)VALUES(990102,'CI_LEAF_1','CI_ONE',990101,1),(990103,'CI_LEAF_2','CI_TWO',990101,2)");
 sqlite.prepare("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(990101,?,1,1)").run(rule);
 sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at)VALUES('CI_ATTEMPT',990001,?,?,1,'typesafe/jev-1.13','[]','complete','CI')").run(title,titleHash);
 sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at)VALUES(990001,'CI_ATTEMPT',990101,'CI_ROLE','match','typesafe/jev-1.13','typesafe/jev-1.13',?,1,?,1,'CI')").run(ruleHash,titleHash);
 sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at)VALUES('CI_SNAPSHOT',990001,?,?,?,?, 'CI','free','valid','{}','CI')").run(url,url,markdown,contentHash);
 sqlite.exec("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision)VALUES(1,'CI_EXTRACT',1,'openai','gpt-6-luna','medium',1)");
 sqlite.prepare("INSERT INTO vacancy_role_settings(id,markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision)VALUES(1,?,1,?,'gpt-6-luna',?,'medium',1)").run(prompt,spec.provider==='jev'?'openrouter':'openai',spec.provider==='jev'?'jev':'best_free');
 sqlite.prepare('INSERT INTO openrouter_settings(id,key_encrypted,revision)VALUES(1,?,1)').run(cipher);
 let calls=[],phase='extract';globalThis.fetch=async(u,opts)=>{
  const path=String(u);if(!opts?.body){if(path.includes('output_modalities=decisions'))return Response.json({data:[{id:'typesafe/jev-1.13',context_length:262144,pricing:{prompt:'0.00000001',completion:'0'}}]});return Response.json({data:[{id:'gpt-6-luna'}]})}
  const payload=JSON.parse(opts.body);if(phase==='leaf')calls.push(payload);
  if(path.includes('/alpha/decisions'))return Response.json({model:'typesafe/jev-1.13',answers:spec.answers,usage:{cost:0.001}});
  const content=phase==='extract'?extracted:spec.value;return Response.json({model:'gpt-6-luna',status:'completed',output:[{type:'message',role:'assistant',content:[{type:'output_text',text:JSON.stringify(content)}]}],usage:{input_tokens:10,output_tokens:2}})
 };
 const env={DB:generationBoundDb(db,gen),SESSION_SECRET:secret,OPENAI_API_KEY:'CI_SYNTHETIC_KEY',SCHEDULER_BILLING:{run(_meta,work){return work()}}};
 await extractCandidateMarkdown(env,990001,{allowImported:true});phase='leaf';let expected;
 try{const r=await analyzeVacancyLeafRoles(env,990001);expected={ok:true,ids:r.roleIds}}catch(e){expected={ok:false,code:e.code||null,status:e.status||null,message:e.message}}
 const stored=sqlite.prepare('SELECT input_hash,tree_hash,prompt_version,config_hash,model_id,provider,role_ids,status,error_code,message,input_tokens,output_tokens,cost_usd FROM vacancy_role_results WHERE candidate_id=990001').get();
 if(stored?.role_ids)stored.role_ids=JSON.parse(stored.role_ids);
 frames.push({name:spec.name,provider:spec.provider,title,markdown,prompt,allowed:[{id:990102,path:'CI_ROLE → CI_ONE'},{id:990103,path:'CI_ROLE → CI_TWO'}],value:spec.value,answers:spec.answers,payload:calls[0],stored,...expected});
 }finally{sqlite.close()}
}}finally{globalThis.fetch=oldFetch;console.info=oldInfo}
console.log(JSON.stringify(frames));
