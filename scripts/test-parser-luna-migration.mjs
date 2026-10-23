import assert from 'node:assert/strict';import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,sqlite}=f;
try{
 const defaults=await api.readPromptModels(db);assert.equal(defaults.vacancy_extraction.provider,'openai');assert.equal(defaults.vacancy_extraction.openaiModel,'gpt-6-luna');assert.equal(defaults.vacancy_role.openaiModel,'gpt-6-luna');
 sqlite.prepare("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,model_revision) VALUES(1,'User custom prompt',7,'openrouter',3)").run();
 sqlite.prepare("INSERT INTO vacancy_role_settings(id,markdown,provider,openai_model,model_revision) VALUES(1,'Existing role prompt','openai','old-model',2)").run();
 const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,revision:s.revision,dayBudgetUsd:2,monthBudgetUsd:10},'Eugene');
 const ids={};
 for(const [key,status,code,provider,model,generation] of [
  ['retry','failed','provider_503','openrouter','legacy/model:free',null],
  ['openai','failed','provider_503','openai','gpt-6-luna',null],
  ['validation','failed','invalid_output','openrouter','legacy/model:free',null],
  ['cancelled','cancelled','provider_503','openrouter','legacy/model:free',null],
  ['old','failed','provider_503','openrouter','legacy/model:free','previous-restore']]){
  const job=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'migration-'+key});ids[key]=job.id;
  sqlite.prepare('UPDATE scheduler_jobs SET status=?,reason_code=?,attempts=4,generation=coalesce(?,generation) WHERE id=?').run(status,code,generation,job.id);
  sqlite.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,provider,model) VALUES(?,?,'fixture','extract_vacancy',?,'failed',?,?)").run(key,job.id,Date.now(),provider,model);
 }
 const sql=fs.readFileSync('drizzle/0069_parser_luna_models.sql','utf8');sqlite.exec(sql);
 const row=sqlite.prepare('SELECT * FROM vacancy_detail_settings').get();assert.equal(row.provider,'openai');assert.equal(row.openai_model,'gpt-6-luna');assert.equal(row.markdown,'User custom prompt');assert.equal(row.prompt_revision,7);assert.equal(row.model_revision,4);
 assert.equal(sqlite.prepare('SELECT openai_model FROM vacancy_role_settings').get().openai_model,'gpt-6-luna');
 assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(ids.retry).status,'pending');assert.equal(sqlite.prepare('SELECT attempts FROM scheduler_jobs WHERE id=?').get(ids.retry).attempts,0);
 for(const key of ['openai','validation','old'])assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(ids[key]).status,'failed');assert.equal(sqlite.prepare('SELECT status FROM scheduler_jobs WHERE id=?').get(ids.cancelled).status,'cancelled');
 sqlite.exec(sql);assert.equal(sqlite.prepare('SELECT model_revision FROM vacancy_detail_settings').get().model_revision,4,'idempotent model initialization');
 console.log('Luna migration: explicit parser settings, missing fallback, custom prompts/revisions, scoped latest-provider recovery, cancellation/validation/generation guards and idempotence passed');
}finally{f.close();}
