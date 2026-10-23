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
        const prepared = sqlite.prepare(sql);
        if (prepared.columns().length) return {success:true, results:prepared.all(...values)};
        const result = prepared.run(...values);
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
const worker = (await import(new URL('../.tests/backend.mjs?task-schedule-test', import.meta.url))).default;
import fs from 'node:fs';
import vm from 'node:vm';
import {buildBackupSchema} from './backup-schema.mjs';
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
sqlite.exec("INSERT INTO company_crm(company_name) VALUES('Synthetic'); INSERT INTO company_vacancies(id,company_name,url,title) VALUES(1,'Synthetic','https://example.test/v','Synthetic vacancy'); INSERT INTO company_tasks(id,user_name,company_name,vacancy_id,type,note,due_at,completed,completed_at) VALUES(1,'Eugene','Synthetic',1,'call','Keep this note','2026-09-28T09:00:00.000Z',1,'2026-09-28 09:01:00'),(2,'Nastya','Synthetic',1,'meeting','Private','2026-09-29T08:00:00.000Z',0,NULL);");
async function login(name){const r=await worker.fetch(new Request('https://test.example/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test-only'})}),env);assert.equal(r.status,303);return r.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(id,payload,cookie=admin,origin='https://test.example',activeEnv=env)=>worker.fetch(new Request('https://test.example/api/tasks/'+id,{method:'PUT',headers:{...(cookie?{cookie}:{}),origin,'content-type':'application/json'},body:JSON.stringify(payload)}),activeEnv);
const row=()=>sqlite.prepare('SELECT * FROM company_tasks WHERE id=1').get();
const oldFetch=globalThis.fetch;
try{
 assert.equal((await request(1,{dueAt:'2026-09-30T20:15:00+03:00'},null)).status,401);
 assert.equal((await request(1,{dueAt:'2026-09-30T20:15:00+03:00'},user)).status,404);
 assert.equal((await request(1,{dueAt:'2026-09-30T20:15:00+03:00'},admin,'https://wrong.test')).status,403);
 const before=row();
 for(const dueAt of ['',null,0,'garbage','2026-09-30','2026-09-30T20:15:00','2026-99-30T20:15:00Z','2026-02-30T20:15:00Z'])assert.equal((await request(1,{dueAt})).status,400);
 assert.deepEqual(row(),before);
 let response=await request(1,{dueAt:'2026-09-30T20:15:00+03:00'});assert.equal(response.status,200);assert.equal((await response.json()).dueAt,'2026-09-30T17:15:00.000Z');assert.deepEqual({...row(),due_at:before.due_at},{...before});
 assert.equal((await request(1,{completed:false,dueAt:'2026-10-01T00:15:00Z'})).status,400);
 assert.equal((await request(999,{dueAt:'2026-10-01T00:15:00Z'})).status,404);
 assert.equal((await request(1,{completed:false})).status,200);assert.equal(row().completed,0);assert.equal(row().due_at,'2026-09-30T17:15:00.000Z');
 const list=await worker.fetch(new Request('https://test.example/api/tasks',{headers:{cookie:admin}}),env);assert.equal((await list.json()).tasks.find(t=>t.id===1).dueAt,'2026-09-30T17:15:00.000Z');
 const internals=await import('../.tests/backend.mjs');
 const cipher=await internals.encryptGoogleToken(env.SESSION_SECRET,'synthetic-refresh-token');
 sqlite.prepare('INSERT INTO google_calendar_user_connection(user_name,account_email,refresh_token_encrypted,calendar_id) VALUES(?,?,?,?)').run('Eugene','test@example.test',cipher,'primary');
 sqlite.exec("UPDATE company_tasks SET google_event_id='existing-event',google_sync_status='synced' WHERE id=1");
 const calendarEnv={...env,GOOGLE_CALENDAR_CLIENT_ID:'synthetic-client',GOOGLE_CALENDAR_CLIENT_SECRET:'synthetic-secret'};
 let fail=false,patches=[];
 globalThis.fetch=async(url,options)=>{if(url==='https://oauth2.googleapis.com/token')return Response.json({access_token:'synthetic-token'});assert.match(url,/\/events\/existing-event\?sendUpdates=none$/);assert.equal(options.method,'PATCH');const body=JSON.parse(options.body);assert.deepEqual(Object.keys(body).sort(),['end','start']);patches.push(body);return fail?Response.json({error:{message:'Synthetic failure'}},{status:503}):Response.json({id:'existing-event',htmlLink:'https://calendar.test/event'});};
 response=await request(1,{dueAt:'2026-10-02T00:15:00+03:00'},admin,'https://test.example',calendarEnv);assert.equal((await response.json()).calendarSync.status,'synced');assert.equal(patches[0].start.dateTime,'2026-10-01T21:15:00.000Z');assert.equal(patches[0].end.dateTime,'2026-10-01T21:45:00.000Z');assert.equal(row().google_event_id,'existing-event');
 fail=true;response=await request(1,{dueAt:'2026-10-03T00:15:00+03:00'},admin,'https://test.example',calendarEnv);assert.equal(response.status,200);assert.equal((await response.json()).calendarSync.status,'error');assert.equal(row().due_at,'2026-10-02T21:15:00.000Z');assert.equal(row().google_sync_status,'error');
 fail=false;assert.equal((await internals.syncUnsyncedTasks(calendarEnv,'Eugene','https://test.example')).synced,1);assert.equal(row().google_sync_status,'synced');assert.equal(patches.at(-1).start.dateTime,row().due_at);
 await (await import('./test-react.mjs')).testTaskSchedule();
 sqlite.exec('UPDATE company_tasks SET vacancy_id=NULL WHERE id=1');
 response=await request(1,{dueAt:'2026-10-04T12:00:00+03:00'},admin,'https://test.example',calendarEnv);assert.equal(response.status,200);assert.equal(row().due_at,'2026-10-04T09:00:00.000Z');assert.equal(row().vacancy_id,null);
 console.log('Task schedule verified: owned-task persistence, unchanged completion/details, Moscow dates across midnight, validation/CSRF/isolation, inline editor, existing Google event PATCH and retry after failure.');
}finally{globalThis.fetch=oldFetch;sqlite.close();}
