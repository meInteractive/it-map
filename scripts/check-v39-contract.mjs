// Synthetic v39 contracts only. No SQL, backup files, providers or network.
import {readFile} from 'node:fs/promises';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import {backupDigest,validateFullBackup,readBackupRequest} from '../server/backup/index.js';
import {BACKUP_FORMAT,BACKUP_VERSION,MAX_BACKUP_BYTES,MAX_BACKUP_ROWS} from '../server/constants.js';
globalThis.fetch=()=>{throw new Error('v39 oracle forbids network access');};
const raw=await readFile(new URL('../server/generated/backup-schema.json',import.meta.url),'utf8');
assert.equal(await readFile(new URL('../internal/v39/schema.json',import.meta.url),'utf8'),raw,'Embedded trusted source schema changed');
const schema=JSON.parse(raw);
assert.equal(schema.tables.length,52);assert.equal(schema.operationalTables.length,19);
const stdio=process.argv.includes('--stdio');let checked=0;
const emit=value=>{checked++;if(stdio)process.stdout.write(JSON.stringify(value)+'\n');};
const initial=()=>({format:BACKUP_FORMAT,version:BACKUP_VERSION,createdAt:'2026-10-26T00:00:00.000Z',catalog:{companies:[],agencies:[]},storage:{tables:schema.tables.map(t=>({name:t.name,columns:t.columns.map(c=>({name:c.name})),rows:[]}))},manifest:[],metadata:{schema:[],sequences:[],migrations:[],missingTables:[]}});
const table=(b,name)=>b.storage.tables.find(t=>t.name===name);
const row=(b,name,values)=>{const t=table(b,name);const r=Object.fromEntries(t.columns.map(c=>[c.name,null]));Object.assign(r,values);t.rows.push(r);return r;};
const sign=async b=>{b.manifest=b.storage.tables.map(t=>({name:t.name,columns:t.columns.map(c=>c.name),rows:t.rows.length}));b.integrity={algorithm:'SHA-256',digest:await backupDigest(b)};return b;};
async function validation(name,change,want=true,after){
 const b=initial();change?.(b);await sign(b);await after?.(b);
 let ok=true;try{await validateFullBackup(b);}catch{ok=false;}
 assert.equal(ok,want,name);const body=JSON.stringify(b);
 const formatted=JSON.stringify(b,null,2);
 emit({type:'validation',name,body,ok,digest:await backupDigest(b),encodedStatus:Buffer.byteLength(formatted)>MAX_BACKUP_BYTES?413:200,formattedHash:createHash('sha256').update(formatted).digest('hex')});
}
await validation('all 52 empty application sections');
await validation('columns may reorder',b=>table(b,'user_profiles').columns.reverse());
await validation('table order may change',b=>b.storage.tables.reverse());
await validation('scalar text Unicode and NULL preserved',b=>row(b,'user_profiles',{user_name:'CI synthetic user',resume_md:'  😀 <&> \u2028 \u2029 \\u2028 \ud800  ',revision:2}));
await validation('NULL fields remain layout-valid',b=>row(b,'company_identity',{}));
await validation('valid catalog and raw exact order',b=>row(b,'app_catalog',{id:1,data_json:JSON.stringify(b.catalog),updated_at:'CI fixed time'}));
await validation('nested raw JSON with literal lone surrogate',b=>{b.catalog.note='\ud800';row(b,'app_catalog',{id:1,data_json:'{"companies":[],"agencies":[],"note":"\ud800"}'});});
await validation('case-distinct root fields cannot override',b=>{b.Format='other';b.Version=0;});
await validation('case-distinct table fields cannot override',b=>{table(b,'user_profiles').Name='other';});
await validation('catalog names may contain BOM around real text',b=>b.catalog.companies.push({name:'\uFEFF CI \uFEFF'}));
for(const [name,change] of [
 ['wrong format',b=>b.format='other'],['other version outside v39 slice',b=>b.version=40],
 ['wrong-case required format',b=>{b.Format=b.format;delete b.format;}],
 ['missing diagnostic section',b=>b.storage.tables=b.storage.tables.filter(t=>t.name!=='parser_error_daily')],
 ['operational section',b=>b.storage.tables.push({name:'app_runtime_generation',columns:[{name:'id'},{name:'generation'}],rows:[]})],
 ['unknown section',b=>b.storage.tables[0].name='CI_unknown'],['duplicate table',b=>b.storage.tables[1]=structuredClone(b.storage.tables[0])],
 ['missing column',b=>table(b,'user_profiles').columns.pop()],['extra column',b=>table(b,'user_profiles').columns.push({name:'CI_extra'})],
 ['duplicate column',b=>table(b,'user_profiles').columns[1]={name:'user_name'}],
 ['missing row field',b=>{const r=row(b,'user_profiles',{});delete r.revision;}],
 ['extra row field',b=>row(b,'user_profiles',{CI_extra:'x'})],
 ['boolean row field',b=>row(b,'user_profiles',{revision:true})],
 ['array row field',b=>row(b,'user_profiles',{revision:[]})],
 ['object row field',b=>row(b,'user_profiles',{revision:{}})],
 ['catalog whitespace-only name',b=>b.catalog.companies.push({name:' \t\uFEFF'})],
 ['catalog mismatch',b=>row(b,'app_catalog',{id:1,data_json:'{"companies":[{"name":"CI other"}]}'} )],
 ['catalog property order mismatch',b=>row(b,'app_catalog',{id:1,data_json:'{"agencies":[],"companies":[]}'} )],
 ['invalid raw catalog JSON',b=>row(b,'app_catalog',{id:1,data_json:'{'} )],
 ['orphan FK',b=>row(b,'vacancy_role_classification',{role_id:1})],
 ['typed FK string vs numeric',b=>{row(b,'vacancy_roles',{id:1});row(b,'vacancy_role_classification',{role_id:'1'});}],
 ['operational sequence',b=>b.metadata.sequences.push({name:'scheduler_jobs',seq:1})],
 ['unknown sequence',b=>b.metadata.sequences.push({name:'CI_unknown',seq:1})],
 ['negative sequence',b=>b.metadata.sequences.push({name:'company_vacancies',seq:-1})],
 ['unsafe sequence',b=>b.metadata.sequences.push({name:'company_vacancies',seq:9007199254740992})],
 ['fraction sequence',b=>b.metadata.sequences.push({name:'company_vacancies',seq:1.5})],
 ['NULL sequence',b=>b.metadata.sequences.push({name:'company_vacancies',seq:null})],
 ['missing sequence number',b=>b.metadata.sequences.push({name:'company_vacancies'})],
 ['duplicate sequence',b=>b.metadata.sequences.push({name:'company_vacancies',seq:1},{name:'company_vacancies',seq:2})],
 ['missing schema metadata',b=>delete b.metadata.schema],['missing migrations metadata',b=>delete b.metadata.migrations],
])await validation(name,change,false);
await validation('valid typed FK',b=>{row(b,'vacancy_roles',{id:1});row(b,'vacancy_role_classification',{role_id:1});});
await validation('nullable FK',b=>row(b,'vacancy_role_classification',{role_id:null}));
await validation('safe retained highwater sequence',b=>b.metadata.sequences.push({name:'company_vacancies',seq:9007199254740991}));
await validation('manifest order matters',null,false,async b=>{b.manifest[0]={rows:0,columns:b.manifest[0].columns,name:b.manifest[0].name};b.integrity.digest=await backupDigest(b);});
await validation('manifest count mismatch',null,false,async b=>{b.manifest[0].rows=1;b.integrity.digest=await backupDigest(b);});
await validation('changed content without re-sign',null,false,b=>{b.createdAt='CI tampered';});
await validation('wrong digest algorithm',null,false,b=>{b.integrity.algorithm='sha256';});
{
 const b=initial();row(b,'user_profiles',{revision:null});await sign(b);
 const body=JSON.stringify(b).replace('"revision":null','"revision":1e400');
 let ok=true;try{await validateFullBackup(JSON.parse(body));}catch{ok=false;}assert.equal(ok,false);
 emit({type:'validation',name:'overflow numeric cell before stringify',body,ok,digest:await backupDigest(JSON.parse(body))});
}
await validation('maximum row count',b=>{const t=table(b,'agency_identity');t.rows=Array.from({length:MAX_BACKUP_ROWS},()=>({id:null,agency_name:null}));});
await validation('row count above maximum',b=>{const t=table(b,'agency_identity');t.rows=Array.from({length:MAX_BACKUP_ROWS+1},()=>({id:null,agency_name:null}));},false);
await validation('formatted export cap includes whitespace',null,true,async b=>{
 b.metadata.schema=[''];const overhead=Buffer.byteLength(JSON.stringify(b));
 b.metadata.schema=['x'.repeat(MAX_BACKUP_BYTES-overhead)];b.integrity.digest=await backupDigest(b);
 assert.equal(Buffer.byteLength(JSON.stringify(b)),MAX_BACKUP_BYTES);
 assert.ok(Buffer.byteLength(JSON.stringify(b,null,2))>MAX_BACKUP_BYTES);
});
const jsonBodies=[
 '{"z":1.0,"a":1e0,"integrity":{"ignored":true},"0":"zero","10":"ten","2":"two"}',
 '{"a":1,"b":2,"a":3,"integrity":null}',
 '{"u":"😀 <&> / \\u2028 \\u2029 \\ud800 \\udc00 \\ud83d\\ude00 \\\\u2028","01":1,"4294967294":2,"4294967295":3}',
 '{"values":[-0,1e-7,1e-6,1e20,1e21,1e23,1e400,9007199254740993,5e-324,1.7976931348623157e308]}',
];
let seed=1n;const random=[];for(let i=0;i<160;i++){seed=(seed*6364136223846793005n+1442695040888963407n)&((1n<<64n)-1n);const a=new BigUint64Array([seed]);const n=new Float64Array(a.buffer)[0];if(Number.isFinite(n))random.push(n);}
jsonBodies.push(JSON.stringify({numbers:random}));
for(const [i,body]of jsonBodies.entries())emit({type:'digest',name:'JSON.stringify semantics '+i,body,digest:await backupDigest(JSON.parse(body))});
for(const [name,body,length,status]of [['declared byte cap','{}',MAX_BACKUP_BYTES+1,413],['actual stream cap',' '.repeat(MAX_BACKUP_BYTES+1),0,413],['exact byte cap','{}'+' '.repeat(MAX_BACKUP_BYTES-2),0,200],['invalid JSON','{',0,400],['BOM supported','\uFEFF{}',0,200]]){
 let actual=200;try{await readBackupRequest(new Request('https://itmap.test/api/backup',{method:'POST',headers:length?{'content-length':String(length)}:{},body}));}catch(error){actual=error.status||400;}
 assert.equal(actual,status,name);emit({type:'request',name,bodySpec:name==='actual stream cap'?'oversize-spaces':name==='exact byte cap'?'exact-limit-spaces':body,length,status});
}
for(const [i,units]of [[0xff],[0xe1,0x80],[0xed,0xa0,0x80],[0xc0,0xaf],[0xf0,0x9f,0x98,0x80]].entries()){
 const bytes=new Uint8Array([123,34,120,34,58,34,...units,34,125]);
 const decoded=await readBackupRequest(new Request('https://itmap.test/api/backup',{method:'POST',body:bytes}));
 emit({type:'request',name:'TextDecoder UTF8 replacement '+i,bodySpec:'hex:'+Buffer.from(bytes).toString('hex'),length:0,status:200,normalized:JSON.stringify(decoded)});
}
if(!stdio)console.log(`PASS: ${checked} source v39 layout/integrity/request cases; 52 application/19 operational; no SQL/network`);
