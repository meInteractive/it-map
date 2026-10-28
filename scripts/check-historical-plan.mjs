// Unchanged historical source planner/restore on metadata spies; no SQL/files/keys.
import {readFileSync,writeFileSync} from 'node:fs';
import assert from 'node:assert/strict';
import {prepareBackupRestore,restoreBackup} from '../server/backup/index.js';
import {schema,initial,table,row,sign} from './historical-format-support.mjs';
globalThis.fetch=()=>{throw new Error('Historical plan oracle forbids network');};
const fixed='2026-10-27T08:00:00.000Z',NativeDate=Date;
globalThis.Date=class extends NativeDate{constructor(...args){super(...(args.length?args:[fixed]));}static now(){return NativeDate.parse(fixed)}};
import {emptyCurrent,currentRow,spy} from './historical-plan-support.mjs';
const warnings={};let roles;
for(let v=16;v<=39;v++){const p=initial(v);await sign(p);const plan=await prepareBackupRestore(spy(emptyCurrent()),p);warnings[v]=plan.warnings;if(v===16)roles=plan.tables.find(t=>t.name==='vacancy_roles').rows.map(({created_at,updated_at,...r})=>r);}
// ECMAScript full Russian/default lower case, including newer Unicode mappings.
const lower={},cased=[],ignorable=[];let cStart=-1,iStart=-1;
for(let cp=0;cp<=0x110000;cp++){
 const text=cp<0x110000?String.fromCodePoint(cp):'',lo=text.toLocaleLowerCase('ru');if(text!==lo)lower[cp]=lo;
 const c=cp<0x110000&&/\p{Cased}/u.test(text),i=cp<0x110000&&/\p{Case_Ignorable}/u.test(text);
 if(c&&cStart<0)cStart=cp;if(!c&&cStart>=0){cased.push([cStart,cp-1]);cStart=-1}
 if(i&&iStart<0)iStart=cp;if(!i&&iStart>=0){ignorable.push([iStart,cp-1]);iStart=-1}
}
const policy=JSON.stringify({warnings,roles,lower,cased,ignorable})+'\n',policyPath=new URL('../internal/v39/historical_policy.json',import.meta.url);
if(process.argv.includes('--write-policy'))writeFileSync(policyPath,policy);else assert.equal(readFileSync(policyPath,'utf8'),policy,'Historical source/Unicode policy drift');
const stdio=process.argv.includes('--stdio');let count=0;
async function vector(v,name,change,want=true){
 const p=initial(v),current=emptyCurrent();change?.(p,current);await sign(p);const before=JSON.stringify({p,current}),db=spy(current);let expected,ok=true;
 try{const plan=await prepareBackupRestore(db,p),reply=await restoreBackup(db,p);assert.equal(db.batches.length,1);const batch=db.batches[0],rows=new Map();
  for(const s of batch){const n=s.sql.match(/^INSERT INTO "([^"]+)" /)?.[1];if(n){const out=rows.get(n)||[];out.push(...JSON.parse(s.args[0]));rows.set(n,out)}}
  const deletes=batch.map(s=>s.sql.match(/^DELETE FROM "([^"]+)"$/)?.[1]).filter(Boolean);assert.equal(deletes.length,52);
  expected={tables:plan.tables.map(t=>({name:t.name,columns:t.columns.map(c=>c.name),rows:rows.get(t.name)||[]})),warnings:plan.warnings,insertOrder:[...deletes].reverse(),deleteOrder:deletes,rows:reply.rows};
 }catch{ok=false;assert.equal(db.batches.length,0,'Invalid historical plan reached batch')}
 assert.equal(ok,want,`v${v} ${name}`);assert.equal(JSON.stringify({p,current}),before,'Source modified input/state');count++;
 if(stdio)process.stdout.write(JSON.stringify({name:`v${v} ${name}`,body:JSON.stringify(p),current,now:fixed,ok,expected})+'\n');
}
const saved=(p,current)=>{
 for(const n of ['user_profiles','system_prompts','prompt_model_settings','openai_settings','openrouter_settings','openrouter_ai_logs','openrouter_model_metrics','vacancy_cover_letters']){
  const d=schema.tables.find(t=>t.name===n);currentRow(current,n,Object.fromEntries(d.columns.map(c=>[c.name,c.type==='TEXT'?'  CI saved 😀  ':c.name==='id'?1:7])));
 }
 currentRow(current,'firecrawl_settings',{id:1,scheduler_json:'{"enabled":true,"cloudOwnerId":"CI old"}',revision:7,hobby_key_encrypted:'CI saved opaque'});
 currentRow(current,'apify_settings',{id:1,enabled:1,paid_allowed:1,revision:9,free_key_encrypted:'CI opaque',free_account_json:'CI volatile'});
 currentRow(current,'parser_error_daily',{owner_name:'Demo',day:'CI day'});
 currentRow(current,'parser_candidate_intakes',{candidate_token:'CI keep',parser:'manual'});currentRow(current,'parser_candidate_intakes',{candidate_token:'CI stale',parser:'manual'});
 if(table(p,'vacancy_candidates'))row(p,'vacancy_candidates',{id:1,creation_token:'CI keep'});
 if(table(p,'firecrawl_settings')){const t=table(p,'firecrawl_settings'),values={id:1,scheduler_json:'{"enabled":true,"ownerName":"CI source"}',revision:2};row(p,'firecrawl_settings',Object.fromEntries(Object.entries(values).filter(([k])=>t.columns.some(c=>c.name===k))))};
};
for(let v=16;v<=39;v++){
 await vector(v,'empty old format');await vector(v,'preserve omissions reset volatile and filter immutable tokens',saved);
 await vector(v,'empty destination inventory',(_p,current)=>current.splice(0));
 await vector(v,'reordered incoming tables and columns',p=>{p.storage.tables.reverse();for(const t of p.storage.tables)t.columns.reverse()});
 if(v<27){for(const values of [ ['Менеджер проектов; Редактор','IT Manager','Журналист'],['IT Manager → Менеджер проектов','CI неизвестная роль','CI неизвестная роль'],['CI Mixed','ci mixed','CI Mixed; Редактор'],['IT  Manager',' ЖУРНАЛИСТ ','Редактор, Дата-журналист'],['İΣ ΟΣ','i̇ς ος','𐐀𐐁','\ud800CI'],[0,7,null,'; , ']])await vector(v,'legacy roles '+JSON.stringify(values),p=>{for(const [i,role]of values.entries())row(p,'company_vacancies',{id:i+1,role})});}
 if(v<28)await vector(v,'role prompts remapped by stable key not numeric ID',(p,current)=>{
  currentRow(current,'vacancy_roles',{id:91,stable_key:'root-it-manager',parent_id:null});currentRow(current,'vacancy_roles',{id:92,stable_key:'CI removed',parent_id:null});
  currentRow(current,'vacancy_role_classification',{role_id:91,markdown:'CI preserve prompt'});currentRow(current,'vacancy_role_classification',{role_id:92,markdown:'CI discard prompt'});
  if(table(p,'vacancy_roles'))row(p,'vacancy_roles',{id:7,stable_key:'root-it-manager',parent_id:null});
 });
 if(v>=27){await vector(v,'role children before parent',p=>{row(p,'vacancy_roles',{id:2,parent_id:1});row(p,'vacancy_roles',{id:1,parent_id:null})});await vector(v,'role cycle rejected',p=>row(p,'vacancy_roles',{id:1,parent_id:1}),false)}
 if(v>=35)await vector(v,'invalid scheduler rejected',p=>row(p,'firecrawl_settings',{id:1,scheduler_json:'{'}),false);
 if(v>=23&&v<26)await vector(v,'expanded old prompt model retains supplied model',p=>{table(p,'prompt_model_settings').columns.push({name:'openrouter_model'});row(p,'prompt_model_settings',{kind:'relevance',openrouter_model:'jev'})});
 if(v>=21&&v<25)await vector(v,'expanded old personal letter retains explicit fields',p=>{table(p,'vacancy_cover_letters').columns.push({name:'override_shared'},{name:'revision'});row(p,'company_vacancies',{id:1});row(p,'vacancy_cover_letters',{vacancy_id:1,override_shared:1,revision:8})});
}
await vector(27,'equivalent JSON escape forms in preserved stable identity',(p,current)=>{
 currentRow(current,'vacancy_roles',{id:91,stable_key:'CI key\u2028😀',parent_id:null});currentRow(current,'vacancy_role_classification',{role_id:91,markdown:'CI kept prompt'});row(p,'vacancy_roles',{id:7,stable_key:'CI key\u2028😀',parent_id:null});
});
await vector(38,'equivalent JSON escape forms in candidate tokens',(p,current)=>{
 row(p,'vacancy_candidates',{id:1,creation_token:'CI token\u2028😀'});currentRow(current,'parser_candidate_intakes',{candidate_token:'CI token\u2028😀',parser:'manual'});
});
await vector(16,'missing date uses injected clock',p=>{p.createdAt=null});
await vector(30,'JS string revision increment preserved for SQL representability gate',p=>row(p,'firecrawl_settings',{id:1,revision:'7'}));
if(!stdio)console.log(`PASS: ${count} unchanged historical restore plans; metadata spies, zero SQL/network`);
