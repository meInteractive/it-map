// Public synthetic v14–15 source oracle. No SQL, database files or network.
import assert from 'node:assert/strict';
import {validateFullBackup} from '../server/backup/index.js';
import {REVIEW_SOURCE_META,AGENCY_RATING_INPUT_META} from '../server/constants.js';
import {initial,table,row,schema} from './historical-format-support.mjs';
globalThis.fetch=()=>{throw new Error('Legacy format oracle forbids network');};
const stdio=process.argv.includes('--stdio');let count=0,accepted=0;
const sourceKeys=REVIEW_SOURCE_META.map(([key])=>key);
const company=()=>({name:'CI Legacy Company',reviewSources:Object.fromEntries(sourceKeys.map(k=>[k,''])),reviews:{},aiScore:null,aiEvidence:''});
const agency=()=>({name:'CI Legacy Agency',reviewSources:{twoGis:'',otzovik:'',personAgency:'',yell:''}});
export function legacyInitial(version){const p=initial(16);p.version=version;p.storage.tables=p.storage.tables.filter(t=>!['app_catalog','google_calendar_connection','vacancy_audit_runs'].includes(t.name));delete p.createdAt;delete p.metadata;return p;}
async function vector(version,name,change,want=true,rawChange){
 const p=legacyInitial(version);change?.(p);const body=rawChange?.(JSON.stringify(p))??JSON.stringify(p);const parsed=JSON.parse(body);let ok=true,error='';
 try{await validateFullBackup(parsed);}catch(e){ok=false;error=e.message;}
 assert.equal(ok,want,`v${version} ${name}: ${error}`);count++;accepted+=Number(ok);
 // Native TypeError wording is engine-specific. Every rejected body must still
 // be refused safely; source validation messages are compared exactly otherwise.
 const exactError=!error.startsWith('Cannot ')&&!error.includes('is not a function');
 if(stdio)process.stdout.write(JSON.stringify({name:`v${version} ${name}`,body,ok,error,exactError,version,tables:parsed.storage?.tables?.length??0})+'\n');
}
const c=(p)=>{const x=company();p.catalog.companies=[x];return x;};
const a=(p)=>{const x=agency();p.catalog.agencies=[x];return x;};
for(const version of [14,15]){
 await vector(version,'minimal full unsigned file');
 await vector(version,'reordered tables and columns with case-distinct ignored fields',p=>{p.Format='CI ignored';p.storage.tables.reverse();for(const t of p.storage.tables){t.columns.reverse();t.Name='CI ignored';}});
 await vector(version,'arbitrary metadata manifest and invalid integrity are ignored',p=>{p.createdAt={arbitrary:1};p.metadata={sequences:[{name:'not SQL',seq:-1}],schema:'not executable'};p.manifest=false;p.integrity={digest:'invalid'};});
 await vector(version,'optional audit section',p=>{const d=schema.tables.find(t=>t.name==='vacancy_audit_runs');p.storage.tables.push({...d,rows:[]});});
 await vector(version,'scalar Unicode and null',p=>row(p,'agency_identity',{id:7,agency_name:' CI 😀 \ud800 \u2028 '}));
 await vector(version,'invalid format',p=>p.format='CI invalid',false);
 await vector(version,'case-sensitive format',p=>{delete p.format;p.Format='moscow-it-employers-backup';},false);
 await vector(version,'string version',p=>p.version=String(version),false);
 await vector(version,'missing catalog',p=>delete p.catalog,false);
 for(const catalog of [null,[],{}, {companies:{}},{companies:[null]},{companies:[{name:3}]},{companies:[{name:''}]},{companies:[{name:' \uFEFF\u2009'}]}])await vector(version,`invalid catalog ${JSON.stringify(catalog)}`,p=>p.catalog=catalog,false);
 await vector(version,'non-JS whitespace in company name',p=>c(p).name='\u0085');
 await vector(version,'lone UTF16 company name',p=>c(p).name='\ud800');
 await vector(version,'required company fields',p=>c(p));
 await vector(version,'AI fields absent',p=>{const x=c(p);delete x.aiScore;delete x.aiEvidence;},version===14);
 for(const key of ['aiScore','aiEvidence'])await vector(version,`AI absent ${key}`,p=>delete c(p)[key],version===14);
 for(const value of [null,0,5,10,-0,'5',true,false,1,15,{},[]])await vector(version,`AI score ${JSON.stringify(value)}`,p=>{const x=c(p);x.aiScore=value;x.aiEvidence=' CI source ';},value===null||typeof value==='number'&&[0,5,10].includes(value));
 for(const value of ['', ' \uFEFF\u2028', '\u0085', ' CI source '])await vector(version,`positive AI evidence ${JSON.stringify(value)}`,p=>{const x=c(p);x.aiScore=5;x.aiEvidence=value;},value==='\u0085'||value===' CI source ');
 for(const value of [null,false,10,{},[], '😀'.repeat(5000), '😀'.repeat(5000)+'x'])await vector(version,`AI evidence type/boundary ${typeof value} ${String(value).length}`,p=>c(p).aiEvidence=value,typeof value==='string'&&value.length<=10000);
 await vector(version,'missing review sources',p=>delete c(p).reviewSources,false);
 for(const key of sourceKeys){await vector(version,`missing company source ${key}`,p=>delete c(p).reviewSources[key],false);await vector(version,`null company source ${key}`,p=>c(p).reviewSources[key]=null,false);}
 for(const value of [null,false,1,'text',{},[]])await vector(version,`reviews container ${JSON.stringify(value)}`,p=>c(p).reviews=value,value!==null&&typeof value==='object');
 for(const [key,max] of [['plusShort',512],['minusShort',512],['plus',2000],['minus',2000]]){
  for(const value of [null,{},false,1,'😀'.repeat(max/2),'😀'.repeat(max/2)+'x','\ud800'.repeat(max),'\ud800'.repeat(max+1)])await vector(version,`review ${key} ${typeof value} ${String(value).length}`,p=>c(p).reviews[key]=value,value===null||typeof value==='string'&&value.length<=max);
 }
 for(const agencies of [null,{},'ignored'])await vector(version,`non-array agencies ${JSON.stringify(agencies)}`,p=>p.catalog.agencies=agencies);
 await vector(version,'agency source requirements even for discarded nameless row',p=>{const x=a(p);delete x.name;});
 await vector(version,'null agency item',p=>p.catalog.agencies=[null],false);
 for(const key of ['twoGis','otzovik','personAgency','yell'])await vector(version,`missing agency source ${key}`,p=>delete a(p).reviewSources[key],false);
 const validHalf=[null,0,0.5,10,0.5000000001,' 5.5 ','0xA','0b101','0o12','0005.5','5e0','5.','+.5',true,false,[5],[[5]],'0x'+'0'.repeat(80)+'5'];
 const invalidHalf=['',' \uFEFF\u2028',-0.5,10.5,5.25,0.500000001,'NaN','Infinity','1e400','-0x5','0x+5','0x_5','0b2','+','.',[],[null],[false],[1,2],{},'5_0','\u00855','infinity'];
 for(const [key] of AGENCY_RATING_INPUT_META){for(const value of validHalf)await vector(version,`agency ${key} accepts ${JSON.stringify(value)}`,p=>a(p)[key]=value);for(const value of invalidHalf)await vector(version,`agency ${key} rejects ${JSON.stringify(value)}`,p=>a(p)[key]=value,false);}
 for(const [key,max,integer] of [['reviewsScore',80,false],['relevanceScore',10,true],['revenueScore',10,true]]){
  for(const value of [null,0,max,'0xA',true,false,[5],[[5]],' 5 ', '0005',1.5,'2.3','',[],{},'NaN','Infinity',-1,max+1]){
   const empty=value!=null&&String(value).trim()==='';const n=Number(value);const want=value==null||!empty&&Number.isFinite(n)&&n>=0&&n<=max&&(!integer||Number.isInteger(n));
   await vector(version,`agency calculated ${key} ${JSON.stringify(value)}`,p=>a(p)[key]=value,want);
  }
 }
 const base=legacyInitial(version);
 for(const t of base.storage.tables){
  await vector(version,`missing table ${t.name}`,p=>p.storage.tables=p.storage.tables.filter(x=>x.name!==t.name),false);
  for(const col of t.columns)await vector(version,`missing column ${t.name}.${col.name}`,p=>table(p,t.name).columns=table(p,t.name).columns.filter(x=>x.name!==col.name),false);
 }
 for(const name of ['app_catalog','google_calendar_connection','user_profiles','app_runtime_generation','unknown'])await vector(version,`forbidden section ${name}`,p=>p.storage.tables.push({name,columns:[],rows:[]}),false);
 await vector(version,'duplicate section',p=>p.storage.tables.push(structuredClone(p.storage.tables[0])),false);
 await vector(version,'too many tables',p=>p.storage.tables=Array.from({length:53},(_,i)=>({name:`CI_${i}`,columns:[],rows:[]})),false);
 for(const value of [null,{},1,'tables'])await vector(version,`invalid tables ${JSON.stringify(value)}`,p=>p.storage.tables=value,false);
 for(const field of ['name','columns','rows'])await vector(version,`missing table property ${field}`,p=>delete p.storage.tables[0][field],false);
 await vector(version,'nonstring table name',p=>p.storage.tables[0].name=1,false);
 for(const columns of [null,{},[null],[{name:null}],[]])await vector(version,`malformed columns ${JSON.stringify(columns)}`,p=>table(p,'agency_identity').columns=columns,false);
 await vector(version,'duplicate columns',p=>table(p,'agency_identity').columns[1]=table(p,'agency_identity').columns[0],false);
 await vector(version,'extra columns',p=>table(p,'agency_identity').columns.push({name:'extra'}),false);
 for(const value of [null,[],{},true,1,'row'])await vector(version,`malformed row ${JSON.stringify(value)}`,p=>table(p,'agency_identity').rows=[value],false);
 for(const value of [null,0,'0',-0,'😀\ud800',1.25])await vector(version,`scalar ${JSON.stringify(value)}`,p=>row(p,'agency_identity',{id:value}));
 for(const value of [true,false,{},[]])await vector(version,`nonscalar cell ${JSON.stringify(value)}`,p=>row(p,'agency_identity',{id:value}),false);
 await vector(version,'missing row field',p=>{const r=row(p,'agency_identity',{});delete r.id;},false);
 await vector(version,'extra row field',p=>row(p,'agency_identity',{extra:'CI'}),false);
 await vector(version,'case-sensitive row field',p=>{const r=row(p,'agency_identity',{});delete r.id;r.Id=1;},false);
 await vector(version,'overflow numeric literal in row',p=>row(p,'agency_identity',{id:'CI_OVERFLOW'}),false,s=>s.replace('"CI_OVERFLOW"','1e400'));
 await vector(version,'duplicate JSON member last wins',p=>row(p,'agency_identity',{id:7}),true,s=>s.replace('"id":7','"id":false,"id":7'));
 for(const d of schema.tables.filter(t=>base.storage.tables.some(x=>x.name===t.name)))for(const fk of d.foreignKeys){
  for(const typed of [false,true])await vector(version,`FK ${d.name}.${fk.from} typed ${typed}`,p=>{row(p,fk.table,{[fk.to]:7});row(p,d.name,{[fk.from]:typed?'7':7});},!typed);
  await vector(version,`FK ${d.name}.${fk.from} missing parent`,p=>row(p,d.name,{[fk.from]:7}),false);
 }
}
// Cheap-to-transfer inputs exercise both full row and byte guards independently.
for(const version of [14,15])await vector(version,'row cap before full layout',p=>table(p,'agency_identity').rows=Array(200001).fill(null),false);
if(!stdio)console.log(`PASS: ${count} unchanged legacy v14-15 format vectors (${accepted} accepted); no SQL/files/network`);
