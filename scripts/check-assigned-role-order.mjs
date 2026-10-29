// Неизменённый attachVacancyRoles выполняет запрос назначений на настоящей SQLite.
// Каталог передаётся явно: его дерево и метаданные проверены отдельно в GO-319.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {attachVacancyRoles} from '../server/repositories/vacancy-roles.js';
assert.equal(process.versions.node,'24.19.0');
assert.equal(process.versions.icu,'78.3');
globalThis.fetch=()=>{throw new Error('Assigned role oracle forbids network');};
async function reference(input){
 const sqlite=new DatabaseSync(':memory:');
 try{
  sqlite.exec('CREATE TABLE vacancy_role_assignments(vacancy_id INTEGER,role_id INTEGER)');
  const insert=sqlite.prepare('INSERT INTO vacancy_role_assignments VALUES(?,?)');
  for(const a of input.assignments)insert.run(a.vacancyId,a.roleId);
  const db={prepare(sql){let bindings=[];return {bind(...args){bindings=args;return this;},async all(){return {results:sqlite.prepare(sql).all(...bindings)};}};}};
  const catalog={byId:new Map(input.roles.map(r=>[r.id,r]))};
  return {...input,expected:await attachVacancyRoles(db,input.vacancies,catalog)};
 }finally{sqlite.close();}
}
if(process.argv.includes('--reference')){
 process.stdout.write(JSON.stringify(await reference(JSON.parse(readFileSync(0,'utf8')))));
}else{
 const role=(id,name,path=name)=>({id,stableKey:'CI_role_'+id,name,path,parentId:null,sortOrder:0,usageCount:1,childCount:0,isLeaf:true,descendantIds:[id]});
 const fixtures=[];
 const add=(name,roles,assignments=roles.map(r=>({vacancyId:1,roleId:r.id})),vacancies=[{id:1,role:'CI прежняя роль'},{id:2,role:''}])=>fixtures.push({name,roles,assignments,vacancies});
 add('empty',[],[],[]);
 add('no assignments',[]);
 add('mixed alphabet',['Z','А','а','A','Ёлка','Елка','_','-', '😀','Я'].map((n,i)=>role(i+1,n)));
 add('canonical stable ties',[role(8,'Å'),role(2,'A\u030a'),role(4,'Å')]);
 add('hierarchy paths',[role(5,'A','ИТ / Я'),role(3,'Я','ИТ / A'),role(7,'Б','А / Б'),role(1,'а','ИТ / а')]);
 add('unknown catalog role',[role(1,'CI role')],[{vacancyId:1,roleId:99},{vacancyId:1,roleId:1},{vacancyId:3,roleId:1}]);
 add('fallback text',[role(1,'CI role')],[],[{id:1,role:'CI исходная роль'},{id:2,role:null},{id:3,role:''}]);
 const names=['А','а','Ё','Е','Я','я','ИТ','Z','A','a','A10','A2','ß','İ','É','E\u0301','Å','A\u030a','😀','_','-','ﬁ','中'];
 let seed=336;const next=()=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed;};
 for(let n=0;n<160;n++){
  const roles=Array.from({length:24},(_,i)=>role(i+1,names[next()%names.length],(n%2?'ИТ / ':'')+names[next()%names.length]));
  const assignments=roles.map(r=>({vacancyId:next()%2+1,roleId:r.id})).reverse();
  add('generated '+n,roles,assignments);
 }
 for(const input of fixtures)process.stdout.write(JSON.stringify(await reference(input))+'\n');
 console.error('Assigned role source SQLite: '+fixtures.length+' frames');
}
