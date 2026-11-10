import assert from 'node:assert/strict';
import {serializeDebugJSON,DEBUG_JSON_MAX_BYTES} from '../shared/debug-json.mjs';
import {parserIssuesSnapshot,parserFunnelSnapshot,hhScanScheduleSnapshot} from '../shared/parser-statistics-debug.mjs';

const now=Date.parse('2026-11-07T15:00:00.000Z');
const secret=['sk','PRIVATE','token'].join('_');
const days=Array.from({length:30},(_,i)=>new Date(Date.UTC(2026,9,9+i)).toISOString().slice(0,10));
const points=days.map((day,i)=>({
  day,
  p1:i===5?0:i===12?2:null,
  p2:i===5?100:i===12?101:null,
  observedAt:i===5?now-2*86400000:i===12?now-86400000:null,
  companyName:secret,message:secret,
}));
const report={
  checkedAt:now,historicalFailed:670,sampleLimited:true,limit:120,
  activeCounts:{p1:2,p2:101},
  issues:Array.from({length:13},(_,i)=>({
    id:'P2_'+i,priority:2,status:i===12?'planned':'observed',target:'queue',
    stage:'extract_vacancy',sourceId:'hh',reasonCode:'retry_due',
    observedAt:now-i*3600000,affected:150-i,
    title:'PRIVATE_CUSTOMER_'+secret,
    description:'https://private.example/?token='+secret,
    jobs:[{id:'CI_PRIVATE_JOB',message:secret,companyName:secret}],
  })),
  history:{timezone:'Europe/Moscow',metric:'daily_peak_active',
    days:30,observedDays:2,average:{p1:1,p2:100.5},points},
};
const issues=JSON.parse(serializeDebugJSON({section:'parser_issues',capturedAt:now,
  ...parserIssuesSnapshot(report)}));
assert.equal(issues.section,'parser_issues');
assert.equal(issues.summary.metric,'daily_peak_active');
assert.equal(issues.summary.checkedAt,new Date(now).toISOString());
assert.equal(issues.dataAsOf,new Date(now).toISOString());
assert.equal(issues.summary.historicalFailed,670);
assert.equal(issues.summary.activeCounts.p2,101);
assert.equal(issues.summary.sampleLimited,true);
assert.equal(issues.summary.limit,120);
assert.equal(issues.summary.observedDays,2);
assert.equal(issues.summary.average.p2,100.5);
assert.equal(issues.summary.todayPeak.p2,null,'last day without sample is unknown');
assert.equal(issues.summary.p2Breakdown.available,false);
assert.equal(issues.summary.historicalP2BreakdownAvailable,false);

const p2Breakdown={
  available:true,totalActiveP2:125,sampleLimited:true,limit:10,
  truncated:true,groupTruncated:false,
  byStatus:[{value:'failed',count:100},{value:'pending',count:25}],
  byType:[{value:'extract_vacancy',count:125}],
  bySourceId:[{value:'hh',count:125}],
  byReasonCode:[{value:'extract_failed',count:100},{value:'retry_due',count:25}],
  recentJobs:Array.from({length:10},(_,i)=>({
    jobId:'job-'+String(i).padStart(16,'0'),status:'failed',
    type:'extract_vacancy',sourceId:'hh',reasonCode:'extract_failed',
    updatedAt:now-i*1000,message:secret,companyName:secret,
  })),
  rawError:secret,
};
const detailed=JSON.parse(serializeDebugJSON({
  section:'parser_issues',capturedAt:now,
  ...parserIssuesSnapshot({...report,p2Breakdown}),
}));
assert.equal(detailed.summary.p2Breakdown.available,true);
assert.equal(detailed.summary.p2Breakdown.totalActiveP2,125);
assert.equal(detailed.summary.p2Breakdown.truncated,true);
assert.equal(detailed.summary.p2Breakdown.sampleLimited,true);
assert.equal(detailed.summary.p2Breakdown.byStatus[1].count,25);
assert.equal(detailed.summary.p2Breakdown.recentJobs.length,10);
assert.ok(!JSON.stringify(detailed).includes(secret));
assert.throws(()=>serializeDebugJSON({
  section:'parser_issues',capturedAt:now,
  ...parserIssuesSnapshot({...report,p2Breakdown:{
    ...p2Breakdown,recentJobs:[...p2Breakdown.recentJobs,p2Breakdown.recentJobs[0]],
  }}),
}),/invalid_debug_array/,'nested jobs cannot exceed ten');

