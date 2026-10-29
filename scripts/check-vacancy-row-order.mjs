// Неизменённый readVacancyProjectionRows выполняет исходный ORDER BY на SQLite.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {readVacancyProjectionRows} from '../server/services/vacancy-projection.js';
import {matchesPublicationBounds,publicationBounds} from '../shared/publication-period.ts';
assert.equal(process.versions.node,'24.19.0');
globalThis.fetch=()=>{throw new Error('Vacancy row oracle forbids network');};
async function reference(input){
 const sqlite=new DatabaseSync(':memory:');
 try{
  sqlite.exec(`CREATE TABLE company_vacancies(id INTEGER PRIMARY KEY,company_name TEXT NOT NULL,entity_type TEXT DEFAULT 'company',real_company_name TEXT DEFAULT '',title TEXT DEFAULT 'CI row',salary_from INTEGER,salary_to INTEGER,role TEXT DEFAULT '',level TEXT DEFAULT '',published_at TEXT,loaded_at TEXT,archived INTEGER DEFAULT 0,created_at TEXT NOT NULL);
CREATE TABLE company_vacancy_user_state(user_name TEXT,vacancy_id INTEGER,relevance INTEGER,favorite INTEGER,hidden INTEGER);
CREATE TABLE company_applications(id INTEGER,user_name TEXT,vacancy_id INTEGER,status TEXT,updated_at TEXT);
CREATE TABLE vacancy_telegram_sources(vacancy_id INTEGER,channel_id INTEGER);
CREATE TABLE telegram_channels(id INTEGER,name TEXT);
CREATE TABLE vacancy_role_assignments(vacancy_id INTEGER,role_id INTEGER);`);
  const insert=sqlite.prepare('INSERT INTO company_vacancies(id,company_name,published_at,loaded_at,created_at) VALUES(?,?,?,?,?)');
  for(const r of input.rows)insert.run(r.id,r.company,r.published,r.loaded,r.created);
  const insertState=sqlite.prepare('INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite,hidden) VALUES(?,?,?,?,?)');
  for(const state of input.states??[])insertState.run(state.user,state.id,state.relevance,state.favorite,state.hidden);
  const db={prepare(sql){let args=[];return {bind(...v){args=v;return this;},async all(){return {results:sqlite.prepare(sql).all(...args)};}};}};
  const vacancies=await readVacancyProjectionRows(db,input.user??'Demo',null,{byId:new Map()});
  const expected={};
  for(const period of ['none','all','week','month','twoMonths','archive'])expected[period]=period==='none'?vacancies:vacancies.filter(v=>matchesPublicationBounds(v,publicationBounds(period,input.now)));
  return {...input,expected};
 }finally{sqlite.close();}
}
if(process.argv.includes('--reference'))process.stdout.write(JSON.stringify(await reference(JSON.parse(readFileSync(0,'utf8')))));
else{
 const now=Date.parse('2026-10-29T10:00:00Z');
 const row=(id,company,published=null,loaded=null,created='2026-10-01 00:00:00')=>({id,company,published,loaded,created});
 const cases=[],add=(name,rows)=>cases.push({name,rows,now});
 add('empty',[]);
 add('ASCII case',[row(1,'alpha'),row(2,'Zulu'),row(3,'Beta')]);
 add('case ties dates IDs',[row(1,'A','2026-10-29'),row(2,'a','2026-10-28'),row(3,'A','2026-10-29'),row(4,'a',null),row(5,'A','')]);
 add('NULL vs empty',[row(1,'CI',null,'2026-10-29'),row(2,'CI','','2026-10-28'),row(3,'CI','2026-10-01'),row(4,'CI','2026-10-29'),row(5,'CI','')]);
 add('Unicode stays case sensitive',['А','а','Б','б','Ё','Е','Å','A\u030a','É','E\u0301','😀','中','A','a','İ','ı'].map((n,i)=>row(i+1,n)));
 add('embedded NUL',['a\0x','A\0Y','a','a\0long','Aq','a\0z','\0a','\0b','\0abc'].map((n,i)=>row(i+1,n,i%2?'2026-10-28':null)));
 add('NUL byte lengths',['a\0я','A\0aa','a\0😀','A\0xxxx','a\0é','A\0zz'].map((n,i)=>row(i+1,n)));
 add('spaces and suffixes',['A ','A','a\t','a\n','a\u00a0','a.','A-','a_'].map((n,i)=>row(i+1,n)));
 add('binary published text',[null,'','2026-10-29','2026-10-29 ','2026-10-29\0x','2026-10-29\0y','А','а','A','a','2026-02-30','2026-10-29T23:00:00Z','2026-10-01'].map((d,i)=>row(i+1,'CI',d,'2026-10-29')));
 const names=['A','a','B','b','CI','ci','А','а','Ё','Е','😀','a\0x','A\0y','a\0long',' A','A ','中','İ','i'];
 const dates=[null,'','2026-10-29','2026-10-28','2026-10-01','2026-09-01','2026-08-29','2025-12-31','invalid','2026-02-30','2026-10-29T23:00:00Z','2026-10-29\0x'];
 let seed=338;const next=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed;};
 for(let n=0;n<96;n++)add('generated '+n,Array.from({length:36},(_,i)=>row(i+1,names[next()%names.length],dates[next()%dates.length],dates[next()%dates.length],dates[next()%dates.length]||'2026-10-01')).reverse());
 for(const input of cases)process.stdout.write(JSON.stringify(await reference(input))+'\n');
 console.error('Vacancy row source SQLite: '+cases.length+' frames, 6 periods each');
}
