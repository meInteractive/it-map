// Неизменённый readCompactVacancyList читает полную исходную SQLite-схему.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState,EUGENE_RELEVANCE_PROFILE_VERSION} from '../server/constants.js';
import {readCompactVacancyList} from '../server/services/vacancy-projection.js';
assert.equal(process.versions.node,'24.19.0');
globalThis.fetch=()=>{throw new Error('Application summary oracle forbids network');};
const users=['Eugene','Nastya','Demo'],periods=['none','all','week','month','twoMonths','archive'];
async function reference(input){
 const sqlite=new DatabaseSync(':memory:');
 try{
  assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
  sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
  sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run('{"companies":[],"agencies":[]}');
  // Профиль подготовлен заранее: исходный GET не должен запускать backfill.
  sqlite.prepare('INSERT INTO vacancy_relevance_profile_state(user_name,profile_version) VALUES(?,?)').run('Eugene',EUGENE_RELEVANCE_PROFILE_VERSION);
  const names=new Set([...input.rows.flatMap(r=>[r.company,r.real]),...input.apps.map(a=>a.company)]);
  for(const name of names)sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(name);
  for(const r of input.rows)sqlite.prepare('INSERT INTO company_vacancies(id,company_name,real_company_name,url,title,published_at,created_at) VALUES(?,?,?,?,?,?,?)').run(r.id,r.company,r.real,`https://example.invalid/go339/${r.id}`,'CI summary',r.published,r.created);
  for(const a of input.apps)sqlite.prepare('INSERT INTO company_applications(user_name,company_name,vacancy_id) VALUES(?,?,?)').run(a.user,a.company,a.vacancy);
  const db={prepare(sql){let args=[];return {bind(...values){args=values;return this;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async first(column){const row=sqlite.prepare(sql).get(...args)??null;return column&&row?row[column]:row;},async run(){return {meta:sqlite.prepare(sql).run(...args)};}};},async batch(statements){const out=[];for(const s of statements)out.push(await s.run());return out;}};
  const before=sqlite.prepare('SELECT total_changes() n').get().n,expected={},vacancies={};
  for(const user of users){
   expected[user]={};
   for(const p of periods){
    const body=await readCompactVacancyList(db,user,{period:p==='none'?undefined:p,now:input.now});
    expected[user][p]=body.applicationSummary;if(p==='none')vacancies[user]=body.vacancies;
   }
  }
  assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'prepared source GET wrote data');
  return {...input,expected,vacancies};
 }finally{sqlite.close();}
}
if(process.argv.includes('--reference')){
 process.stdout.write(JSON.stringify(await reference(JSON.parse(readFileSync(0,'utf8'))))+'\n');
}else{
 const frames=[],now=Date.parse('2026-10-29T10:00:00Z');
 const add=(name,pairs,options={})=>{
  const rows=pairs.map((pair,i)=>({id:i+1,company:pair[0],real:pair[1]??'',published:i%2?'2026-07-01':'2026-10-28',created:'2026-10-28 10:00:00'}));
  const names=[...new Set([...pairs.flat(),'', 'CI absent'])];
  const apps=names.flatMap((company,i)=>users.flatMap((user,u)=>((i+u)%3===0?[]:[{user,company,vacancy:null}])));
  if(rows.length){apps.push({user:'Demo',company:rows[0].company,vacancy:rows[0].id});apps.push({user:'Demo',company:rows[0].company,vacancy:null});}
  frames.push({name,rows,apps,now,...options});
 };
 add('empty',[]);
 add('ASCII source order',[['alpha'],['Zulu'],['Beta']]);
 add('real name first appearance',[['alpha','zulu'],['Beta','Aardvark'],['zulu','alpha']]);
 add('dedupe exact names',[['alpha','Beta'],['alpha','Beta'],['Beta','alpha'],['ALPHA','beta']]);
 add('period keeps full summary',[['alpha','Old real'],['Beta','New real'],['Zulu']]);
 add('Unicode exact names',[['Я'],['я'],['е\u0308','ё'],['Ё','Е'],['é','e\u0301']]);
 add('embedded NUL',[['a\0Z','a\0A'],['a\0A','a'],['A','a\0Z']]);
 add('empty and whitespace',[['',' '],[' ',''],['\t','alpha'],['alpha','']]);
 add('linked applications only',[['alpha','Real'],['Beta']],{apps:[{user:'Demo',company:'alpha',vacancy:1},{user:'Nastya',company:'Real',vacancy:2}]});
 let seed=339;const rand=n=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed%n;};
 const names=['alpha','ALPHA','Beta','Zulu','Я','я','é','e\u0301',' ','','a\0B','a\0A'];
 for(let i=0;i<48;i++)add('random '+i,Array.from({length:18},()=>[names[rand(names.length)],names[rand(names.length)]]));
 for(const frame of frames)process.stdout.write(JSON.stringify(await reference(frame))+'\n');
 process.stderr.write(`Application summary source SQLite: ${frames.length} datasets × 3 users × 6 periods\n`);
}
