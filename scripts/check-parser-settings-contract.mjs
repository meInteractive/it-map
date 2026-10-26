// Execute the unchanged validation/normalization block, stopping BEFORE SQL.
import {readFile,writeFile} from 'node:fs/promises';
globalThis.fetch=()=>{throw new Error('Parser settings oracle forbids network');};
const {SCHEDULER_DEFAULTS,SCHEDULER_SOURCE_OPTIONS}=await import('../server/repositories/scheduler-schema.js');
const {schedulerError,publicSchedulerSettings}=await import('../server/repositories/scheduler.js');
const source=await readFile(new URL('../server/repositories/scheduler.js',import.meta.url),'utf8');
const route=source.indexOf('export async function saveSchedulerSettings');
const start=source.indexOf(' const old=await schedulerSettings(db),keys=',route),end=source.indexOf(' const r=await db.prepare',start);
if(route<0||start<route||end<start)throw new Error('Legacy scheduler settings boundaries changed; review');
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const oracle=new AsyncFunction('input','db','ownerName','schedulerSettings','schedulerError','SCHEDULER_SOURCE_OPTIONS','SCHEDULER_TYPES','publicSchedulerSettings',source.slice(start,end)+`return {next:publicSchedulerSettings({...next,favoritesOnly:input.favoritesOnly??old.favoritesOnly}),expectedRevision:input.revision};`);
const previous={...structuredClone(SCHEDULER_DEFAULTS),enabled:true,dayBudgetUsd:5,monthBudgetUsd:50,revision:7,sourceIds:['hh'],favoritesOnly:true,cloudOwnerId:'CI_SYNTHETIC_OWNER_NOT_A_TOKEN'};
const defaults=publicSchedulerSettings({...structuredClone(SCHEDULER_DEFAULTS),favoritesOnly:true});
const base={enabled:false,revision:7,dayBudgetUsd:0,monthBudgetUsd:0};
const cases=[
 ['base',{}],['enable',{enabled:true}],['budgets',{dayBudgetUsd:1.25,monthBudgetUsd:12.5}],['budget-limit',{dayBudgetUsd:10000,monthBudgetUsd:10000}],
 ['negative-day',{dayBudgetUsd:-1}],['negative-month',{monthBudgetUsd:-1}],['over-day',{dayBudgetUsd:10000.01}],['over-month',{monthBudgetUsd:10001}],['string-budget',{dayBudgetUsd:'1'}],['null-budget',{monthBudgetUsd:null}],
 ['bad-enabled',{enabled:1}],['null-enabled',{enabled:null}],['string-revision',{revision:'7'}],['null-revision',{revision:null}],['fraction-revision',{revision:1.5}],['max-safe-revision',{revision:9007199254740991}],['unsafe-revision',{revision:9007199254740992}],['negative-revision-preserved-for-cas',{revision:-1}],
 ['unknown-field',{token:'CI SYNTHETIC placeholder'}],['owner-cannot-be-input',{ownerName:'Demo'}],['cloud-owner-cannot-be-input',{cloudOwnerId:'CI SYNTHETIC not token'}],
 ['agencies-on',{agenciesEnabled:true}],['agencies-null',{agenciesEnabled:null}],['agencies-number',{agenciesEnabled:1}],['favorites-off',{favoritesOnly:false}],['favorites-null',{favoritesOnly:null}],['favorites-string',{favoritesOnly:'true'}],
 ['interval-one',{intervalDays:1}],['interval-three',{intervalDays:3}],['interval-seven',{intervalDays:7}],['interval-two',{intervalDays:2}],['interval-string',{intervalDays:'1'}],['interval-null',{intervalDays:null}],
 ['sources-empty',{sourceIds:[]}],['sources-all-supported',{sourceIds:SCHEDULER_SOURCE_OPTIONS.filter(s=>s.supported).map(s=>s.id)}],['sources-duplicate',{sourceIds:['hh','hh']}],['sources-unsupported',{sourceIds:['telegram']}],['sources-unknown',{sourceIds:['CI synthetic source']}],['sources-null',{sourceIds:null}],['sources-string',{sourceIds:'hh'}],['sources-number-item',{sourceIds:[1]}],['sources-many',{sourceIds:Array(9).fill('hh')}],
 ['priorities-empty',{priorities:{}}],['priorities-null',{priorities:null}],['priorities-partial',{priorities:{scan_source:95}}],['priorities-zero',{priorities:Object.fromEntries(Object.keys(SCHEDULER_DEFAULTS.priorities).map(k=>[k,0]))}],['priorities-limit',{priorities:{scan_source:100}}],['priorities-negative',{priorities:{scan_source:-1}}],['priorities-over',{priorities:{scan_source:101}}],['priorities-fraction',{priorities:{scan_source:1.5}}],['priorities-string-value',{priorities:{scan_source:'10'}}],['priorities-null-value',{priorities:{scan_source:null}}],['priorities-unknown',{priorities:{CI_synthetic:5}}],['priorities-array',{priorities:[]}],['priorities-string',{priorities:'CI synthetic'}],
];
const vectors=[];
const add=async(name,input)=>{try{vectors.push({name,input,valid:true,normalized:await oracle(input,null,'Eugene',async()=>structuredClone(previous),schedulerError,SCHEDULER_SOURCE_OPTIONS,Object.keys(SCHEDULER_DEFAULTS.priorities),publicSchedulerSettings)})}catch(e){vectors.push({name,input,valid:false,error:e.message,status:e.status})}};
for(const [name,patch]of cases)await add(name,{...base,...patch});
for(const key of Object.keys(base)){const input={...base};delete input[key];await add('missing-'+key,input);}
for(const [name,input]of [['null',null],['array',[]],['boolean',true]])await add(name,input);
const output=JSON.stringify({oracle:'Unchanged saveSchedulerSettings validation/normalization before SQL; synthetic previous state; no DB/network',defaults,options:SCHEDULER_SOURCE_OPTIONS,previous:publicSchedulerSettings(previous),vectors},null,2)+'\n';
const target=new URL('../internal/parser/testdata/settings-contract.json',import.meta.url);
if(process.argv.length===3&&process.argv[2]==='--write')await writeFile(target,output);else if(process.argv.length!==2)throw new Error('Use --write only for reviewed fixture regeneration');else if(await readFile(target,'utf8')!==output)throw new Error('Legacy settings contract differs; review before regenerating');
console.log(`PASS: ${vectors.length} parser settings vectors from unchanged JS; no SQL/network`);
