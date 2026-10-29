// Исходная запись/чтение истории, существующая SQLite schema, только синтетика.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {generationBoundDb} from '../server/repositories/generation-bound-db.js';
import {recordParserErrorSnapshot,readParserErrorHistory} from '../server/repositories/parser-error-history.js';
globalThis.fetch=()=>{throw new Error('Error history oracle forbids network');};
const generation='11111111-2222-4333-8444-555555555555',now=Date.parse('2026-10-29T21:00:00Z'),day=86400000;
const sample=(owner,p1,p2,time=now)=>({owner,countsRaw:JSON.stringify({p1,p2}),now:time});
const cases=[],add=(name,records=[],extra={})=>cases.push({name,records,owner:'Eugene',now,...extra});
add('empty');add('zero observed',[sample('Eugene',0,0)]);
add('same day independent peaks',[sample('Eugene',5,1),sample('Eugene',2,7,now+1000)]);
add('out of order samples',[sample('Eugene',1,8,now+5000),sample('Eugene',9,2,now+1000)]);
add('sparse window and retained old rows',[sample('Eugene',1,2,now-70*day),sample('Eugene',3,4,now-30*day),sample('Eugene',5,6,now-29*day),sample('Eugene',7,8,now-10*day),sample('Eugene',9,10),sample('Eugene',11,12,now+day)]);
add('Moscow midnight',[sample('Eugene',1,2,now-1),sample('Eugene',3,4,now)]);
for(const timestamp of ['2027-01-01T00:00:00Z','2000-03-01T00:00:00Z','1900-03-01T00:00:00Z','1970-01-01T00:00:00Z']){const at=Date.parse(timestamp);add('calendar '+timestamp,[sample('Eugene',1,2,at-day),sample('Eugene',3,4,at)],{now:at});}
const owners=['Eugene','Nastya','eugene','Eugene ','Евгений 😀',''];
for(const owner of owners)add('owner '+JSON.stringify(owner),owners.map((o,i)=>sample(o,i+1,i+2)),{owner});
add('source Number integer rounding',[{owner:'Eugene',countsRaw:'{"p1":9007199254740993,"p2":1000000000000000129}',now}]);
add('negative counts refused',[sample('Eugene',-1,2)]);
add('generation rotates before record',[sample('Eugene',3,4)],{rotate:true});
const d1=v=>typeof v==='bigint'?Number(v):Array.isArray(v)?v.map(d1):v&&typeof v==='object'?Object.fromEntries(Object.entries(v).map(([k,x])=>[k,d1(x)])):v;
for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');for(const o of schema.objects)sqlite.exec(o.sql);
 sqlite.prepare('INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)').run(generation);
 const base={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const s=sqlite.prepare(sql);s.setReadBigInts(true);const r=d1(s.get(...args))??null;return column&&r?r[column]:r;},async all(){const s=sqlite.prepare(sql);s.setReadBigInts(true);return {results:d1(s.all(...args))};},async run(){const s=sqlite.prepare(sql);s.setReadBigInts(true);return s.columns().length?{results:d1(s.all(...args))}:{meta:d1(s.run(...args))};}};},async batch(ss){sqlite.exec('BEGIN');try{const out=[];for(const s of ss)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 const db=generationBoundDb(base,generation),errors=[];
 for(const r of c.records){if(c.rotate)sqlite.prepare("UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1").run();try{await recordParserErrorSnapshot(db,r.owner,JSON.parse(r.countsRaw),r.now);errors.push('ok');}catch(e){errors.push(e.code==='restored'?'restored':'storage');}}
 const before=sqlite.prepare('SELECT total_changes() n').get().n;let body=null,readError='';try{body=await readParserErrorHistory(db,c.owner,c.now);}catch(e){readError=e.code==='restored'?'restored':'storage';}
 assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'history read mutated source');
 const s=sqlite.prepare('SELECT * FROM parser_error_daily ORDER BY owner_name,day');s.setReadBigInts(true);const rawRows=s.all(),rows=d1(rawRows),storageRows=rawRows.map(r=>Object.fromEntries(Object.entries(r).map(([k,v])=>[k,typeof v==='bigint'?v.toString():v])));
 process.stdout.write(JSON.stringify({...c,errors,readError,body,rows,storageRows})+'\n');sqlite.close();
}
process.stderr.write(`Parser error daily history source SQLite: ${cases.length} frames\n`);
