// Unchanged legacy planner and restore on metadata spies; no SQL/files/keys.
import assert from 'node:assert/strict';
import {prepareBackupRestore,restoreBackup} from '../server/backup/index.js';
import {initial,table,row,schema} from './historical-format-support.mjs';
import {emptyCurrent,currentRow,spy} from './historical-plan-support.mjs';
import {REVIEW_SOURCE_META} from '../server/constants.js';
globalThis.fetch=()=>{throw Error('Unsigned plan oracle forbids network');};
const fixed='2026-10-27T08:00:00.000Z',NativeDate=Date;
globalThis.Date=class extends NativeDate{constructor(...args){super(...(args.length?args:[fixed]));}static now(){return NativeDate.parse(fixed)}};
const company=name=>({name,reviewSources:Object.fromEntries(REVIEW_SOURCE_META.map(([key])=>[key,''])),reviews:{},aiScore:null,aiEvidence:''});
const minimal=version=>{const p=initial(16);p.version=version;p.storage.tables=p.storage.tables.filter(t=>!['app_catalog','google_calendar_connection','vacancy_audit_runs'].includes(t.name));delete p.createdAt;delete p.metadata;return p;};
const stdio=process.argv.includes('--stdio');let count=0;
async function vector(version,name,change,want=true,rawChange){
 let p=minimal(version);const current=emptyCurrent();change?.(p,current);const body=rawChange?.(JSON.stringify(p))??JSON.stringify(p);p=JSON.parse(body);const before=JSON.stringify({p,current}),db=spy(current);let expected,ok=true;
 try {
  const plan=await prepareBackupRestore(db,p),reply=await restoreBackup(db,p);assert.equal(db.batches.length,1);const batch=db.batches[0],rows=new Map();
  for(const s of batch){const n=s.sql.match(/^INSERT INTO "([^"]+)" /)?.[1];if(n){const out=rows.get(n)||[];out.push(...JSON.parse(s.args[0]));rows.set(n,out)}}
  const deletes=batch.map(s=>s.sql.match(/^DELETE FROM "([^"]+)"$/)?.[1]).filter(Boolean);assert.equal(deletes.length,52);
  expected={tables:plan.tables.map(t=>({name:t.name,columns:t.columns.map(c=>c.name),rows:rows.get(t.name)||[]})),warnings:plan.warnings,insertOrder:[...deletes].reverse(),deleteOrder:deletes,rows:reply.rows};
 }catch(e){ok=false;assert.equal(db.batches.length,0,'Invalid unsigned plan reached batch');if(want)console.error(name,e.message)}
 assert.equal(ok,want,`v${version} ${name}`);assert.equal(JSON.stringify({p,current}),before,'Source mutated input/state');count++;
 if(stdio)process.stdout.write(JSON.stringify({name:`v${version} ${name}`,body,current,now:fixed,ok,expected})+'\n');
}
const saved=(_p,current)=>{
 for(const n of ['google_calendar_connection','vacancy_audit_runs','user_profiles','openrouter_settings','openrouter_ai_logs','system_prompts','vacancy_cover_letters','openrouter_model_metrics','prompt_model_settings','openai_settings','firecrawl_settings','apify_settings','parser_error_daily'])currentRow(current,n,{id:7,revision:7});
 currentRow(current,'vacancy_roles',{id:91,stable_key:'root-it-manager',parent_id:null});currentRow(current,'vacancy_role_classification',{role_id:91,markdown:'CI preserve'});
 currentRow(current,'parser_candidate_intakes',{candidate_token:'CI removed',parser:'manual'});
};
for(const version of [14,15]){
 await vector(version,'minimal unsigned format');
 await vector(version,'preserved destination omissions',saved);
 await vector(version,'missing destination inventory',(_p,current)=>current.splice(0));
 await vector(version,'source audit explicitly clears',(p,current)=>{saved(p,current);p.storage.tables.push({...schema.tables.find(t=>t.name==='vacancy_audit_runs'),rows:[]})});
 await vector(version,'source audit overrides saved rows',(p,current)=>{saved(p,current);p.storage.tables.push({...schema.tables.find(t=>t.name==='vacancy_audit_runs'),rows:[]});row(p,'vacancy_audit_runs',{run_id:'CI source audit'})});
 for(const createdAt of [null,0,false,'',7,'CI original date',{CI:'date'}])await vector(version,`untrusted timestamp ${JSON.stringify(createdAt)}`,p=>p.createdAt=createdAt);
 await vector(version,'incoming tables and columns reversed',p=>{p.storage.tables.reverse();for(const t of p.storage.tables)t.columns.reverse()});
 await vector(version,'untrusted metadata ignored',p=>{p.metadata={schema:[{sql:'CI NOT EXECUTABLE'}],sequences:[{name:'company_vacancies',seq:9000}],migrations:[{id:7}]};p.integrity={algorithm:'bad'};p.manifest=false;});
 for(const names of [['Я','A','Б','a','Ё','Е','е','Е\u0308'],['CI 😀','CI \ud800','CI \ud801','CI \0','CI 2','CI 20'],['é','e\u0301','é','e\u0301']])await vector(version,'catalog stable sort '+JSON.stringify(names),p=>p.catalog.companies=names.map(name=>company(name)));
 await vector(version,'score precedes name and overwrites rank',p=>{p.catalog.companies=['А','Я','Б','А'].map((name,i)=>({...company(name),rank:50,aiScore:i%2?10:0,aiEvidence:'CI source'}))});
 await vector(version,'agency source order and unknown properties',p=>{p.catalog.extra='CI retained';p.catalog.agencies=['Я','А',' '].map((name,i)=>({name,rank:100-i,reviewSources:{twoGis:'',otzovik:'',personAgency:'',yell:''},unknown:'CI keep'}))});
 for(const roles of [['IT Manager','Журналист','Редактор'],['CI роль','CI роль','ci роль','CI 😀\ud800']])await vector(version,'role migration '+JSON.stringify(roles),p=>{for(const [i,role]of roles.entries())row(p,'company_vacancies',{id:i+1,role})});
 for(let trial=0;trial<60;trial++)await vector(version,`mixed catalog ${trial}`,p=>{
  const names=['А','Ё','Я','a','A','CI 😀','CI \ud800','é','e\u0301','Й','И\u0306'];
  p.catalog.companies=Array.from({length:12},(_,i)=>({...company(names[(i*7+trial)%names.length]),tag:i,ratingInputs:{roleFit:(i+trial)%6,deferment:(i*3+trial)%16,wlbScore:(i+trial)%16},ratingFormulaVersion:trial%6,wlbScore:(i*7+trial)%101,middle:[250000+i,300000],aiScore:i%3*5,aiEvidence:'CI source'}));
 });
 await vector(version,'overflow arithmetic precedes stringify',p=>p.catalog.companies=[{...company('CI overflow'),ratingFormulaVersion:'CI_INF',wlbScore:90,ratingInputs:{wlbScore:8}}],true,s=>s.replaceAll('"CI_INF"','1e400'));
 await vector(version,'overflow timestamp retains truthiness',p=>p.createdAt='CI_INF',true,s=>s.replace('"CI_INF"','1e400'));
 await vector(version,'throwing normalization before any write',p=>p.catalog.companies=[{...company('CI'),roles:{toString:7}}],false);
 await vector(version,'missing required table',p=>p.storage.tables.splice(0,1),false);
}
// Explicit approved departure from unchanged source order is tested in Go with
// a separate catalog case, not disguised as source parity in this oracle.
if(!stdio)console.log(`PASS: ${count} unchanged unsigned plans; no SQL/network`);
