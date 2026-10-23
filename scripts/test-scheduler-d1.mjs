import assert from 'node:assert/strict';import migrationFs from 'node:fs';import {fileURLToPath} from 'node:url';import {prepareBackend} from './test-support.mjs';import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');await prepareBackend();let outbound=0;
const token='synthetic-scheduler-secret',options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'scheduler-d1'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'scheduler-d1-secret',SCHEDULER_TOKEN:token},outboundService:async()=>{outbound++;throw new Error('No external API allowed')}};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 // Apply the queue extension on native D1 with a live lease and billed attempt.
 await db.prepare('DROP TABLE scheduler_jobs').run();
 const legacy=migrationFs.readFileSync('drizzle/0061_parser_scheduler.sql','utf8');const oldQueue=legacy.slice(legacy.indexOf('CREATE TABLE scheduler_jobs ('),legacy.indexOf('CREATE TABLE scheduler_runs ('));
 await db.batch(oldQueue.split(';').map(s=>s.trim()).filter(Boolean).map(sql=>db.prepare(sql)));
 await db.prepare("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,status,priority,run_after,locked_by,locked_until,created_at,updated_at) VALUES('migration-job','old-generation','key','scan_source','running',20,1,'lease-owner',9999999999999,1,1)").run();
 await db.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES('migration-cost','migration-job','run','scan_source',1,'running',0.25,NULL)").run();
 const beforeMigration=await db.prepare("SELECT * FROM scheduler_jobs WHERE id='migration-job'").first();
 const upgrade=migrationFs.readFileSync('drizzle/0072_scheduler_relevance.sql','utf8').replace(/^--.*$/gm,'');await db.batch(upgrade.split(';').map(s=>s.trim()).filter(Boolean).map(sql=>db.prepare(sql)));
 assert.deepEqual(await db.prepare("SELECT * FROM scheduler_jobs WHERE id='migration-job'").first(),beforeMigration);assert.equal((await db.prepare("SELECT reserved_usd FROM scheduler_attempts WHERE id='migration-cost'").first()).reserved_usd,.25);
 await db.prepare("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,priority,run_after,created_at,updated_at) VALUES('new-type','old-generation','score','score_relevance',85,1,1,1)").run();
 await db.batch([db.prepare("DELETE FROM scheduler_jobs WHERE generation='old-generation'"),db.prepare("DELETE FROM scheduler_attempts WHERE id='migration-cost'")]);
 // The isolated legacy queue was rebuilt above; advance it to the current owner layout before exercising current HTTP.
 await db.prepare('ALTER TABLE scheduler_jobs ADD agency_id INTEGER CHECK(agency_id IS NULL OR company_id IS NULL)').run();
 await db.prepare("CREATE UNIQUE INDEX scheduler_agency_source_active_idx ON scheduler_jobs(generation,agency_id,source_id) WHERE agency_id IS NOT NULL AND type='scan_source' AND status IN ('pending','running')").run();
 const login=async name=>(await mf.dispatchFetch('https://scheduler.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'})).headers.get('set-cookie').split(';')[0];const admin=await login('Eugene'),user=await login('Nastya');
 const req=(path,method='GET',body,cookie=admin,extra={})=>mf.dispatchFetch('https://scheduler.test'+path,{method,headers:{...(cookie?{cookie}:{}),'content-type':'application/json',...extra},...(body===undefined?{}:{body:JSON.stringify(body)})});
 assert.equal((await req('/api/scheduler','GET',undefined,user)).status,403);let view=await (await req('/api/scheduler')).json();assert.equal(view.settings.enabled,false);assert.equal(view.settings.favoritesOnly,true);
 assert.equal((await req('/api/scheduler/run','POST',{requestId:crypto.randomUUID()},admin,{origin:'https://evil.test'})).status,403);
 assert.equal((await req('/api/internal/scheduler/tick','POST',{},null)).status,401);
 const receiptKey=crypto.randomUUID(),receipt=await (await req('/api/scheduler/run','POST',{requestId:receiptKey},admin)).json();
 const readback=await req('/api/scheduler/run?requestId='+receiptKey,'GET',undefined,admin);assert.equal(readback.status,200);assert.equal((await readback.json()).id,receipt.id);
 assert.equal((await req('/api/scheduler/run?requestId='+crypto.randomUUID(),'GET',undefined,admin)).status,404);
 assert.equal((await req('/api/scheduler/run?requestId=bad','GET',undefined,admin)).status,400);
 const streamed=await req('/api/scheduler/run','POST',{requestId:crypto.randomUUID()},admin,{accept:'application/x-ndjson'});assert.match(streamed.headers.get('content-type'),/ndjson/);const events=(await streamed.text()).trim().split('\n').map(l=>JSON.parse(l));assert.equal(events[0].type,'progress');assert.equal(events.at(-1).type,'done');assert.equal(events.at(-1).run.status,'paused');
 const id=crypto.randomUUID(),call=()=>req('/api/internal/scheduler/tick','POST',{requestId:id,diagnostic:true},null,{authorization:'Bearer '+token});
 const results=await Promise.all([call(),call()]);assert.ok(results.some(r=>r.status===200));assert.ok(results.every(r=>[200,409].includes(r.status)));
 let response=await call();assert.equal(response.status,200,await response.clone().text());const probe=await response.json();assert.equal(probe.job.status,'completed');assert.equal(probe.externalRequests,0);assert.equal((await db.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='diagnostic'").first()).n,1);assert.equal(outbound,0);
 response=await req('/api/internal/scheduler/tick','POST',{},null,{authorization:'Bearer '+token});assert.equal((await response.json()).status,'paused');assert.equal(outbound,0);
 response=await req('/mcp','POST',{jsonrpc:'2.0',id:1,method:'tools/list'},null);assert.equal(response.status,200);assert.ok((await response.json()).result.tools.some(t=>t.name==='scheduler_tick'));
 const rpc={jsonrpc:'2.0',id:2,method:'tools/call',params:{name:'scheduler_status',arguments:{}}};assert.equal((await req('/mcp','POST',rpc,null)).status,401);assert.equal((await req('/mcp','POST',rpc,null,{'oai-authenticated-user-id':'someone'})).status,403);
 assert.equal((await req('/api/scheduler/cloud-owner','POST',{confirmed:true})).status,403);
 assert.equal((await req('/api/scheduler/cloud-owner','POST',{confirmed:true},admin,{'oai-authenticated-user-id':'test-owner'})).status,200);
 const modernHeaders={'mcp-protocol-version':'2026-07-28'};
 const discovered=await req('/mcp','POST',{jsonrpc:'2.0',id:'modern',method:'server/discover',params:{_meta:{'io.modelcontextprotocol/protocolVersion':'2026-07-28','io.modelcontextprotocol/clientCapabilities':{}}}},null,modernHeaders);assert.equal(discovered.status,200);assert.equal((await discovered.json()).result.resultType,'complete');
 // No-argument preparation is harmless even if its response is lost.
 await db.prepare("UPDATE firecrawl_settings SET scheduler_json=json_set(scheduler_json,'$.enabled',json('true')) WHERE id=1").run();
 const beforePrepare=(await db.prepare('SELECT count(*) n FROM scheduler_runs').first()).n;
 const prepareRpc={jsonrpc:'2.0',id:'prepare-series',method:'tools/call',params:{name:'scheduler_tick',arguments:{}}},ownerHeaders={...modernHeaders,'oai-authenticated-user-id':'test-owner'};
 const preparedResponse=await req('/mcp','POST',prepareRpc,null,ownerHeaders),preparedEnvelope=await preparedResponse.json(),prepared=JSON.parse(preparedEnvelope.result.content[0].text);assert.equal(preparedEnvelope.result.resultType,'complete');assert.equal(prepared.status,'prepared');assert.match(prepared.continuationRequestId,/^cloud:[0-9a-f-]+:1$/);
 const prepareStream=await req('/mcp','POST',prepareRpc,null,{...ownerHeaders,accept:'text/event-stream'});const prepareFrames=(await prepareStream.text()).split('\n').filter(l=>l.startsWith('data: ')).map(l=>JSON.parse(l.slice(6))),preparedAgain=JSON.parse(prepareFrames.at(-1).result.content[0].text);assert.equal(preparedAgain.status,'prepared');assert.notEqual(preparedAgain.continuationRequestId,prepared.continuationRequestId);assert.equal((await db.prepare('SELECT count(*) n FROM scheduler_runs').first()).n,beforePrepare);assert.equal(outbound,0);
 await db.prepare("UPDATE firecrawl_settings SET scheduler_json=json_set(scheduler_json,'$.enabled',json('false')) WHERE id=1").run();
 const workRpc={...prepareRpc,params:{name:'scheduler_tick',arguments:{requestId:prepared.continuationRequestId}}},work=JSON.parse((await (await req('/mcp','POST',workRpc,null,ownerHeaders)).json()).result.content[0].text);assert.equal(work.status,'paused');assert.equal(work.trigger,'automatic');assert.equal(work.continuationStopReason,'paused');
 const repeatedWork=JSON.parse((await (await req('/mcp','POST',workRpc,null,ownerHeaders)).json()).result.content[0].text);assert.equal(repeatedWork.reused,true);assert.equal(work.id,repeatedWork.id);assert.equal((await db.prepare('SELECT count(*) n FROM scheduler_runs').first()).n,beforePrepare+1);assert.equal(outbound,0);
 const dashboardApi=await import('../.tests/backend.mjs');
 const diagnosticCandidate=await dashboardApi.upsertVacancyCandidate(db,{titleRaw:'Synthetic candidate',vacancyUrl:'https://careers.fixture.org/vacancies/321'});
 const diagnosticJob=await dashboardApi.enqueueScheduledJob(db,{type:'assign_roles',key:'dashboard-result',candidateId:diagnosticCandidate.id});
 await db.prepare("UPDATE scheduler_jobs SET status='completed' WHERE id=?").bind(diagnosticJob.id).run();
 await db.prepare("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,result_json) VALUES('dashboard-result',?,'synthetic-run','assign_roles',?,'completed',?)").bind(diagnosticJob.id,Date.now(),JSON.stringify({roleIds:[],private_body:'DO_NOT_EXPOSE',token:'SECRET_RESULT_TOKEN'})).run();
 response=await req('/mcp','POST',rpc,null,{'oai-authenticated-user-id':'test-owner'});assert.equal(response.status,200);const status=JSON.parse((await response.json()).result.content[0].text);assert.equal(status.settings.cloudConnected,true);const diagnosticAttempt=status.attempts.find(a=>a.id==='dashboard-result');assert.equal(diagnosticAttempt.candidate_id,diagnosticCandidate.id);assert.deepEqual(diagnosticAttempt.outcome,{roleIds:[],filtered:true});assert.ok(!JSON.stringify(status).includes('DO_NOT_EXPOSE'));assert.ok(!JSON.stringify(status).includes('SECRET_RESULT_TOKEN'));assert.ok(!Object.hasOwn(diagnosticAttempt,'result_json'));assert.ok(!JSON.stringify(status).includes('test-owner'));
 const probeRpc={jsonrpc:'2.0',id:'stream-probe',method:'tools/call',params:{name:'scheduler_probe',arguments:{},_meta:{progressToken:10}}};
 response=await req('/mcp','POST',probeRpc,null,{'oai-authenticated-user-id':'test-owner',accept:'application/json, text/event-stream'});assert.match(response.headers.get('content-type'),/text\/event-stream/);
 const frames=(await response.text()).split('\n').filter(l=>l.startsWith('data: ')).map(l=>JSON.parse(l.slice(6)));assert.equal(frames.at(-1).id,'stream-probe');assert.equal(JSON.parse(frames.at(-1).result.content[0].text).job.status,'completed');assert.equal(outbound,0);
 const modernProbe=await req('/mcp','POST',probeRpc,null,{...modernHeaders,'oai-authenticated-user-id':'test-owner',accept:'text/event-stream'});
 const modernFrames=(await modernProbe.text()).split('\n').filter(l=>l.startsWith('data: ')).map(l=>JSON.parse(l.slice(6)));assert.equal(modernFrames.at(-1).result.resultType,'complete');assert.equal(outbound,0);

 response=await req('/mcp','POST',probeRpc,null,{'oai-authenticated-user-id':'other-owner',accept:'text/event-stream'});assert.equal(response.status,403,'SSE does not bypass owner identity');
 const sources=await (await req('/api/scheduler/sources')).json(),source=sources.sources.find(s=>s.supported);assert.ok(source);assert.equal(source.autoEnabled,false);
 assert.deepEqual(view.settings.sourceIds,[]);assert.equal(view.settings.intervalDays,7);assert.ok(view.sourceOptions.some(s=>s.id==='companySite'&&s.supported));
 assert.equal((await req('/api/scheduler/sources/'+source.companyId+'/'+source.sourceId,'PUT',{autoEnabled:true,intervalDays:3,revision:source.revision})).status,410,'individual scheduling is retired');
 const body={enabled:false,dayBudgetUsd:0,monthBudgetUsd:0,revision:1,sourceIds:['companySite','habr'],intervalDays:3,favoritesOnly:false};
 view=await (await req('/api/scheduler')).json();body.revision=view.settings.revision;
 response=await req('/api/scheduler/settings','PUT',body);assert.equal(response.status,200,await response.clone().text());const saved=await response.json();assert.deepEqual(saved.sourceIds,['companySite','habr']);assert.equal(saved.favoritesOnly,false);assert.equal(saved.intervalDays,3);assert.equal(saved.cloudConnected,true,'binding preserved');
 assert.equal((await req('/api/scheduler/settings','PUT',body)).status,409,'stale global form rejected');
 assert.equal((await req('/api/scheduler/settings','PUT',{...body,revision:saved.revision,sourceIds:['telegram']})).status,400,'unsupported source rejected');
 // Exercise global auto-configuration against actual D1, with providers disabled.
 const catalog={companies:[{name:'Native company A',vacancySources:{companySite:'https://careers.fixture.org/jobs'}},{name:'Native company B',vacancySources:{companySite:'https://other.fixture.org/jobs'}}],agencies:[]};
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify(catalog)).run();
 response=await req('/api/scheduler/settings','PUT',{...body,enabled:true,sourceIds:['companySite'],revision:saved.revision});assert.equal(response.status,200,await response.clone().text());
 response=await req('/api/scheduler/run','POST',{requestId:crypto.randomUUID()});assert.equal(response.status,200,await response.clone().text());
 const materialized=(await db.prepare('SELECT * FROM vacancy_source_parsing WHERE auto_enabled=1').all()).results;assert.equal(materialized.length,2,'global choice materializes both companies in native D1');assert.ok(materialized.every(r=>r.interval_days===3&&r.enabled===1));assert.equal(outbound,0,'global selection does not enable Firecrawl or spend');
 assert.equal((await req('/api/scheduler/queue?page=1&type=diagnostic')).status,200);assert.equal((await req('/api/scheduler/queue?page=0')).status,400);assert.equal((await db.prepare('PRAGMA foreign_key_check').all()).results.length,0);assert.equal(outbound,0);
 for(const period of ['week','month','twoMonths','archive']){const dated=await req('/api/vacancies/list?period='+period);assert.equal(dated.status,200,await dated.clone().text());assert.ok(Array.isArray((await dated.json()).vacancies));}assert.equal(outbound,0,'period projection uses native Intl and local D1 only');
 response=await req('/api/scheduler/issues');assert.equal(response.status,200,await response.clone().text());const issues=await response.json();assert.ok(issues.issues.some(i=>i.priority===2&&i.status==='verification'));assert.ok(issues.issues.some(i=>i.priority===3));assert.equal(outbound,0,'issues read never calls providers');
 response=await req('/api/scheduler/queue?search=Native%20company&agencyId=77');assert.equal(response.status,200);assert.equal((await response.json()).total,0,'agency namespace cannot match company names');
 // Execute the one-time timeout repair against native D1, preserving audit history.
 const api=await import('../.tests/backend.mjs'),fs=await import('node:fs');
 const historical=await api.enqueueScheduledJob(db,{type:'extract_vacancy',key:'native-historical-timeout'});
 await db.prepare("UPDATE scheduler_jobs SET status='failed',reason_code='23.0',attempts=1,completed_at=1 WHERE id=?").bind(historical.id).run();
 const repair=fs.readFileSync('drizzle/0063_llm_timeout_retry.sql','utf8');await db.prepare(repair).run();await db.prepare(repair).run();
 const repaired=await db.prepare('SELECT * FROM scheduler_jobs WHERE id=?').bind(historical.id).first();assert.equal(repaired.status,'pending');assert.equal(repaired.attempts,0);assert.equal(repaired.completed_at,null);assert.equal(repaired.reason_code,'');
 // The exact-page predicate must work in native D1, under generation/lease guards.
 const hh=await api.enqueueScheduledJob(db,{type:'scan_source',key:'native-exact-page',sourceId:'hh',companyId:materialized[0].company_id});
 await db.prepare("INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,'Eugene',?,?,'hh','https://hh.ru/employer/42',1,1,?,1,9999999999999)").bind(hh.id,hh.generation,hh.company_id,JSON.stringify({paused:{apify:{page:1}}})).run();
 await db.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('native-page-ledger',?,?,'free','fixture','synthetic','start','end',.25,.01,'succeeded','fixtureRun','fixtureData',1,1)").bind(hh.generation,'listing:'+hh.id+':0').run();
 const owner=await api.beginSchedulerRun(db,{requestId:'native-page-owner'});
 assert.equal(await api.claimScheduledJob(owner.db,owner.run.id,{jobId:hh.id,allowApifyStarts:false}),null,'page0 ledger does not authorize page1');
 await db.prepare('UPDATE apify_runs SET operation_key=? WHERE id=?').bind('listing:'+hh.id+':1','native-page-ledger').run();
 const allowed=await api.claimScheduledJob(owner.db,owner.run.id,{jobId:hh.id,allowApifyStarts:false});assert.equal(allowed.job.id,hh.id);await api.finishScheduledJob(owner.db,allowed);
 await db.prepare("UPDATE scheduler_runs SET status='completed',lease_until=0,finished_at=? WHERE id=?").bind(Date.now(),owner.run.id).run();assert.equal(outbound,0);
 console.log('Native workerd/D1 scheduler: anonymous token tick/probe durable write without browser, concurrent identical signals, paused no outbound, admin+Origin ACL, global source/scope CAS, automatic materialization/3day schedule, pagination validation, MCP discovery vs identity-bound access passed');
 const historyResponse=await req('/api/scheduler/issues');assert.equal(historyResponse.status,200);const historyReport=await historyResponse.json();assert.equal(historyReport.history.points.length,30);assert.equal(historyReport.history.observedDays,1);assert.ok(historyReport.activeCounts.p2>=0);assert.ok(await db.prepare('SELECT * FROM parser_error_daily WHERE owner_name=?').bind('Eugene').first());
 const funnelResponse=await req('/api/scheduler/funnel?sourceId=all&parser=all');assert.equal(funnelResponse.status,200);const funnelReport=await funnelResponse.json();assert.equal(funnelReport.daily.length,30);assert.ok(funnelReport.cohort.received>=0);assert.equal((await req('/api/scheduler/funnel?parser=invalid')).status,400);assert.equal((await req('/api/scheduler/funnel','GET',undefined,user)).status,403);
}finally{await mf.dispose();}
