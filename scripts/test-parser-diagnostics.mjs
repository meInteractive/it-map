import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,oldFetch=globalThis.fetch;
const {parserDiagnostics}=await moduleFor('server/services/parser-diagnostics.js');
const token='apify_api_SYNTHETIC_DIAG_1234',cipher=await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token),hash=await api.candidateHash(token);
await api.schedulerSettings(db);const generation=sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation;
sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(cipher);
for(let n=1;n<=4;n++){
 sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(?,?,?,? ,?,'hh','site','now','now','now','now')").run(n,'candidate'+n,'Manager','a'.repeat(64),'https://hh.ru/vacancy/'+n);
 sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,run_id,dataset_id,created_at,updated_at) VALUES(?,?,?,'free','account',?,'start','end',0.25,'succeeded',?,?,?,?)").run('r'+n,generation,'detail:'+n+':1:missing',hash,'run'+n,'data'+n,n,n);
}
await api.enqueueScheduledJob(db,{type:'classify_title',key:'diag-classification',sourceId:'hh'});
sqlite.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
sqlite.prepare("INSERT INTO company_vacancies(company_name,url,title,published_at,loaded_at,created_at) VALUES('Synthetic','https://hh.ru/vacancy/999','Synthetic','2026-10-16','2026-10-19','2026-10-18 22:00:00')").run();
const failed=await api.enqueueScheduledJob(db,{type:'fetch_full_text',key:'diag-failed',sourceId:'hh',candidateId:1});
sqlite.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='apify_incomplete' WHERE id=?").run(failed.id);
sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES('public-snapshot',1,'https://hh.ru/vacancy/1','https://hh.ru/vacancy/1','PRIVATE_BODY',?,'now','free','valid',?, 'now')").run('b'.repeat(64),JSON.stringify({provider:'hh-public'}));
sqlite.prepare("INSERT INTO vacancy_content_fetches(id,candidate_id,snapshot_id,source_url,final_url,firecrawl_mode,started_at,duration_ms,status) VALUES('public-fetch',1,'public-snapshot','https://hh.ru/vacancy/1','https://hh.ru/vacancy/1','hh-public',1000,1,'success')").run();
let calls=0,missing=false;
globalThis.fetch=async(url,options)=>{
 calls++;assert.equal(options.method,'GET');assert.equal(options.headers.authorization,'Bearer '+token);assert.ok(!url.includes(token));
 if(url.includes('/key-value-stores/'))return Response.json({api_only:false,results_wanted:1,max_pages:1,include_raw_data:false,proxyConfiguration:{useApifyProxy:false},secret:'DO_NOT_RETURN'});
 if(url.includes('/logs/'))return new Response('WARN detail request failed: 403 captcha PRIVATE_BODY '+token);
 if(url.includes('/actor-runs/'))return Response.json({data:{status:url.endsWith('run11')?'TIMED-OUT':'SUCCEEDED',defaultKeyValueStoreId:'store',secret:'DO_NOT_RETURN'}});
 const n=Number(new URL(url).pathname.split('/')[3].slice(4));
 return Response.json(n===2?[]:[{title:'Manager',url:'https://hh.ru/vacancy/'+n,vacancy_id:String(n),description_text:n===4?'Обязанности. Опыт. Условия. '.repeat(20):'',description_html:'PRIVATE_BODY',private_field:'DO_NOT_RETURN',...(missing&&n===3?{title:undefined,description_html:undefined}:{})}]);
};
try{
 const before=sqlite.prepare('SELECT * FROM apify_runs').all(),d=await parserDiagnostics(env);
 assert.equal(d.queue.find(x=>x.status==='pending').total,1);
 assert.equal(d.waitingJobs.length,1);assert.equal(d.waitingJobs[0].type,'classify_title');assert.ok(Number.isSafeInteger(d.waitingJobs[0].runAfter));assert.ok(!JSON.stringify(d.waitingJobs).includes('payload'));
 assert.equal(d.failedJobs[0].candidateId,1);assert.equal(d.failedJobs[0].code,'apify_incomplete');
 assert.equal(d.hhContent[0].snapshotProvider,'hh-public');assert.equal(d.hhContent[0].markdownLength,12);assert.equal(d.hhContent[0].contentStatus,'valid');assert.equal(d.apifyDetails.length,4);assert.equal(calls,24);
 assert.equal(d.freshness.timezone,'Europe/Moscow');assert.equal(d.freshness.publicationDates[0].latest_published_at,'2026-10-16');assert.equal(d.freshness.publicationDates[0].latest_loaded_at,'2026-10-19');
 assert.equal(d.apifyDetails[0].diagnosis,'valid');assert.equal(d.apifyDetails[0].input.proxyEnabled,false);assert.deepEqual(d.apifyDetails[0].logSummary.httpErrors,['403']);assert.equal(d.apifyDetails[0].logSummary.captcha,true);
 sqlite.prepare('UPDATE apify_runs SET created_at=10 WHERE id=\'r3\'').run();const incomplete=await parserDiagnostics(env);assert.equal(incomplete.apifyDetails[0].diagnosis,'invalid_description');assert.equal(incomplete.apifyDetails[0].htmlLength,12);
 sqlite.prepare('UPDATE apify_runs SET created_at=20 WHERE id=\'r2\'').run();const empty=await parserDiagnostics(env);assert.equal(empty.apifyDetails[0].diagnosis,'empty_dataset');
 missing=true;sqlite.prepare("UPDATE apify_runs SET created_at=30 WHERE id='r3'").run();const bad=await parserDiagnostics(env);assert.equal(bad.apifyDetails[0].diagnosis,'incomplete_record');assert.deepEqual(bad.apifyDetails[0].missingFields,['title','description']);
assert.ok(!JSON.stringify(d).includes(token));assert.ok(!JSON.stringify(d).includes('PRIVATE_BODY'));assert.ok(!JSON.stringify(d).includes('DO_NOT_RETURN'));
 assert.deepEqual(sqlite.prepare('SELECT state,charged_usd FROM apify_runs').all().map(r=>({...r})),before.map(r=>({state:r.state,charged_usd:r.charged_usd})),'GET diagnostics never mutate run state or billing');
 for(const page of [0,1,2])sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,run_id,dataset_id,created_at,updated_at) VALUES(?,?,?,'free','account',?,'start','end',0.25,'succeeded',?,?,?,?)").run('listing'+page,generation,'listing:00000000-0000-0000-0000-000000000001:'+page,hash,'run'+(10+page),'data'+(10+page),100-page,100-page);
 sqlite.prepare("UPDATE apify_runs SET state='failed' WHERE id='listing1'").run();const pages=await parserDiagnostics(env);assert.deepEqual(pages.apifyListings.map(x=>x.operationKey.split(':').at(-1)),['0','2','1'],'bounded latest/deepest/latest failed evidence');assert.equal(pages.apifyListings[2].diagnosis,'partial_listing_dataset');assert.ok(pages.apifyListings[2].rawSample.length>0,'terminal failure still exposes available sample fields');
 sqlite.prepare('UPDATE apify_runs SET key_hash=?').run('changed');calls=0;const changed=await parserDiagnostics(env);assert.ok(changed.apifyDetails.every(x=>x.diagnosis==='key_changed'));assert.equal(calls,0);
 sqlite.prepare('UPDATE apify_settings SET enabled=0').run();const disabled=await parserDiagnostics(env);assert.ok(disabled.apifyDetails.every(x=>x.diagnosis==='disabled'));assert.equal(calls,0);
 console.log('Parser diagnostics: bounded existing-run GETs, field lengths and exact blocker, queue groups, no Actor starts/token/body leakage, no billing writes, changed key/disabled guards passed.');
}finally{globalThis.fetch=oldFetch;f.close();}
