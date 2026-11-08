import assert from 'node:assert/strict';
import {composeAllDiagnostics,ALL_DIAGNOSTICS_SECTIONS} from '../shared/all-diagnostics-debug.mjs';
import {DEBUG_JSON_MAX_BYTES} from '../shared/debug-json.mjs';

const now='2026-11-08T10:00:00.000Z',secret='SECRET_TOKEN_PRIVATE';
const days=Array.from({length:30},(_,i)=>new Date(Date.UTC(2026,9,10+i)).toISOString().slice(0,10));
const daily=days.map(day=>({day,p1:null,p2:null,observedAt:null,message:secret}));
const funnelDaily=days.map(day=>({day,received:1,accepted:1,fulltext:1,structured:1,roles:1,published:1}));
const fields={received:30,accepted:30,fulltext:30,structured:30,roles:30,published:30};
const snapshots={
  schedulerOverview:{summary:{enabled:true,cloudStatus:'signal_received',counts:{pending:1},privateKey:secret}},
  queue:{filters:{status:'all',type:'all',searchApplied:false},
    summary:{totalMatched:150},dataAsOf:now,total:150,coverage:'server_latest',
    items:Array.from({length:150},(_,i)=>({jobId:'job_'+i,status:'pending',createdAt:Date.parse(now)-i,
      sourceId:'hh',message:secret})),
  },
  runs:{items:[],total:0,coverage:'server_latest',summary:{totalMatched:null}},
  attempts:{items:[],total:0,coverage:'server_latest',summary:{totalMatched:null}},
  aiSpending:{available:false,errorCode:'ai_spending_unavailable'},
  apify:{available:false,errorCode:'apify_unavailable'},
  firecrawl:{available:false,errorCode:'firecrawl_unavailable'},
  issues:{dataAsOf:now,summary:{
    metric:'daily_peak_active',activeCounts:{p1:0,p2:0},historicalFailed:0,
    observedDays:0,days:30,average:{p1:null,p2:null},todayPeak:{p1:null,p2:null},
    p2Breakdown:{available:false},
  },items:[],total:0,coverage:'complete',daily},
  funnel:{dataAsOf:now,filters:{sourceId:'all',parser:'all'},
    summary:{checkedAt:now,metric:'stage_event_daily_counts',
      from:days[0],to:days.at(-1),days:30,totals:fields,averages:fields,
      cohort:{...fields,created:0,linked:0,rejected:0,error:0,waiting:0,undatedPublished:0}},
    daily:funnelDaily},
};
const json=composeAllDiagnostics(snapshots,now),v=JSON.parse(json);
assert.equal(Object.keys(v.sections).length,Object.keys(ALL_DIAGNOSTICS_SECTIONS).length);
assert.equal(v.schemaVersion,1);
assert.equal(v.sections.queue.recentItems.total,150);
assert.equal(v.sections.queue.recentItems.returned,10);
assert.equal(v.sections.queue.recentItems.truncated,true);
assert.deepEqual(v.sections.queue.filters,{status:'all',type:'all',searchApplied:false});
assert.equal(v.sections.issues.daily.length,30);
assert.equal(v.sections.funnel.daily.length,30);
assert.equal(v.sections.aiSpending.available,false);
assert.equal(v.sections.firecrawl.errorCode,'firecrawl_unavailable');
assert.equal(v.sections.issues.stale,false);
assert.equal(v.sections.schedulerOverview.stale,null);
assert.ok(!json.includes(secret),'raw private diagnostics must not be exported');
assert.ok(new TextEncoder().encode(json).length<DEBUG_JSON_MAX_BYTES);
const missing=JSON.parse(composeAllDiagnostics({},now));
assert.ok(Object.values(missing.sections).every(x=>x.available===false&&x.errorCode==='section_unavailable'));
console.log('S11-DIAG-05 JSON: nine sections, 150 queue rows -> 10, complete 30-day charts, partial failure, no raw PII, bounded valid JSON');