assert.equal(issues.daily.length,30);
assert.equal(issues.daily.filter(day=>day.p2===null).length,28);
assert.equal(issues.daily[5].p2,100);
assert.equal(issues.daily[12].p2,101);
assert.equal(issues.daily.at(-1).observedAt,null);
assert.equal(issues.recentItems.total,13);
assert.equal(issues.recentItems.returned,10);
assert.equal(issues.recentItems.truncated,true);
assert.ok(!JSON.stringify(issues).includes(secret));
assert.ok(!JSON.stringify(issues).includes('companyName'));
assert.ok(!JSON.stringify(issues).includes('message'));
assert.ok(!JSON.stringify(issues).includes('jobs'));
assert.ok(!JSON.stringify(issues).includes('description'));
assert.ok(JSON.stringify(issues).length<DEBUG_JSON_MAX_BYTES);
assert.throws(()=>parserIssuesSnapshot({...report,history:{...report.history,points:points.slice(0,29)}}),/issues_history_unavailable/);

const stageNames=['received','accepted','fulltext','structured','roles','published'];
const counts={received:4,accepted:3,fulltext:4,structured:2,roles:2,published:2};
const daily=days.map((day,i)=>({day,...Object.fromEntries(
  stageNames.map(stage=>[stage,i===29?counts[stage]:0])),newVacancies:i===29?1:0}));
const cohort={...counts,rejected:1,waiting:0,error:1,created:1,linked:1,undatedPublished:0};
const funnelReport={
  checkedAt:now,timezone:'Europe/Moscow',from:days[0],to:days.at(-1),days:30,
  filters:{sourceId:'hh',parser:'apify'},sourceOptions:[],parserOptions:[],
  daily,totals:{...counts,newVacancies:1},
  averages:{...Object.fromEntries(stageNames.map(stage=>[stage,counts[stage]/30])),newVacancies:1/30},
  undatedNewVacancies:0,cohort,privateDataset:secret,
};
const funnel=JSON.parse(serializeDebugJSON({
  section:'parser_funnel',capturedAt:now,...parserFunnelSnapshot(funnelReport),
}));
assert.equal(funnel.section,'parser_funnel');
assert.deepEqual(funnel.filters,{sourceId:'hh',parser:'apify'});
assert.equal(funnel.summary.checkedAt,new Date(now).toISOString());
assert.equal(funnel.summary.conversionBase,'cohort_received');
assert.equal(funnel.summary.stageConversionPercent.accepted,75);
assert.equal(funnel.summary.stageConversionPercent.fulltext,100);
assert.equal(funnel.summary.uniqueNewVacanciesInPeriod,1);
assert.equal(funnel.summary.undatedNewVacancies,0);
assert.equal(funnel.summary.cohortCandidatesWithCreate,1);
assert.equal(funnel.summary.cohortCandidatesLinkedToExisting,1);
assert.equal(funnel.summary.metric,'candidate_stage_events_and_unique_vacancy_creates');
assert.equal(funnel.summary.cohort.undatedPublished,0);
assert.equal(funnel.summary.cohort.created,1);
assert.equal(funnel.summary.cohort.linked,1);
assert.equal(funnel.daily.length,30);
assert.equal(funnel.daily.at(-1).published,2);
assert.equal(funnel.daily.at(-1).newVacancies,1);
assert.equal(funnel.summary.totals.newVacancies,1);
assert.equal(funnel.summary.averages.newVacancies,1/30);
assert.ok(!JSON.stringify(funnel).includes(secret));
const empty={...funnelReport,cohort:{...cohort,...Object.fromEntries(
  [...stageNames,'created','linked','error','waiting','rejected','undatedPublished'].map(k=>[k,0]))}};
