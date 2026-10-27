// Public synthetic historical format helpers; no database or network.
import {readFileSync} from 'node:fs';
import {backupDigest} from '../server/backup/index.js';
import {IMPORT_TABLE_NAMES} from '../server/repositories/vacancy-import-schema.js';
import {DETAIL_TABLE_NAMES} from '../server/repositories/vacancy-detail-schema.js';
import {SCHEDULER_SOURCE_COLUMNS} from '../server/repositories/scheduler-schema.js';
export const schema=JSON.parse(readFileSync(new URL('../server/generated/backup-schema.json',import.meta.url),'utf8'));
export const since={user_profiles:18,openrouter_settings:19,openrouter_ai_logs:19,system_prompts:20,vacancy_cover_letters:21,openrouter_model_metrics:22,prompt_model_settings:23,openai_settings:24,vacancy_roles:27,vacancy_role_assignments:27,vacancy_role_classification:28,vacancy_candidates:29,vacancy_candidate_identities:29,vacancy_classification_attempts:29,vacancy_classifications:29,firecrawl_settings:30,vacancy_source_parsing:30,apify_settings:36,parser_error_daily:39,parser_candidate_intakes:39};
for(const n of IMPORT_TABLE_NAMES)since[n]=33;for(const n of DETAIL_TABLE_NAMES)since[n]=32;
const removed=(v,n,c)=>(v<25&&n==='vacancy_cover_letters'&&['override_shared','revision'].includes(c))||(v<26&&n==='prompt_model_settings'&&c==='openrouter_model')||(v<38&&['vacancy_candidates','vacancy_source_parsing'].includes(n)&&c==='agency_id')||(v<37&&n==='vacancy_role_settings'&&c==='openrouter_model')||(v<31&&n==='firecrawl_settings'&&['mode','hobby_key_encrypted','hobby_key_suffix','hobby_connection_status','hobby_last_error','hobby_checked_at','free_key_hash','hobby_key_hash','free_usage_json','hobby_usage_json'].includes(c))||(v<34&&n==='firecrawl_settings'&&c==='favorites_only')||(v<35&&n==='firecrawl_settings'&&c==='scheduler_json')||(v<35&&n==='vacancy_source_parsing'&&Object.hasOwn(SCHEDULER_SOURCE_COLUMNS,c));
export const initial=v=>({format:'moscow-it-employers-backup',version:v,createdAt:'CI synthetic time',catalog:{companies:[],agencies:[]},storage:{tables:schema.tables.filter(t=>(since[t.name]||16)<=v).map(t=>({name:t.name,columns:t.columns.filter(c=>!removed(v,t.name,c.name)).map(c=>({name:c.name})),rows:[]}))},metadata:{schema:[],migrations:[],sequences:[]}});
export const table=(p,n)=>p.storage.tables.find(t=>t.name===n);
export const row=(p,n,values)=>{const t=table(p,n),r=Object.fromEntries(t.columns.map(c=>[c.name,null]));Object.assign(r,values);t.rows.push(r);return r;};
export async function sign(p){p.manifest=p.storage.tables.map(t=>({name:t.name,columns:t.columns.map(c=>c.name),rows:t.rows.length}));p.integrity={algorithm:'SHA-256',digest:await backupDigest(p)};}
