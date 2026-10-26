// Every HTTP request is intercepted locally; no env, DB, DNS or paid Actor.
import {readFile,writeFile} from 'node:fs/promises';
globalThis.fetch=()=>{throw new Error('Live Apify requests forbidden in oracle');};
const {apifyRequest,hhInput,apifyError,APIFY_ACTOR,APIFY_RUN_CAP_USD,APIFY_READ_RESERVE_USD}=await import('../server/integrations/apify.js');
const {apifyDatasetItems,apifyDatasetInfo}=await import('../server/services/apify-runs.js');
const runsSource=await readFile(new URL('../server/services/apify-runs.js',import.meta.url),'utf8');
const start=runsSource.indexOf('function checkedRun('),end=runsSource.indexOf('export async function recordApifyRun',start);
const stateStart=runsSource.indexOf(' const state='),stateEnd=runsSource.indexOf(',recent=',stateStart);
if(start<0||end<start||stateStart<0||stateEnd<stateStart)throw new Error('Legacy Apify run contract changed; review');
const checkRun=new Function('value','idPattern','apifyError',runsSource.slice(start,end)+'return checkedRun(value);');
const state=new Function('run','return '+runsSource.slice(stateStart+' const state='.length,stateEnd));
const hh=[];
const hhCases=[
 ['employer','https://hh.ru/employer/1',{}],['subdomain','https://sber.hh.ru/employer/123/',{page:3}],['search','https://hh.ru/search/vacancy?employer_id=1&text=CI+synthetic',{}],['query-order','https://hh.ru/search/vacancy?text=CI%20synthetic&employer_id=1&page=9&per_page=20',{page:2}],
 ['vacancy','https://hh.ru/vacancy/123#CI',{}],['detail','https://hh.ru/vacancy/123?text=CI#CI',{detail:true}],['vacancy-query-unicode',"https://hh.ru/vacancy/123?text=CI синтетика~*'<>",{}],['subdomain-detail','https://sber.hh.ru/vacancy/123/',{detail:true}],['default-port','https://hh.ru:443/employer/1',{}],['mixed-case-host','https://HH.RU/employer/1',{}],
 ['order','https://hh.ru/employer/1',{orderBy:'publication_time'}],['preserve-order','https://hh.ru/search/vacancy?order_by=relevance&employer_id=1',{orderBy:'publication_time'}],['bad-order','https://hh.ru/employer/1',{orderBy:'CI'}],
 ['title','https://hh.ru/employer/1',{titleQuery:'NAME:(CI synthetic)'}],['title-combined','https://hh.ru/search/vacancy?employer_id=1&text=CI+original',{titleQuery:'NAME:(CI synthetic)'}],['title-special-form','https://hh.ru/search/vacancy?employer_id=1&text=CI~*',{titleQuery:'NAME:(CI~*)'}],['title-empty-old','https://hh.ru/search/vacancy?text=&employer_id=1',{titleQuery:'NAME:(CI)'}],['duplicate-page','https://hh.ru/search/vacancy?page=1&employer_id=1&page=2',{page:3}],
 ['title-invalid','https://hh.ru/employer/1',{titleQuery:'CI'}],['title-control','https://hh.ru/employer/1',{titleQuery:'NAME:(CI\n)'}],['title-over','https://hh.ru/employer/1',{titleQuery:'NAME:('+'x'.repeat(2000)+')'}],['title-null','https://hh.ru/employer/1',{titleQuery:null}],
 ['http','http://hh.ru/employer/1',{}],['foreign','https://ci.invalid/employer/1',{}],['lookalike','https://hh.ru.ci.invalid/employer/1',{}],['other-region','https://hh.kz/employer/1',{}],['userinfo','https://CI@hh.ru/employer/1',{}],['port','https://hh.ru:444/employer/1',{}],['ip','https://127.0.0.1/employer/1',{}],['search-no-employer','https://hh.ru/search/vacancy?text=CI',{}],['search-two-employers','https://hh.ru/search/vacancy?employer_id=1&employer_id=2',{}],['employer-id-invalid','https://hh.ru/search/vacancy?employer_id=CI',{}],['detail-list','https://hh.ru/employer/1',{detail:true}],['wrong-path','https://hh.ru/CI?employer_id=1',{}],['encoded-employer','https://hh.ru/search/vacancy?employer_id=%31',{}],['employer-drops-query','https://hh.ru/employer/001?employer_id=2&text=CI',{}]
];
for(const [name,url,options]of hhCases){try{hh.push({name,url,options,valid:true,normalized:hhInput(url,options)})}catch(e){hh.push({name,url,options,valid:false,code:e.code,status:e.status,message:e.message})}}
const transport=[];
const cases=[];const add=(name,path,options={},response={status:200,body:'{"synthetic":true}'})=>cases.push({name,path,options,response});
for(const path of ['/users/me','/users/me/limits','/users/me/usage/monthly','/actor-runs/Run123','/datasets/Data123','/datasets/Data123/items?format=json','/key-value-stores/Store123/records/INPUT'])add('get-'+path,path);
add('start','/actors/'+APIFY_ACTOR+'/runs?timeout=180&maxTotalChargeUsd=0.25&restartOnError=false&waitForFinish=0',{method:'POST',body:{synthetic:'CI'}});
add('text-log','/logs/Log123',{text:true},{status:200,body:'CI synthetic log'});
for(const [name,path,options]of [['foreign','https://ci.invalid/',{}],['relative-host','//ci.invalid/',{}],['path-traversal','/actor-runs/../CI',{}],['wrong-resource','/CI',{}],['dataset-post','/datasets/Data123',{method:'POST'}],['account-post','/users/me',{method:'POST'}],['log-post','/logs/Log123',{method:'POST'}],['text-run','/actor-runs/Run123',{text:true}]])add(name,path,options);
for(const status of [200,201,204,302,400,401,402,403,404,408,429,500,503])add('status-'+status,'/actor-runs/Run123',{},status===204?{status,mode:'empty'}:{status,body:'{"synthetic":true}'});
add('text-no-content','/logs/Log123',{text:true},{status:204,mode:'empty'});
add('bad-json','/actor-runs/Run123',{}, {status:200,body:'CI not JSON'});add('too-large','/actor-runs/Run123',{maxBytes:2},{status:200,body:'{"synthetic":true}'});add('network','/actor-runs/Run123',{}, {mode:'network'});add('stream-error','/actor-runs/Run123',{}, {mode:'stream-error'});add('bom','/actor-runs/Run123',{}, {status:200,body:'\uFEFF{"synthetic":true}'});
const original=globalThis.fetch;
try{for(const input of cases){let calls=0,request=null;globalThis.fetch=async(url,options)=>{calls++;request={url,method:options.method,redirect:options.redirect,authOK:options.headers.authorization==='Bearer CI_SYNTHETIC_NOT_AN_API_KEY',contentType:options.headers['content-type'],body:options.body??null};const r=input.response;if(r.mode==='network')throw new Error('CI synthetic network failure');if(r.mode==='stream-error')return new Response(new ReadableStream({start(controller){controller.error(new Error('CI synthetic body failure'));}}));if(r.mode==='empty')return new Response(null,{status:r.status});return new Response(r.body,{status:r.status});};try{const value=await apifyRequest('CI_SYNTHETIC_NOT_AN_API_KEY',input.path,input.options);transport.push({...input,valid:true,value,calls,request})}catch(e){transport.push({...input,valid:false,code:e.code,status:e.status,message:e.message,calls,request})}}}finally{globalThis.fetch=original;}
const runs=[];for(const status of ['READY','RUNNING','SUCCEEDED','FAILED','TIMED-OUT','ABORTING','ABORTED','TIMING-OUT','CI_UNKNOWN']){const input={id:'Run123',defaultDatasetId:'Data123',status,usageTotalUsd:0.02};try{const result=checkRun(input,/^[A-Za-z0-9]+$/,apifyError);runs.push({input,valid:true,state:state(result)})}catch(e){runs.push({input,valid:false,code:e.code,status:e.status})}}
for(const input of [null,{id:'bad-id',defaultDatasetId:'Data123',status:'RUNNING'},{id:'Run123',status:'RUNNING'},{id:123,defaultDatasetId:456,status:'RUNNING'},{id:null,defaultDatasetId:'Data123',status:'RUNNING'}]){
 try{const checked=checkRun(input,/^[A-Za-z0-9]+$/,apifyError);const canonical=typeof checked.id==='string'&&typeof checked.defaultDatasetId==='string';runs.push({input,legacyAccepted:true,valid:canonical,...(canonical?{state:state(checked)}:{code:'apify_data',status:502,reason:'Legacy regexp coerces missing/null/numeric ID; Go rejects noncanonical provider identity before persistence'})})}
 catch(e){runs.push({input,legacyAccepted:false,valid:false,code:e.code,status:e.status})}
}
const datasets=[];
const datasetCases=[
 {name:'items-empty',kind:'items',id:'Data123',offset:0,clean:false,body:[]},
 {name:'items-ten',kind:'items',id:'Data123',offset:10,clean:true,body:Array(10).fill({synthetic:true})},
 {name:'items-eleven',kind:'items',id:'Data123',offset:0,clean:false,body:Array(11).fill({})},
 {name:'items-null',kind:'items',id:'Data123',offset:0,clean:false,body:null},
 {name:'items-object',kind:'items',id:'Data123',offset:0,clean:false,body:{}},
 {name:'items-invalid-id',kind:'items',id:'bad-id',offset:0,clean:false,body:[]},
 {name:'items-negative-offset',kind:'items',id:'Data123',offset:-1,clean:false,body:[]},
 {name:'items-unsafe-offset',kind:'items',id:'Data123',offset:9007199254740992,clean:false,body:[]},
 {name:'info',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:12,cleanItemCount:10,modifiedAt:'2026-10-26T12:00:00.000Z'}}},
 {name:'info-clean-null',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:12,cleanItemCount:null}}},
 {name:'info-clean-invalid',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:12,cleanItemCount:'10',modifiedAt:'CI invalid date'}}},
 {name:'info-invalid-date-offset',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:12,modifiedAt:'2026-10-26T12:00:00+24:00'}}},
 {name:'info-foreign-id',kind:'info',id:'Data123',body:{data:{id:'Other',itemCount:1}}},
 {name:'info-negative-count',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:-1}}},
 {name:'info-string-count',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:'1'}}},
 {name:'info-null-count',kind:'info',id:'Data123',body:{data:{id:'Data123',itemCount:null}}},
 {name:'info-missing-count',kind:'info',id:'Data123',body:{data:{id:'Data123'}}},
 {name:'info-invalid-id',kind:'info',id:'bad-id',body:{data:{id:'bad-id',itemCount:1}}}
];
try{for(const input of datasetCases){let calls=0,requestURL=null;globalThis.fetch=async(url)=>{calls++;requestURL=url;return Response.json(input.body);};try{const value=input.kind==='items'?await apifyDatasetItems('CI_SYNTHETIC_NOT_AN_API_KEY',input.id,input.offset,{clean:input.clean}):await apifyDatasetInfo('CI_SYNTHETIC_NOT_AN_API_KEY',input.id);datasets.push({...input,valid:true,value,calls,requestURL})}catch(e){datasets.push({...input,valid:false,code:e.code,status:e.status,message:e.message,calls,requestURL})}}}finally{globalThis.fetch=original;}

const result=JSON.stringify({oracle:'Unchanged hhInput/apifyRequest/checkedRun/state; intercepted synthetic HTTP only',actor:APIFY_ACTOR,runCapUsd:APIFY_RUN_CAP_USD,readReserveUsd:APIFY_READ_RESERVE_USD,hh,transport,runs,datasets},null,2)+'\n';
const target=new URL('../internal/integrations/apify/testdata/transport-contract.json',import.meta.url);
if(process.argv.length===3&&process.argv[2]==='--write')await writeFile(target,result);else if(process.argv.length!==2)throw new Error('Use --write only for reviewed fixture regeneration');else if(await readFile(target,'utf8')!==result)throw new Error('Legacy Apify transport contract differs; review before regenerating');
console.log(`PASS: ${hh.length} HH input/${transport.length} HTTP/${runs.length} run-state/${datasets.length} dataset vectors from unchanged JS, no live requests`);
