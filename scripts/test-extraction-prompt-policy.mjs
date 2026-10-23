import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {DEFAULT_EXTRACTION_PROMPT,LEGACY_EXTRACTION_PROMPT,ALIGN_EXTRACTION_PROMPT_SQL} from '../server/services/vacancy-extraction-prompt.js';
import {buildBackupSchema} from './backup-schema.mjs';

const migration=fs.readFileSync('drizzle/0077_extraction_prompt_10000.sql','utf8');
const schema=buildBackupSchema(process.cwd());
for(const text of [LEGACY_EXTRACTION_PROMPT,'# Мой промт\nСохрани эти правила.','😀'.repeat(5000)]){
 const db=new DatabaseSync(':memory:');
 try{
  db.exec('CREATE TABLE vacancy_candidates(id INTEGER PRIMARY KEY); INSERT INTO vacancy_candidates VALUES(1)');
  db.exec(fs.readFileSync('drizzle/0058_vacancy_details.sql','utf8'));
  db.prepare("INSERT INTO vacancy_detail_settings(id,markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision,updated_at) VALUES(1,?,0,'openai','gpt-6-luna','medium',3,'2026-10-17 17:16:48')").run(text);
  db.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('raw',1,'https://example.com/job','https://example.com/job','ORIGINAL','hash','2026-10-20','free','valid','{}','2026-10-20')").run();
  const before=db.prepare('SELECT * FROM vacancy_detail_settings').get();
  db.exec('BEGIN;'+migration+'COMMIT;');
  const row=()=>db.prepare('SELECT * FROM vacancy_detail_settings').get(),aligned=row();
  if(text===LEGACY_EXTRACTION_PROMPT){assert.equal(aligned.markdown,DEFAULT_EXTRACTION_PROMPT);assert.equal(aligned.prompt_revision,1);}else assert.deepEqual(aligned,before,'custom prompt, revision and timestamp are preserved');
  for(const field of ['provider','openai_model','reasoning_effort','model_revision'])assert.equal(aligned[field],before[field]);
  db.exec(ALIGN_EXTRACTION_PROMPT_SQL);assert.deepEqual(row(),aligned,'alignment replay does not overwrite or increment twice');
  db.prepare('UPDATE vacancy_detail_settings SET markdown=?,prompt_revision=7').run(LEGACY_EXTRACTION_PROMPT);const edited=row();db.exec(ALIGN_EXTRACTION_PROMPT_SQL);assert.deepEqual(row(),edited,'an explicitly saved old text is still a manual edit');
  db.prepare('UPDATE vacancy_detail_settings SET markdown=?').run('😀'.repeat(10000));assert.equal(Array.from(row().markdown).length,10000);
  assert.throws(()=>db.prepare('UPDATE vacancy_detail_settings SET markdown=?').run('я'.repeat(10001)),/CHECK constraint/);
  assert.equal(db.prepare('SELECT markdown_raw FROM vacancy_raw_snapshots').get().markdown_raw,'ORIGINAL');
  // Restore DDL is derived from the append-only migration and accepts the new limit.
  const restored=new DatabaseSync(':memory:');try{restored.exec(schema.objects.find(o=>o.name==='vacancy_detail_settings').sql);const r=row();restored.prepare('INSERT INTO vacancy_detail_settings VALUES(?,?,?,?,?,?,?,?)').run(...Object.values(r));assert.deepEqual(restored.prepare('SELECT * FROM vacancy_detail_settings').get(),r);}finally{restored.close();}
 }finally{db.close();}
}
assert.ok(Array.from(DEFAULT_EXTRACTION_PROMPT).length<=10000);
console.log('Extraction prompt: populated migration, exact default alignment/replay, custom edits/models/raw data preservation, 10000 Unicode DB and restore DDL passed');
