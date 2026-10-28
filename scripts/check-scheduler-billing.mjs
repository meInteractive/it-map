// Unchanged dispatcher + schedulerBilling, synthetic SQLite :memory: only.
import {DatabaseSync} from 'node:sqlite';
import {readFileSync} from 'node:fs';
import assert from 'node:assert/strict';
import {schema} from './historical-format-support.mjs';
import {runDueTasks} from '../server/services/scheduler-dispatcher.js';
import {schedulerUpperCost,schedulerPeriod} from '../server/services/scheduler-budget.js';
globalThis.fetch=()=>{throw new Error('Billing oracle forbids network');};
const generation='11111111-2222-4333-8444-555555555555',now=1800000000000;
async function reference(v){
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 sqlite.function('julianday',()=>v.now/86400000+2440587.5);const oldNow=Date.now;Date.now=()=>v.now;
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)};}};},async batch(statements){sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 try{
  sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(v.generation);
  sqlite.prepare('INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,?)').run(JSON.stringify({enabled:true,dayBudgetUsd:v.day,monthBudgetUsd:v.month}));
  sqlite.prepare("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,priority,run_after,created_at,updated_at) VALUES('CI_job',?,'CI_job','diagnostic',0,0,0,0)").run(v.generation);
  const period=schedulerPeriod(v.now);
  for(const [i,seed] of (v.seeds??[]).entries())sqlite.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES(?,'CI_other_job','CI_old_generation_run','diagnostic',?,'interrupted',?,?)").run('CI_seed_'+i,period[seed.period]+(seed.offset??0),seed.reserved,seed.cost??null);
  let unknown=false,requests=0,atWork=null,executions=0;
  await runDueTasks({DB:db},{requestId:'CI_billing',diagnostic:true,limit:1,handlers:{diagnostic:async env=>{
   const b=env.SCHEDULER_BILLING;
   try{await b.run(v.request,async()=>{executions++;atWork=sqlite.prepare("SELECT reserved_usd,cost_usd FROM scheduler_attempts WHERE job_id='CI_job'").get();if(v.throw)throw Object.assign(new Error('CI synthetic request failure'),{code:v.throw,status:503});return v.responseRaw?JSON.parse(v.responseRaw):v.response;});return {mock:true};}
   finally{unknown=b.unknown;requests=b.requests;}
  }}});
  const attempt=sqlite.prepare("SELECT * FROM scheduler_attempts WHERE job_id='CI_job'").get(),job=sqlite.prepare("SELECT * FROM scheduler_jobs WHERE id='CI_job'").get();
  assert.ok(attempt);return {...v,upper:schedulerUpperCost(v.request),unknown,requests,executions,atWork,attempt,job};
 }finally{Date.now=oldNow;sqlite.close();}
}
if(process.argv.includes('--reference')){const values=JSON.parse(readFileSync(0,'utf8'));const out=[];for(const v of values)out.push(await reference(v));process.stdout.write(JSON.stringify(out));}
else{
 const paid={provider:'apify',model:'CI_actor',capUsd:0.26},openai={provider:'openai',model:'gpt-4.1',payload:{input:[],max_output_tokens:100}},jev={provider:'openrouter',model:'CI_jev',contextLength:1000,pricing:{prompt:'0.000001',completion:'0.000002'},payload:{questions:{}}};
 const cases=[],add=(name,request,response,extra={})=>cases.push({name,generation,now,day:10,month:10,request,response,...extra});
 add('explicit cost',paid,{usage:{cost:0.01}});add('unknown usage',paid,{usage:{}});add('network retains reserve',paid,{}, {throw:'network'});
 add('known free zero budget',{...paid,free:true},{},{day:0,month:0});add('explicit paid usage on free request',{...paid,free:true},{usage:{cost:0.02}},{day:0,month:0});
 add('exact day/month cap',paid,{usage:{cost:0.01}},{day:0.26,month:0.26});
 add('day denied',paid,{}, {day:0.25});add('month denied',paid,{}, {month:0.25});
 add('global old reserve denied',paid,{}, {day:0.3,seeds:[{period:'day',reserved:0.1}]});
 add('known cost overrides old reserve',paid,{usage:{cost:0.01}},{day:0.3,seeds:[{period:'day',reserved:9,cost:0.01}]});
 add('prior day counts toward month',paid,{}, {month:0.3,seeds:[{period:'day',offset:-1,reserved:0.1}]});
 add('prior month excluded',paid,{usage:{cost:0.01}},{day:0.26,month:0.26,seeds:[{period:'month',offset:-1,reserved:9}]});
 add('jev known',jev,{usage:{cost:0.0001}});add('jev missing cost',jev,{usage:{}});
 for(const cost of [null,'0.01',true,-1,0,0.123])add('cost scalar '+JSON.stringify(cost),paid,{usage:{cost}});
 add('token fallback',openai,{usage:{input_tokens:1000,output_tokens:50}});
 add('response model tariff',openai,{model:'gpt-4.1-mini',usage:{input_tokens:1000,output_tokens:50}});
 add('unknown response model',openai,{model:'CI_unknown',usage:{input_tokens:1000,output_tokens:50}});
 for(const cached of [null,false,0,'',1,true,'0',{},[]])add('cached '+JSON.stringify(cached),openai,{usage:{input_tokens:1000,output_tokens:50,input_tokens_details:{cached_tokens:cached}}});
 add('explicit wins malformed tokens',openai,{usage:{cost:0.01,input_tokens:'CI',output_tokens:false}});
 add('missing output tokens',openai,{usage:{input_tokens:1000}});add('string input tokens',openai,{usage:{input_tokens:'1000',output_tokens:50}});
 add('negative tokens',openai,{usage:{input_tokens:-1,output_tokens:50}});
 add('overflow explicit stays unknown',paid,{}, {responseRaw:'{"usage":{"cost":1e400}}'});
 add('overflow explicit token fallback',openai,{}, {responseRaw:'{"usage":{"cost":1e400,"input_tokens":1000,"output_tokens":50}}'});
 for(const v of cases)process.stdout.write(JSON.stringify(await reference(v))+'\n');
}
