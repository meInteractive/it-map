// Diagnostic reproductions: assertions confirm current defects, not desired behavior.
// Keep outside test-*; invert assertions when implementing fixes.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {prepareBackend,moduleFor,vacancyTestPayload,applicationTestIntent} from '../test-support.mjs';
await prepareBackend();
const worker=(await import('../../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const file of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
// One SQLite connection serializes batches, as native D1 does.
let failSQL,batchQueue=Promise.resolve();
const db={prepare(sql){let args=[];return {bind(...v){args=v;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){if(failSQL?.test(sql)){failSQL=null;throw Error('Synthetic write failure')}const q=sqlite.prepare(sql);if(q.columns().length)return {results:q.all(...args)};const r=q.run(...args);return {meta:{changes:Number(r.changes),last_row_id:Number(r.lastInsertRowid)}}}}},batch(items){const operation=batchQueue.then(async()=>{sqlite.exec('BEGIN');try{const out=[];for(const item of items)out.push(await item.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}});batchQueue=operation.catch(()=>{});return operation}};
const env={DB:db,SITE_PASSWORD:'audit-only',NASTYA_PASSWORD:'audit-only',DEMO_PASSWORD:'audit-only',SESSION_SECRET:'audit-only-secret'},base='https://audit.invalid';
const cookies={};for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'audit-only'})}),env);assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0]}
const req=async (path,name='Eugene',method='GET',body,extra={})=>{body=await vacancyTestPayload(()=>req(path,name),path,method,body);return worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[name]||'',origin:base,'content-type':'application/json',...extra},...(body===undefined?{}:{body:JSON.stringify(body)})}),env)};
const out=[];const observed=(id,details)=>{out.push({id,...details});console.log(JSON.stringify(out.at(-1)))};
const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
const catalog={...seed,companies:['Audit A','Audit B'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[{name:'Audit agency'}]};
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
for(const c of catalog.companies)sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(c.name);
const input={coverLetterScope:'shared',companyName:'Audit A',entityType:'company',url:'https://audit.invalid/vacancy',title:'Original title',role:'Менеджер проектов',level:'Senior',salaryFrom:200000,salaryTo:300000,relevance:50,publishedAt:'2026-10-01',loadedAt:'2026-10-01',descriptionMd:'Original description',coverLetterSubject:'',coverLetterMd:''};
let root,oldFetch=globalThis.fetch;
try{
 let response=await req('/api/vacancies','Eugene','POST',input);assert.equal(response.status,201,await response.clone().text());const id=(await response.json()).vacancies.find(v=>v.url===input.url).id;
 // Access controls: candidates cannot mutate shared/admin objects or another user's CRM/task.
 for(const [path,method] of [['/api/profile/prompts','GET'],['/api/docs','GET'],['/api/backup','GET'],['/api/openrouter','GET'],['/api/vacancies/'+id,'PUT']])assert.equal((await req(path,'Nastya',method,method==='GET'?undefined:input)).status,403,path);
 sqlite.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').run('Eugene',id,'PRIVATE AUDIT SUBJECT','PRIVATE AUDIT LETTER');
 assert.equal((await (await req('/api/vacancies/'+id,'Nastya')).json()).vacancy.coverLetterMd,'');
 const detail=(await (await req('/api/vacancies/'+id)).json()).vacancy;
 const {default:Editor}=await moduleFor('frontend/features/vacancies/VacancyEditor.tsx');
 globalThis.IS_REACT_ACT_ENVIRONMENT=true;globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.document={activeElement:null};let savedPayload;
 globalThis.fetch=async(url,options={})=>{if(url==='/api/channels')return Response.json({channels:[]});savedPayload=JSON.parse(options.body);return req(url,'Eugene',options.method,savedPayload)};
 await act(async()=>{root=create(h(Editor,{owner:{name:'Audit A',entityType:'company'},record:detail,onSaved(){},onClose(){}}))});
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
 const unchangedOther=(await (await req('/api/vacancies/'+id,'Nastya')).json()).vacancy;
 assert.equal(savedPayload.coverLetterMd,undefined);assert.equal(unchangedOther.coverLetterMd,'','S01a prevents unchanged promotion');
 await act(async()=>root.unmount());
 await act(async()=>{root=create(h(Editor,{owner:{name:'Audit A',entityType:'company'},record:(await (await req('/api/vacancies/'+id)).json()).vacancy,onSaved(){},onClose(){}}))});
 // S01b regression: explicitly edited personal text remains private.
 await act(async()=>root.root.findAllByType('textarea').find(n=>n.props.value==='PRIVATE AUDIT LETTER').props.onChange({target:{value:'PRIVATE AUDIT EDITED'}}));
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
 const other=(await (await req('/api/vacancies/'+id,'Nastya')).json()).vacancy;
 assert.equal(other.coverLetterMd,'');
 assert.equal((await (await req('/api/vacancies/'+id)).json()).vacancy.coverLetterMd,'PRIVATE AUDIT EDITED');
 observed('S01',{unchangedPromotionFixed:true,explicitPersonalEditStillShared:false,through:'real React VacancyEditor -> PUT -> another user GET'});
 await act(async()=>root.unmount());root=null;globalThis.fetch=oldFetch;
 // S02b regression: stale full-form save cannot undo the first writer.
 const stale={...input,...(await (await req('/api/vacancies/'+id)).json()).vacancy};
 assert.equal((await req('/api/vacancies/'+id,'Eugene','PUT',{...stale,title:'First editor title'})).status,200);
 assert.equal((await req('/api/vacancies/'+id,'Eugene','PUT',{...stale,salaryFrom:250000})).status,409);
 assert.equal(sqlite.prepare('SELECT title FROM company_vacancies WHERE id=?').get(id).title,'First editor title');
 observed('S02',{concurrentFullSaveSilentlyRevertedTitle:false});
 // S03b1 regression: a late failure rolls the core vacancy mutation back.
 failSQL=/INSERT INTO company_vacancy_user_state/;
 response=await req('/api/vacancies/'+id,'Eugene','PUT',{...input,title:'Partial committed title'});assert.equal(response.status,400);
 assert.equal(sqlite.prepare('SELECT title FROM company_vacancies WHERE id=?').get(id).title,'First editor title');
 observed('S03b1',{httpStatus:400,vacancyChangedDespiteFailure:false});
 // S03a regression: failure rolls application back; retry creates exactly one record.
 const application={companyName:'Audit A',vacancyId:null,status:'Откликнулся',comment:'Audit',applicationSources:['hh'],...await applicationTestIntent(()=>req('/api/applications/create-context','Nastya'))};
 failSQL=/INSERT INTO company_application_status_history/;
 response=await req('/api/applications','Nastya','POST',application);assert.equal(response.status,503);
 assert.equal(sqlite.prepare("SELECT COUNT(*) n FROM company_applications WHERE user_name='Nastya'").get().n,0);
 assert.equal((await req('/api/applications','Nastya','POST',application)).status,201);
 assert.equal(sqlite.prepare("SELECT COUNT(*) n FROM company_applications WHERE user_name='Nastya'").get().n,1);
 observed('S03a',{applicationCreatedDespiteFailure:false,retryCreatedDuplicate:false});
 const own=sqlite.prepare("SELECT id FROM company_applications WHERE user_name='Nastya' LIMIT 1").get().id;
 assert.equal((await req('/api/applications/'+own,'Demo','DELETE')).status,404);
 // Same stale full-record pattern can revert status while saving only a comment in another tab.
 const record=(await (await req('/api/applications?company=Audit%20A','Nastya')).json()).records.find(r=>r.id===own);
 assert.equal((await req('/api/applications/'+own,'Nastya','PUT',{...record,status:'Офер'})).status,200);
 assert.equal((await req('/api/applications/'+own,'Nastya','PUT',{...record,comment:'New comment'})).status,409);
 assert.equal(sqlite.prepare('SELECT status FROM company_applications WHERE id=?').get(own).status,'Офер');
 observed('S02',{commentSaveFromStaleRecordRevertedOffer:false});
 // S03b2 regression: catalogue failure must roll back the vacancy salary.
 const agencyInput={...input,companyName:'Audit agency',entityType:'agency',url:'https://audit.invalid/agency-vacancy'};
 let agencyResponse=await req('/api/vacancies','Eugene','POST',agencyInput);assert.equal(agencyResponse.status,201,await agencyResponse.clone().text());
 const agencyId=(await agencyResponse.json()).vacancies.find(v=>v.url===agencyInput.url).id;
 failSQL=/UPDATE app_catalog/;
 agencyResponse=await req('/api/vacancies/'+agencyId,'Eugene','PUT',{...agencyInput,salaryFrom:600000,salaryTo:700000});assert.equal(agencyResponse.status,400);
 assert.equal(sqlite.prepare('SELECT salary_from FROM company_vacancies WHERE id=?').get(agencyId).salary_from,200000);
 observed('S03b2',{catalogueFailureAfterVacancyCommit:false});
 // R03a regression: CREATE rolls a new vacancy back on catalogue failure.
 const createFailureInput={...agencyInput,url:'https://audit.invalid/agency-create-failure',salaryFrom:800000,salaryTo:900000};
 failSQL=/UPDATE app_catalog/;
 const createFailure=await req('/api/vacancies','Eugene','POST',createFailureInput);assert.equal(createFailure.status,400);
 assert.equal(sqlite.prepare('SELECT id FROM company_vacancies WHERE url=?').get(createFailureInput.url),undefined);
 observed('R03a',{newVacancyPersistsAfterCreateFailure:false});
 // S07 regression: deleting the only source is rejected; the vacancy remains visible.
 const channelResponse=await req('/api/channels','Eugene','POST',{name:'Audit channel',url:'https://t.me/audit_channel'});assert.equal(channelResponse.status,201);
 const channelId=(await channelResponse.json()).channelId;
 const channelInput={...input,companyName:'',entityType:'channel',url:'https://t.me/audit_channel/1',telegramChannelId:channelId,telegramMessageUrl:'https://t.me/audit_channel/1'};
 const vacancyResponse=await req('/api/vacancies','Eugene','POST',channelInput);assert.equal(vacancyResponse.status,201,await vacancyResponse.clone().text());
 const channelVacancy=sqlite.prepare('SELECT id FROM company_vacancies WHERE url=?').get(channelInput.url).id;
 const models=await moduleFor('frontend/features/vacancies/model.ts');const {defaultFilters}=await moduleFor('frontend/features/companies/Filters.tsx');
 let list=await (await req('/api/vacancies/list')).json();
 assert.equal(models.visibleVacancy(list.vacancies.find(v=>v.id===channelVacancy),'channel',new Map(list.entities.map(e=>[e.name,e])),defaultFilters,models.defaultVacancyFilters,''),true);
 assert.equal((await req('/api/channels/'+channelId,'Eugene','DELETE')).status,409);
 list=await (await req('/api/vacancies/list')).json();const orphan=list.vacancies.find(v=>v.id===channelVacancy);assert.ok(orphan);assert.equal(orphan.telegramSources.length,1);
 const visibility=['company','agency','channel'].map(type=>models.visibleVacancy(orphan,type,new Map(list.entities.map(e=>[e.name,e])),{...defaultFilters,showHidden:true},{...models.defaultVacancyFilters,showHidden:true},''));assert.deepEqual(visibility,[false,false,true]);
 observed('S07',{channelVacancyStillInDatabase:true,visibleInAnyVacancyTab:true,sourceLinkAndContactCascadeDeleted:false});
 console.log('Access controls tested: candidate admin API denial and foreign application deletion denial passed. All defect assertions above reproduced against synthetic SQLite.');
 fs.writeFileSync('.tests/serious-backend-audit.json',JSON.stringify(out,null,2));
}finally{if(root)await act(async()=>root.unmount());globalThis.fetch=oldFetch;sqlite.close()}
