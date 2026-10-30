// Только SQLite :memory:, синтетические ключи и подставные Decisions ответы.
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {classifyStoredCandidate} from '../server/services/stored-vacancy-classification.js';
import {candidateHash} from '../server/repositories/vacancy-candidates.js';
import {encryptGoogleToken} from '../server/integrations/google-calendar.js';
import {JEV_MODEL_ID} from '../server/integrations/jev.js';
const original=fetch,secret='CI_SYNTHETIC_SECRET',cipher=await encryptGoogleToken(secret+':openrouter:v1','CI_SYNTHETIC_KEY'),frames=[];
const generation='11111111-2222-4333-8444-555555555555',title=' CI_TITLE ',titleHash=await candidateHash(title.trim());schemaState.ready=true;
try{for(const [name,answers] of Object.entries({complete:{role_990101:{type:'choice',choice:'match'},role_990102:{type:'choice',choice:'reject'}},partial:{role_990101:{type:'choice',choice:'match'}},empty:{},rejected:{role_990101:{type:'choice',choice:'reject'},role_990102:{type:'choice',choice:'reject'}}})){
 const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 const db={
  prepare(sql){let args=[];return {
   bind(...a){args=a;return this},
   async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r},
   async all(){return {results:sqlite.prepare(sql).all(...args)}},
   async run(){const st=sqlite.prepare(sql);return st.columns().length?{results:st.all(...args)}:{meta:st.run(...args)}}
  }},
  async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const st of ss)out.push(await st.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}}
 };

 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation)VALUES(1,?)').run(generation);
 sqlite.prepare('INSERT INTO openrouter_settings(id,key_encrypted,revision)VALUES(1,?,1)').run(cipher);
 sqlite.exec("INSERT INTO company_identity(id,company_name)VALUES(990002,'CI_STAGE_COMPANY')");
 sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,company_id,title_raw,title_hash,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at)VALUES(990001,'CI_TOKEN',990002,?,?,'companySite','manual','CI_TIME','CI_TIME','CI_TIME','CI_TIME')").run(title,titleHash);
 for(const id of [990101,990102]){sqlite.prepare('INSERT INTO vacancy_roles(id,stable_key,name,sort_order)VALUES(?,?,?,?)').run(id,'CI_ROOT_'+id,'CI_ROLE_'+id,id);sqlite.prepare('INSERT INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled)VALUES(?,?,1,1)').run(id,'CI_RULES_'+id)}
 let calls=0;globalThis.fetch=async(url,opts)=>{if(!opts?.body)return Response.json({data:[{id:JEV_MODEL_ID,context_length:262144,pricing:{prompt:'0.00000001',completion:'0'}}]});calls++;return Response.json({model:JEV_MODEL_ID,answers,usage:{cost:0.001}})};
 const env={DB:generationBoundDb(db,generation),SESSION_SECRET:secret,SCHEDULER_BILLING:{run(_meta,work){return work()}}};
 try{
 await classifyStoredCandidate(env,990001);
 const attempts=sqlite.prepare('SELECT title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,errors_json,outcome,cost_usd FROM vacancy_classification_attempts ORDER BY rowid').all().map(a=>({...a,requested_roles_json:JSON.parse(a.requested_roles_json),errors_json:JSON.parse(a.errors_json)}));
 const decisions=sqlite.prepare('SELECT root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision FROM vacancy_classifications ORDER BY root_role_id').all();
 frames.push({name,title,titleHash,answers,attempts,decisions,calls});
 }finally{sqlite.close()}
}}finally{globalThis.fetch=original}
console.log(JSON.stringify(frames));