const zero=JSON.parse(serializeDebugJSON({
  section:'parser_funnel',capturedAt:now,...parserFunnelSnapshot(empty),
}));
assert.equal(zero.summary.stageConversionPercent.accepted,null,'zero cohort denominator is not a rate');
assert.throws(()=>parserFunnelSnapshot({...funnelReport,daily:daily.slice(0,20)}),/funnel_history_unavailable/);
const noIssues=JSON.parse(serializeDebugJSON({
  section:'parser_issues',capturedAt:now,available:false,errorCode:'issues_unavailable',
}));
assert.equal(noIssues.available,false);
assert.equal(noIssues.errorCode,'issues_unavailable');
assert.deepEqual(noIssues.summary,{});
assert.equal(noIssues.daily,undefined);
console.log('S11-DIAG-04: 30 P1/P2 points, 28 unknown days, observed-only average, 10 sanitized issues, P2 breakdown available or unavailable with real full-scope counts, complete funnel events and cohort counts, 0 denominator, no PII passed');

// GO-439: HH-расписание содержит агрегаты, а не исходные job/source/actor записи.
const hhSchedule={
  checkedAt:now,sourceId:'hh',enabled:true,sourceSelected:true,
  favoritesOnly:true,intervalDays:1,vacancyAgeDays:3,finalEligibilityVerified:false,
  summary:{configured:30,disabled:0,outsideScope:0,activeScan:6,
    waiting:23,potentiallyDue:1,nextDueAt:now+3*3600000,
    recalculated:1,unknownLastSuccess:1,
    successfulLast24h:21,successfulLast48h:21,successDateUnknown:1,
    noSuccess48hWaiting:6,noSuccess48hActive:3,noSuccess48hDue:0,
    noSuccess48hWaitPartial:2,
    companyName:secret,providerToken:secret},
  pendingHHScans:6,runningHHScans:0,
  pendingBreakdown:{pending:6,notDue:0,unverifiedCursor:1,
    unverifiedLineage:1,missingOperation:0,orphanPaid:0,
    reconcileCandidate:1,launchCooldown:4,launchCandidate:0,other:0,
    unconfirmedRunId:0,oldestUnconfirmedAt:null,
    oldestPendingAt:now-6*86400000,oldestUnverifiedAt:now-6*86400000,
    oldestReconcileAt:now-3*86400000,oldestCooldownAt:now-86400000,
    jobId:secret,runId:secret},
  companyIds:[secret],sourceUrl:'https://private.example/?token='+secret,
};
const hhJSON=JSON.parse(serializeDebugJSON({
  section:'parser_hh_schedule',capturedAt:now,...hhScanScheduleSnapshot(hhSchedule),
}));
assert.equal(hhJSON.section,'parser_hh_schedule');
assert.equal(hhJSON.dataAsOf,new Date(now).toISOString());
assert.equal(hhJSON.filters.intervalDays,1);
assert.equal(hhJSON.filters.vacancyAgeDays,3);
assert.equal(hhJSON.summary.enabledInScope,30);
assert.equal(hhJSON.summary.successfulLast24h,21);
assert.equal(hhJSON.summary.successfulLast48h,21);
assert.equal(hhJSON.summary.noSuccess48hWaiting,6);
assert.equal(hhJSON.summary.noSuccess48hActive,3);
assert.equal(hhJSON.summary.noSuccess48hWaitPartial,2);
assert.equal(hhJSON.summary.pendingBreakdown.pending,6);
assert.equal(hhJSON.summary.pendingBreakdown.launchCooldown,4);
assert.equal(hhJSON.summary.pendingBreakdown.unverifiedLineage,1);
assert.equal(hhJSON.summary.pendingBreakdown.missingOperation,0);
assert.equal(hhJSON.summary.pendingBreakdown.unconfirmedRunId,0);
assert.equal(hhJSON.summary.pendingBreakdown.oldestUnconfirmedAt,null);
assert.equal(hhJSON.summary.pendingBreakdown.oldestReconcileAt,
  new Date(now-3*86400000).toISOString());
assert.equal(hhJSON.summary.pendingBreakdown.reconcileCandidate,1);
assert.equal(hhJSON.summary.pendingBreakdown.oldestPendingAt,
  new Date(now-6*86400000).toISOString());
