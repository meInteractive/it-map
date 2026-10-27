// Unchanged legacy validator; public synthetic scalar rows, no SQL/files/network.
import assert from 'node:assert/strict';
import {backupDigest,validateFullBackup} from '../server/backup/index.js';
globalThis.fetch=()=>{throw new Error('Historical oracle forbids network');};
import {schema,initial,table,row,sign} from './historical-format-support.mjs';
let count=0;const stdio=process.argv.includes('--stdio');
async function vector(v,name,change,want=true,after){const p=initial(v);change?.(p);await sign(p);await after?.(p);let ok=true;try{await validateFullBackup(p);}catch{ok=false};assert.equal(ok,want,`v${v} ${name}`);count++;if(stdio)process.stdout.write(JSON.stringify({name:`v${v} ${name}`,body:JSON.stringify(p),ok,version:v,tables:p.storage.tables.length})+'\n');}
for(let v=16;v<=39;v++){
 await vector(v,'all required old sections');
 await vector(v,'reordered tables columns and case-distinct ignored fields',p=>{p.storage.tables.reverse();p.Format='CI ignored';for(const t of p.storage.tables){t.columns.reverse();t.Name='CI ignored';}});
 await vector(v,'UTF16 scalar and NULL remain lossless',p=>row(p,'agency_identity',{id:7,agency_name:'  CI 😀 \ud800  '}));
 await vector(v,'missing required section',p=>p.storage.tables.splice(p.storage.tables.findIndex(t=>t.name==='agency_identity'),1),false);
 await vector(v,'unexpected future or operational section',p=>p.storage.tables.push({name:v<39?'parser_error_daily':'app_runtime_generation',columns:[{name:'id'}],rows:[]}),false);
 await vector(v,'duplicate section',p=>p.storage.tables[1]=structuredClone(p.storage.tables[0]),false);
 await vector(v,'missing required column',p=>table(p,'agency_identity').columns.pop(),false);
 await vector(v,'boolean cell',p=>row(p,'agency_identity',{id:true}),false);
 await vector(v,'missing row field',p=>{const r=row(p,'agency_identity',{});delete r.id;},false);
 await vector(v,'typed foreign key mismatch',p=>{row(p,'company_vacancies',{id:1});row(p,'company_tasks',{vacancy_id:'1'});},false);
 await vector(v,'valid typed FK',p=>{row(p,'company_vacancies',{id:1});row(p,'company_tasks',{vacancy_id:1});});
 await vector(v,'manifest object order',null,false,async p=>{p.manifest[0]={rows:0,columns:p.manifest[0].columns,name:p.manifest[0].name};p.integrity.digest=await backupDigest(p);});
 await vector(v,'bad integrity',null,false,p=>p.createdAt='CI changed');
 await vector(v,'invalid sequence',p=>p.metadata.sequences.push({name:'company_vacancies',seq:1.5}),false);
 await vector(v,'valid raw catalog',p=>row(p,'app_catalog',{id:1,data_json:JSON.stringify(p.catalog)}));
 await vector(v,'catalog raw property order mismatch',p=>row(p,'app_catalog',{id:1,data_json:'{"agencies":[],"companies":[]}'}),false);
 const future=schema.tables.find(t=>t.name==='firecrawl_settings');
 if(v>=30&&v<35)await vector(v,'future scheduler column forbidden',p=>table(p,future.name).columns.push({name:'scheduler_json'}),false);
 if(v>=29&&v<38)await vector(v,'future agency FK column forbidden',p=>table(p,'vacancy_candidates').columns.push({name:'agency_id'}),false);
 if(v>=33&&v<37)await vector(v,'future role model column forbidden',p=>table(p,'vacancy_role_settings').columns.push({name:'openrouter_model'}),false);
 if(v>=21&&v<25){await vector(v,'cover letter expanded optional layout',p=>table(p,'vacancy_cover_letters').columns.push({name:'override_shared'},{name:'revision'}));await vector(v,'cover letter half upgraded layout',p=>table(p,'vacancy_cover_letters').columns.push({name:'revision'}),false);}
 if(v>=23&&v<26)await vector(v,'prompt model expanded optional layout',p=>table(p,'prompt_model_settings').columns.push({name:'openrouter_model'}));
}
for(const v of [15,40,16.5]){const p=initial(16);p.version=v;await sign(p);let ok=true;try{await validateFullBackup(p);}catch{ok=false};assert.equal(ok,false);count++;if(stdio)process.stdout.write(JSON.stringify({name:`unsupported signed modern version ${v}`,body:JSON.stringify(p),ok})+'\n');}
if(!stdio)console.log(`PASS: ${count} unchanged legacy v16-39 format vectors; no SQL/files/network`);
