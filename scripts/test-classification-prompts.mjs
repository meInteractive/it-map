import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?vacancy-roles'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);


try{
 assert.equal((await request('/api/classification/settings','GET',undefined,user)).status,403);
 let response=await request('/api/classification/settings');assert.equal(response.status,200);let settings=await response.json();
 assert.equal(settings.roles.length,2);assert.ok(settings.roles.every(r=>r.enabled&&r.markdown&&r.revision===1));
 const it=settings.roles.find(r=>r.name==='IT Manager');
 const old=await (await request('/api/profile/prompts')).json();
 response=await request('/api/classification/prompts/'+it.roleId,'PUT',{markdown:'# Custom 😀',revision:it.revision});assert.equal(response.status,200);const saved=await response.json();
 assert.equal(saved.revision,2);
 assert.equal((await request('/api/classification/prompts/'+it.roleId,'PUT',{markdown:'stale',revision:1})).status,409);
 assert.equal(settings.maxLength,10000);assert.match(it.markdown,/Лидер продукта, владелец продукта/);assert.match(it.markdown,/Product Owner/);assert.doesNotMatch(it.markdown,/SberPay|плат[её]ж|fintech|эквайринг/i);
 assert.equal((await request('/api/classification/prompts/'+it.roleId,'PUT',{markdown:'😀'.repeat(10001),revision:2})).status,400);
 response=await request('/api/classification/prompts/'+it.roleId,'PUT',{markdown:'😀'.repeat(10000),revision:2});assert.equal(response.status,200);assert.equal(Array.from((await response.json()).markdown).length,10000);
 assert.equal((await request('/api/classification/prompts/'+it.roleId,'PUT',{markdown:'# Custom 😀',revision:3})).status,200);
 assert.equal((await request('/api/classification/prompts/'+it.roleId,'PUT',{markdown:'x',revision:2},user)).status,403);
 await request('/api/vacancy-roles/'+it.roleId,'PUT',{name:'Renamed root'});
 settings=await (await request('/api/classification/settings')).json();assert.equal(settings.roles.find(r=>r.roleId===it.roleId).markdown,'# Custom 😀');
 const tree=await (await request('/api/vacancy-roles')).json(),leaf=tree.leaves[0];
 assert.equal((await request('/api/classification/prompts/'+leaf.id,'PUT',{markdown:'bad',revision:0})).status,409);
 await request('/api/vacancy-roles','POST',{name:'New root'});
 settings=await (await request('/api/classification/settings')).json();const fresh=settings.roles.find(r=>r.name==='New root');assert.equal(fresh.markdown,'');assert.equal(fresh.enabled,false);assert.equal(fresh.defaultMarkdown,null);
 assert.equal((await request('/api/vacancy-roles/'+fresh.roleId,'DELETE')).status,200);
 const journalist=settings.roles.find(r=>r.name==='Журналист');
 await request('/api/vacancy-roles/'+it.roleId,'PUT',{parentId:journalist.roleId});
 settings=await (await request('/api/classification/settings')).json();assert.ok(!settings.roles.some(r=>r.roleId===it.roleId));
 await request('/api/vacancy-roles/'+it.roleId,'PUT',{parentId:null});
 settings=await (await request('/api/classification/settings')).json();assert.equal(settings.roles.find(r=>r.roleId===it.roleId).markdown,'# Custom 😀');
 assert.deepEqual(await (await request('/api/profile/prompts')).json(),old,'legacy prompts unchanged');
 const backup=await (await request('/api/backup')).json();assert.equal(backup.version,39);assert.ok(backup.storage.tables.find(t=>t.name==='vacancy_role_classification').rows.some(r=>r.markdown==='# Custom 😀'));
 const current=(await (await request('/api/classification/settings')).json()).roles.find(r=>r.roleId===it.roleId);
 assert.equal((await request('/api/classification/roles/'+it.roleId,'PUT',{enabled:false,revision:current.revision},user)).status,403);
 response=await request('/api/classification/roles/'+it.roleId,'PUT',{enabled:false,revision:current.revision});assert.equal(response.status,200);
 const toggled=(await (await request('/api/classification/settings')).json()).roles.find(r=>r.roleId===it.roleId);assert.equal(toggled.enabled,false);assert.equal(toggled.markdown,'# Custom 😀');
 assert.equal((await request('/api/classification/roles/'+it.roleId,'PUT',{enabled:true,revision:current.revision})).status,409);
 assert.equal((await request('/api/classification/roles/'+leaf.id,'PUT',{enabled:true,revision:0})).status,409);
 console.log('Classification prompts: admin/CAS/Unicode/defaults/rename/move/new-root/backup passed');
}finally{sqlite.close();}
