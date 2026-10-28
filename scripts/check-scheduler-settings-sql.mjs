// Actual unchanged functions + SQLite :memory:. Prepared warm schema; no network,
// provider calls or real settings. Bootstrap/DDL is not part of this oracle.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {schedulerSettings,saveSchedulerSettings,publicSchedulerSettings} from '../server/repositories/scheduler.js';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
globalThis.fetch=()=>{throw new Error('Settings SQL oracle forbids network');};
const cases=[],base={enabled:false,dayBudgetUsd:1.25,monthBudgetUsd:12.5,revision:7};
const add=(name,raw='{"revision":7,"cloudOwnerId":"CI_SYNTHETIC_OWNER","future":{"keep":true}}',patch={},extra={})=>cases.push({name,raw,initialRevision:3,favorite:1,legacy:['hh'],input:{...base,...patch},ownerName:'Eugene',...extra});
add('base');add('enabled',undefined,{enabled:true});add('sources',undefined,{sourceIds:['rabota','hh'],intervalDays:3});
add('favorites unchanged',undefined,{favoritesOnly:true});add('favorites changed',undefined,{favoritesOnly:false});add('no favorite stays false',undefined,{}, {favorite:0});
add('new singleton',null,{revision:0});add('stale',undefined,{revision:6});add('missing revision','{}',{revision:0});add('null revision','{"revision":null}',{revision:0});
add('true revision','{"revision":true}',{revision:1});add('false revision','{"revision":false}',{revision:0});add('string revision','{"revision":"7"}');add('array revision','{"revision":[7]}');add('object revision','{"revision":{}}',{revision:0});
add('throwing revision','{"revision":{"toString":null}}',{revision:0});
add('first vs last','{"revision":6,"revision":7}',{revision:6});add('last is not CAS','{"revision":6,"revision":7}');
add('last is string','{"revision":6,"revision":"7"}',{revision:6});add('first is boolean','{"revision":false,"revision":3}',{revision:0});
add('escaped revision','{"\\u0072evision":7}');add('negative revision','{"revision":-1}',{revision:-1});
for(const number of ['1.0','1e0','1.0000000000000001','1e-999','-0','9007199254740991'])add('numeric '+number,'{"revision":'+number+'}',{revision:number==='1e-999'||number==='-0'?0:number==='9007199254740991'?9007199254740991:1});
add('null sourceIds','{"revision":7,"sourceIds":null}');add('empty sourceIds','{"revision":7,"sourceIds":[]}');add('scalar sourceIds','{"revision":7,"sourceIds":5}');
add('null agencies','{"revision":7,"agenciesEnabled":null}');add('stored derived favorite','{"revision":7,"favoritesOnly":false}');
add('unknown case member','{"revision":7,"Priorities":"CI case sensitive","future":{"1":true}}');
add('bad old priority','{"revision":7,"priorities":{"scan_source":"bad"}}');
add('repair old priority','{"revision":7,"priorities":{"scan_source":"bad"}}',{priorities:{scan_source:30}});
add('unknown old priority','{"revision":7,"priorities":{"CI_future":1}}');
add('invalid input before bad priority','{"revision":7,"priorities":{"scan_source":"bad"}}',{dayBudgetUsd:-1});
add('unsupported source',undefined,{sourceIds:['telegram']});add('owner input rejected',undefined,{ownerName:'Demo'});
add('legacy filter',undefined,{}, {legacy:['telegram','hh','rabota']});
add('private fields untouched',undefined,{favoritesOnly:false},{unrelated:{key_encrypted:'CI_SYNTHETIC_OPAQUE_ENCRYPTED',key_suffix:'CI',max_pages:4,timeout_ms:5000,last_error:'CI prior error'}});

for(const v of cases){
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
 const generation='11111111-2222-4333-8444-555555555555';sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(generation);
 sqlite.prepare("INSERT INTO company_identity(id,company_name) VALUES(1,'CI settings owner')").run();
 for(const source_id of v.legacy)sqlite.prepare('INSERT INTO vacancy_source_parsing(company_id,source_id,source_url,auto_enabled) VALUES(1,?,?,1)').run(source_id,'https://example.invalid/'+source_id);
 if(v.raw!==null){const row={id:1,scheduler_json:v.raw,favorites_only:v.favorite,revision:v.initialRevision,...v.unrelated};const keys=Object.keys(row);sqlite.prepare(`INSERT INTO firecrawl_settings(${keys.join(',')}) VALUES(${keys.map(()=>'?').join(',')})`).run(...keys.map(k=>row[k]));}
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)};}};},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const s of ss)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 const bound=generationBoundDb(db,generation);
 let status='ok',message='',body='';
 try{body=JSON.stringify(await saveSchedulerSettings(bound,v.input,v.ownerName));}catch(e){status=e.code==='changed'?'changed':e.status===400?'input':'storage';if(status==='input')message=e.message;}
 let read='';try{read=JSON.stringify(publicSchedulerSettings(await schedulerSettings(bound)));}catch{}
 const row=sqlite.prepare('SELECT * FROM firecrawl_settings WHERE id=1').get()??null;
 process.stdout.write(JSON.stringify({...v,status,message,body,read,row})+'\n');sqlite.close();
}
process.stderr.write(`Scheduler settings actual SQLite: ${cases.length} full-row transitions\n`);
