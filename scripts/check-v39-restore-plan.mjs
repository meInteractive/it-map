// Unchanged legacy plan/restore, with metadata DB spies. No SQL is executed.
import {readFile,writeFile} from 'node:fs/promises';
import assert from 'node:assert/strict';
import {backupDigest,prepareBackupRestore,restoreBackup} from '../server/backup/index.js';
import {SCHEDULER_DEFAULTS,SCHEDULER_SOURCE_OPTIONS} from '../server/repositories/scheduler-schema.js';
globalThis.fetch=()=>{throw new Error('Restore plan oracle forbids network access');};
const schema=JSON.parse(await readFile(new URL('../server/generated/backup-schema.json',import.meta.url),'utf8'));
const definitions=[...schema.tables,...schema.operationalTables];
const initial=()=>({format:'moscow-it-employers-backup',version:39,createdAt:'2026-10-27T00:00:00.000Z',catalog:{companies:[],agencies:[]},storage:{tables:schema.tables.map(t=>({name:t.name,columns:t.columns.map(c=>({name:c.name})),rows:[]}))},manifest:[],metadata:{schema:[],sequences:[],migrations:[],missingTables:[]}});
const table=(p,n)=>p.storage.tables.find(t=>t.name===n);
function row(p,n,values){const t=table(p,n),r=Object.fromEntries(t.columns.map(c=>[c.name,null]));Object.assign(r,values);t.rows.push(r);return r;}
async function sign(p){p.manifest=p.storage.tables.map(t=>({name:t.name,columns:t.columns.map(c=>c.name),rows:t.rows.length}));p.integrity={algorithm:'SHA-256',digest:await backupDigest(p)};return p;}
function spy(){
 const result={batches:[],prepared:[]};
 result.prepare=sql=>{const statement={sql,args:[],bind(...args){this.args=args;return this;},async all(){
  if(/FROM sqlite_schema/.test(sql))return {results:definitions.map(t=>({name:t.name}))};
  const name=sql.match(/PRAGMA (?:table_info|foreign_key_list)\("([^"]+)"\)/)?.[1],t=definitions.find(t=>t.name===name);
  if(t&&sql.startsWith('PRAGMA table_info'))return {results:t.columns.map(c=>({name:c.name,type:c.type,notnull:Number(c.notNull),pk:Number(c.primaryKey),dflt_value:c.defaultValue}))};
  if(t&&sql.startsWith('PRAGMA foreign_key_list'))return {results:t.foreignKeys};
  throw new Error('Unexpected source query in pure restore oracle');
 },async first(){throw new Error('Unexpected source read');},async run(){throw new Error('Pure restore oracle forbids SQL execution');}};
 result.prepared.push(sql);return statement;};
 result.batch=async statements=>{result.batches.push(statements);return [];};return result;
}
const empty=await sign(initial()),emptyPlan=await prepareBackupRestore(spy(),empty);
const policy=JSON.stringify({schedulerDefaults:SCHEDULER_DEFAULTS,supportedSources:SCHEDULER_SOURCE_OPTIONS.filter(s=>s.supported).map(s=>s.id),warnings:emptyPlan.warnings},null,2)+'\n';
const policyPath=new URL('../internal/v39/restore_policy.json',import.meta.url);
if(process.argv.includes('--write-policy'))await writeFile(policyPath,policy);
else assert.equal(await readFile(policyPath,'utf8'),policy,'Trusted restore policy differs from unchanged source');
const stdio=process.argv.includes('--stdio');let count=0;
async function vector(name,change,want=true){
 const p=initial();change?.(p);await sign(p);const before=JSON.stringify(p),db=spy();let projected,ok=true;
 try{
  const plan=await prepareBackupRestore(db,p);const reply=await restoreBackup(db,p);
  assert.equal(db.batches.length,1);const batch=db.batches[0];
  assert.match(batch.at(-1).sql,/INSERT INTO app_runtime_generation/);
  const deletes=batch.map(s=>s.sql.match(/^DELETE FROM "([^"]+)"$/)?.[1]).filter(Boolean);
  assert.equal(deletes.length,52);
  const rows=new Map();
  for(const s of batch){const n=s.sql.match(/^INSERT INTO "([^"]+)" /)?.[1];if(n){const a=rows.get(n)||[];a.push(...JSON.parse(s.args[0]));rows.set(n,a);}}
  projected={tables:plan.tables.map(t=>({name:t.name,rows:rows.get(t.name)||[]})),insertOrder:[...deletes].reverse(),deleteOrder:deletes,warnings:plan.warnings,rows:reply.rows,sequences:p.metadata.sequences,catalog:p.catalog};
 }catch{ok=false;assert.equal(db.batches.length,0,'Invalid source plan reached batch');}
 assert.equal(ok,want,name);assert.equal(JSON.stringify(p),before,'Legacy plan mutated input file');
 assert.ok(!db.prepared.some(s=>s.includes('CI_UNTRUSTED_SQL_MARKER')),'Source executed uploaded schema');
 count++;if(stdio)process.stdout.write(JSON.stringify({name,body:before,ok,expected:projected})+'\n');
}
const supported=SCHEDULER_SOURCE_OPTIONS.filter(s=>s.supported).map(s=>s.id);
await vector('empty v39');
await vector('input table order preserved',p=>p.storage.tables.reverse());
await vector('scheduler forced off while other settings remain',p=>row(p,'firecrawl_settings',{id:1,enabled:1,enhanced_allowed:1,key_encrypted:'CI opaque fixture text',hobby_key_encrypted:'CI opaque hobby fixture',free_usage_json:'{"CI":7}',hobby_usage_json:'{"CI":9}',scheduler_json:JSON.stringify({...SCHEDULER_DEFAULTS,enabled:true,agenciesEnabled:true,cloudOwnerId:'CI synthetic cloud owner',ownerName:'Demo',dayBudgetUsd:7.5,monthBudgetUsd:21,revision:9,sourceIds:[supported[0]],intervalDays:3,unknown:{text:'CI 😀'}})}));
for(const [name,value]of [['minimal',{}],['null sourceIds',{sourceIds:null}],['explicit empty sourceIds',{sourceIds:[]}],['sourceIds wrong type preserved',{sourceIds:'CI preserved'}],['array spread',['CI 😀',1]],['string UTF16 spread','😀\ud800'],['numeric spread',7],['boolean spread',true],['numeric index property',{'10':'CI ten','2':'CI two',enabled:true}],['prototype property',JSON.parse('{"__proto__":{"CI":"safe"},"enabled":true}')]]){
 await vector('scheduler '+name,p=>{
  row(p,'firecrawl_settings',{id:1,scheduler_json:JSON.stringify(value)});
 for(const [id,enabled]of [[supported[0],1],[supported[0],1],[supported[1],1],['CI_unknown',1],['telegram',1],[supported[2],0],[supported[3],'1']])row(p,'vacancy_source_parsing',{source_id:id,auto_enabled:enabled});
 });
}
await vector('JSON number scheduler SQL scalar',p=>row(p,'firecrawl_settings',{id:1,scheduler_json:3}));
await vector('invalid scheduler JSON',p=>row(p,'firecrawl_settings',{id:1,scheduler_json:'{'}),false);
await vector('null scheduler object',p=>row(p,'firecrawl_settings',{id:1,scheduler_json:'null'}),false);
await vector('NULL scheduler SQL scalar',p=>row(p,'firecrawl_settings',{id:1,scheduler_json:null}),false);
await vector('scheduler Unicode own property names preserved',p=>row(p,'firecrawl_settings',{id:1,scheduler_json:JSON.stringify({'ключ':'CI text','😀':'CI emoji',enabled:true})}));
await vector('Apify stop and clear only volatile fields',p=>row(p,'apify_settings',{id:1,enabled:1,paid_allowed:1,free_key_encrypted:'CI opaque free fixture',starter_key_encrypted:'CI opaque starter fixture',free_key_suffix:'CI_F',starter_key_suffix:'CI_S',free_account_json:'CI cached free account',starter_account_json:'CI cached starter account',free_error:'CI stale free error',starter_error:'CI stale paid error',revision:8}));
await vector('unrelated user data whitespace Unicode intact',p=>row(p,'user_profiles',{user_name:'CI user',resume_md:'  CI 😀 \u2028 \\u2028 \ud800  ',revision:7,updated_at:'CI original time'}));
await vector('untrusted metadata SQL ignored',p=>p.metadata.schema.push({type:'table',name:'CI_unknown',sql:'CI_UNTRUSTED_SQL_MARKER'}));
await vector('roles stable parent-first with child before parent',p=>{
 row(p,'vacancy_roles',{id:3,parent_id:2});row(p,'vacancy_roles',{id:2,parent_id:1});row(p,'vacancy_roles',{id:4,parent_id:null});row(p,'vacancy_roles',{id:1,parent_id:null});
});
await vector('roles self cycle rejected before batch',p=>row(p,'vacancy_roles',{id:1,parent_id:1}),false);
await vector('roles two-node cycle rejected before batch',p=>{row(p,'vacancy_roles',{id:1,parent_id:2});row(p,'vacancy_roles',{id:2,parent_id:1});},false);
await vector('roles missing parent',p=>row(p,'vacancy_roles',{id:1,parent_id:2}),false);
await vector('roles numeric versus string parent',p=>{row(p,'vacancy_roles',{id:1,parent_id:null});row(p,'vacancy_roles',{id:2,parent_id:'1'});},false);
await vector('duplicate role IDs remain a SQL constraint decision',p=>{row(p,'vacancy_roles',{id:1,parent_id:null});row(p,'vacancy_roles',{id:1,parent_id:null});});
await vector('catalog and counters preserved',p=>{p.catalog.companies=[{name:'CI company'}];p.catalog.agencies=[{name:'CI agency'}];row(p,'app_catalog',{id:1,data_json:JSON.stringify(p.catalog)});p.metadata.sequences.push({name:'company_vacancies',seq:100000});});
if(!stdio)console.log(`PASS: ${count} unchanged legacy v39 restore plans; metadata spies only, zero SQL/network/files`);
