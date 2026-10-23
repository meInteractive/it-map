import {prepareBackend,applicationTestIntent,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {DatabaseSync} from 'node:sqlite';
import {gzipSync} from 'node:zlib';
const worker=(await import('../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const file of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
// Match native D1 scheduling on this single SQLite connection.
let queries=[],batchQueue=Promise.resolve();
const db={prepare(sql){let args=[];return {bind(...v){args=v;return this},async first(){queries.push({sql,args});return sqlite.prepare(sql).get(...args)??null},async all(){queries.push({sql,args});return {results:sqlite.prepare(sql).all(...args)}},async run(){queries.push({sql,args});const p=sqlite.prepare(sql);return p.columns().length?{results:p.all(...args)}:{meta:p.run(...args)}}}},batch(items){const operation=batchQueue.then(async()=>{sqlite.exec('BEGIN');try{const out=[];for(const item of items)out.push(await item.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}});batchQueue=operation.catch(()=>{});return operation}};
const env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',DEMO_PASSWORD:'test',SESSION_SECRET:'test-secret'},base='https://vacancies.test';
const cookies={};for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);cookies[name]=r.headers.get('set-cookie').split(';')[0]}
const req=(path,user='Nastya',method='GET',body)=>worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[user]||'',origin:base,'content-type':'application/json'},...(body?{body:JSON.stringify(body)}:{})}),env);
const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
const catalog={...seed,companies:Array.from({length:30},(_,i)=>[1,80,201][i%3]).map((rank,i)=>({...seed.companies[i],name:'Employer '+i,rank,jobs:[],applicationUrl:'https://employer.invalid/'+i,middle:[200,300],senior:[300,400],lead:[400,500],head:[500,600]})),agencies:Array.from({length:10},(_,i)=>({...seed.agencies?.[0],name:'Agency '+i,entityType:'agency',rank:20+i,middle:[210,310],jobs:[],applicationUrl:'https://agency.invalid'}))};
const levels=['Middle','Senior','Lead','Head',''],roles=['Менеджер проектов','Менеджер продукта','Руководитель разработки','Директор по разработке',''];
try{
 sqlite.exec('DELETE FROM company_applications; DELETE FROM company_tasks; DELETE FROM company_vacancies');
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 for(const name of [...catalog.companies.map(c=>c.name),...catalog.agencies.map(c=>c.name),...Array.from({length:10},(_,i)=>'__telegram_channel__:'+i)])sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(name);
 sqlite.exec("INSERT INTO company_user_state(user_name,company_name,favorite,hidden) VALUES('Nastya','Employer 0',1,0),('Nastya','Employer 1',0,1),('Nastya','Agency 1',1,0); INSERT INTO telegram_channels(id,name,url,username,normalized_username) VALUES(1,'Channel A','https://t.me/a','a','a'),(2,'Channel B','https://t.me/b','b','b')");
 const insert=sqlite.prepare('INSERT INTO company_vacancies(id,company_name,entity_type,real_company_name,url,title,role,level,salary_from,salary_to,published_at,loaded_at,created_at,description_md,cover_letter_subject,cover_letter_md,archived) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)');
 for(let i=0;i<300;i++){
  const type=['company','agency','channel'][i%3],name=type==='company'?'Employer '+(i%30):type==='agency'?'Agency '+(i%10):'__telegram_channel__:'+(i%10);
  insert.run(900000+i,name,type,type==='company'?'':i%2?'Employer 2':'Unknown employer','https://vacancy.invalid/'+i,'Role '+i,roles[i%5],levels[i%5],i%4?200000:null,i%4?400000:null,i%2?'2026-09-20':null,'2026-09-21','2026-09-21 10:00:'+String(i%60).padStart(2,'0'),'PRIVATE_DESCRIPTION '+('Управление командой и разработкой. '.repeat(160)),'PRIVATE_SUBJECT','PRIVATE_LETTER '+('Опыт кандидата. '.repeat(50)),i%7===0?1:0);
  const roleName=roles[i%5]==='Менеджер продукта'?'Менеджер продуктов':roles[i%5];
  if(roleName){const roleId=sqlite.prepare('SELECT id FROM vacancy_roles WHERE name=?').get(roleName)?.id;if(roleId)sqlite.prepare('INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)').run(900000+i,roleId);}
  sqlite.prepare('INSERT INTO company_vacancy_user_state(user_name,vacancy_id,favorite,hidden,relevance) VALUES(?,?,?,?,?)').run('Nastya',900000+i,i%2,i%3===0?1:0,i%101);
  if(type==='channel'||i%11===0){sqlite.prepare('INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url,telegram_contact) VALUES(?,?,?,?)').run(900000+i,1,'https://t.me/a/'+(i+1),'PRIVATE_CONTACT');if(i%11===0)sqlite.prepare('INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url) VALUES(?,?,?)').run(900000+i,2,'https://t.me/b/'+(i+1));}
 }
 sqlite.exec("INSERT INTO company_applications(id,company_name,user_name,status,vacancy_id) VALUES(1,'Employer 0','Nastya','Офер',NULL),(2,'Agency 1','Nastya','Отказ',900001),(3,'Employer 0','Demo','Откликнулся',900000)");
 const html=await shell();assert.ok(!html.includes('PRIVATE_DESCRIPTION'));
 queries=[];const response=await req('/api/vacancies/list');assert.equal(response.status,200);const raw=await response.text(),compact=JSON.parse(raw);assert.equal(compact.vacancies.length,300);assert.equal(queries.length,6);const listQueries=[...queries];
 assert.ok(!raw.includes('PRIVATE_'));assert.ok(!queries.some(q=>/description_md|cover_letter|last_checked_at/.test(q.sql)),'Normal list SQL must not read heavy fields');
 const fields=['id','companyName','entityType','realCompanyName','title','salaryFrom','salaryTo','role','roles','level','relevance','favorite','hidden','publishedAt','loadedAt','archived','createdAt','applicationId','applicationStatus','telegramSources'].sort();
 for(const v of compact.vacancies){assert.deepEqual(Object.keys(v).sort(),fields);for(const t of v.telegramSources)assert.deepEqual(Object.keys(t).sort(),['channelId','channelName']);}
 const legacyRaw=await (await req('/api/vacancies')).text(),legacy=JSON.parse(legacyRaw);
 for(const v of compact.vacancies){const old=legacy.vacancies.find(x=>x.id===v.id);for(const key of fields){const expected=key==='telegramSources'?old[key].map(({channelId,channelName})=>({channelId,channelName})):old[key];assert.deepEqual(v[key],expected,'Field parity: '+key);}}
 assert.equal(compact.vacancies.find(v=>v.id===900001).applicationStatus,'Отказ');assert.equal(compact.vacancies.find(v=>v.id===900000).applicationId,null);
 const other=await (await req('/api/vacancies/list','Demo')).json();assert.equal(other.vacancies.find(v=>v.id===900000).applicationId,3);assert.equal(other.vacancies.find(v=>v.id===900001).hidden,false);
 sqlite.prepare("UPDATE company_vacancies SET cover_letter_md='',cover_letter_subject='' WHERE id=900000").run();sqlite.prepare("INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES('Nastya',900000,'Personal subject','PRIVATE_PERSONAL_LETTER')").run();
 const detail=await (await req('/api/vacancies/900000')).json();assert.match(detail.vacancy.descriptionMd,/PRIVATE_DESCRIPTION/);assert.equal(detail.vacancy.coverLetterMd,'PRIVATE_PERSONAL_LETTER');assert.equal((await (await req('/api/vacancies/900000','Demo')).json()).vacancy.coverLetterMd,'');assert.equal((await req('/api/vacancies/123456')).status,404);assert.equal((await req('/api/vacancies/list','')).status,401);assert.equal((await req('/api/vacancies/list','Nastya','POST')).status,405);
 const {visibleVacancy,defaultVacancyFilters,comparePublication}=await moduleFor('frontend/features/vacancies/model.ts');
 const {defaultFilters}=await moduleFor('frontend/features/companies/Filters.tsx');
 const {VacancyTable}=await moduleFor('frontend/features/vacancies/Vacancies.tsx');
 const directory=data=>new Map(data.entities.map(e=>[e.name,e]));
 for(const type of ['company','agency','channel'])for(let mask=0;mask<16;mask++){
  const f={...defaultFilters,favoriteOnly:!!(mask&1),showHidden:!!(mask&2)},vf={...defaultVacancyFilters,favoriteOnly:!!(mask&4),showHidden:!!(mask&8)};
  const rows=data=>data.vacancies.filter(v=>visibleVacancy(v,type,directory(data),f,vf,'')).sort(comparePublication);
  assert.equal(render(VacancyTable,{records:rows(compact),entities:compact.entities,type,context:true,nav,admin:true}),render(VacancyTable,{records:rows(legacy),entities:legacy.entities,type,context:true,nav,admin:true}));
 }
 for(const type of ['company','agency','channel'])for(const level of ['',...levels])for(const role of ['',...roles])for(const query of ['', 'role 1','PRIVATE_DESCRIPTION'])for(let mask=0;mask<4;mask++){
  const f={...defaultFilters,showHidden:true,vacanciesOnly:!!(mask&1),applicationsOnly:!!(mask&2)},vf={...defaultVacancyFilters,showHidden:true,levels:level?[level]:[],roles:role?[role]:[]};
  const ids=data=>data.vacancies.filter(v=>visibleVacancy(v,type,directory(data),f,vf,query)).map(v=>v.id);assert.deepEqual(ids(compact),ids(legacy));
 }
 await (await import('./test-react.mjs')).testCache();
 await (await import('./test-react.mjs')).testDrawerTabs();
 const applied=await req('/api/vacancies/900009/apply','Nastya','POST',{companyName:compact.vacancies.find(v=>v.id===900009).companyName,...await applicationTestIntent(()=>req('/api/applications/create-context'))});assert.equal(applied.status,200);const appliedData=await applied.json();assert.ok(appliedData.vacancy.applicationId);
 await req('/api/vacancies/900009/state','Nastya','PUT',{hidden:true});const refreshed=await (await req('/api/vacancies/list')).json();assert.equal(refreshed.vacancies.find(v=>v.id===900009).hidden,true);assert.ok(refreshed.vacancies.find(v=>v.id===900009).applicationId);
 await req('/api/applications/'+appliedData.vacancy.applicationId,'Nastya','DELETE');assert.equal((await (await req('/api/vacancies/list')).json()).vacancies.find(v=>v.id===900009).applicationId,null);
 // The historical relevance backfill remains one bulk operation, regardless of vacancy count.
 sqlite.prepare("DELETE FROM vacancy_relevance_profile_state WHERE user_name='Eugene'").run();queries=[];await req('/api/vacancies/list','Eugene');assert.equal(queries.length,9);queries=[];await req('/api/vacancies/list','Eugene');assert.equal(queries.length,6);
 // Inspect the plan: applications and states are joined, not per-row HTTP/SQL work.
 const plan=listQueries.flatMap(q=>sqlite.prepare('EXPLAIN QUERY PLAN '+q.sql).all(...q.args));assert.ok(plan.some(p=>p.detail.includes('INDEX')));assert.ok(!plan.some(p=>p.detail.includes('CORRELATED')));
 console.log(JSON.stringify({test:'vacancy-loading',vacancies:300,sqlStatements:6,beforeBytes:Buffer.byteLength(legacyRaw),afterBytes:Buffer.byteLength(raw),beforeGzip:gzipSync(legacyRaw).length,afterGzip:gzipSync(raw).length}));
 console.log('Vacancy projection verified: exact rendered table parity for owners/sorts/flags, all levels/roles/search, application isolation, archived parity, strict compact SQL and personal detail.');
}finally{sqlite.close()}
