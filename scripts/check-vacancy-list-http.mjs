// Неизменённый GET списка вакансий и URLSearchParams на полной SQLite-схеме.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState,EUGENE_RELEVANCE_PROFILE_VERSION} from '../server/constants.js';
import {createSession} from '../server/auth/session.js';
import router from '../server/routes/index.js';
assert.equal(process.versions.node,'24.19.0');
globalThis.fetch=()=>{throw new Error('Vacancy list HTTP oracle forbids network');};
const now=Date.parse('2026-10-29T10:00:00Z');Date.now=()=>now;
const secret='CI_SYNTHETIC_LIST_READ_SECRET_342',path='/api/vacancies/list';
const queries=['','period=all','period=week','period=month','period=twoMonths','period=archive','period=','period=year','period=ALL','period=%20week','period=week+','period=week%00','period=%FF','period=%ZZ','period=week;extra=1','period=week;bad&period=all','period=all&period=week;bad','period=&period=all','period=all&period=','period=week&unknown=1','%70eriod=all','%ZZ=1&period=week','x=1;period=all','Period=week','period[]=week','period%3Dall','period','&&period=all&=ignored','period=all%26period=week','period=all;','period=all%3B','period=week;period=all','period=%ED%A0%80'];
const cases=[];for(const user of ['Eugene','Nastya','Demo',''])for(const query of queries)cases.push({name:(user||'anonymous')+' '+query,user,query,path});
for(const user of ['Eugene','Nastya','Demo'])for(const query of queries.slice(0,6))cases.push({name:'empty '+user+' '+query,user,query,path,empty:true});
for(const user of ['Eugene','Nastya','Demo',''])for(const query of ['','period=','period=week;bad'])cases.push({name:'no DB '+(user||'anonymous')+' '+query,user,query,path,noDB:true});
for(const user of ['Eugene','Nastya','Demo',''])for(const p of ['/api/vacancies/%6cist','/api/%76acancies/list'])cases.push({name:'alias '+(user||'anonymous')+' '+p,user,query:'period=',path:p});
for(const user of ['Eugene','Nastya','Demo'])cases.push({name:'GET ignores Origin '+user,user,query:'period=week',path,origin:'https://other.invalid'});
for(const query of ['','period=','period=week;bad'])cases.push({name:'bad cookie '+query,user:'Demo',badCookie:true,query,path});
for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');
 try{
  assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
  sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run('{"companies":[],"agencies":[]}');
  sqlite.prepare('INSERT INTO vacancy_relevance_profile_state(user_name,profile_version) VALUES(?,?)').run('Eugene',EUGENE_RELEVANCE_PROFILE_VERSION);
  if(!c.empty){
   sqlite.exec("INSERT INTO company_crm(company_name) VALUES('alpha'),('Beta'); INSERT INTO company_vacancies(id,company_name,url,title,published_at,created_at) VALUES(1,'alpha','https://example.invalid/go342/1','CI period','2026-10-28','2026-10-28 10:00:00'),(2,'Beta','https://example.invalid/go342/2','CI period','2026-07-01','2026-07-01 10:00:00'); INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite,hidden) VALUES('Demo',1,11,1,0),('Nastya',1,22,0,1); INSERT INTO company_applications(user_name,company_name,vacancy_id) VALUES('Demo','alpha',NULL),('Nastya','Beta',NULL)");
  }
  const db={prepare(sql){assert.match(sql,/^\s*(SELECT|WITH)\b/i,'source GET attempted write/DDL');let args=[];return {bind(...a){args=a;return this;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async first(column){const row=sqlite.prepare(sql).get(...args)??null;return column&&row?row[column]:row;}};}};
  const headers={};if(c.user)headers.cookie='it_map_session='+(c.badCookie?'CI_INVALID_COOKIE':await createSession(secret,c.user));if(c.origin)headers.origin=c.origin;
  const request=new Request('https://example.invalid'+c.path+'?'+c.query,{headers}),before=sqlite.prepare('SELECT total_changes() n').get().n;
  const response=await router.fetch(request,{SESSION_SECRET:secret,SITE_PASSWORD:'CI_UNUSED',DB:c.noDB?undefined:db},{});
  assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'source GET mutated prepared data');
  process.stdout.write(JSON.stringify({...c,now,period:new URL(request.url).searchParams.get('period'),status:response.status,responseBody:await response.text()})+'\n');
 }finally{sqlite.close();}
}
process.stderr.write(`Vacancy list GET unchanged router/SQLite: ${cases.length} frames\n`);
