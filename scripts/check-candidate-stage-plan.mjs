// Actual nextCandidateStage на синтетической SQLite; сетевые запросы запрещены.
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {candidateHash,candidateClassificationContext,getVacancyCandidate} from '../server/repositories/vacancy-candidates.js';
import {nextCandidateStage} from '../server/services/scheduler-pipeline.js';
import {extractionContext} from '../server/services/vacancy-extraction.js';
import {leafRoleContext} from '../server/services/vacancy-leaf-roles.js';
import {EXTRACTION_SCHEMA} from '../server/services/vacancy-extraction-schema.js';
const gen='11111111-2222-4333-8444-555555555555',title='CI_TITLE',rule='CI_ROLE_RULES',raw='CI_MARKDOWN',url='https://example.com/ci-job';schemaState.ready=true;const original=globalThis.fetch;globalThis.fetch=()=>{throw new Error('network forbidden')};
const extracted=Object.fromEntries(EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(extracted,{title,level:'Senior',description_md:'CI_DESCRIPTION '.repeat(12)});
const cases=[{name:'classify',decision:null},{name:'partial_two_roots',decision:'match',second:true},{name:'reject',decision:'reject'},{name:'fetch',decision:'match'},{name:'fetch_rabota',decision:'match',source:'rabota'},
 {name:'refresh',decision:'match',snapshot:true,refresh:true},{name:'extract',decision:'match',snapshot:true},
 {name:'leaf',decision:'match',snapshot:true,extraction:true},{name:'leaf_empty',decision:'match',snapshot:true,extraction:true,leaf:[]},
 {name:'import',decision:'match',snapshot:true,extraction:true,leaf:[990102]},{name:'source_disabled',decision:null,disabled:true}];
try{for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const st=sqlite.prepare(sql);return st.columns().length?{results:st.all(...args)}:{meta:st.run(...args)}}}},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const st of ss)out.push(await st.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}}};
 try{
  const titleHash=await candidateHash(title),promptVersion=await candidateHash(rule),hash=await candidateHash(raw),source=c.source||'companySite';
  sqlite.prepare('INSERT INTO app_runtime_generation(id,generation)VALUES(1,?)').run(gen);sqlite.exec("INSERT INTO company_identity(id,company_name)VALUES(990002,'CI_COMPANY')");
  sqlite.prepare("INSERT INTO firecrawl_settings(id,favorites_only,scheduler_json)VALUES(1,0,?)").run(JSON.stringify({enabled:true,sourceIds:c.disabled?[]:[source],ownerName:'Eugene'}));
  sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,title_updated_at,created_at,updated_at)VALUES(990001,'CI',990002,?,?,?,?,'manual','CI','CI',?,'CI','CI')").run(title,titleHash,url,source,c.refresh?'2026-01-02T00:00:00.000Z':null);
  sqlite.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(990101,'CI_ROOT','CI_ROLE',1)");sqlite.exec("INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order)VALUES(990102,'CI_LEAF','CI_ONE',990101,1)");sqlite.prepare("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(990101,?,1,1)").run(rule);
  if(c.second){sqlite.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(990104,'CI_ROOT_2','CI_ROLE_2',0)");sqlite.prepare("INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(990104,?,1,1)").run('CI_RULES_2')}
  if(c.decision){sqlite.prepare("INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,requested_roles_json,outcome,classified_at)VALUES('CI_ATTEMPT',990001,?,?,1,'typesafe/jev-1.13','[]','complete','CI')").run(title,titleHash);sqlite.prepare("INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at)VALUES(990001,'CI_ATTEMPT',990101,'CI_ROLE',?,'typesafe/jev-1.13','typesafe/jev-1.13',?,1,?,1,'CI')").run(c.decision,promptVersion,titleHash)}
  if(c.snapshot)sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at)VALUES('CI_SNAPSHOT',990001,?,?,?,?, '2026-01-01T00:00:00.000Z','free','valid','{}','2026-01-01')").run(url,url,raw,hash);
  const bound=generationBoundDb(db,gen);let x;if(c.snapshot)x=await extractionContext(bound);
  if(c.extraction)sqlite.prepare("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,extracted_data,processed_at,duration_ms,status,diagnostics)VALUES('CI_EXTRACTION',990001,'CI_SNAPSHOT',?,?,?, ?,2,?,'CI',0,'success','{}')").run(x.config.provider,x.config.openaiModel,x.configHash,x.promptVersion,JSON.stringify(extracted));
  let leaf;if(c.extraction)leaf=await leafRoleContext(bound,990001);
  if(c.leaf)sqlite.prepare("INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,prompt_version,config_hash,model_id,provider,role_ids,status,processed_at,duration_ms)VALUES('CI_RESULT',990001,'CI_SNAPSHOT',?,?,?,?, 'CI_MODEL','openrouter',?,'success','CI',0)").run(leaf.inputHash,leaf.treeHash,leaf.promptVersion,leaf.configHash,JSON.stringify(c.leaf));
  const active=await candidateClassificationContext(bound),details=await getVacancyCandidate(bound,990001),byId=new Map(details.currentResults.map(r=>[r.roleId,r.decision]));c.roles=active.map(r=>({id:r.roleId,promptVersion:r.promptVersion,modelKey:r.modelKey,decision:byId.get(r.roleId)??null}));
  c.source=source;c.titleHash=titleHash;c.promptVersion=promptVersion;c.rawHash=hash;c.extractionConfigHash=x?.configHash??null;c.extractionPromptVersion=x?.promptVersion??null;c.leafInputHash=leaf?.inputHash??null;c.result=await nextCandidateStage({DB:bound},990001);
 }finally{sqlite.close()}
}}finally{globalThis.fetch=original}
console.log(JSON.stringify(cases));
