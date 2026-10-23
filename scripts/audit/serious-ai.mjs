// Synthetic regression diagnostic: former S04/S06 findings now assert guards.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend} from '../test-support.mjs';
await prepareBackend();
const worker=(await import('../../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const file of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
let failSQL,batchQueue=Promise.resolve();
const db={prepare(sql){let args=[];return {bind(...v){args=v;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){if(failSQL?.test(sql)){failSQL=null;throw Error('Synthetic write failure')}const q=sqlite.prepare(sql);if(q.columns().length)return {results:q.all(...args)};const r=q.run(...args);return {meta:{changes:Number(r.changes),last_row_id:Number(r.lastInsertRowid)}}}}},batch(items){const result=batchQueue.then(async()=>{sqlite.exec('BEGIN');try{const out=[];for(const item of items)out.push(await item.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}});batchQueue=result.catch(()=>{});return result}};
const env={DB:db,SITE_PASSWORD:'audit-only',NASTYA_PASSWORD:'audit-only',DEMO_PASSWORD:'audit-only',SESSION_SECRET:'audit-only-secret'},base='https://audit.invalid';
const cookies={};for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'audit-only'})}),env);assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0]}
const req=(path,name='Eugene',method='GET',body,extra={})=>worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[name]||'',origin:base,'content-type':'application/json',...extra},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);
const originalFetch=fetch,waiting=[],sent=[];let hold=false;
globalThis.fetch=async(url,options)=>{
 if(url.includes('/models?'))return Response.json({data:[{id:'audit/model',name:'Audit',context_length:262144,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:0,completion:0}}]});
 if(url.includes('/benchmarks?'))return Response.json({data:[]});
 if(url.endsWith('/chat/completions')){const body=JSON.parse(options.body);sent.push(body);if(hold)await new Promise(resolve=>waiting.push(resolve));const title=JSON.parse(body.messages[1].content).vacancyTitle;return Response.json({model:body.model,choices:[{message:{content:JSON.stringify({subject:'For '+title,markdown:'Letter for '+title})}}]})}
 throw Error('Unexpected external request');
};
const waitFor=async count=>{for(let i=0;i<200&&waiting.length<count;i++)await new Promise(r=>setTimeout(r,2));assert.equal(waiting.length,count)};
const letter=id=>req('/api/vacancies/'+id+'/cover-letter','Nastya','POST');
try{
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 const catalog={...seed,companies:[{...seed.companies[0],name:'Audit A',jobs:[]}],agencies:[]};
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 sqlite.exec("INSERT INTO company_crm(company_name) VALUES('Audit A'); INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(7001,'Audit A','https://audit.invalid/1','Backup vacancy B','Description B'),(7002,'Audit A','https://audit.invalid/2','Concurrent vacancy','Description')");
 assert.equal((await req('/api/profile/resume','Nastya','PUT',{resumeMd:'Synthetic CV',revision:0})).status,200);
 assert.equal((await req('/api/profile/prompts/cover_letter','Eugene','PUT',{markdown:'Synthetic prompt',revision:0})).status,200);
 assert.equal((await req('/api/openrouter/key','Eugene','PUT',{apiKey:'FAKE_TEST_TOKEN_e2c025e609d6'})).status,200);
 const backupResponse=await req('/api/backup');assert.equal(backupResponse.status,200);const backup=await backupResponse.json();
 // Inference holds old input while a real backup restore replaces the same ID.
 sqlite.prepare("UPDATE company_vacancies SET title='Pre-restore vacancy A',description_md='Description A' WHERE id=7001").run();
 hold=true;const running=letter(7001);await waitFor(1);
 const restored=await req('/api/backup','Eugene','POST',backup);assert.equal(restored.status,200,await restored.clone().text());
 assert.equal(sqlite.prepare('SELECT title FROM company_vacancies WHERE id=7001').get().title,'Backup vacancy B');
 waiting.shift()();const response=await running;assert.equal(response.status,409);const data=await response.json();
 assert.match(data.error,/не сохранён/);
 assert.equal(sqlite.prepare("SELECT markdown FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=7001").get(),undefined);
 console.log(JSON.stringify({id:'S04a',inflightGenerationWrotePreRestoreContentIntoRestoredVacancy:false,realChangedInputBackupRestore:true,identicalSnapshotRestoreBarrierStillPending:false}));
 // S06 regression: parallel requests share D1 ownership and one provider call.
 const first=letter(7002),second=letter(7002);await waitFor(1);assert.equal(sent.length,2);
 waiting.shift()();const firstResponse=await first;assert.equal(firstResponse.status,200);const secondResponse=await second;assert.equal(secondResponse.status,200);
 assert.equal(sqlite.prepare('SELECT COUNT(*) n FROM vacancy_cover_letters WHERE vacancy_id=7002').get().n,1);
 console.log(JSON.stringify({id:'S06',sameUserSameVacancyConcurrentProviderCalls:1,persistedLetterRows:1}));
 // S04b regression: identical restore changes the durable generation.
 const identicalBackup=await (await req('/api/backup')).json();
 const identicalRunning=letter(7001);await waitFor(1);
 assert.equal((await req('/api/backup','Eugene','POST',identicalBackup)).status,200);
 waiting.shift()();assert.equal((await identicalRunning).status,409);
 assert.equal(sqlite.prepare("SELECT COUNT(*) n FROM vacancy_cover_letters WHERE user_name='Nastya' AND vacancy_id=7001").get().n,0);
 console.log(JSON.stringify({id:'S04b',preRestoreOperationStillWritesIntoIdenticalRestoredSnapshot:false,durableRestoreGeneration:true}));
}finally{globalThis.fetch=originalFetch;sqlite.close()}
