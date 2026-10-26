// Execute the unchanged PUT /api/crm typed UI validation block; no SQL/network.
import {readFile,writeFile} from 'node:fs/promises';
import {CRM_STATUSES} from '../server/constants.js';
globalThis.fetch=()=>{throw new Error('CRM oracle must not use the network');};
const source=await readFile(new URL('../server/routes/index.js',import.meta.url),'utf8');
const route=source.indexOf('if (url.pathname === "/api/crm")');
const start=source.indexOf('const status = String(payload?.status || "");',route);
const end=source.indexOf('return jsonResponse(await saveCompanyCrm(',start);
if(route<0||start<route||end<start)throw new Error('Legacy CRM route boundaries changed; review oracle');
const oracle=new Function('payload','CRM_STATUSES','jsonResponse',source.slice(start,end)+'return {valid:true,normalized:{status,comment}};');
const cases=[['empty',{}],['null-fields',{status:null,comment:null}],['comment-only',{comment:'CI synthetic comment'}],['no-trim',{status:'Откликнулся',comment:'  CI synthetic comment  '}],['unknown-status',{status:'CI unknown'}],['padded-status',{status:' Откликнулся '}],['ascii-limit',{comment:'x'.repeat(1000)}],['ascii-over-limit',{comment:'x'.repeat(1001)}],['emoji-limit',{comment:'😀'.repeat(500)}],['emoji-over-limit',{comment:'😀'.repeat(500)+'x'}],['bom',{comment:'\uFEFFCI synthetic comment\uFEFF'}],['nel',{comment:'\u0085CI synthetic comment\u0085'}],['markup',{comment:'CI синтетический <b>&</b>\n😀\u2028'}]];
for(const status of CRM_STATUSES)cases.push(['status-'+status,{status,comment:'CI synthetic status comment'}]);
const vectors=cases.map(([name,input])=>({name,input,...oracle(input,CRM_STATUSES,(_body,status)=>({valid:false,status}))}));
const target=new URL('../internal/crm/testdata/save-contract.json',import.meta.url);
const output=JSON.stringify({oracle:'Unchanged Sites PUT /api/crm validation block; typed UI strings/null/omitted, no SQL/network',vectors},null,2)+'\n';
if(process.argv.length===3&&process.argv[2]==='--write')await writeFile(target,output);
else if(process.argv.length!==2)throw new Error('Use --write only for reviewed regeneration');
else if(await readFile(target,'utf8')!==output)throw new Error('Legacy CRM vectors changed; review before regenerating');
console.log(`PASS: ${vectors.length} CRM save vectors from unchanged legacy route; no SQL/network`);
