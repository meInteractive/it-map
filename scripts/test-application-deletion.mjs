import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import { DatabaseSync } from 'node:sqlite';

// Exercise the real HTTP handlers against SQLite, including fresh Worker instances.
const sqlite = new DatabaseSync(':memory:');
sqlite.exec('PRAGMA foreign_keys = ON');
const db = {
  prepare(sql) {
    let values = [];
    return {
      bind(...args) { values = args; return this; },
      async all() { return {results:sqlite.prepare(sql).all(...values)}; },
      async first() { return sqlite.prepare(sql).get(...values) ?? null; },
      async run() {
        const result = sqlite.prepare(sql).run(...values);
        return {success:true,meta:{changes:Number(result.changes),last_row_id:Number(result.lastInsertRowid)}};
      }
    };
  },
  async batch(statements) {
    sqlite.exec('BEGIN');
    try {
      const results = [];
      for (const statement of statements) results.push(await statement.run());
      sqlite.exec('COMMIT');
      return results;
    } catch (error) { sqlite.exec('ROLLBACK'); throw error; }
  }
};
const env = {DB:db,SITE_PASSWORD:'test-only',NASTYA_PASSWORD:'test-only',SESSION_SECRET:'test-only-secret'};
let generation = 0;
const freshWorker = async () => (await import(new URL('../.tests/backend.mjs?deletion-test='+generation++,import.meta.url))).default;
let worker = await freshWorker();
const login = async name => {
  const response = await worker.fetch(new Request('https://test.example/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test-only'})}),env);
  assert.equal(response.status,303);
  return response.headers.get('set-cookie').split(';')[0];
};
const eugene = await login('Eugene'), nastya = await login('Nastya');
const request = async (path,method='GET',cookie=eugene) => {
  const response = await worker.fetch(new Request('https://test.example'+path,{method,headers:{cookie}}),env);
  const body = await response.json();
  return {status:response.status,body};
};
assert.equal((await request('/api/crm/applications')).status,200);
for (const name of ['Linked','General','Keep']) {
  sqlite.prepare("INSERT INTO company_crm(company_name,status,comment_md,favorite) VALUES (?,'Собес 3','old comment',1)").run(name);
  sqlite.prepare("INSERT INTO company_status_history(company_name,to_status) VALUES (?,'Собес 3')").run(name);
}
sqlite.exec("INSERT INTO company_vacancies(id,company_name,title,url,application_status) VALUES (10,'Linked','Manager','https://example.test/10','Собес 3')");
sqlite.exec("INSERT INTO company_applications(id,user_name,company_name,vacancy_id,status) VALUES (1,'Eugene','Linked',10,'Собес 3'),(2,'Eugene','General',NULL,'Собес 3'),(3,'Nastya','Keep',NULL,'Офер'),(4,'Eugene','Keep',NULL,'Скрининг')");
for (const id of [1,2,3,4]) sqlite.prepare("INSERT INTO company_application_status_history(application_id,to_status) VALUES (?,'Собес 3')").run(id);
sqlite.exec("INSERT INTO company_tasks(company_name,vacancy_id,type,due_at) VALUES ('Linked',10,'call','2026-10-01T12:00:00Z')");
// Foreign-user deletion is rejected and must preserve the record and its history.
assert.equal((await request('/api/applications/3','DELETE')).status,404);
for (const id of [1,2]) {
  const removed = await request('/api/applications/'+id,'DELETE');
  assert.equal(removed.status,200);
  assert.equal(removed.body.records.length,0);
  assert.equal(sqlite.prepare('SELECT COUNT(*) AS n FROM company_application_status_history WHERE application_id=?').get(id).n,0);
}
for (let restart=0;restart<3;restart++) {
  worker = await freshWorker();
  const result = await request('/api/crm/applications');
  assert.equal(result.status,200);
  assert.deepEqual(result.body.records.map(record=>record.id),[4], 'Deleted applications returned after a cold start');
  assert.deepEqual((await request('/api/crm/applications','GET',nastya)).body.records.map(record=>record.id),[3]);
}
assert.equal((await request('/api/applications/1','DELETE')).status,404);
assert.equal(sqlite.prepare('SELECT COUNT(*) AS n FROM company_vacancies WHERE id=10').get().n,1);
assert.equal(sqlite.prepare('SELECT COUNT(*) AS n FROM company_tasks WHERE vacancy_id=10').get().n,1);
assert.equal(sqlite.prepare('SELECT favorite FROM company_crm WHERE company_name=?').get('Linked').favorite,1);
assert.equal(sqlite.prepare('SELECT COUNT(*) AS n FROM company_application_status_history WHERE application_id IN (3,4)').get().n,2);
sqlite.close();
console.log('Permanent deletion verified through HTTP: linked/general responses, three cold starts, history removal, user isolation and retained vacancies/tasks');
