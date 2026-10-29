// Неизменённый роутер/dashboard и подготовленная SQLite :memory:; только синтетика.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import router from '../server/routes/index.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {createSession} from '../server/auth/session.js';
globalThis.fetch=()=>{throw new Error('Dashboard oracle forbids external network');};
const generation=process.env.ITMAP_TEST_DISPOSABLE_GENERATION||'11111111-2222-4333-8444-555555555555';
assert.match(generation,/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
const oldGeneration='22222222-3333-4333-8444-555555555555',now=Date.parse('2026-10-01T21:00:00.000Z'),secret='CI_SYNTHETIC_DASHBOARD_SECRET_326';
const make=(name,extra={})=>({name,now,raw:'{"revision":7}',favorite:1,legacy:['hh'],jobs:[],runs:[],attempts:[],...extra});
const run=(id,extra={})=>({id:'CI_run_'+id,generation,trigger:'manual',started_at:now-1000,finished_at:null,status:'completed',succeeded:0,...extra});
const job=(id,extra={})=>({id:'CI_job_'+id,generation,dedupe_key:'CI_key_'+id,type:'diagnostic',status:'pending',priority:0,run_after:now,created_at:now,updated_at:now,...extra});
const attempt=(id,extra={})=>({id:'CI_attempt_'+id,job_id:'CI_missing',run_id:'CI_old_run',type:'diagnostic',started_at:now-1000,status:'completed',reserved_usd:0,cost_usd:null,credits:null,result_json:'{}',...extra});
const cases=[make('empty'),make('private connected',{raw:'{"revision":7,"cloudOwnerId":"CI_PRIVATE_CLOUD_OWNER","future":{"keep":true}}'}),make('favorites false',{favorite:0}),make('null legacy scope',{raw:'{"revision":7,"sourceIds":null}'}),make('no legacy scope',{legacy:[]})];
for(const [name,age] of [['recent',1],['before threshold',4500000-1],['at threshold',4500000],['after threshold',4500001],['future',-1]])cases.push(make('signal '+name,{runs:[run(name,{trigger:'automatic',started_at:now-age})]}));
for(const clock of [now-1,now,now+1,Date.parse('2026-10-31T20:59:59.999Z'),Date.parse('2026-10-31T21:00:00.000Z')])cases.push(make('clock '+clock,{now:clock,runs:[run('clock',{trigger:'automatic',started_at:clock-1})],attempts:[attempt('clock',{started_at:now-1,reserved_usd:0.26}),attempt('clock2',{started_at:clock,reserved_usd:1.25})]}));
cases.push(make('zero signal',{runs:[run('zero',{trigger:'automatic',started_at:0})]}));
cases.push(make('current counts',{jobs:['pending','running','completed','failed','cancelled'].map((status,i)=>job(i,{status})).concat(job('old',{generation:oldGeneration,status:'running'}))}));
cases.push(make('history20 and last success',{runs:Array.from({length:25},(_,i)=>run(i,{started_at:now-i*1000,finished_at:now-i*1000+500,succeeded:i===22?1:0})).concat(run('old',{generation:oldGeneration,started_at:now+1000,trigger:'automatic',succeeded:1}),run('probe',{started_at:now+500,trigger:'probe',succeeded:1}),run('failed',{started_at:now+501,status:'failed',succeeded:1}))}));
cases.push(make('attempt50 and joins',{jobs:[job('linked',{company_id:1,candidate_id:null})],attempts:Array.from({length:55},(_,i)=>attempt(i,{job_id:i===0?'CI_job_linked':'CI_missing',started_at:now-i,company_id:1,message:'CI visible message',type:'classify_title',result_json:JSON.stringify({accepted:i%2===0,prompt:'CI_PRIVATE_PROMPT',payload:{private:'CI_PRIVATE_RESPONSE'}})}))}));
cases.push(make('bounded role and import outcomes',{attempts:[attempt('roles',{started_at:now-1,type:'assign_roles',result_json:'{"roleIds":[1,2],"prompt":"CI_PRIVATE_PROMPT"}'}),attempt('superseded',{started_at:now-2,type:'import_vacancy',result_json:'{"superseded":true,"nextStage":"assign_roles","private":"CI_PRIVATE_RESPONSE"}'}),attempt('truncated',{started_at:now-3,type:'classify_title',result_json:JSON.stringify({accepted:true,private:'CI_PRIVATE_RESPONSE'.repeat(300)})}),attempt('character limit',{started_at:now-4,type:'classify_title',result_json:JSON.stringify({accepted:true,private:'😀'.repeat(600)})})]}));
for(const found of ['3','true','"3tail"','"not numeric"','[3]','null','3.25'])cases.push(make('statistics found '+found,{attempts:[attempt('found',{type:'scan_source',result_json:'{"found":'+found+'}'}),attempt('failed',{type:'scan_source',status:'failed',result_json:'{"found":999}'})]}));
cases.push(make('statistics first duplicate',{attempts:[attempt('found',{type:'scan_source',result_json:'{"found":2,"found":7}'}),attempt('accepted',{type:'classify_title',result_json:'{"accepted":true,"accepted":false}'}),attempt('import',{type:'import_vacancy',result_json:'{"action":"create","action":"update"}'})]}));
cases.push(make('statistics strict values',{attempts:[attempt('accepted string',{type:'classify_title',result_json:'{"accepted":"1"}'}),attempt('accepted one',{type:'classify_title',status:'failed',result_json:'{"accepted":1}'}),attempt('import padded',{type:'import_vacancy',result_json:'{"action":"create "}'}),attempt('import create',{type:'import_vacancy',result_json:'{"action":"create"}'})]}));
cases.push(make('spending global periods',{attempts:[attempt('old month',{started_at:Date.parse('2026-09-30T20:59:59.999Z'),cost_usd:999,credits:9}),attempt('month start',{started_at:Date.parse('2026-09-30T21:00:00.000Z'),type:'scan_source',reserved_usd:7,cost_usd:1.25,credits:2}),attempt('day before',{started_at:now-1,type:'scan_source',reserved_usd:0.26,credits:null}),attempt('day start',{started_at:now,type:'assign_roles',cost_usd:0.5,credits:3})]}));
cases.push(make('type grouping',{attempts:['CI','ci','CI ','😀','Ж'].map((type,i)=>attempt(i,{type,started_at:now-i,reserved_usd:0.25,cost_usd:i===0?0:null}))}));
cases.push(make('found integer boundary',{attempts:[attempt('found',{type:'scan_source',result_json:'{"found":9223372036854775807}'})]}));
cases.push(make('found real clears overflow',{attempts:[attempt('1',{type:'scan_source',result_json:'{"found":9223372036854775807}'}),attempt('2',{type:'scan_source',result_json:'{"found":1}'}),attempt('3',{type:'scan_source',result_json:'{"found":0.0}'})]}));
cases.push(make('found infinity',{attempts:[attempt('found',{type:'scan_source',result_json:'{"found":1e400}'})]}));
cases.push(make('spending infinity',{attempts:[attempt('1',{started_at:now,reserved_usd:Number.MAX_VALUE,credits:Number.MAX_VALUE}),attempt('2',{started_at:now+1,reserved_usd:Number.MAX_VALUE,credits:Number.MAX_VALUE})]}));
cases.push(make('credits compensated sum',{attempts:[1.55e308,1.23,3.2e-16,-1.55e308,-1.23].map((credits,i)=>attempt(i,{started_at:now+i,credits}))}));
// D1 возвращает JS Number; node:sqlite требует явного преобразования int64.
const d1Value=v=>typeof v==='bigint'?Number(v):Array.isArray(v)?v.map(d1Value):v&&typeof v==='object'?Object.fromEntries(Object.entries(v).map(([k,n])=>[k,d1Value(n)])):v;
let responses=0;
for(const c of cases){
 Date.now=()=>c.now;
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
 sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
 const add=(table,row)=>{const keys=Object.keys(row);sqlite.prepare(`INSERT INTO ${table}(${keys.map(k=>'"'+k+'"').join(',')}) VALUES(${keys.map(()=>'?').join(',')})`).run(...keys.map(k=>row[k]));};
 add('app_runtime_generation',{id:1,generation});add('company_identity',{id:1,company_name:'CI settings owner'});
 add('firecrawl_settings',{id:1,scheduler_json:c.raw,favorites_only:c.favorite});
 for(const id of c.legacy)add('vacancy_source_parsing',{company_id:1,source_id:id,source_url:'https://example.invalid/'+id,auto_enabled:1});
 for(const [table,rows] of [['scheduler_jobs',c.jobs],['scheduler_runs',c.runs],['scheduler_attempts',c.attempts]])for(const row of rows)add(table,row);
 const base={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const stmt=sqlite.prepare(sql);stmt.setReadBigInts(true);const r=d1Value(stmt.get(...args))??null;return column&&r?r[column]:r;},async all(){const stmt=sqlite.prepare(sql);stmt.setReadBigInts(true);return {results:d1Value(stmt.all(...args))};},async run(){const s=sqlite.prepare(sql);s.setReadBigInts(true);return s.columns().length?{results:d1Value(s.all(...args))}:{meta:d1Value(s.run(...args))};}};},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const s of ss)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 const db=generationBoundDb(base,generation),before=sqlite.prepare('SELECT total_changes() n').get().n,requests=[];
 for(const user of ['Eugene','Demo','Nastya','']){
  const headers=user?{cookie:'it_map_session='+await createSession(secret,user)}:{};
  const response=await router.fetch(new Request('https://example.invalid/api/scheduler',{headers}),{SESSION_SECRET:secret,SITE_PASSWORD:'CI_UNUSED',DB:db},{});
  requests.push({user,status:response.status,body:await response.text()});responses++;
 }
 assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'prepared source dashboard GET mutated rows');
 process.stdout.write(JSON.stringify({...c,requests})+'\n');sqlite.close();
}
process.stderr.write(`Scheduler dashboard actual source router/SQLite: ${cases.length} frames/${responses} responses; zero GET changes\n`);
