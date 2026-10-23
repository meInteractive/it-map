import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {prepareBackend,moduleFor,vacancyTestPayload} from './test-support.mjs';
await prepareBackend();
const backend=await import('../.tests/backend.mjs');
const worker=backend.default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const file of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
let failSQL;
const db={prepare(sql){let args=[];return {bind(...v){args=v;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){if(failSQL?.test(sql)){failSQL=null;throw Error('Synthetic write failure')}const q=sqlite.prepare(sql);if(q.columns().length)return {results:q.all(...args)};const r=q.run(...args);return {meta:{changes:Number(r.changes),last_row_id:Number(r.lastInsertRowid)}}}}},async batch(items){sqlite.exec('BEGIN');try{const out=[];for(const item of items)out.push(await item.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}}};
const env={DB:db,SITE_PASSWORD:'audit-only',NASTYA_PASSWORD:'audit-only',DEMO_PASSWORD:'audit-only',SESSION_SECRET:'audit-only-secret'},base='https://audit.invalid';
const cookies={};for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'audit-only'})}),env);assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0]}
const req=async (path,name='Eugene',method='GET',body,extra={})=>{body=await vacancyTestPayload(()=>req(path,name),path,method,body);return worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[name]||'',origin:base,'content-type':'application/json',...extra},...(body===undefined?{}:{body:JSON.stringify(body)})}),env)};
const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
const catalog={...seed,companies:[{...seed.companies[0],name:'Audit A',jobs:[]}],agencies:[]};
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run('Audit A');
const input={companyName:'Audit A',entityType:'company',url:'https://audit.invalid/vacancy',title:'Original title',role:'Менеджер проектов',level:'Senior',salaryFrom:200000,salaryTo:300000,relevance:50,publishedAt:'2026-10-01',loadedAt:'2026-10-01',descriptionMd:'Original description',coverLetterSubject:'',coverLetterMd:''};
const {default:Editor}=await moduleFor('frontend/features/vacancies/VacancyEditor.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.document={activeElement:null};
let root,payload;const oldFetch=fetch;
const mount=async(record)=>{await act(async()=>{root=create(h(Editor,{owner:{name:'Audit A',entityType:'company'},record,onSaved(){},onClose(){}}))})};
const saveTitle=async(title)=>{await act(async()=>root.root.findAllByType('input').find(n=>n.props.value==='Original title').props.onChange({target:{value:title}}));await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}))};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=null};
try{
 let r=await req('/api/vacancies','Eugene','POST',input);assert.equal(r.status,201);const id=(await r.json()).vacancies.find(v=>v.url===input.url).id;
 sqlite.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').run('Eugene',id,'Private subject','PRIVATE EUGENE');
 sqlite.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').run('Nastya',id,'Other subject','PRIVATE NASTYA');
 globalThis.fetch=async(url,options={})=>{if(url==='/api/channels')return Response.json({channels:[]});payload=JSON.parse(options.body);return req(url,'Eugene',options.method,payload)};
 const detail=(await (await req('/api/vacancies/'+id)).json()).vacancy;assert.equal(detail.coverLetterMd,'PRIVATE EUGENE');assert.equal(detail.coverLetterScope,'personal');
 await mount(detail);await saveTitle('Changed title');
 assert.ok(!Object.hasOwn(payload,'coverLetterMd'));assert.ok(!Object.hasOwn(payload,'coverLetterSubject'));
 assert.equal(sqlite.prepare('SELECT title FROM company_vacancies WHERE id=?').get(id).title,'Changed title');
 assert.equal(sqlite.prepare('SELECT cover_letter_md FROM company_vacancies WHERE id=?').get(id).cover_letter_md,'');
 for(const [user,expected] of [['Eugene','PRIVATE EUGENE'],['Nastya','PRIVATE NASTYA'],['Demo','']])assert.equal((await (await req('/api/vacancies/'+id,user)).json()).vacancy.coverLetterMd,expected,user+' letter isolation');
 await stop();
 // Real editor explicitly edits the current user's personal text, never shared storage.
 await mount((await (await req('/api/vacancies/'+id)).json()).vacancy);
 await act(async()=>root.root.findAllByType('textarea').find(n=>n.props.value==='PRIVATE EUGENE').props.onChange({target:{value:'PRIVATE EDITED'}}));
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
 assert.equal(payload.coverLetterScope,'personal');
 assert.equal(sqlite.prepare('SELECT cover_letter_md FROM company_vacancies WHERE id=?').get(id).cover_letter_md,'');
 for(const [user,expected] of [['Eugene','PRIVATE EDITED'],['Nastya','PRIVATE NASTYA'],['Demo','']])assert.equal((await (await req('/api/vacancies/'+id,user)).json()).vacancy.coverLetterMd,expected);
 await stop();
 assert.equal((await req('/api/vacancies','Eugene','POST',{...input,url:'https://audit.invalid/copy',coverLetterScope:'personal',coverLetterMd:'PRIVATE COPY'})).status,400);
 // Legacy or malformed writes fail before changing either the vacancy or letters.
 const before=sqlite.prepare('SELECT * FROM company_vacancies WHERE id=?').get(id);
 for(const patch of [{},{coverLetterScope:'invalid'},{coverLetterScope:'personal',coverLetterSubject:undefined},{coverLetterScope:'personal',coverLetterSubject:''}]) {
   const bad=await req('/api/vacancies/'+id,'Eugene','PUT',{...input,title:'Must not save',coverLetterMd:'PRIVATE LEAK',...patch});
   assert.equal(bad.status,400);assert.deepEqual(sqlite.prepare('SELECT * FROM company_vacancies WHERE id=?').get(id),before);
 }
 assert.equal((await req('/api/vacancies/'+id,'Nastya','PUT',{...input,coverLetterScope:'shared'})).status,403);
 // Fault during personal write rolls the simultaneous vacancy update back.
 failSQL=/INSERT INTO vacancy_cover_letters/;
 assert.equal((await req('/api/vacancies/'+id,'Eugene','PUT',{...input,title:'Rollback',coverLetterScope:'personal',coverLetterSubject:'Subject',coverLetterMd:'Changed'})).status,400);
 assert.deepEqual(sqlite.prepare('SELECT * FROM company_vacancies WHERE id=?').get(id),before);
 assert.equal(sqlite.prepare('SELECT markdown FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?').get('Eugene',id).markdown,'PRIVATE EDITED');
 // Excel export/import operates only on shared fields, never the effective personal projection.
 const rows=await backend.vacanciesExport(db,'Eugene');const exported=rows.find(v=>v.url===input.url);
 assert.equal(exported.coverLetterMd,'');assert.equal(exported.coverLetterSubject,'');
 let imported=await backend.importVacancies(db,'Eugene',[exported],'update');assert.equal(imported.updated,1);
 assert.equal(sqlite.prepare('SELECT markdown FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?').get('Eugene',id).markdown,'PRIVATE EDITED');
 imported=await backend.importVacancies(db,'Eugene',[{...exported,coverLetterScope:'personal',coverLetterMd:'PRIVATE LEAK'}],'update');assert.equal(imported.invalid,1);
 // Deliberate shared editing still works and does not change any personal row.
 await req('/api/vacancies/'+id,'Eugene','PUT',{...input,coverLetterScope:'shared',coverLetterSubject:'Shared subject',coverLetterMd:'Old shared'});
 await mount((await (await req('/api/vacancies/'+id)).json()).vacancy);
 await act(async()=>root.root.findAllByType('textarea').find(n=>n.props.value==='Old shared').props.onChange({target:{value:'Shared edited'}}));
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
 assert.equal(payload.coverLetterScope,'shared');
 assert.equal((await (await req('/api/vacancies/'+id,'Demo')).json()).vacancy.coverLetterMd,'Shared edited');
 assert.equal(sqlite.prepare('SELECT markdown FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?').get('Nastya',id).markdown,'PRIVATE NASTYA');
 await stop();
 // Personal deletion is scoped by session user; shared text survives it.
 assert.equal((await req('/api/vacancies/'+id,'Eugene','PUT',{...input,coverLetterScope:'personal',coverLetterSubject:'',coverLetterMd:''})).status,200);
 assert.equal(sqlite.prepare('SELECT COUNT(*) n FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?').get('Eugene',id).n,0);
 assert.equal((await (await req('/api/vacancies/'+id,'Demo')).json()).vacancy.coverLetterMd,'Shared edited');
 // Omitting unchanged shared fields also preserves a letter changed elsewhere while editing title.
 sqlite.prepare("UPDATE company_vacancies SET title='Original title',cover_letter_subject='Shared subject',cover_letter_md='Old shared' WHERE id=?").run(id);
 await mount((await (await req('/api/vacancies/'+id)).json()).vacancy);
 sqlite.prepare("UPDATE company_vacancies SET cover_letter_md='New shared' WHERE id=?").run(id);
 await saveTitle('Unrelated title edit');assert.equal(sqlite.prepare('SELECT cover_letter_md FROM company_vacancies WHERE id=?').get(id).cover_letter_md,'New shared');
 console.log('Letter privacy: real editor/PUT, explicit personal/shared editing, three users, legacy denial, scoped deletion, rollback, Excel roundtrip and unchanged preservation passed.');
}finally{await stop();globalThis.fetch=oldFetch;sqlite.close()}
