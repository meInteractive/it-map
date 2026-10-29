// Неизменённый router читает роли на полной исходной SQLite-схеме.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {createSession} from '../server/auth/session.js';
import router from '../server/routes/index.js';
import {readVacancyRoleCatalog} from '../server/repositories/vacancy-roles.js';
assert.equal(process.versions.node,'24.19.0');assert.equal(process.versions.icu,'78.3');
globalThis.fetch=()=>{throw new Error('Role read oracle forbids network');};
const secret='CI_SYNTHETIC_ROLE_READ_SECRET_341',root='/api/vacancy-roles';
const seeds=[{id:1,stableKey:'CI_341_it',name:'ИТ',parentId:null,sortOrder:0},{id:2,stableKey:'CI_341_e',name:'Е',parentId:null,sortOrder:0},{id:3,stableKey:'CI_341_a',name:'Å',parentId:1,sortOrder:0},{id:4,stableKey:'CI_341_combining_a',name:'A\u030a',parentId:1,sortOrder:0},{id:5,stableKey:'CI_341_ya',name:'Я',parentId:1,sortOrder:1},{id:6,stableKey:'CI_341_leaf',name:'CI лист 😀',parentId:3,sortOrder:0}];
const assignments=[{vacancy:1,role:5},{vacancy:2,role:5},{vacancy:1,role:6}];
const paths=[root,root+'/roots',root+'/leaves',...[1,'01',2,3,4,5,6,999,0,'000','9007199254740992','9223372036854775807','9223372036854775808','9'.repeat(320),'+1','-1','1e0','0x1','%31','1%20','%2B1','%EF%BC%91','%00'].map(id=>root+'/'+id+'/descendants'),'/api/%76acancy-roles',root+'/%72oots',root+'/1/%64escendants',root+'/1/descendants?extra=1'];
const cases=[];
for(const user of ['Eugene','Nastya','Demo',''])for(const path of paths)cases.push({name:(user||'anonymous')+' '+path,path,user});
for(const user of ['Eugene','Nastya','Demo'])for(const path of paths.slice(0,4))cases.push({name:'empty '+user+' '+path,path,user,empty:true});
for(const user of ['Eugene','Nastya','Demo',''])for(const path of [root,root+'/roots',root+'/leaves',root+'/0/descendants',root+'/9223372036854775808/descendants',root+'/+1/descendants',root+'/%31/descendants'])cases.push({name:'no DB '+(user||'anonymous')+' '+path,path,user,noDB:true});
for(const user of ['Eugene','Nastya','Demo'])cases.push({name:'GET ignores Origin '+user,path:root+'/1/descendants',user,origin:'https://other.invalid'});
for(const path of [root,root+'/0/descendants',root+'/+1/descendants'])cases.push({name:'bad cookie '+path,path,user:'Demo',badCookie:true});
for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');
 try{
  assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
  sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
  sqlite.exec("INSERT INTO company_crm(company_name) VALUES('CI GO341 employer'); INSERT INTO company_vacancies(id,company_name,url,title) VALUES(1,'CI GO341 employer','https://example.invalid/go341/1','CI role usage'),(2,'CI GO341 employer','https://example.invalid/go341/2','CI role usage')");
  if(!c.empty){
   for(const r of seeds)sqlite.prepare('INSERT INTO vacancy_roles(id,stable_key,name,parent_id,sort_order) VALUES(?,?,?,?,?)').run(r.id,r.stableKey,r.name,r.parentId,r.sortOrder);
   for(const a of assignments)sqlite.prepare('INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)').run(a.vacancy,a.role);
  }
  let reads=0,rows=[];
  const db={prepare(sql){assert.match(sql,/^\s*(SELECT|WITH)\b/i,'source read attempted a write/DDL');reads++;return {async all(){const result=sqlite.prepare(sql).all();rows=structuredClone(result);return {results:result};}};}};
  // Фиксируем реальный SQL read-set и для путей, которые router не читает.
  await readVacancyRoleCatalog(db);const preparedRows=rows;reads=0;
  const headers={};if(c.user)headers.cookie='it_map_session='+(c.badCookie?'CI_INVALID_COOKIE':await createSession(secret,c.user));if(c.origin)headers.origin=c.origin;
  const before=sqlite.prepare('SELECT total_changes() n').get().n;
  const response=await router.fetch(new Request('https://example.invalid'+c.path,{headers}),{SESSION_SECRET:secret,SITE_PASSWORD:'CI_UNUSED',DB:c.noDB?undefined:db},{});
  assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'role GET mutated source');
  process.stdout.write(JSON.stringify({...c,seeds:c.empty?[]:seeds,assignments:c.empty?[]:assignments,rows:preparedRows,reads,status:response.status,responseBody:await response.text()})+'\n');
 }finally{sqlite.close();}
}
process.stderr.write(`Role GET unchanged router/SQLite: ${cases.length} frames\n`);
