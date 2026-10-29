// Неизменённый GET /scheduler/run и SQLite; POST и выполнение заданий запрещены.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {candidateHash} from '../server/repositories/vacancy-candidates.js';
import router from '../server/routes/index.js';
import {createSession} from '../server/auth/session.js';
globalThis.fetch=()=>{throw new Error('Run lookup oracle forbids network');};
const generation='11111111-2222-4333-8444-555555555555',other='22222222-3333-4333-8444-555555555555',secret='CI_SYNTHETIC_RUN_READ_SECRET_331';
const ids=Array.from({length:8},(_,i)=>`00000000-0000-f000-f000-${String(i).padStart(12,'0')}`);ids.push('00000000-0000-0000-0000-000000000000');
const runs=[];
for(let i=0;i<ids.length;i++)runs.push({id:await candidateHash(generation+':'+ids[i]),generation,trigger:['manual','automatic','probe'][i%3],started_at:1000+i,finished_at:i%2?2000+i:null,status:['running','completed','paused','interrupted','failed','CI_future_status','cancelled','completed','completed'][i],ready:i,processed:i,succeeded:i,errors:i,remaining:i,reason:'CI сохранённый запуск 😀',cursor_id:i,lease_until:3000+i});
runs.push({...runs[0],id:await candidateHash(other+':'+ids[0]),generation:other,reason:'CI old generation'});
const cases=[],add=(name,query='',patch={})=>cases.push({name,query,user:'Eugene',...patch});
for(let i=0;i<ids.length;i++)add('existing run '+i,'requestId='+ids[i]);
add('missing query');add('empty ID','requestId=');add('unknown valid ID','requestId=aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa');
for(const id of [ids[0].toUpperCase(),ids[0]+' ',ids[0].slice(1),'not-a-uuid','%FF','%00'])add('invalid '+id,'requestId='+(id==='%FF'||id==='%00'?id:encodeURIComponent(id)));
add('first valid duplicate','requestId='+ids[0]+'&requestId='+ids[1]);add('first invalid duplicate','requestId=bad&requestId='+ids[0]);add('last invalid duplicate','requestId='+ids[0]+'&requestId=bad');add('first empty duplicate','requestId=&requestId='+ids[0]);
add('escaped key','%72equestId='+ids[0]);add('unknown query','requestId='+ids[0]+'&extra=1');add('unknown before missing UUID','extra=1&requestId=');add('malformed key','%ZZ=1');
add('same hash wrong generation','requestId='+ids[0],{wrongGeneration:true});
for(const user of ['Demo','Nastya',''])add('access '+(user||'anonymous'),'requestId=bad',{user});
add('storage before invalid UUID','requestId=bad',{noDB:true});add('GET ignores Origin','requestId='+ids[0],{origin:'https://other.invalid'});
const d1Value=v=>typeof v==='bigint'?Number(v):Array.isArray(v)?v.map(d1Value):v&&typeof v==='object'?Object.fromEntries(Object.entries(v).map(([k,n])=>[k,d1Value(n)])):v;
for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(generation);
 for(const row of runs){const keys=Object.keys(row);sqlite.prepare(`INSERT INTO scheduler_runs(${keys.map(k=>'"'+k+'"').join(',')}) VALUES(${keys.map(()=>'?').join(',')})`).run(...keys.map(k=>row[k]));}
 sqlite.exec(`UPDATE scheduler_runs SET ready=9007199254740993 WHERE id='${runs[0].id}'`);if(c.wrongGeneration)sqlite.prepare('UPDATE scheduler_runs SET generation=? WHERE id=?').run(other,runs[0].id);
 const base={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const s=sqlite.prepare(sql);s.setReadBigInts(true);const row=d1Value(s.get(...args))??null;return column&&row?row[column]:row;},async all(){const s=sqlite.prepare(sql);s.setReadBigInts(true);return {results:d1Value(s.all(...args))};},async run(){const s=sqlite.prepare(sql);s.setReadBigInts(true);return s.columns().length?{results:d1Value(s.all(...args))}:{meta:d1Value(s.run(...args))};}};},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const s of ss)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 const db=generationBoundDb(base,generation),before=sqlite.prepare('SELECT total_changes() n').get().n,headers={};if(c.user)headers.cookie='it_map_session='+await createSession(secret,c.user);if(c.origin)headers.origin=c.origin;
 const request=new Request('https://example.invalid/api/scheduler/run'+(c.query?'?'+c.query:''),{headers});const response=await router.fetch(request,{SESSION_SECRET:secret,SITE_PASSWORD:'CI_UNUSED',DB:c.noDB?undefined:db},{});
 assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'run GET mutated prepared source');
 process.stdout.write(JSON.stringify({...c,query:new URL(request.url).search.slice(1),generation,runs,firstRequestId:new URL(request.url).searchParams.get('requestId'),queryObject:Object.fromEntries(new URL(request.url).searchParams),status:response.status,responseBody:await response.text()})+'\n');sqlite.close();
}
process.stderr.write(`Scheduler run lookup source router/SQLite: ${cases.length} frames\n`);
