import assert from 'node:assert/strict';
import {buildDebugJSON,serializeDebugJSON,DEBUG_JSON_EVENT_LIMIT,DEBUG_JSON_MAX_BYTES} from '../shared/debug-json.mjs';
const at='2026-11-07T15:00:00.000Z';
const secret='sk-proj-DO_NOT_COPY';
const jobs=Array.from({length:15},(_,i)=>({
  jobId:'JOB_'+String(i).padStart(2,'0'),type:'scan_source',sourceId:'hh',
  status:'pending',priority:i,createdAt:Date.parse(at)+i*1000,
  updatedAt:Date.parse(at)+i*1000,reasonCode:'not_due',
  payload:{apiKey:secret},result_json:secret,message:secret,
  companyName:'Частная компания',candidateTitle:'Приватная вакансия',keyMask:secret,
}));
jobs[13].createdAt=jobs[14].createdAt;  // Повтор времени: сортировка по ID.
const request={section:'scheduler_queue',capturedAt:at,dataAsOf:at,
  filters:{status:'pending',search:'Приватная компания',searchApplied:true,secret},
  summary:{totalMatched:15,counts:{pending:15,failed:0,rawPrompt:secret},apiKey:secret},
  items:jobs,total:15,coverage:'complete',appVersion:'c'.repeat(40)};
const json=serializeDebugJSON(request),data=JSON.parse(json);
assert.equal(data.schemaVersion,1);
assert.equal(data.timezone,'Europe/Moscow');
assert.equal(data.appVersion,'c'.repeat(40));
assert.equal(data.recentItems.total,15);
assert.equal(data.recentItems.returned,DEBUG_JSON_EVENT_LIMIT);
assert.equal(data.recentItems.limit,10);
assert.equal(data.recentItems.truncated,true);
assert.equal(data.recentItems.items[0].jobId,'JOB_14');
assert.equal(data.recentItems.items[1].jobId,'JOB_13');
assert.equal(data.recentItems.items.at(-1).jobId,'JOB_05');
assert.equal(data.filters.search,undefined);
assert.equal(data.filters.searchApplied,true);
assert.equal(data.summary.counts.failed,0);
assert.equal(data.summary.apiKey,undefined);
for(const bad of [secret,'Частная компания','Приватная вакансия','payload','result_json','keyMask','rawPrompt','DO_NOT_COPY']) assert.ok(!json.includes(bad),bad);
assert.ok(new TextEncoder().encode(json).length < DEBUG_JSON_MAX_BYTES);
assert.equal(serializeDebugJSON(request),json,'same snapshot yields deterministic bytes');

assert.throws(()=>buildDebugJSON({...request,total:100}),/incomplete_debug_source/);
assert.throws(()=>buildDebugJSON({...request,coverage:'server_latest',items:jobs.slice(0,2)}),/incomplete_debug_latest/);
const paged=buildDebugJSON({...request,coverage:'server_latest',total:100,items:jobs.slice(-10)});
assert.equal(paged.recentItems.total,100);
assert.equal(paged.recentItems.truncated,true);
assert.equal(paged.recentItems.items[0].jobId,'JOB_14');
assert.equal(paged.recentItems.coverage,'server_latest');
assert.throws(()=>buildDebugJSON({...request,section:'not_registered'}),/invalid_debug_section/);
const withDirty={...request,items:[{...jobs[0],reasonCode:'Bearer sk-EXFIL',provider:'https://example/?key=secret'}],total:1};
const clean=serializeDebugJSON(withDirty);
assert.ok(!clean.includes('EXFIL'));
assert.ok(!clean.includes('https://'));
assert.ok(!clean.includes('reasonCode'),'suspicious code omitted');

const absent=buildDebugJSON({section:'parser_issues',capturedAt:at,available:false,errorCode:'backend_not_migrated',summary:{activeCounts:{p1:0,p2:0}},daily:Array(30).fill({day:'2026-11-07',p1:0,p2:0})});
assert.equal(absent.available,false);
assert.equal(absent.errorCode,'backend_not_migrated');
assert.equal(absent.daily,undefined);
assert.equal(absent.summary.activeCounts.p1,0,'unavailable summary is not silently rewritten to zero');
const unavailableCost=buildDebugJSON({section:'ai_spending',capturedAt:at,summary:{confirmedUsd:0,unknownReservedUsd:null,upperEstimateUsd:null,credits:17},items:[{jobId:'A',type:'score_relevance',startedAt:at,costUsd:null,reservedUsd:0}]});
assert.equal(unavailableCost.summary.confirmedUsd,0);
assert.equal(unavailableCost.summary.unknownReservedUsd,null);
assert.equal(unavailableCost.summary.upperEstimateUsd,null);
assert.equal(unavailableCost.recentItems.items[0].costUsd,null);

const daily=Array.from({length:30},(_,i)=>({day:new Date(Date.UTC(2026,9,9+i)).toISOString().slice(0,10),p1:i<28?null:0,p2:i<28?null:i===28?100:101,observedAt:i<28?null:Date.parse(at),message:secret}));
const history=buildDebugJSON({section:'parser_issues',capturedAt:at,summary:{observedDays:2,days:30,average:{p1:0,p2:100.5},todayPeak:{p1:null,p2:null}},daily});
assert.equal(history.daily.length,30);
assert.equal(history.summary.average.p2,100.5);
assert.equal(history.daily.filter(d=>d.p2===null).length,28);
assert.equal(history.summary.todayPeak.p2,null);
assert.equal(history.daily[29].p2,101);
assert.equal(history.daily[29].message,undefined);
assert.throws(()=>buildDebugJSON({section:'parser_issues',capturedAt:at,daily:daily.slice(0,10)}),/invalid_debug_daily/);
const missingSource=buildDebugJSON({section:'scheduler_queue',capturedAt:at,summary:{totalMatched:0}});
assert.equal(missingSource.recentItems,undefined,'absence of list never claims zero entries');

console.log('S11-DIAG-01 JSON serializer: allowlist, secrets, 10 newest, stable ties, coverage, null/zero, unavailable, 30 days passed.');
