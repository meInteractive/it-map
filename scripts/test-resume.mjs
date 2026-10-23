import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {DatabaseSync} from 'node:sqlite';
const worker=(await import('../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');
const db = {
 prepare(sql) {
  let args=[];
  return {
   bind(...values){args=values;return this},
   async first(){return sqlite.prepare(sql).get(...args)||null},
   async all(){return {results:sqlite.prepare(sql).all(...args)}},
   async run(){return {success:true,meta:sqlite.prepare(sql).run(...args)}}
  };
 }
};
const env={DB:db,SITE_PASSWORD:'profile-test',NASTYA_PASSWORD:'profile-test',DEMO_PASSWORD:'profile-test',SESSION_SECRET:'profile-test-secret'};
const base='https://resume-test.example';
const cookies={};
for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'profile-test'})}),env);assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0]}
const req=(user,method='GET',payload,extra={})=>worker.fetch(new Request(base+'/api/profile/resume',{method,headers:{...(user?{cookie:cookies[user]}:{}),'content-type':'application/json',...extra},body:payload===undefined?undefined:JSON.stringify(payload)}),env);
const get=async name=>(await req(name)).json();
try{
 assert.equal((await req(null)).status,401);
 for(const name of Object.keys(cookies)){
  assert.deepEqual(await get(name),{resumeMd:'',revision:0,updatedAt:null,maxLength:20000});
  const text='# '+name+'\n\n**Опыт** и достижения';const saved=await req(name,'PUT',{resumeMd:text,revision:0});assert.equal(saved.status,200);assert.equal((await saved.json()).revision,1);assert.equal((await get(name)).resumeMd,text);
  const {default:Settings}=await moduleFor('frontend/features/settings/Settings.tsx');const page=render(Settings,{user:sessionUser(name==='Eugene'),tab:'resume'});assert.match(page,/Резюме/);assert.equal(render(Settings,{user:sessionUser(name==='Eugene'),tab:'docs'}).includes('Документация'),name==='Eugene');assert.ok(!page.includes(text));

 }
 const before=await get('Nastya');
 assert.equal((await req('Nastya','PUT',{resumeMd:'foreign',revision:1,userName:'Eugene'})).status,400);
 assert.equal((await req('Nastya','PUT',{resumeMd:'x'.repeat(20001),revision:1})).status,400);
 assert.equal((await req('Nastya','PUT',{resumeMd:null,revision:1})).status,400);
 assert.equal((await req('Nastya','PUT',{resumeMd:'abc\0def',revision:1})).status,400);
 assert.equal((await req('Nastya','PUT',{resumeMd:'origin',revision:1},{origin:'https://other.example'})).status,403);
 assert.deepEqual(await get('Nastya'),before);
 assert.equal((await req('Nastya','PUT',{resumeMd:'😀'.repeat(20000),revision:1})).status,200);
 assert.equal(Array.from((await get('Nastya')).resumeMd).length,20000);
 assert.equal((await req('Nastya','PUT',{resumeMd:'stale',revision:1})).status,409);
 assert.equal((await req('Nastya','PUT',{resumeMd:'',revision:2})).status,200);assert.equal((await get('Nastya')).resumeMd,'');
 assert.equal((await get('Eugene')).resumeMd,'# Eugene\n\n**Опыт** и достижения');
 assert.equal((await req('Eugene','DELETE')).status,405);
 assert.equal((await req('Eugene','PUT',{resumeMd:'x'.repeat(256*1024),revision:1})).status,413);
 await (await import('./test-react.mjs')).testRevisionEditor();
 console.log('Resume verified: per-user isolation, all accounts, 20000 Unicode characters, persistence/clearing, optimistic conflicts, CSRF/size limits, safe HTML, client load/save/preview/navigation and retained drafts.');
}finally{sqlite.close()}
