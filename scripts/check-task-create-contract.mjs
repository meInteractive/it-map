// Execute the unchanged POST /api/tasks validation block; no SQL/network/providers.
import {readFile,writeFile} from 'node:fs/promises';
import {TASK_TYPES} from '../server/constants.js';
globalThis.fetch=()=>{throw new Error('Task contract oracle must not use the network');};
const source=await readFile(new URL('../server/routes/index.js',import.meta.url),'utf8');
const route=source.indexOf('if (url.pathname === "/api/tasks")');
const start=source.indexOf('const companyName = String(payload?.companyName || "").trim();',route);
const end=source.indexOf('        try {\n          return jsonResponse(await createTask',start);
if(route<0||start<route||end<start)throw new Error('Legacy task route boundaries changed; review the oracle');
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const oracle=new AsyncFunction('payload','TASK_TYPES','readCompanyNames','env','jsonResponse',source.slice(start,end)+`
 return {valid:true,normalized:{companyName,vacancyId,type,note,dueAt:new Date(dueAt).toISOString()}};`);
const base={companyName:'CI Synthetic Employer',vacancyId:null,type:'call',note:'',dueAt:'2026-10-26T14:45:00+03:00'};
const names=new Set([base.companyName,'\u0085'+base.companyName+'\u0085']);
const cases=[
 ['base',{}],['spaces',{companyName:' '+base.companyName+' ',note:'  CI synthetic task  '}],
 ['bom-company',{companyName:'\uFEFF'+base.companyName+'\uFEFF'}],
 ['nel-company',{companyName:'\u0085'+base.companyName+'\u0085'}],
 ['bom-note',{note:'\uFEFFCI synthetic task\uFEFF'}],['nel-note',{note:'\u0085CI synthetic task\u0085'}],
 ['all-js-whitespace',{note:'\t\v\f\r \u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000\uFEFFCI synthetic task\n'}],
 ['empty-note',{note:''}],['only-bom-note',{note:'\uFEFF'}],['unicode-note',{note:'CI синтетическая задача 😀 <&> \u2028'}],
 ['ascii-limit',{note:'x'.repeat(1200)}],['ascii-over-limit',{note:'x'.repeat(1201)}],
 ['emoji-limit',{note:'😀'.repeat(600)}],['emoji-over-limit',{note:'😀'.repeat(600)+'x'}],['emoji-601',{note:'😀'.repeat(601)}],
 ['trim-before-limit',{note:'\uFEFF'+'😀'.repeat(600)+'\uFEFF'}],
 ['linked-vacancy',{vacancyId:1}],['largest-safe-vacancy',{vacancyId:9007199254740991}],['unsafe-vacancy',{vacancyId:9007199254740992}],
 ['utc-date',{dueAt:'2026-10-26T11:45:00Z'}],['millisecond-date',{dueAt:'2026-10-26T11:45:00.123Z'}],
 ['msk-midnight',{dueAt:'2026-10-26T00:15:00+03:00'}],['negative-offset',{dueAt:'2026-10-26T23:15:00-03:30'}],
 ['leap-day',{dueAt:'2028-02-29T12:00:00Z'}],['invalid-date',{dueAt:'CI invalid date'}],
 ['offset-hour-24',{dueAt:'2026-10-26T12:00:00+24:00'}],['offset-minute-60',{dueAt:'2026-10-26T12:00:00+00:60'}],['comma-fraction',{dueAt:'2026-10-26T12:00:00,123Z'}],
 ['unknown-type',{type:'CI unknown type'}],['empty-company',{companyName:''}],
];
for(const type of TASK_TYPES)cases.push(['type-'+type,{type}]);
const vectors=[];
for(const [name,patch]of cases){
 const input={...base,...patch};
 const result=await oracle(input,TASK_TYPES,async()=>names,{DB:null},(_body,status)=>({valid:false,status}));
 vectors.push({name,input,...result});
}
const target=new URL('../internal/tasks/testdata/create-contract.json',import.meta.url);
const output=JSON.stringify({oracle:'Unchanged Sites POST /api/tasks validation block; synthetic typed input, fixed catalog name lookup, no SQL/network',vectors},null,2)+'\n';
if(process.argv.length===3&&process.argv[2]==='--write')await writeFile(target,output);
else if(process.argv.length!==2)throw new Error('Use --write only for reviewed fixture regeneration');
else if(await readFile(target,'utf8')!==output)throw new Error('Legacy task oracle differs from committed vectors; review before regenerating');
console.log(`PASS: ${vectors.length} task create vectors from unchanged legacy route; no SQL/network`);
