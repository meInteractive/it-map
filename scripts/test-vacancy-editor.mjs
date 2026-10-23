import {vacancyTestPayload,applicationTestIntent} from './test-support.mjs';
import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import { DatabaseSync } from 'node:sqlite';

// Exercise the real HTTP handlers against SQLite, including fresh Worker instances.
const sqlite = new DatabaseSync(':memory:');
sqlite.exec('PRAGMA foreign_keys = ON');
// One SQLite connection serializes concurrent batches, as native D1 does.
let batchQueue = Promise.resolve();
const db = {
  prepare(sql) {
    let values = [];
    return {
      bind(...args) { values = args; return this; },
      async all() { return {results:sqlite.prepare(sql).all(...values)}; },
      async first() { return sqlite.prepare(sql).get(...values) ?? null; },
      async run() {
        const prepared = sqlite.prepare(sql);
        if (prepared.columns().length) return {success:true, results:prepared.all(...values)};
        const result = prepared.run(...values);
        return {success:true,meta:{changes:Number(result.changes),last_row_id:Number(result.lastInsertRowid)}};
      }
    };
  },
  batch(statements) {
    const operation = batchQueue.then(async () => {
      sqlite.exec('BEGIN');
      try {
        const results = [];
        for (const statement of statements) results.push(await statement.run());
        sqlite.exec('COMMIT');
        return results;
      } catch (error) { sqlite.exec('ROLLBACK'); throw error; }
    });
    batchQueue = operation.catch(() => {});
    return operation;
  }
};
const env = {DB:db,SITE_PASSWORD:'test-only',NASTYA_PASSWORD:'test-only',SESSION_SECRET:'test-only-secret'};
const worker = (await import(new URL('../.tests/backend.mjs?vacancy-editor-test', import.meta.url))).default;
import fs from 'node:fs';
import vm from 'node:vm';
import {buildBackupSchema} from './backup-schema.mjs';
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));
const companyName=catalog.companies[0].name;
async function login(name){const r=await worker.fetch(new Request('https://test.example/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test-only'})}),env);assert.equal(r.status,303);return r.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
async function request(path,method='GET',payload,cookie=admin,origin='https://test.example'){
 payload=await vacancyTestPayload(()=>request(path,'GET',undefined,cookie,origin),path,method,payload);
 return worker.fetch(new Request('https://test.example'+path,{method,headers:{...(cookie?{cookie}:{}),origin,'content-type':'application/json'},...(payload===undefined?{}:{body:JSON.stringify(payload)})}),env);
}
const input={coverLetterScope:'shared',companyName,entityType:'company',url:'https://vacancy.test/manual',title:'Ручная вакансия',role:'Менеджер проектов',level:'Senior',salaryFrom:150000,salaryTo:240000,relevance:87,publishedAt:'2026-09-27',loadedAt:'2026-09-28',descriptionMd:'## Обязанности\n\n- **Управление** проектами\n- Аналитика\n\n<script>alert(1)</script>',coverLetterSubject:'Отклик на вакансию',coverLetterMd:'Здравствуйте!\n\n**Мой опыт** подходит.'};
for(const [cookie,origin,status] of [[null,'https://test.example',401],[user,'https://test.example',403],[admin,'https://other.test',403]])assert.equal((await request('/api/vacancies','POST',input,cookie,origin)).status,status);
let response=await request('/api/vacancies','POST',input);assert.equal(response.status,201,await response.clone().text());
const created=(await response.json()).vacancies.find(v=>v.url===input.url);assert.ok(created);
for(const key of ['title','role','level','salaryFrom','salaryTo','relevance','publishedAt','loadedAt','descriptionMd','coverLetterSubject','coverLetterMd'])assert.equal(created[key],input[key],key);
const id=created.id;
assert.equal((await request('/api/vacancies','POST',input)).status,400);
assert.equal((await request('/api/vacancies/'+id,'PUT',{...input,title:'Forbidden'},user)).status,403);
const before=sqlite.prepare('SELECT * FROM company_vacancies WHERE id=?').get(id);
for(const patch of [{salaryFrom:250000,salaryTo:1},{salaryFrom:-1},{salaryTo:1.5},{relevance:101},{descriptionMd:''},{url:'javascript:alert(1)'}])assert.equal((await request('/api/vacancies/'+id,'PUT',{...input,...patch})).status,400);
assert.deepEqual(sqlite.prepare('SELECT * FROM company_vacancies WHERE id=?').get(id),before);
const application=await request('/api/vacancies/'+id+'/apply','POST',{companyName,...await applicationTestIntent(()=>request('/api/applications/create-context'))});assert.equal(application.status,200,await application.clone().text());
sqlite.prepare('INSERT INTO company_tasks(user_name,company_name,vacancy_id,type,note,due_at) VALUES(?,?,?,?,?,?)').run('Eugene',companyName,id,'call','Не менять','2026-10-01T12:00:00.000Z');
await request('/api/vacancies/'+id+'/state','PUT',{favorite:true});
sqlite.prepare('INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite) VALUES(?,?,?,?)').run('Nastya',id,42,1);
const applicationsBefore=sqlite.prepare('SELECT * FROM company_applications WHERE vacancy_id=?').all(id),tasksBefore=sqlite.prepare('SELECT * FROM company_tasks WHERE vacancy_id=?').all(id);
const updated={...input,title:'Обновлённая вакансия',salaryFrom:null,salaryTo:300000,relevance:0,descriptionMd:'# Новый текст\n\n- Сохранить **Markdown**',coverLetterSubject:'',coverLetterMd:''};
response=await request('/api/vacancies/'+id,'PUT',updated);assert.equal(response.status,200,await response.clone().text());
response=await request('/api/vacancies?company='+encodeURIComponent(companyName));const reread=(await response.json()).vacancies.find(v=>v.id===id);
for(const key of ['title','salaryFrom','salaryTo','relevance','descriptionMd','coverLetterSubject','coverLetterMd'])assert.equal(reread[key],updated[key],key);
assert.equal(reread.favorite,true);assert.deepEqual(sqlite.prepare('SELECT * FROM company_applications WHERE vacancy_id=?').all(id),applicationsBefore);assert.deepEqual(sqlite.prepare('SELECT * FROM company_tasks WHERE vacancy_id=?').all(id),tasksBefore);
assert.equal(sqlite.prepare('SELECT relevance FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=?').get('Nastya',id).relevance,42);
await (await import('./test-react.mjs')).testVacancyEditor();
console.log('Vacancy editor: API authorization/fields/relations, React POST/PUT, null/zero values, retained drafts and source preservation passed.');
