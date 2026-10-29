// Неизменённый source router: смена поколения строго при чтении body, без сети.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import router from '../server/routes/index.js';
import {createSession} from '../server/auth/session.js';
globalThis.fetch=()=>{throw new Error('Settings HTTP oracle forbids network');};
const original=spawnSync(process.execPath,[new URL('./check-scheduler-settings-sql.mjs',import.meta.url).pathname],{encoding:'utf8'});
assert.equal(original.status,0,'required full-row source oracle failed');
const sqlCases=original.stdout.trim().split('\n').map(JSON.parse),base=sqlCases[0];
const valid=JSON.stringify({...base.input,enabled:true,favoritesOnly:false});
const restoredRaw=JSON.stringify({revision:7,enabled:false,cloudOwnerId:'',future:{restored:true}});
const cases=[
 {name:'stable valid',action:'none',body:valid,status:200},
 {name:'rotate valid',action:'rotate',body:valid,status:409},
 {name:'restored same revision',action:'replace',body:valid,status:409},
 {name:'restored absent settings',action:'delete',body:valid,status:409},
 {name:'restore before validation',action:'replace',body:'{}',status:409},
 {name:'malformed before restore fence',action:'replace',body:'{',status:500},
 {name:'method before restore fence',action:'replace',body:valid,method:'POST',status:405},
 {name:'stable invalid',action:'none',body:'{}',status:400},
];
const secret='CI_SYNTHETIC_SCHEDULER_HTTP_SECRET_324';
for(const c of cases){
 const v=base,sqlite=new DatabaseSync(':memory:');
 assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
 sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
 const generation='11111111-2222-4333-8444-555555555555';
 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(generation);
 sqlite.prepare("INSERT INTO company_identity(id,company_name) VALUES(1,'CI settings owner')").run();
 for(const id of v.legacy)sqlite.prepare('INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,auto_enabled) VALUES(1,?,?,1)').run(id,'https://example.invalid/'+id);
 if(v.raw!==null){const row={id:1,scheduler_json:v.raw,favorites_only:v.favorite,revision:v.initialRevision,...v.unrelated},keys=Object.keys(row);sqlite.prepare(`INSERT INTO firecrawl_settings(${keys.join(',')}) VALUES(${keys.map(()=>'?').join(',')})`).run(...keys.map(k=>row[k]));}
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)};}};},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const s of ss)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 c.user='Eugene';const headers={};if(c.user)headers.cookie='it_map_session='+await createSession(secret,c.user);
 if(c.origin)headers.origin=c.origin;if(c.contentType)headers['content-type']=c.contentType;if(c.contentLength)headers['content-length']=c.contentLength;
 const bytes=Buffer.from(c.body),request=new Request('https://example.invalid/api/scheduler/settings',{method:c.method||'PUT',headers,body:bytes});
 let reads=0,before;
 const getReader=request.body.getReader.bind(request.body);
 request.body.getReader=()=>{
  assert.equal(reads++,0);
  if(c.action!=='none')sqlite.prepare('UPDATE app_runtime_generation SET generation=? WHERE id=1').run('22222222-3333-4333-8444-555555555555');
  if(c.action==='replace')sqlite.prepare('UPDATE firecrawl_settings SET scheduler_json=?,favorites_only=1,revision=41 WHERE id=1').run(restoredRaw);
  if(c.action==='delete')sqlite.prepare('DELETE FROM firecrawl_settings WHERE id=1').run();
  before=sqlite.prepare('SELECT * FROM firecrawl_settings WHERE id=1').get()??null;
  return getReader();
 };
 const response=await router.fetch(request,{SESSION_SECRET:secret,SITE_PASSWORD:'CI_UNUSED',DB:c.noDB?undefined:db},{});
 const body=await response.text(),row=sqlite.prepare('SELECT * FROM firecrawl_settings WHERE id=1').get()??null;
 assert.equal(reads,1,'тест обязан дойти до чтения body');
 assert.equal(response.status,c.status,'source precedence отличается от ожидаемой');
 const finalGeneration=sqlite.prepare('SELECT generation FROM app_runtime_generation WHERE id=1').get().generation;
 assert.equal(finalGeneration,c.action==='none'?generation:'22222222-3333-4333-8444-555555555555');
 if(c.status!==200)assert.deepEqual(row,before,'отклонённый запрос изменил восстановленные настройки');
 if(c.status===409)assert.equal(JSON.parse(body).code,'restored');
 process.stdout.write(JSON.stringify({name:c.name,fixture:v,user:c.user,method:c.method||'PUT',action:c.action,restoredRaw,requestBody:bytes.toString('base64'),status:response.status,body,row})+'\n');sqlite.close();
}
process.stderr.write(`Scheduler body-generation full source HTTP/SQLite: ${cases.length} responses\n`);
