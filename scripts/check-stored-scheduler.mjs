// Exact source function bodies in their ESM strict environment. Explicit read
// sets only: this is not SQL/CAS, schema bootstrap or runtime acceptance.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {SCHEDULER_DEFAULTS,SCHEDULER_SOURCE_OPTIONS} from '../server/repositories/scheduler-schema.js';
import {publicSchedulerSettings,schedulerError} from '../server/repositories/scheduler.js';
globalThis.fetch=()=>{throw new Error('Stored settings oracle forbids network');};
const source=readFileSync(new URL('../server/repositories/scheduler.js',import.meta.url),'utf8');
const a=source.indexOf('export async function schedulerSettings(db){'),b=source.indexOf('\nexport function publicSchedulerSettings',a);
const c=source.indexOf('export async function saveSchedulerSettings(db,input,ownerName){'),d=source.indexOf('\n const r=await db.prepare',c);
assert.ok(a>=0&&b>a&&c>b&&d>c);
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const read=new AsyncFunction('db','firecrawlSettings','SCHEDULER_DEFAULTS','SCHEDULER_SOURCE_OPTIONS',"'use strict';"+source.slice(a+'export async function schedulerSettings(db){'.length,b).replace(/}\s*$/,''));
const save=new AsyncFunction('db','input','ownerName','schedulerSettings','SCHEDULER_SOURCE_OPTIONS','SCHEDULER_TYPES','schedulerError','publicSchedulerSettings',"'use strict';"+source.slice(c+'export async function saveSchedulerSettings(db,input,ownerName){'.length,d)+"return {stored:JSON.stringify(next),public:JSON.stringify(publicSchedulerSettings({...next,favoritesOnly:input.favoritesOnly??old.favoritesOnly}))};");
const fixtures=JSON.parse(readFileSync(new URL('../internal/parser/testdata/settings-contract.json',import.meta.url),'utf8')).vectors.filter(v=>v.valid);
const stored=(name,value,save=true)=>({name,raw:JSON.stringify(value),save});
const variants=[stored('defaults',{}),stored('old sources absent',{revision:7}),stored('selected sources',{...SCHEDULER_DEFAULTS,sourceIds:['hh'],revision:7}),stored('null selected sources',{sourceIds:null,revision:7}),stored('null agencies',{agenciesEnabled:null,revision:7}),stored('private owner',{cloudOwnerId:'CI_SYNTHETIC_OWNER',revision:7}),stored('private object',{cloudOwnerId:{CI:true},cloudConnected:false,revision:7}),stored('private false',{cloudOwnerId:0,cloudConnected:true,revision:7}),stored('metadata',{future:{keep:[1,null,'CI']},revision:7}),stored('HTML JSON escaping',{future:'<CI>&\u2028',revision:7}),stored('numeric spread',{'1':'CI numeric',future:{'1':'CI nested'},revision:7}),stored('array',[]),stored('array indices',['CI array',null]),stored('priorities',{priorities:{scan_source:95},revision:7}),stored('null priorities',{priorities:null,revision:7}),{name:'duplicate/raw UTF16/infinity',raw:'{"revision":6,"revision":7,"cloudOwnerId":"\\ud800","future":{"\\ud800":1e400,"1":true}}',save:true},stored('max safe revision',{revision:9007199254740991}),stored('unknown priorities',{priorities:{CI_future:1},revision:7}),stored('bad priority',{priorities:{scan_source:101},revision:7}),stored('priority string',{priorities:'CI'},false),stored('revision string',{revision:'7'},false),stored('source scalar',{sourceIds:5},false),stored('null',null,false),stored('string','CI',false),stored('number',1,false),stored('boolean',false,false),{name:'malformed',raw:'not JSON',save:false}];
let count=0;
for(const v of variants)for(const favorites of [0,1])for(const legacySources of [[],['hh','telegram','companySite','rabota'],['getmatch','hh','habr']]){
 const db={prepare(sql){assert.equal(sql,'SELECT DISTINCT source_id FROM vacancy_source_parsing WHERE auto_enabled=1');return {async all(){return {results:legacySources.map(source_id=>({source_id}))};}};}};
 const readState=()=>read(db,async()=>({scheduler_json:v.raw,favorites_only:favorites}),SCHEDULER_DEFAULTS,SCHEDULER_SOURCE_OPTIONS);
 let state;
 try{state=await readState();process.stdout.write(JSON.stringify({kind:'read',name:v.name,raw:v.raw,favorites:favorites!==0,legacySources,body:JSON.stringify(publicSchedulerSettings(state)),error:false})+'\n');}
 catch{process.stdout.write(JSON.stringify({kind:'read',name:v.name,raw:v.raw,favorites:favorites!==0,legacySources,error:true})+'\n');}
 count++;
 if(state&&v.save)for(const f of fixtures){
  const frame={kind:'save',name:v.name+'/'+f.name,raw:v.raw,favorites:favorites!==0,legacySources,input:f.input,ownerName:'Eugene'};
  try{const next=await save(db,f.input,'Eugene',readState,SCHEDULER_SOURCE_OPTIONS,Object.keys(SCHEDULER_DEFAULTS.priorities),schedulerError,publicSchedulerSettings);process.stdout.write(JSON.stringify({...frame,...next,error:false})+'\n');}
  catch(e){if(e.status!==400)throw e;process.stdout.write(JSON.stringify({...frame,error:true,message:e.message})+'\n');}
  count++;
 }
}
process.stderr.write(`Stored scheduler source: ${count} read/validated-save frames; no SQL/provider calls\n`);
