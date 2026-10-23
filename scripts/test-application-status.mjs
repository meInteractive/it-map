import {prepareBackend,applicationTestIntent,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import { DatabaseSync } from 'node:sqlite';

// Exercise the real HTTP handlers against SQLite, including fresh Worker instances.
const sqlite = new DatabaseSync(':memory:');
sqlite.exec('PRAGMA foreign_keys = ON');
// Native D1 serializes batches. This single SQLite connection must do the same
// when production response readers issue independent guarded reads in parallel.
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
const worker = (await import(new URL('../.tests/backend.mjs?application-status-test', import.meta.url))).default;
import fs from 'node:fs';
import vm from 'node:vm';
import {buildBackupSchema} from './backup-schema.mjs';
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));
const companyName=catalog.companies[0].name;
async function login(name){const r=await worker.fetch(new Request('https://test.example/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test-only'})}),env);assert.equal(r.status,303);return r.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),other=await login('Nastya');
async function request(path,method='GET',payload,cookie=admin){return worker.fetch(new Request('https://test.example'+path,{method,headers:{cookie,origin:'https://test.example','content-type':'application/json'},...(payload===undefined?{}:{body:JSON.stringify(payload)})}),env);}
let response=await request('/api/applications','POST',{companyName,status:'Отказ',comment:'Получен отказ',applicationSources:['hh'],...await applicationTestIntent(()=>request('/api/applications/create-context'))});
assert.equal(response.status,201,await response.clone().text());
let records=(await response.json()).records;
assert.equal(records.length,1);assert.equal(records[0].status,'Отказ');assert.equal(records[0].history[0].toStatus,'Отказ');
const id=records[0].id;
response=await request('/api/crm/applications');records=(await response.json()).records;
assert.equal(records.find(record=>record.id===id).status,'Отказ','API must keep rejected applications');
response=await request('/api/crm/applications','GET',undefined,other);assert.ok(!(await response.json()).records.some(record=>record.id===id));
response=await request('/api/applications/'+id,'PUT',{revision:records.find(r=>r.id===id).revision,status:'Откликнулся',comment:'Повторная попытка',applicationSources:['hh']});assert.equal(response.status,200,await response.clone().text());
response=await request('/api/applications/'+id,'PUT',{revision:(await response.clone().json()).records.find(r=>r.id===id).revision,status:'Отказ',comment:'Повторный отказ',applicationSources:['hh']});assert.equal(response.status,200,await response.clone().text());
const history=sqlite.prepare('SELECT from_status, to_status FROM company_application_status_history WHERE application_id=? ORDER BY id').all(id);
assert.deepEqual(history.map(row=>[row.from_status,row.to_status]),[['','Отказ'],['Отказ','Откликнулся'],['Откликнулся','Отказ']]);
assert.equal(sqlite.prepare('SELECT status FROM company_applications WHERE id=?').get(id).status,'Отказ');
const {applicationMatchesStatusFilter:matches,StatusOptions}=await moduleFor('frontend/features/applications/Applications.tsx');assert.match(render(StatusOptions),/<option value="Отказ">Отказ<\/option>/);
for(const status of ['', 'Откликнулся','Офер','Отказ']){
 assert.equal(matches({status},''),status!=='Отказ','Default filter: '+status);
 assert.equal(matches({status},'Отказ'),status==='Отказ','Explicit rejection filter: '+status);
 assert.equal(matches({status},'__none__'),status==='','No-status filter: '+status);
}
console.log('Application rejection verified: POST/PUT, history, owner isolation, API preservation, both UI status controls and default/explicit filters.');
