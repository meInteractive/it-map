// Неизменённый router и SQLite: только retry/cancel, без выполнения заданий и сети.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import router from '../server/routes/index.js';
import {createSession} from '../server/auth/session.js';
globalThis.fetch=()=>{throw new Error('Job action oracle forbids network');};
const now=1800000000000;Date.now=()=>now;
const generation='11111111-2222-4333-8444-555555555555',other='22222222-3333-4333-8444-555555555555';
const id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',neighbor='bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb';
const job=(key,status='pending',epoch=generation)=>({id:key,generation:epoch,dedupe_key:key,type:'diagnostic',company_id:null,agency_id:null,candidate_id:null,source_id:null,payload:'{"opaque":"preserve"}',status,priority:17,run_after:9,attempts:3,max_attempts:4,locked_by:'CI_OLD_OWNER',locked_until:9000000000000,last_error:'CI prior error',reason_code:'prior',created_at:2,updated_at:3,completed_at:4});
const cases=[],add=(name,patch={})=>cases.push({name,action:'retry',state:'pending',user:'Eugene',body:'{}',method:'POST',...patch});
for(const action of ['retry','cancel']){
 for(const state of ['pending','failed','cancelled','running','completed'])add(action+' '+state,{action,state});
 add(action+' old generation',{action,old:true});add(action+' missing',{action,missing:true});
 add(action+' restore during body',{action,rotate:true});add(action+' repeat same clock',{action,repeat:2});
}
for(const user of ['','Demo','Nastya'])add('access '+(user||'anonymous'),{user,body:'{'});
add('storage before origin',{noDB:true,origin:'https://other.invalid'});
add('origin denied',{origin:'https://other.invalid',body:'{'});add('origin allowed',{origin:'https://example.invalid'});
for(const body of ['', '{','[]','null','"value"','{"unknown":true}'])add('body '+(body||'empty'),{body});
for(const method of ['PUT','PATCH','DELETE','OPTIONS','GET'])add('method '+method,{method,body:method==='GET'?'':'{}'});
add('BOM',{body:'\ufeff{}'});add('exact limit',{body:'{}'+' '.repeat(32766)});
add('stream oversized',{body:'{}'+' '.repeat(32767)});add('declared oversized',{contentLength:'32769'});
add('invalid id',{urlId:'A'.repeat(36)});add('source loose id',{urlId:'-'.repeat(36)});
add('invalid action',{urlAction:'unknown'});add('query ignored',{query:'?unused=1'});
const secret='CI_SYNTHETIC_SCHEDULER_ACTION_SECRET_329';
for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
 sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(generation);
 sqlite.prepare('INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,?)').run('{"revision":7}');
 const jobs=[job(neighbor,'failed')];if(!c.missing)jobs.push(job(id,c.state,c.old?other:generation));
 const attempts=[{id:'cccccccc-cccc-4ccc-8ccc-cccccccccccc',job_id:id,run_id:'CI_ACTION_OLD_RUN',type:'diagnostic',status:'running',started_at:1,reserved_usd:2,cost_usd:null,result_json:'{"opaque":"preserve"}'}];
 for(const [table,rows] of [['scheduler_jobs',jobs],['scheduler_attempts',attempts]])for(const row of rows){const keys=Object.keys(row);sqlite.prepare(`INSERT INTO ${table}(${keys.join(',')}) VALUES(${keys.map(()=>'?').join(',')})`).run(...keys.map(k=>row[k]));}
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)};}};},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const s of ss)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 const headers={};if(c.user)headers.cookie='it_map_session='+await createSession(secret,c.user);if(c.origin)headers.origin=c.origin;if(c.contentLength)headers['content-length']=c.contentLength;
 const bytes=Buffer.from(c.body),responses=[];
 for(let n=0;n<(c.repeat||1);n++){
  const request=new Request(`https://example.invalid/api/scheduler/jobs/${c.urlId||id}/${c.urlAction||c.action}${c.query||''}`,{method:c.method,headers,...(c.method==='GET'?{}:{body:bytes})});
  if(c.rotate){const getReader=request.body.getReader.bind(request.body);request.body.getReader=()=>{sqlite.prepare('UPDATE app_runtime_generation SET generation=? WHERE id=1').run(other);return getReader();};}
  const response=await router.fetch(request,{SESSION_SECRET:secret,SITE_PASSWORD:'CI_UNUSED',DB:c.noDB?undefined:db},{});responses.push({status:response.status,body:await response.text()});
 }
 const rows=table=>sqlite.prepare(`SELECT * FROM ${table} ORDER BY id`).all();
 assert.deepEqual(rows('scheduler_attempts').map(r=>[r.status,r.reserved_usd,r.cost_usd,r.result_json]),[['running',2,null,'{"opaque":"preserve"}']]);
 process.stdout.write(JSON.stringify({...c,now,generation,id,jobs,attempts,requestBody:bytes.toString('base64'),responses,finalJobs:rows('scheduler_jobs'),finalAttempts:rows('scheduler_attempts')})+'\n');sqlite.close();
}
process.stderr.write(`Scheduler job action source router/SQLite: ${cases.length} frames\n`);