assert.equal(hhJSON.summary.finalEligibilityVerified,false);
assert.ok(!JSON.stringify(hhJSON).includes(secret));
assert.ok(!JSON.stringify(hhJSON).includes('companyIds'));
assert.ok(!JSON.stringify(hhJSON).includes('jobId'));
assert.ok(!JSON.stringify(hhJSON).includes('runId'));
assert.ok(!JSON.stringify(hhJSON).includes('sourceUrl'));
const hhNoCoverage={...hhSchedule,summary:{...hhSchedule.summary},
  pendingBreakdown:undefined};
delete hhNoCoverage.summary.successfulLast24h;
delete hhNoCoverage.summary.successfulLast48h;
delete hhNoCoverage.summary.successDateUnknown;
const hhUnknown=JSON.parse(serializeDebugJSON({
  section:'parser_hh_schedule',capturedAt:now,...hhScanScheduleSnapshot(hhNoCoverage),
}));
assert.equal(hhUnknown.summary.coverageAvailable,false);
assert.equal(hhUnknown.summary.successfulLast24h,null,'нет данных — не ноль');
assert.equal(hhUnknown.summary.successfulLast48h,null);
assert.equal(hhUnknown.summary.successDateUnknown,null);
assert.equal(hhUnknown.summary.pendingBreakdownAvailable,false);
assert.equal(hhUnknown.summary.pendingBreakdown,null);
assert.equal(hhUnknown.summary.noSuccess48hWaiting,6);
assert.equal(hhUnknown.summary.noSuccess48hActive,3);
// Legacy data remains unknown (null), not a fabricated zero.
// The reason count is a subset of "other", not a new top-level bucket.
const uncertainReport={...hhSchedule,pendingBreakdown:{...hhSchedule.pendingBreakdown,
  other:1,launchCooldown:3,unconfirmedRunId:1,
  oldestUnconfirmedAt:now-5*86400000,
  providerRunId:secret,
}};
const uncertainJSON=JSON.parse(serializeDebugJSON({
  section:'parser_hh_schedule',capturedAt:now,...hhScanScheduleSnapshot(uncertainReport),
}));
assert.equal(uncertainJSON.summary.pendingBreakdown.unconfirmedRunId,1);
assert.equal(uncertainJSON.summary.pendingBreakdown.other,1);
assert.equal(uncertainJSON.summary.pendingBreakdown.oldestUnconfirmedAt,
  new Date(now-5*86400000).toISOString());
assert.equal(uncertainJSON.summary.pendingBreakdown.launchCooldown,3);
assert.ok(!JSON.stringify(uncertainJSON).includes(secret));
assert.ok(!JSON.stringify(uncertainJSON).includes('providerRunId'));
const oldFields={...hhSchedule,summary:{...hhSchedule.summary},
  pendingBreakdown:{...hhSchedule.pendingBreakdown}};
for(const field of ['noSuccess48hWaiting','noSuccess48hActive','noSuccess48hDue',
  'noSuccess48hWaitPartial'])delete oldFields.summary[field];
for(const field of ['unverifiedLineage','missingOperation','oldestUnverifiedAt',
  'oldestReconcileAt','oldestCooldownAt','unconfirmedRunId','oldestUnconfirmedAt'])delete oldFields.pendingBreakdown[field];
const legacy=JSON.parse(serializeDebugJSON({
  section:'parser_hh_schedule',capturedAt:now,...hhScanScheduleSnapshot(oldFields),
}));
assert.equal(legacy.summary.noSuccess48hWaiting,null);
assert.equal(legacy.summary.pendingBreakdown.unverifiedLineage,null);
assert.equal(legacy.summary.pendingBreakdown.oldestUnverifiedAt,null);
assert.equal(legacy.summary.pendingBreakdown.unconfirmedRunId,null);
assert.equal(legacy.summary.pendingBreakdown.oldestUnconfirmedAt,null);
console.log('GO-440: stale 48h by wait/active/due and proof variants, oldest category timestamps, unavailable fields remain null, no identifiers.');
console.log('GO-439: HH schedule JSON has timestamps, configured/due/coverage/pending groups; missing is not zero; PII rejected.');
